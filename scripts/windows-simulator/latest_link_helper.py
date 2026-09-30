#!/usr/bin/env python3
"""Copy one latest WeChat article URL through a calibrated desktop UI flow.

The bridge sends one JSON account object to stdin and expects exactly one JSON
object on stdout. This helper intentionally uses ordinary UI actions only: it
does not parse private protocols, solve login challenges, or fetch article
content.

Configure the target window and click points with environment variables. See
README.md in this directory before enabling it in the bridge config.
"""

from __future__ import annotations

import json
import os
import platform
import re
import shlex
import subprocess
import sys
import time
import webbrowser
from dataclasses import dataclass
from types import SimpleNamespace
from typing import Any
from urllib.parse import urlparse


@dataclass
class AutomationError(Exception):
    code: str
    message: str
    manual: bool = False


def response(ok: bool, **kwargs: Any) -> None:
    print(json.dumps({"ok": ok, **kwargs}, ensure_ascii=False), flush=True)


def parse_point(name: str) -> tuple[int, int]:
    value = os.getenv(name, "").strip()
    if not value:
        raise AutomationError(
            "coordinates_not_configured",
            f"{name} is required; calibrate it relative to the target window",
            True,
        )
    try:
        x, y = value.split(",", 1)
        return int(x.strip()), int(y.strip())
    except ValueError as exc:
        raise AutomationError("invalid_coordinates", f"{name} must be x,y", True) from exc


def hotkey(value: str) -> list[str]:
    values = [piece.strip() for piece in value.split(",") if piece.strip()]
    if not values:
        raise AutomationError("invalid_hotkey", "empty keyboard shortcut", True)
    return values


def require_automation_modules() -> tuple[Any, Any]:
    try:
        import pyautogui  # type: ignore
        import pyperclip  # type: ignore
    except ImportError as exc:
        raise AutomationError(
            "automation_dependency_missing",
            "Install pyautogui and pyperclip from scripts/windows-simulator/requirements.txt",
            True,
        ) from exc
    if sys.platform == "win32":
        try:
            import pywinauto  # type: ignore # noqa: F401
        except ImportError as exc:
            raise AutomationError(
                "automation_dependency_missing",
                "Install the Windows dependencies from scripts/windows-simulator/requirements.txt",
                True,
            ) from exc
    elif sys.platform == "darwin":
        try:
            import Quartz  # type: ignore # noqa: F401
        except ImportError as exc:
            raise AutomationError(
                "automation_dependency_missing",
                "Install pyobjc-framework-Quartz on macOS",
                True,
            ) from exc
    else:
        raise AutomationError("unsupported_platform", f"unsupported platform: {platform.system()}", True)
    return pyautogui, pyperclip


def launch_initial_link(initial_link: str) -> None:
    command = os.getenv("WECHAT_SIM_LAUNCH_COMMAND", "").strip()
    if not command:
        webbrowser.open_new_tab(initial_link)
        return
    # The command is configured by the local operator. It is split without a
    # shell and only the literal {initial_link} placeholder is expanded.
    args = [part.replace("{initial_link}", initial_link) for part in shlex.split(command)]
    if not args:
        raise AutomationError("invalid_launch_command", "WECHAT_SIM_LAUNCH_COMMAND is empty", True)
    subprocess.Popen(args, shell=False)


_launch_baseline_window_ids: set[int] | None = None


class MacWindow:
    def __init__(self, owner: str, bounds: dict[str, Any], window_id: int = 0, title: str = ""):
        self.owner = owner
        self.bounds = bounds
        self.window_id = window_id
        self.title = title

    def rectangle(self) -> Any:
        return SimpleNamespace(
            left=int(self.bounds.get("X", 0)),
            top=int(self.bounds.get("Y", 0)),
            width=int(self.bounds.get("Width", 0)),
            height=int(self.bounds.get("Height", 0)),
        )

    def focus_click_point(self) -> tuple[int, int]:
        """A point inside the article/content area (avoid clicking the chat list)."""
        rect = self.rectangle()
        # Landscape article views: aim right-of-center (browser pane), not left sidebar.
        width, height = int(rect.width), int(rect.height)
        if width >= int(height * 0.85):
            x = rect.left + max(80, int(width * 0.55))
        else:
            x = rect.left + max(40, width // 2)
        y = rect.top + max(48, min(int(height * 0.12), 120))
        return x, y

    def set_focus(self, pyautogui: Any | None = None) -> None:
        # Raising this window matters more than clicking inside it. A content
        # click lands on whichever app is painted on top of those pixels.
        del pyautogui
        if sys.platform == "darwin":
            _raise_mac_window(self.rectangle())
        configured = os.getenv("WECHAT_SIM_MAC_APP", "").strip()
        candidates = [configured] if configured else []
        candidates.extend([self.owner, "WeChat"])
        seen: set[str] = set()
        for candidate in candidates:
            if not candidate or candidate in seen:
                continue
            seen.add(candidate)
            result = subprocess.run(
                ["open", "-a", candidate], check=False, capture_output=True
            )
            if result.returncode == 0:
                return


def _raise_mac_window(rect: Any) -> None:
    """Put this WeChat window above whatever is covering its screen pixels."""
    width, height = int(getattr(rect, "width", 0)), int(getattr(rect, "height", 0))
    if width < 200 or height < 100 or sys.platform != "darwin":
        return
    script = f'''
tell application "System Events"
  tell process "WeChat"
    set frontmost to true
    repeat with w in windows
      try
        set sz to size of w
        if (item 1 of sz as integer) is {width} and (item 2 of sz as integer) is {height} then
          perform action "AXRaise" of w
        end if
      end try
    end repeat
  end tell
end tell
'''
    try:
        subprocess.run(["osascript", "-e", script], check=False, capture_output=True, timeout=3)
    except (OSError, subprocess.TimeoutExpired):
        return


def snapshot_mac_wechat_window_ids(title_regex: str, target_app: str | None = None) -> set[int]:
    ids: set[int] = set()
    for window in list_mac_windows(title_regex, target_app):
        window_id = int(getattr(window, "window_id", 0) or 0)
        if window_id:
            ids.add(window_id)
    return ids


def remember_launch_baseline(title_regex: str, target_app: str | None = None) -> None:
    global _launch_baseline_window_ids
    _launch_baseline_window_ids = snapshot_mac_wechat_window_ids(title_regex, target_app)


def _window_member(rect: Any, *names: str) -> int | None:
    """Read a rectangle member exposed as a property or zero-arg method."""
    for name in names:
        try:
            value = rect.get(name) if isinstance(rect, dict) else getattr(rect, name)
        except (AttributeError, KeyError, TypeError):
            continue
        if callable(value):
            value = value()
        try:
            return int(value)
        except (TypeError, ValueError):
            continue
    return None


def _normalize_windows_rectangle(rect: Any) -> Any:
    """Normalize pywinauto RECT variants to the fields used by the helper."""
    left = _window_member(rect, "left", "x", "X")
    top = _window_member(rect, "top", "y", "Y")
    right = _window_member(rect, "right", "Right", "MaxX")
    bottom = _window_member(rect, "bottom", "Bottom", "MaxY")
    width = _window_member(rect, "width", "Width")
    height = _window_member(rect, "height", "Height")
    if left is None or top is None:
        raise AutomationError("invalid_window_bounds", "target window has no usable origin", True)
    if (width is None or width <= 0) and right is not None:
        width = right - left
    if (height is None or height <= 0) and bottom is not None:
        height = bottom - top
    if width is None or height is None or width <= 0 or height <= 0:
        raise AutomationError("invalid_window_bounds", "target window has invalid bounds", True)
    return SimpleNamespace(left=left, top=top, width=width, height=height)


class WindowsWindow:
    """Small adapter for pywinauto's platform-specific window wrapper."""

    def __init__(self, raw: Any):
        self.raw = raw
        self.title = self._title()

    def _title(self) -> str:
        try:
            value = getattr(self.raw, "window_text")
            return str(value() if callable(value) else value)
        except Exception:
            return ""

    def rectangle(self) -> Any:
        return _normalize_windows_rectangle(self.raw.rectangle())

    def set_focus(self, _pyautogui: Any | None = None) -> None:
        focus = getattr(self.raw, "set_focus", None)
        if not callable(focus):
            raise AutomationError("window_focus_failed", "target window does not support focus", True)
        # pywinauto's set_focus() accepts no arguments.  The optional argument
        # keeps this adapter source-compatible with the macOS window wrapper.
        focus()


def find_windows_window(title_regex: str, target_app: str | None = None) -> Any:
    from pywinauto import Desktop  # type: ignore

    raw_windows = Desktop(backend="uia").windows(title_re=title_regex, visible_only=True)
    windows: list[WindowsWindow] = []
    for raw in raw_windows:
        try:
            candidate = WindowsWindow(raw)
            candidate.rectangle()
        except AutomationError:
            continue
        windows.append(candidate)
    if not windows:
        return None
    if len(windows) > 1:
        labels = [window.title or "<untitled>" for window in windows]
        app_hint = f" for {target_app!r}" if target_app else ""
        raise AutomationError(
            "ambiguous_window",
            f"{len(windows)} visible windows match {title_regex!r}{app_hint}: "
            f"{', '.join(labels)}; narrow WECHAT_SIM_WINDOW_TITLE_REGEX",
            True,
        )
    return windows[0]


def _owner_allowed(owner: str, target_app: str) -> bool:
    """Keep automation on the WeChat process unless a different app was named."""
    owner_key = owner.strip().lower()
    wechat = owner_key in {"微信", "wechat"}
    if not target_app or target_app in {"微信", "wechat"}:
        return wechat
    return owner_key == target_app


def _mac_window_area(window: MacWindow) -> int:
    rect = window.rectangle()
    return int(rect.width) * int(rect.height)


def list_mac_windows(
    title_regex: str,
    target_app: str | None = None,
    attach_titles: bool = True,
) -> list[MacWindow]:
    """On-screen WeChat windows in front-to-back order (Quartz list order)."""
    from Quartz import (  # type: ignore
        CGWindowListCopyWindowInfo,
        kCGNullWindowID,
        kCGWindowListOptionOnScreenOnly,
    )

    target_app = (target_app if target_app is not None else os.getenv("WECHAT_SIM_TARGET_APP", "")).strip().lower()
    pattern = re.compile(title_regex, re.IGNORECASE)
    windows = CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly, kCGNullWindowID) or []
    matched: list[MacWindow] = []
    for info in windows:
        owner = str(info.get("kCGWindowOwnerName", ""))
        title = str(info.get("kCGWindowName", ""))
        bounds = info.get("kCGWindowBounds", {}) or {}
        # A loose title regex such as "微信.*|WeChat.*" also matches Cursor
        # files named wechat-*.png and Chrome tabs that mention 微信. Only the
        # WeChat process may be clicked unless the operator named another app.
        if not _owner_allowed(owner, target_app):
            continue
        if int(info.get("kCGWindowLayer", 0)) != 0:
            continue
        width = int(bounds.get("Width", 0))
        height = int(bounds.get("Height", 0))
        if width < 200 or height < 100:
            continue
        if pattern.search(f"{owner} {title}"):
            matched.append(
                MacWindow(
                    owner,
                    bounds,
                    int(info.get("kCGWindowNumber", 0) or 0),
                    title,
                )
            )
    if attach_titles:
        attach_mac_accessibility_titles(matched)
    return matched


_GENERIC_WECHAT_TITLES = {"微信", "微信 (窗口)", "wechat", "wechat (窗口)"}


def is_generic_wechat_title(title: str) -> bool:
    text = title.strip()
    if not text:
        return True
    return text.lower() in _GENERIC_WECHAT_TITLES


def attach_mac_accessibility_titles(windows: list[MacWindow]) -> None:
    """Quartz leaves WeChat window names empty; Accessibility has the real title.

    The article reader is often a narrow window whose title is the account or
    article name (for example \"滴滴出行…\"), while the chat shell is just \"微信\".
    """
    if sys.platform != "darwin" or not windows:
        return
    script = r'''
tell application "System Events"
  if not (exists process "WeChat") then return ""
  tell process "WeChat"
    set out to ""
    repeat with w in windows
      try
        set n to name of w
        set p to position of w
        set s to size of w
        set out to out & n & tab & (item 1 of p as text) & tab & (item 2 of p as text) & tab & (item 1 of s as text) & tab & (item 2 of s as text) & linefeed
      end try
    end repeat
    return out
  end tell
end tell
'''
    try:
        result = subprocess.run(
            ["osascript", "-e", script],
            capture_output=True,
            text=True,
            check=False,
            timeout=8,
        )
    except (OSError, subprocess.TimeoutExpired):
        return
    if result.returncode != 0 or not (result.stdout or "").strip():
        return
    ax_rows: list[tuple[str, int, int, int, int]] = []
    for line in result.stdout.splitlines():
        parts = line.split("\t")
        if len(parts) != 5:
            continue
        try:
            ax_rows.append((parts[0], int(parts[1]), int(parts[2]), int(parts[3]), int(parts[4])))
        except ValueError:
            continue
    for window in windows:
        rect = window.rectangle()
        for name, x, y, width, height in ax_rows:
            if abs(rect.left - x) <= 8 and abs(rect.top - y) <= 8 and abs(rect.width - width) <= 12 and abs(rect.height - height) <= 12:
                window.title = name
                break


def titled_article_windows(windows: list[MacWindow]) -> list[MacWindow]:
    return [window for window in windows if not is_generic_wechat_title(window.title)]


def _mac_window_rect(window: MacWindow) -> tuple[int, int, int, int]:
    rect = window.rectangle()
    return int(rect.width), int(rect.height), int(rect.left), int(rect.top)


def pick_article_like_windows(windows: list[MacWindow]) -> list[MacWindow]:
    """Prefer the Accessibility-titled article reader, including the narrow Mac pane."""
    # Native Mac WeChat commonly labels the article reader "微信 (窗口)"
    # while the logged-in chat shell is simply "微信".  Prefer that explicit
    # reader title before applying geometry heuristics.
    session_windows = [
        window
        for window in windows
        if window.title.strip() in {"微信 (窗口)", "WeChat (窗口)"}
    ]
    if session_windows:
        return session_windows
    titled = titled_article_windows(windows)
    if titled:
        return titled
    candidates: list[MacWindow] = []
    for window in windows:
        width, height, _, _ = _mac_window_rect(window)
        if width < 600 or height < 400:
            continue
        if width < int(height * 0.85):
            continue
        candidates.append(window)
    if not candidates:
        return pick_mac_windows(windows, force_mode="frontmost")
    if len(candidates) == 1:
        return candidates
    # The main chat shell is usually the smaller generic window (around
    # 880x640), while the native article reader is the larger window (around
    # 920x789).  Choosing the smallest candidate sends the menu click to the
    # chat shell and leaves the clipboard unchanged.  Prefer the largest
    # article-like window when Accessibility did not provide a title.
    return [max(candidates, key=_mac_window_area)]


def find_current_article_window(
    title_regex: str,
    target_app: str | None,
    pyautogui: Any,
) -> Any:
    """Select an article already open in the native WeChat client.

    Windows WeChat keeps the chat shell and the article reader as separate
    top-level windows.  A broad ``微信.*`` match therefore must not be passed
    straight to ``wait_for_window``: that can focus 文件传输助手 (or another
    chat) and send the publisher click to the wrong surface.  Prefer a
    non-generic article window and fail closed when more than one candidate is
    visible.
    """
    if sys.platform == "win32":
        from pywinauto import Desktop  # type: ignore

        raw_windows = Desktop(backend="uia").windows(title_re=title_regex, visible_only=True)
        windows: list[WindowsWindow] = []
        for raw in raw_windows:
            try:
                candidate = WindowsWindow(raw)
                candidate.rectangle()
            except AutomationError:
                continue
            windows.append(candidate)
        article_candidates = [
            window
            for window in windows
            if (
                window.title.strip() in _SESSION_WINDOW_TITLES
                or not is_generic_wechat_title(window.title)
            )
            and window.title.strip().lower() not in {"文件传输助手", "file transfer assistant"}
            and not _has_login_marker(window.title)
        ]
        if len(article_candidates) > 1:
            labels = [window.title or "<untitled>" for window in article_candidates]
            raise AutomationError(
                "ambiguous_article_window",
                "multiple non-chat WeChat windows are visible: "
                + ", ".join(labels)
                + "; close the extra article or narrow WECHAT_SIM_WINDOW_TITLE_REGEX",
                True,
            )
        if article_candidates:
            selected = article_candidates[0]
            selected.set_focus(pyautogui)
            return selected
        if os.getenv("WECHAT_SIM_ALLOW_GENERIC_SHELL_ARTICLE", "").strip().lower() in {"1", "true", "yes"}:
            if not windows:
                raise AutomationError(
                    "current_article_not_opened",
                    "no matching WeChat window is open; open the initial article in WeChat first",
                    True,
                )
            selected = max(windows, key=lambda item: item.rectangle().width * item.rectangle().height)
            selected.set_focus(pyautogui)
            return selected
        raise AutomationError(
            "current_article_not_opened",
            "no non-chat WeChat article window is open; open the initial article in WeChat first",
            True,
        )

    # macOS article windows need the Accessibility title and Quartz geometry.
    windows = list_mac_windows(title_regex, target_app)
    candidates = [
        window
        for window in windows
        if window.title.strip() in {"微信 (窗口)", "WeChat (窗口)"}
        or not is_generic_wechat_title(window.title)
    ]
    if not candidates and os.getenv("WECHAT_SIM_ALLOW_GENERIC_SHELL_ARTICLE", "").strip().lower() in {"1", "true", "yes"}:
        # Some Mac builds render an article inside the generic shell. This is
        # intentionally an expert-only escape hatch: the normal same-window
        # mode must not click a chat shell because it can send navigation to a
        # different conversation or to the shell's refresh control.
        candidates = windows
    if not candidates:
        raise AutomationError(
            "current_article_not_opened",
            "no native WeChat article window is open; open the initial article in WeChat first",
            True,
        )
    selected = pick_article_like_windows(candidates)[0]
    selected.set_focus(pyautogui)
    return selected


def windows_opened_since_launch(windows: list[MacWindow]) -> list[MacWindow]:
    baseline = _launch_baseline_window_ids or set()
    if not baseline:
        return []
    return [
        window
        for window in windows
        if int(getattr(window, "window_id", 0) or 0)
        and int(getattr(window, "window_id", 0) or 0) not in baseline
    ]


def pick_mac_windows(windows: list[MacWindow], force_mode: str | None = None) -> list[MacWindow]:
    if not windows:
        return []
    default_mode = "launched" if sys.platform == "darwin" else "frontmost"
    mode = (force_mode or os.getenv("WECHAT_SIM_MAC_WINDOW_PICK", default_mode)).strip().lower()
    if mode in {"all", "try_all"}:
        return windows
    if mode == "launched":
        launched = windows_opened_since_launch(windows)
        if launched:
            return pick_article_like_windows(launched)
        debug_log("launched: no new WeChat window after open; falling back to article-like pick")
        return pick_article_like_windows(windows)
    if mode == "article":
        return pick_article_like_windows(windows)
    if mode == "largest":
        return [max(windows, key=_mac_window_area)]
    if mode == "smallest":
        return [min(windows, key=_mac_window_area)]
    # frontmost: first entry from Quartz (front-to-back).
    return [windows[0]]


def menu_target_windows(primary: MacWindow, title_regex: str, target_app: str | None) -> list[MacWindow]:
    """Windows that may host the article chrome (⋯ menu), never the main chat shell first."""
    try_all = os.getenv("WECHAT_SIM_TRY_ALL_MAC_WINDOWS", "").strip().lower() in {"1", "true", "yes"}
    if sys.platform != "darwin":
        return [primary]
    all_windows = list_mac_windows(title_regex, target_app)
    launched = windows_opened_since_launch(all_windows)
    article_like = pick_article_like_windows(launched or all_windows)
    ordered: list[MacWindow] = []
    seen_ids: set[int] = set()

    def add(window: MacWindow) -> None:
        if window.window_id and window.window_id in seen_ids:
            return
        if window.window_id:
            seen_ids.add(window.window_id)
        ordered.append(window)

    # Menu mode must try the article reader before the generic chat shell.
    # ``primary`` is still retained as a fallback for older WeChat builds
    # where the article and shell share one window.
    for window in article_like:
        add(window)
    add(primary)
    if try_all:
        for window in launched:
            add(window)
    return ordered or [primary]


def find_mac_window(title_regex: str, target_app: str | None = None) -> Any:
    windows = list_mac_windows(title_regex, target_app)
    picked = pick_mac_windows(windows)
    return picked[0] if picked else None


def debug_log(message: str) -> None:
    if os.getenv("WECHAT_SIM_DEBUG", "").strip().lower() in {"1", "true", "yes"}:
        print(message, file=sys.stderr, flush=True)


def wait_for_window(
    title_regex: str,
    timeout_sec: float,
    target_app: str | None = None,
    pyautogui: Any | None = None,
) -> Any:
    deadline = time.monotonic() + timeout_sec
    last_error = ""
    while time.monotonic() < deadline:
        try:
            if sys.platform == "win32":
                window = find_windows_window(title_regex, target_app)
            else:
                window = find_mac_window(title_regex, target_app)
            if window is not None:
                window.set_focus(pyautogui)
                debug_log(
                    f"target window id={getattr(window, 'window_id', 0)} "
                    f"title={getattr(window, 'title', '')!r} "
                    f"bounds={window.bounds if hasattr(window, 'bounds') else '?'}"
                )
                return window
        except AutomationError:
            raise
        except Exception as exc:  # UI tree can change while a page is loading.
            last_error = str(exc)
        time.sleep(0.25)
    detail = f" ({last_error})" if last_error else ""
    raise AutomationError("window_not_found", f"target window not found: {title_regex}{detail}", True)


def article_pane_origin(window: Any) -> tuple[int, int]:
    """Inner pane offset, only when the operator recorded one.

    The window handle is an id for two positions: where the window sits on
    the desktop, and an optional article-pane origin inside it. It does not
    identify the publisher or the first article. Mac WeChat has no separate
    webview handle, so this stays 0,0 unless WECHAT_SIM_ARTICLE_PANE_ORIGIN
    was measured. Guessing an inset here would shift hand-recorded points.
    """
    raw = os.getenv("WECHAT_SIM_ARTICLE_PANE_ORIGIN", "").strip()
    if raw:
        try:
            ox, oy = raw.split(",", 1)
            return int(ox.strip()), int(oy.strip())
        except ValueError as exc:
            raise AutomationError(
                "invalid_article_pane_origin",
                "WECHAT_SIM_ARTICLE_PANE_ORIGIN must be x,y",
                True,
            ) from exc
    return 0, 0


def absolute_point(window: Any, point: tuple[int, int]) -> tuple[int, int]:
    rect = window.rectangle()
    pane_x, pane_y = article_pane_origin(window)
    # Coordinates are relative to the *outer* window rectangle. This avoids
    # mixing application client coordinates and screen coordinates. Calibrate
    # again if Windows display scaling or window chrome changes.
    return rect.left + pane_x + point[0], rect.top + pane_y + point[1]


def maybe_scale_point(window: Any | None, point: tuple[int, int]) -> tuple[int, int]:
    """Scale calibrated x,y when the live window size differs from the reference."""
    raw = os.getenv("WECHAT_SIM_COORD_REF_SIZE", "").strip()
    if not raw or window is None:
        return point
    try:
        ref_w_s, ref_h_s = raw.split(",", 1)
        ref_w, ref_h = int(ref_w_s.strip()), int(ref_h_s.strip())
    except ValueError:
        return point
    if ref_w <= 0 or ref_h <= 0:
        return point
    rect = window.rectangle()
    x, y = point
    return (
        max(0, int(round(x * rect.width / ref_w))),
        max(0, int(round(y * rect.height / ref_h))),
    )


def vision_enabled() -> bool:
    """Enable visual matching only when explicitly configured.

    Calibrated points are the safe default. The bridge's ``vision`` setting or
    ``WECHAT_SIM_VISION=1`` opts into the screenshot/color fallback.
    """
    raw = os.getenv("WECHAT_SIM_VISION", "").strip().lower()
    if raw in {"0", "false", "no"}:
        return False
    if raw in {"1", "true", "yes"}:
        return True
    return False


def _box_blur_plane(values: list[int], width: int, height: int, radius: int) -> list[int]:
    """Separable box blur. Two passes approximate a Gaussian and steady color hits."""
    if radius <= 0 or width <= 0 or height <= 0:
        return values
    span = radius * 2 + 1
    horizontal = [0] * (width * height)
    for y in range(height):
        row = y * width
        for x in range(width):
            total = 0
            for offset in range(-radius, radius + 1):
                xx = min(width - 1, max(0, x + offset))
                total += values[row + xx]
            horizontal[row + x] = total // span
    blurred = [0] * (width * height)
    for x in range(width):
        for y in range(height):
            total = 0
            for offset in range(-radius, radius + 1):
                yy = min(height - 1, max(0, y + offset))
                total += horizontal[yy * width + x]
            blurred[y * width + x] = total // span
    return blurred


def blur_rgb(
    rgb: list[tuple[int, int, int]],
    width: int,
    height: int,
    radius: int = 1,
    passes: int = 2,
) -> list[tuple[int, int, int]]:
    planes = [
        _box_blur_plane([pixel[channel] for pixel in rgb], width, height, radius)
        for channel in range(3)
    ]
    # A second pass approximates a Gaussian for solid color blobs such as an avatar.
    for _ in range(max(0, passes - 1)):
        planes = [_box_blur_plane(plane, width, height, radius) for plane in planes]
    return [
        (planes[0][index], planes[1][index], planes[2][index])
        for index in range(width * height)
    ]


def _saturated_pixel(red: int, green: int, blue: int) -> bool:
    """Avatar-like color: clearly tinted, not white page background or black text."""
    return max(red, green, blue) - min(red, green, blue) >= 45 and (red + green + blue) // 3 < 230 and max(red, green, blue) > 80


def _pixel_kind(red: int, green: int, blue: int) -> str | None:
    """Keep a colored logo separate from the dark text beside it."""
    if _saturated_pixel(red, green, blue):
        return "color"
    luminance = (red + green + blue) // 3
    chroma = max(red, green, blue) - min(red, green, blue)
    if luminance <= 100 and chroma < 55:
        return "dark"
    return None


def _white_ring_ratio(
    rgb: list[tuple[int, int, int]],
    width: int,
    height: int,
    min_x: int,
    min_y: int,
    max_x: int,
    max_y: int,
) -> float:
    ring = 0
    white = 0
    for yy in range(min_y - 5, max_y + 6):
        for xx in range(min_x - 5, max_x + 6):
            if xx < 0 or yy < 0 or xx >= width or yy >= height:
                continue
            if min_x - 2 <= xx <= max_x + 2 and min_y - 2 <= yy <= max_y + 2:
                continue
            ring += 1
            red, green, blue = rgb[yy * width + xx]
            luminance = (red + green + blue) // 3
            if luminance >= 225 and max(red, green, blue) - min(red, green, blue) < 30:
                white += 1
    if ring == 0:
        return 0.0
    return white / ring


def find_account_avatar(
    rgb: list[tuple[int, int, int]],
    width: int,
    height: int,
) -> dict[str, Any] | None:
    """Lowest small avatar on the article page, including a dark logo.

    The click point is the name to the right of that avatar. ``mean`` is the
    unblurred color of the avatar itself and is used to check that a later
    article still belongs to this account. Two box-blur passes stand in for a
    Gaussian so a small logo stays stable.
    """
    blurred = blur_rgb(rgb, width, height, passes=2)
    x0, x1 = int(width * 0.04), int(width * 0.55)
    y0, y1 = int(height * 0.08), int(height * 0.98)
    if x1 <= x0 or y1 <= y0:
        return None
    seen = bytearray(width * height)
    best: dict[str, Any] | None = None
    for y in range(y0, y1):
        for x in range(x0, x1):
            index = y * width + x
            if seen[index]:
                continue
            red, green, blue = blurred[index]
            kind = _pixel_kind(red, green, blue)
            if kind is None:
                seen[index] = 1
                continue
            stack = [(x, y)]
            seen[index] = 1
            xs: list[int] = []
            ys: list[int] = []
            while stack:
                cx, cy = stack.pop()
                xs.append(cx)
                ys.append(cy)
                for dx, dy in ((1, 0), (-1, 0), (0, 1), (0, -1)):
                    nx, ny = cx + dx, cy + dy
                    if nx < x0 or nx >= x1 or ny < y0 or ny >= y1:
                        continue
                    neighbor = ny * width + nx
                    if seen[neighbor]:
                        continue
                    seen[neighbor] = 1
                    nred, ngreen, nblue = blurred[neighbor]
                    if _pixel_kind(nred, ngreen, nblue) == kind:
                        stack.append((nx, ny))
            if len(xs) < 8:
                continue
            min_x, max_x = min(xs), max(xs)
            min_y, max_y = min(ys), max(ys)
            side_w = max_x - min_x + 1
            side_h = max_y - min_y + 1
            side_max = max(side_w, side_h)
            side_min = min(side_w, side_h)
            if side_min < 5 or side_max > int(width * 0.12) or side_min / side_max < 0.55:
                continue
            if _white_ring_ratio(blurred, width, height, min_x, min_y, max_x, max_y) < 0.55:
                continue
            center_y = sum(ys) // len(ys)
            if best is not None and center_y <= int(best["center_y"]):
                continue
            total = [0, 0, 0]
            count = 0
            for yy in range(min_y, max_y + 1):
                for xx in range(min_x, max_x + 1):
                    pixel = rgb[yy * width + xx]
                    if _pixel_kind(*pixel) != kind:
                        continue
                    total[0] += pixel[0]
                    total[1] += pixel[1]
                    total[2] += pixel[2]
                    count += 1
            if count < 4:
                continue
            best = {
                "click": (min(width - 2, max_x + max(4, int(width * 0.03))), center_y),
                "mean": (total[0] // count, total[1] // count, total[2] // count),
                "center_y": center_y,
            }
    return best


def _byline_link_pixel(red: int, green: int, blue: int) -> bool:
    """The account name under the title is a blue-gray link, not black body text."""
    if blue < 90 or blue > 210 or blue < red + 18 or blue + 10 < green:
        return False
    luminance = (red + green + blue) // 3
    return 70 <= luminance <= 185


def find_byline_account_click(
    rgb: list[tuple[int, int, int]],
    width: int,
    height: int,
) -> tuple[int, int] | None:
    """Click the official-account name directly under the article title.

    Downsampling breaks the glyphs apart, so the name is the topmost line of
    blue-gray link pixels, gaps included. The avatar at the bottom of the
    article is a different control.
    """
    x0, x1 = int(width * 0.06), int(width * 0.62)
    y0, y1 = int(height * 0.06), int(height * 0.42)
    if x1 <= x0 or y1 <= y0:
        return None
    rows: list[tuple[int, int, int]] = []
    for y in range(y0, y1):
        xs = [x for x in range(x0, x1) if _byline_link_pixel(*rgb[y * width + x])]
        if len(xs) >= 4:
            rows.append((y, xs[0], xs[-1]))
    if not rows:
        return None
    groups: list[list[tuple[int, int, int]]] = []
    current: list[tuple[int, int, int]] = []
    for row in rows:
        if current and row[0] - current[-1][0] > 2:
            groups.append(current)
            current = []
        current.append(row)
    if current:
        groups.append(current)
    best: tuple[int, int] | None = None
    gap_x = max(6, int(width * 0.02))
    for group in groups:
        min_y = group[0][0]
        max_y = group[-1][0]
        if max_y - min_y > max(6, int(height * 0.06)):
            continue
        xs: list[int] = []
        for y, _, _ in group:
            xs.extend(x for x in range(x0, x1) if _byline_link_pixel(*rgb[y * width + x]))
        if not xs:
            continue
        ordered = sorted(set(xs))
        start = previous = ordered[0]
        segments: list[tuple[int, int]] = []
        for x in ordered[1:]:
            if x - previous > gap_x:
                segments.append((start, previous))
                start = x
            previous = x
        segments.append((start, previous))
        # The date sits to the right of the name. Click the name, the left segment.
        for left, right in segments:
            if right - left + 1 < max(8, int(width * 0.03)):
                continue
            if best is not None and min_y >= best[1]:
                break
            best = ((left + right) // 2, (min_y + max_y) // 2)
            break
    return best


def find_account_click(
    rgb: list[tuple[int, int, int]],
    width: int,
    height: int,
) -> tuple[int, int] | None:
    """Click the account name under the title, or the name beside the bottom avatar."""
    byline = find_byline_account_click(rgb, width, height)
    if byline is not None:
        return byline
    avatar = find_account_avatar(rgb, width, height)
    if avatar is None:
        return None
    click = avatar["click"]
    return int(click[0]), int(click[1])


def avatars_match(left: tuple[int, int, int], right: tuple[int, int, int]) -> bool:
    """Same publisher logo. A blue avatar and a black logo must not match."""
    distance = sum((int(a) - int(b)) ** 2 for a, b in zip(left, right)) ** 0.5
    return distance <= 80


def looks_like_account_profile(
    rgb: list[tuple[int, int, int]],
    width: int,
    height: int,
) -> bool:
    """Official-account home: a large avatar in the header, not an article cover."""
    blurred = blur_rgb(rgb, width, height, passes=1)
    x0, x1 = int(width * 0.04), int(width * 0.55)
    y0, y1 = int(height * 0.06), int(height * 0.42)
    if x1 <= x0 or y1 <= y0:
        return False
    seen = bytearray(width * height)
    # A centered column leaves the header avatar under 7% of the window width.
    min_side = max(8, int(width * 0.055))
    max_side = int(width * 0.25)
    for y in range(y0, y1):
        for x in range(x0, x1):
            index = y * width + x
            if seen[index]:
                continue
            kind = _pixel_kind(*blurred[index])
            if kind is None:
                seen[index] = 1
                continue
            stack = [(x, y)]
            seen[index] = 1
            xs: list[int] = []
            ys: list[int] = []
            while stack:
                cx, cy = stack.pop()
                xs.append(cx)
                ys.append(cy)
                for dx, dy in ((1, 0), (-1, 0), (0, 1), (0, -1)):
                    nx, ny = cx + dx, cy + dy
                    if nx < x0 or nx >= x1 or ny < y0 or ny >= y1:
                        continue
                    neighbor = ny * width + nx
                    if seen[neighbor]:
                        continue
                    seen[neighbor] = 1
                    if _pixel_kind(*blurred[neighbor]) == kind:
                        stack.append((nx, ny))
            if len(xs) < min_side * min_side // 2:
                continue
            side_w = max(xs) - min(xs) + 1
            side_h = max(ys) - min(ys) + 1
            side_max = max(side_w, side_h)
            side_min = min(side_w, side_h)
            if side_min < min_side or side_max > max_side or side_min / side_max < 0.65:
                continue
            if _white_ring_ratio(blurred, width, height, min(xs), min(ys), max(xs), max(ys)) < 0.4:
                continue
            return True
    return False


def _card_is_deleted_placeholder(
    rgb: list[tuple[int, int, int]],
    width: int,
    x0: int,
    x1: int,
    y_start: int,
    y_end: int,
) -> bool:
    """A removed post is a flat gray veil. A cover or a black title is not."""
    gray = 0
    ink = 0
    vivid = 0
    total = 0
    for y in range(y_start, y_end):
        row = y * width
        for x in range(x0, x1):
            red, green, blue = rgb[row + x]
            total += 1
            chroma = max(red, green, blue) - min(red, green, blue)
            luminance = (red + green + blue) // 3
            if chroma < 18 and 175 <= luminance <= 235:
                gray += 1
            elif luminance < 80:
                ink += 1
            elif chroma > 40:
                vivid += 1
    if total == 0:
        return False
    return gray / total > 0.55 and ink / total < 0.08 and vivid / total < 0.08


def find_first_article_click(
    rgb: list[tuple[int, int, int]],
    width: int,
    height: int,
) -> tuple[int, int] | None:
    """Click the first article card on an official-account page.

    The card is a wide block of cover image or title below the profile header.
    A one-line label such as 置顶 is not tall enough to count. A gray
    「帖子可能已被删除」 veil is skipped so the click lands on the next article.
    """
    x0, x1 = int(width * 0.12), int(width * 0.88)
    y0, y1 = int(height * 0.42), int(height * 0.90)
    if x1 <= x0 or y1 <= y0:
        return None
    span = x1 - x0
    row_is_card: list[bool] = []
    for y in range(y0, y1):
        filled = 0
        row = y * width
        for x in range(x0, x1):
            red, green, blue = rgb[row + x]
            luminance = (red + green + blue) // 3
            if luminance < 235 or max(red, green, blue) - min(red, green, blue) > 25:
                filled += 1
        row_is_card.append(filled >= int(span * 0.35))
    min_run = max(6, int(height * 0.05))
    # A cover is often split by a thin white line. Keep those pieces as one card
    # so the click lands in the picture, not on the strip above it.
    gap_limit = max(2, int(height * 0.015))
    runs: list[tuple[int, int]] = []
    run = 0
    start = 0
    gap = 0
    for index, is_card in enumerate(row_is_card):
        if is_card:
            if run == 0:
                start = index
            elif gap:
                run += gap
            run += 1
            gap = 0
            continue
        if run == 0:
            continue
        gap += 1
        if gap <= gap_limit:
            continue
        if run >= min_run:
            runs.append((start, run))
        run = 0
        gap = 0
    if run >= min_run:
        runs.append((start, run))
    for start, run in runs:
        y_start = y0 + start
        y_end = y_start + run
        if _card_is_deleted_placeholder(rgb, width, x0, x1, y_start, y_end):
            continue
        return (x0 + x1) // 2, y_start + run // 2
    return None


def find_more_menu_click(
    rgb: list[tuple[int, int, int]],
    width: int,
    height: int,
) -> tuple[int, int] | None:
    """Dark ⋯ cluster in the top-right chrome. Left unblurred so the dots survive."""
    x0 = int(width * 0.86)
    # Skip the top hairline. Its dark pixels sit at y=0 and are not the button.
    y0 = max(2, int(height * 0.012))
    y1 = max(y0 + 3, int(height * 0.08))
    xs: list[int] = []
    ys: list[int] = []
    for y in range(y0, y1):
        row = y * width
        for x in range(x0, width):
            red, green, blue = rgb[row + x]
            if max(red, green, blue) < 80 and green < red + 25:
                xs.append(x)
                ys.append(y)
    if len(xs) < 3:
        return None
    # Several dark icons share this corner. The article ⋯ is the rightmost one;
    # the centroid of all of them lands between the icons and misses the button.
    right = max(xs)
    chosen = [(x, y) for x, y in zip(xs, ys) if x >= right - 6]
    return sum(point[0] for point in chosen) // len(chosen), sum(point[1] for point in chosen) // len(chosen)


def capture_window_samples(window: Any) -> tuple[list[tuple[int, int, int]], int, int] | None:
    """Capture and downsample the target window for the optional visual fallback.

    macOS uses the Quartz window id. Windows uses a bounded ``pyautogui``
    screenshot so the detector never searches the whole desktop. Both paths
    return the same RGB sample grid; a capture failure simply falls back to
    the calibrated relative point.
    """
    try:
        if sys.platform == "win32":
            rect = window.rectangle()
            left, top = int(rect.left), int(rect.top)
            width, height = int(rect.width), int(rect.height)
            if width < 20 or height < 20:
                return None
            import pyautogui  # type: ignore

            image = pyautogui.screenshot(region=(left, top, width, height)).convert("RGB")
            width, height = image.size
            step = max(1, width // 360)
            out_w, out_h = width // step, height // step
            if out_w < 8 or out_h < 8:
                return None
            pixels = image.load()
            rgb = [
                tuple(int(channel) for channel in pixels[x * step, y * step])
                for y in range(out_h)
                for x in range(out_w)
            ]
            return rgb, out_w, out_h

        if sys.platform != "darwin":
            return None
        window_id = int(getattr(window, "window_id", 0) or 0)
        if window_id <= 0:
            return None
        from Quartz import (  # type: ignore
            CGDataProviderCopyData,
            CGImageGetBytesPerRow,
            CGImageGetDataProvider,
            CGImageGetHeight,
            CGImageGetWidth,
            CGRectNull,
            CGWindowListCreateImage,
            kCGWindowImageBoundsIgnoreFraming,
            kCGWindowListOptionIncludingWindow,
        )

        image = CGWindowListCreateImage(
            CGRectNull,
            kCGWindowListOptionIncludingWindow,
            window_id,
            kCGWindowImageBoundsIgnoreFraming,
        )
        if image is None:
            return None
        width = int(CGImageGetWidth(image))
        height = int(CGImageGetHeight(image))
        if width < 20 or height < 20:
            return None
        data = bytes(CGDataProviderCopyData(CGImageGetDataProvider(image)))
        stride = int(CGImageGetBytesPerRow(image))
        step = max(1, width // 360)
        out_w, out_h = width // step, height // step
        if out_w < 8 or out_h < 8:
            return None
        rgb: list[tuple[int, int, int]] = []
        for y in range(out_h):
            row = (y * step) * stride
            for x in range(out_w):
                index = row + (x * step) * 4
                if index + 2 >= len(data):
                    rgb.append((255, 255, 255))
                    continue
                # Quartz window captures are BGRA.
                rgb.append((data[index + 2], data[index + 1], data[index]))
        return rgb, out_w, out_h
    except Exception as exc:
        debug_log(f"vision capture failed: {exc}")
        return None


def locate_visual_point(
    window: Any,
    finder: Any,
) -> tuple[int, int] | None:
    """Map a color hit in the window image back to window-relative pixels."""
    if not vision_enabled():
        return None
    captured = capture_window_samples(window)
    if captured is None:
        return None
    rgb, width, height = captured
    hit = finder(rgb, width, height)
    if hit is None:
        return None
    rect = window.rectangle()
    live_w = max(int(rect.width), 1)
    live_h = max(int(rect.height), 1)
    return (
        min(live_w - 1, max(0, int(round(hit[0] * live_w / width)))),
        min(live_h - 1, max(0, int(round(hit[1] * live_h / height)))),
    )


def choose_relative_point(
    window: Any,
    fallback: tuple[int, int] | None,
    finder: Any,
    label: str,
) -> tuple[int, int]:
    visual = locate_visual_point(window, finder)
    if visual is not None:
        debug_log(f"vision {label}={visual}")
        return visual
    if os.getenv("WECHAT_SIM_REQUIRE_ARTICLE_VISION", "").strip().lower() in {"1", "true", "yes"}:
        raise AutomationError(
            "article_control_not_found",
            f"could not visually locate {label}; refusing fallback coordinates",
            True,
        )
    if fallback is None:
        raise AutomationError(
            "coordinates_not_configured",
            f"{label} point is not configured and visual location failed",
            True,
        )
    scaled = maybe_scale_point(window, fallback)
    debug_log(f"hand-recorded {label}={scaled}")
    return scaled


def _move_process_window(pid: int, x: int, y: int) -> bool:
    """Move one app window. Used so a full-screen overlay does not take the click."""
    script = f"""
tell application "System Events"
  set proc to first process whose unix id is {int(pid)}
  set position of window 1 of proc to {{{int(x)}, {int(y)}}}
end tell
"""
    try:
        result = subprocess.run(["osascript", "-e", script], check=False, capture_output=True, timeout=3)
    except (OSError, subprocess.TimeoutExpired):
        return False
    return result.returncode == 0


def _park_covering_overlays(x: int, y: int) -> list[tuple[int, int, int]]:
    """Slide higher-layer windows off this point. The Dock and WeChat stay put."""
    if sys.platform != "darwin":
        return []
    try:
        from Quartz import (  # type: ignore
            CGWindowListCopyWindowInfo,
            kCGNullWindowID,
            kCGWindowListOptionOnScreenOnly,
        )
    except Exception:
        return []
    skip = {
        "window server",
        "dock",
        "程序坞",
        "systemuiserver",
        "control center",
        "控制中心",
        "通知中心",
        "notification center",
        "微信",
        "wechat",
    }
    parked: list[tuple[int, int, int]] = []
    seen: set[int] = set()
    windows = CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly, kCGNullWindowID) or []
    for info in windows:
        layer = int(info.get("kCGWindowLayer", 0) or 0)
        if layer <= 0 or layer >= 100:
            continue
        owner = str(info.get("kCGWindowOwnerName", "")).strip()
        if owner.lower() in skip:
            continue
        bounds = info.get("kCGWindowBounds") or {}
        left, top = int(bounds.get("X", 0)), int(bounds.get("Y", 0))
        width, height = int(bounds.get("Width", 0)), int(bounds.get("Height", 0))
        if width < 40 or height < 40:
            continue
        if not (left <= x <= left + width and top <= y <= top + height):
            continue
        pid = int(info.get("kCGWindowOwnerPID") or 0)
        if pid <= 0 or pid in seen:
            continue
        seen.add(pid)
        if _move_process_window(pid, -3000, -3000):
            parked.append((pid, left, top))
            debug_log(f"moved covering window aside owner={owner} pid={pid}")
    return parked


_held_overlays: list[tuple[int, int, int]] = []
# Other WeChat windows (the chat shell) can sit on the article pixels and take the click.
_held_siblings: list[tuple[int, int, int, int]] = []
_overlay_hold = 0


def _begin_overlay_hold() -> None:
    global _overlay_hold
    _overlay_hold += 1


def _end_overlay_hold() -> None:
    global _overlay_hold
    _overlay_hold = max(0, _overlay_hold - 1)
    if _overlay_hold != 0:
        return
    if _held_overlays:
        parked = list(_held_overlays)
        _held_overlays.clear()
        _restore_covering_overlays(parked)
    if _held_siblings:
        siblings = list(_held_siblings)
        _held_siblings.clear()
        _restore_wechat_siblings(siblings)


def _raise_wechat_over_click(x: int, y: int) -> None:
    """Put the article window above the editor. Process focus alone leaves it underneath."""
    size = _wechat_window_size_at(x, y)
    if size is None:
        return
    _raise_mac_window(SimpleNamespace(width=size[0], height=size[1]))


def _wechat_window_size_at(x: int, y: int) -> tuple[int, int] | None:
    """Largest WeChat window, preferring the one under this screen point."""
    if sys.platform != "darwin":
        return None
    try:
        from Quartz import (  # type: ignore
            CGWindowListCopyWindowInfo,
            kCGNullWindowID,
            kCGWindowListOptionOnScreenOnly,
        )
    except Exception:
        return None
    best: tuple[int, int, int, int] | None = None
    for info in CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly, kCGNullWindowID) or []:
        if int(info.get("kCGWindowLayer", 0) or 0) != 0:
            continue
        owner = str(info.get("kCGWindowOwnerName", "")).strip().lower()
        if owner not in {"微信", "wechat"}:
            continue
        bounds = info.get("kCGWindowBounds") or {}
        left, top = int(bounds.get("X", 0)), int(bounds.get("Y", 0))
        width, height = int(bounds.get("Width", 0)), int(bounds.get("Height", 0))
        if width < 200 or height < 100:
            continue
        contains = left <= x <= left + width and top <= y <= top + height
        rank = (1 if contains else 0, width * height)
        if best is None or rank > (best[0], best[1]):
            best = (rank[0], rank[1], width, height)
    if best is None:
        return None
    return best[2], best[3]


def _restore_covering_overlays(parked: list[tuple[int, int, int]]) -> None:
    for pid, left, top in reversed(parked):
        _move_process_window(pid, left, top)


def _move_wechat_window_by_size(width: int, height: int, x: int, y: int) -> bool:
    """Move the WeChat window of this size. The article reader and the chat shell differ in size."""
    script = f"""
tell application "System Events"
  tell process "WeChat"
    repeat with w in windows
      try
        set sz to size of w
        if (item 1 of sz as integer) is {int(width)} and (item 2 of sz as integer) is {int(height)} then
          set position of w to {{{int(x)}, {int(y)}}}
          return
        end if
      end try
    end repeat
  end tell
end tell
"""
    try:
        result = subprocess.run(["osascript", "-e", script], check=False, capture_output=True, timeout=3)
    except (OSError, subprocess.TimeoutExpired):
        return False
    return result.returncode == 0


def _largest_wechat_origin() -> tuple[int, int] | None:
    """Top-left of the largest on-screen WeChat window. Moving the chat shell can shift it."""
    if sys.platform != "darwin":
        return None
    try:
        from Quartz import (  # type: ignore
            CGWindowListCopyWindowInfo,
            kCGNullWindowID,
            kCGWindowListOptionOnScreenOnly,
        )
    except Exception:
        return None
    best: tuple[int, int, int] | None = None
    for info in CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly, kCGNullWindowID) or []:
        if int(info.get("kCGWindowLayer", 0) or 0) != 0:
            continue
        owner = str(info.get("kCGWindowOwnerName", "")).strip().lower()
        if owner not in {"微信", "wechat"}:
            continue
        bounds = info.get("kCGWindowBounds") or {}
        left, top = int(bounds.get("X", 0)), int(bounds.get("Y", 0))
        width, height = int(bounds.get("Width", 0)), int(bounds.get("Height", 0))
        area = width * height
        if width < 200 or height < 100:
            continue
        if best is None or area > best[0]:
            best = (area, left, top)
    if best is None:
        return None
    return best[1], best[2]


def _park_overlapping_wechat_windows(x: int, y: int) -> list[tuple[int, int, int, int]]:
    """Move other WeChat windows off this point. The largest one is the article reader and stays."""
    if sys.platform != "darwin":
        return []
    try:
        from Quartz import (  # type: ignore
            CGWindowListCopyWindowInfo,
            kCGNullWindowID,
            kCGWindowListOptionOnScreenOnly,
        )
    except Exception:
        return []
    covering: list[tuple[int, int, int, int, int]] = []
    for info in CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly, kCGNullWindowID) or []:
        if int(info.get("kCGWindowLayer", 0) or 0) != 0:
            continue
        owner = str(info.get("kCGWindowOwnerName", "")).strip().lower()
        if owner not in {"微信", "wechat"}:
            continue
        bounds = info.get("kCGWindowBounds") or {}
        left, top = int(bounds.get("X", 0)), int(bounds.get("Y", 0))
        width, height = int(bounds.get("Width", 0)), int(bounds.get("Height", 0))
        if width < 200 or height < 100:
            continue
        if not (left <= x <= left + width and top <= y <= top + height):
            continue
        covering.append((width * height, width, height, left, top))
    if len(covering) < 2:
        return []
    covering.sort(reverse=True)
    parked: list[tuple[int, int, int, int]] = []
    for _, width, height, left, top in covering[1:]:
        if _move_wechat_window_by_size(width, height, -2000, top):
            parked.append((width, height, left, top))
            debug_log(f"moved overlapping WeChat window aside {width}x{height} from {left},{top}")
    return parked


def _restore_wechat_siblings(parked: list[tuple[int, int, int, int]]) -> None:
    for width, height, left, top in reversed(parked):
        _move_wechat_window_by_size(width, height, left, top)


def ui_click(pyautogui: Any, x: int, y: int) -> None:
    driver = os.getenv("WECHAT_SIM_CLICK_DRIVER", "auto").strip().lower()
    if sys.platform == "darwin" and driver in {"auto", "quartz"}:
        try:
            from Quartz import (  # type: ignore
                CGEventCreateMouseEvent,
                CGEventPost,
                CGEventSetIntegerValueField,
                CGEventSourceCreate,
                kCGEventLeftMouseDown,
                kCGEventLeftMouseUp,
                kCGEventMouseMoved,
                kCGEventSourceStateHIDSystemState,
                kCGHIDEventTap,
                kCGMouseEventClickState,
            )

            source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState)
            point = (float(x), float(y))
            # A full-screen overlay such as 飞连 sits above every normal window.
            # Move it aside, put WeChat back in front, then click.
            parked = _park_covering_overlays(x, y)
            before = _largest_wechat_origin()
            siblings = _park_overlapping_wechat_windows(x, y)
            after = _largest_wechat_origin()
            if before is not None and after is not None:
                x += after[0] - before[0]
                y += after[1] - before[1]
                point = (float(x), float(y))
            try:
                _raise_wechat_over_click(int(point[0]), int(point[1]))
                time.sleep(0.25)
                # Step the cursor onto the control. A down event posted at the
                # old cursor position never reaches the article.
                start = (point[0] - 80.0, point[1] - 40.0)
                for step in range(1, 5):
                    waypoint = (
                        start[0] + (point[0] - start[0]) * step / 4.0,
                        start[1] + (point[1] - start[1]) * step / 4.0,
                    )
                    moved = CGEventCreateMouseEvent(source, kCGEventMouseMoved, waypoint, 0)
                    CGEventPost(kCGHIDEventTap, moved)
                    time.sleep(0.05)
                time.sleep(0.12)
                down = CGEventCreateMouseEvent(source, kCGEventLeftMouseDown, point, 0)
                up = CGEventCreateMouseEvent(source, kCGEventLeftMouseUp, point, 0)
                CGEventSetIntegerValueField(down, kCGMouseEventClickState, 1)
                CGEventSetIntegerValueField(up, kCGMouseEventClickState, 1)
                CGEventPost(kCGHIDEventTap, down)
                time.sleep(0.12)
                CGEventPost(kCGHIDEventTap, up)
                time.sleep(0.2)
                debug_log(f"clicked {int(point[0])},{int(point[1])}")
            finally:
                if _overlay_hold:
                    _held_overlays.extend(parked)
                    _held_siblings.extend(siblings)
                else:
                    _restore_covering_overlays(parked)
                    _restore_wechat_siblings(siblings)
            return
        except Exception:
            if driver == "quartz":
                raise
    pyautogui.click(x, y)


def valid_article_url(value: str) -> bool:
    """Accept only canonical public WeChat article URLs.

    A missed click can leave the account profile or an unrelated browser page
    in the clipboard.  Treating every HTTP URL as success would silently
    violate the one-latest-article contract, so keep the validation narrow.
    """
    parsed = urlparse(value.strip())
    if parsed.scheme not in {"http", "https"}:
        return False
    if (parsed.hostname or "").lower() != "mp.weixin.qq.com":
        return False
    path = (parsed.path or "").lower().rstrip("/")
    return path == "/s" or path.startswith("/s/") or path == "/article" or path.startswith("/article/")


def optional_point(name: str) -> tuple[int, int] | None:
    """Read a point when configured, without making it required.

    The Mac WeChat share popover is not exposed consistently through the
    accessibility tree.  Keeping its two points optional lets the same
    helper continue to work with a browser (address-bar shortcut) and with
    Windows (the existing shortcut flow).
    """
    value = os.getenv(name, "").strip()
    if not value:
        return None
    return parse_point(name)


def validate_seed_initial_link(initial_link: str) -> None:
    """Reject placeholder article URLs before any UI automation."""
    parsed = urlparse(initial_link.strip())
    if parsed.scheme not in {"http", "https"}:
        raise AutomationError("invalid_initial_link", "initialLink must be http(s)", False)
    if (parsed.hostname or "").lower() != "mp.weixin.qq.com":
        raise AutomationError("invalid_initial_link", "initialLink must be on mp.weixin.qq.com", False)
    path = (parsed.path or "").lower().rstrip("/")
    if path == "/article" or path.startswith("/article/"):
        return
    if path == "/s" or path.startswith("/s/"):
        if path.startswith("/s/") and len(path) > len("/s/"):
            if len(path[len("/s/") :]) < 4:
                raise AutomationError("invalid_initial_link", "article slug in initialLink is too short", False)
            return
        from urllib.parse import parse_qs

        sn = (parse_qs(parsed.query).get("sn") or [""])[0].strip()
        if not sn or sn == "0" or len(sn) < 8:
            raise AutomationError(
                "invalid_initial_link",
                "initialLink has invalid sn (WeChat 参数错误); use 复制链接 to paste a real /s/ URL",
                False,
            )
        return
    if "profile_ext" in path:
        return
    raise AutomationError(
        "invalid_initial_link",
        "initialLink must be mp.weixin.qq.com/s/... or mp/profile_ext home link",
        False,
    )


def resolve_navigation_flow(initial_link: str) -> str:
    """Choose the UI path from the kind of link supplied by the caller.

    The business flow normally starts with an article link: open the article,
    click its official-account entry, then click the first article in that
    account.  Existing callers may still provide an official-account profile
    link, which keeps the older list-to-first-article path.
    """
    configured = os.getenv("WECHAT_SIM_FLOW", "auto").strip().lower()
    if configured == "four_step":
        path = urlparse(initial_link).path.lower()
        if not (path == "/s" or path.startswith("/s/") or "/article/" in path):
            raise AutomationError(
                "invalid_four_step_seed",
                "four_step flow requires a real WeChat article initialLink",
                False,
            )
        return "article_to_account"
    if configured not in {"auto", "article_to_account", "account_to_article"}:
        raise AutomationError(
            "invalid_flow",
            "WECHAT_SIM_FLOW must be auto, article_to_account, or account_to_article",
            True,
        )
    if configured != "auto":
        return configured
    path = urlparse(initial_link).path.lower()
    if path == "/s" or path.startswith("/s/") or "/article/" in path:
        return "article_to_account"
    return "account_to_article"


def read_clipboard_text(pyperclip: Any) -> str:
    """Read the pasteboard; on macOS also try ``pbpaste`` for WeChat copies."""
    values: list[str] = []
    try:
        values.append(str(pyperclip.paste()).strip())
    except Exception as exc:
        values.append(str(exc))
    if sys.platform == "darwin":
        try:
            import subprocess

            out = subprocess.run(["pbpaste"], capture_output=True, text=True, check=False)
            values.append((out.stdout or "").strip())
        except Exception as exc:
            values.append(str(exc))
    for value in values:
        if value and value not in {"None", ""}:
            return value
    return values[0] if values else ""


def read_article_link(pyperclip: Any, timeout_sec: float) -> str:
    """Poll the clipboard until a URL is available.

    We clear the clipboard before clicking a copy action.  This prevents a
    previous article URL from being reported as a successful copy when the UI
    click was missed or the popover was still opening.
    """
    deadline = time.monotonic() + max(timeout_sec, 0.1)
    last_value = ""
    while time.monotonic() < deadline:
        last_value = read_clipboard_text(pyperclip)
        if valid_article_url(last_value):
            return last_value
        time.sleep(0.1)
    raise AutomationError(
        "clipboard_not_article_link",
        f"clipboard does not contain an HTTP(S) article link: {last_value!r}",
        False,
    )


def resolve_more_menu_point(window: Any | None = None) -> tuple[int, int]:
    """Article chrome \"⋯\" menu button (not the forward/share popover)."""
    if window is not None:
        visual = locate_visual_point(window, find_more_menu_click)
        if visual is not None:
            debug_log(f"vision more_menu={visual}")
            return visual
        if os.getenv("WECHAT_SIM_REQUIRE_MENU_VISION", "").strip().lower() in {"1", "true", "yes"}:
            raise AutomationError(
                "article_menu_not_found",
                "could not locate the article ⋯ menu; refusing to use fallback coordinates",
                True,
            )
    if window is not None:
        rect = window.rectangle()
        # Calibrated points were taken on a wide shell (~920px). The Mac article
        # reader is a narrow window; scale would miss its top-right ⋯.
        if int(rect.width) < 560:
            return max(24, int(rect.width) - 32), 36
    auto = os.getenv("WECHAT_SIM_MORE_MENU_AUTO", "").strip().lower() in {"1", "true", "yes"}
    if auto and window is not None:
        rect = window.rectangle()
        return max(40, int(rect.width * 0.94)), max(32, int(rect.height * 0.07))
    explicit = os.getenv("WECHAT_SIM_MORE_MENU_POINT", "").strip()
    if explicit:
        return maybe_scale_point(window, parse_point("WECHAT_SIM_MORE_MENU_POINT"))
    # Legacy name: older configs called this share_point.
    return maybe_scale_point(window, parse_point("WECHAT_SIM_SHARE_POINT"))


def menu_use_keyboard() -> bool:
    raw = os.getenv("WECHAT_SIM_MENU_USE_KEYBOARD", "").strip().lower()
    if raw in {"0", "false", "no"}:
        return False
    if raw in {"1", "true", "yes"}:
        return True
    return sys.platform == "darwin"


def menu_down_steps() -> int:
    # The ⋯ menu opens with nothing highlighted: one Down lands on 刷新,
    # two Downs land on 复制链接.
    raw = os.getenv("WECHAT_SIM_MENU_DOWN_STEPS", "2").strip()
    try:
        steps = int(raw)
    except ValueError as exc:
        raise AutomationError("invalid_menu_down_steps", "WECHAT_SIM_MENU_DOWN_STEPS must be an integer", True) from exc
    if steps < 0 or steps > 10:
        raise AutomationError("invalid_menu_down_steps", "WECHAT_SIM_MENU_DOWN_STEPS must be between 0 and 10", True)
    return steps



def activate_target_app() -> None:
    """Bring WeChat forward so clicks are not delivered to a window covering it."""
    if sys.platform != "darwin":
        return
    app = os.getenv("WECHAT_SIM_MAC_APP", "WeChat").strip() or "WeChat"
    subprocess.run(
        ["osascript", "-e", f'tell application "System Events" to tell process "{app}" to set frontmost to true'],
        check=False,
    )
    time.sleep(0.25)


def _copy_row_from_menu(more_menu_point: tuple[int, int], copy_point: tuple[int, int]) -> tuple[int, int]:
    """Keep the calibrated offset from ⋯ to 复制链接 when vision moves the button."""
    raw = os.getenv("WECHAT_SIM_MORE_MENU_POINT", "").strip()
    if not raw:
        return copy_point
    try:
        ref_x_s, ref_y_s = raw.split(",", 1)
        ref_x, ref_y = int(ref_x_s.strip()), int(ref_y_s.strip())
    except ValueError:
        return copy_point
    return more_menu_point[0] + (copy_point[0] - ref_x), more_menu_point[1] + (copy_point[1] - ref_y)


def _press_front_keys(keys: list[str]) -> None:
    """Send menu keys to WeChat itself so they do not land in the editor."""
    if sys.platform == "darwin":
        codes = {"down": 125, "enter": 36, "return": 36, "escape": 53}
        lines = ["set frontmost to true"]
        for key in keys:
            code = codes.get(key)
            if code is None:
                continue
            lines.append(f"key code {code}")
            lines.append("delay 0.08")
        script = "tell application \"System Events\" to tell process \"WeChat\"\n" + "\n".join(lines) + "\nend tell"
        try:
            subprocess.run(["osascript", "-e", script], check=False, capture_output=True, timeout=4)
            return
        except (OSError, subprocess.TimeoutExpired):
            pass
    import pyautogui  # type: ignore

    for key in keys:
        pyautogui.press(key)
        time.sleep(0.08)


def copy_link_from_mac_wechat_menu(
    pyautogui: Any,
    pyperclip: Any,
    window: Any,
    more_menu_point: tuple[int, int],
    copy_point: tuple[int, int] | None,
    wait_sec: float,
    clipboard_timeout_sec: float,
) -> str:
    """Copy a link via macOS WeChat article \"⋯\" → \"复制链接\".

    On macOS the menu opens with no row highlighted. 「刷新」 is first and
    「复制链接」 is second, so the shortcut is Down, Down, Enter.
    """
    title = str(getattr(window, "title", "")).strip().lower()
    if title in {"微信", "wechat"} and os.getenv("WECHAT_SIM_ALLOW_GENERIC_SHELL_ARTICLE", "").strip().lower() not in {"1", "true", "yes"}:
        raise AutomationError(
            "article_window_not_confirmed",
            "refusing to click the WeChat chat shell; open/select the article reader window first",
            True,
        )
    try:
        pyperclip.copy("")
    except Exception:
        pass
    activate_target_app()
    window.set_focus(pyautogui)
    # Page waits are several seconds. The ⋯ menu closes if we wait that long,
    # so the dropdown itself only gets a short pause.
    menu_pause = 0.45
    del wait_sec
    # Keep a covering overlay aside until Down+Enter finishes. Putting it back
    # between the ⋯ click and the key presses closes the menu.
    _begin_overlay_hold()
    try:
        # Focus is already set. Keyboard (⋯, then Down+Enter) is the copy action.
        # The calibrated row is only the backup when that shortcut misses.
        last_error = ""
        if menu_use_keyboard():
            steps = menu_down_steps()
            try:
                pyperclip.copy("")
            except Exception:
                pass
            ui_click(pyautogui, *absolute_point(window, more_menu_point))
            time.sleep(menu_pause)
            _press_front_keys(["down"] * steps + ["enter"])
            try:
                return read_article_link(pyperclip, min(clipboard_timeout_sec, 4.0))
            except AutomationError as exc:
                last_error = exc.message
                _press_front_keys(["escape"])
                time.sleep(0.2)
        if copy_point is not None:
            try:
                pyperclip.copy("")
            except Exception:
                pass
            ui_click(pyautogui, *absolute_point(window, more_menu_point))
            time.sleep(menu_pause)
            scaled_copy = _copy_row_from_menu(more_menu_point, copy_point)
            ui_click(pyautogui, *absolute_point(window, scaled_copy))
            try:
                return read_article_link(pyperclip, clipboard_timeout_sec)
            except AutomationError as exc:
                last_error = exc.message
                _press_front_keys(["escape"])
                time.sleep(0.2)
            raise AutomationError(
                "clipboard_not_article_link",
                last_error or "menu did not copy a link",
                False,
            )
        if menu_use_keyboard():
            raise AutomationError(
                "clipboard_not_article_link",
                last_error or "menu keyboard did not copy a link",
                False,
            )
        raise AutomationError(
            "coordinates_not_configured",
            "WECHAT_SIM_COPY_LINK_POINT is required when WECHAT_SIM_MENU_USE_KEYBOARD=0",
            True,
        )
    finally:
        _end_overlay_hold()


def copy_link_from_shortcut(
    pyautogui: Any,
    pyperclip: Any,
    window: Any,
    address_shortcut: list[str],
    copy_shortcut: list[str],
    clipboard_timeout_sec: float,
) -> str:
    """Copy the current URL from a browser/address-bar style article view."""
    try:
        pyperclip.copy("")
    except Exception:
        pass
    window.set_focus(pyautogui)
    pyautogui.hotkey(*address_shortcut)
    pyautogui.hotkey(*copy_shortcut)
    return read_article_link(pyperclip, clipboard_timeout_sec)


def copy_current_article_link(
    pyautogui: Any,
    pyperclip: Any,
    window: Any,
    copy_shortcut: list[str],
    clipboard_timeout_sec: float,
) -> str:
    """Copy the URL handled by a native article reader with one shortcut.

    Some WeChat article readers intercept Ctrl/C/Command-C and place the
    current article URL on the clipboard even though they have no address
    bar.  This mode is useful for a quick smoke test; menu mode remains the
    fallback when the target build does not provide that shortcut.
    """
    try:
        pyperclip.copy("")
    except Exception:
        pass
    window.set_focus(pyautogui)
    pyautogui.hotkey(*copy_shortcut)
    return read_article_link(pyperclip, clipboard_timeout_sec)


def press_keys(pyautogui: Any, keys: list[str]) -> None:
    """Send either a single key or a modifier combination."""
    if len(keys) == 1:
        pyautogui.press(keys[0])
    else:
        pyautogui.hotkey(*keys)


def pick_chat_shell(windows: list[MacWindow]) -> MacWindow:
    """Main WeChat chat window, not the titled article reader."""
    labeled = [window for window in windows if window.title.strip() in {"微信 (窗口)", "WeChat (窗口)"}]
    if labeled:
        return max(labeled, key=_mac_window_area)
    generic = [window for window in windows if is_generic_wechat_title(window.title)]
    pool = generic or list(windows)
    return max(pool, key=_mac_window_area)


def file_transfer_link_points(window: MacWindow) -> list[tuple[int, int]]:
    explicit = optional_point("WECHAT_SIM_FILE_TRANSFER_LINK_POINT")
    if explicit is not None:
        return [maybe_scale_point(window, explicit)]
    rect = window.rectangle()
    width, height = int(rect.width), int(rect.height)
    # A sent link bubble is normally immediately above the input box.  When
    # only the input point was calibrated (the common file_transfer setup),
    # try that geometry first before the coarse window-relative fallbacks.
    input_point = optional_point("WECHAT_SIM_FILE_TRANSFER_INPUT_POINT")
    default_input_point = input_point is None and sys.platform == "darwin"
    if default_input_point:
        # Keep the click candidates aligned with the same default composer
        # point used by send_link_to_file_transfer after the chat search may
        # resize the shell.
        input_point = (max(80, int(width * 0.72)), max(80, int(height * 0.88)))
    near_input: list[tuple[int, int]] = []
    if input_point is not None:
        if default_input_point:
            input_x, input_y = input_point
        else:
            input_x, input_y = maybe_scale_point(window, input_point)
        # The link bubble is normally about one composer-height above the
        # input field.  Keep several nearby points because the bubble's
        # vertical position changes with wrapping and window scaling.
        for dx, dy in ((0, -115), (0, -100), (-20, -115), (0, -80), (80, -80), (-80, -80), (0, -130)):
            near_input.append(
                (
                    max(8, min(width - 8, input_x + dx)),
                    max(8, min(height - 8, input_y + dy)),
                )
            )
    # Recent bubbles sit above the input box. Sent links are toward the right;
    # received links are toward the left of the chat pane.
    return near_input + [
        (int(width * 0.70), int(height * 0.62)),
        (int(width * 0.48), int(height * 0.62)),
        (int(width * 0.70), int(height * 0.50)),
    ]


def _window_visual_signature(window: Any | None) -> tuple[int, ...] | None:
    """Sparse brightness sample so a newly opened article can be told from the old one."""
    if window is None:
        return None
    captured = capture_window_samples(window)
    if captured is None:
        return None
    rgb, width, height = captured
    sample: list[int] = []
    for y in range(8, height, 19):
        row = y * width
        for x in range(8, width, 29):
            red, green, blue = rgb[row + x]
            sample.append((red + green + blue) // 3)
    return tuple(sample)


def _signature_delta(previous: tuple[int, ...] | None, current: tuple[int, ...] | None) -> float:
    if previous is None or current is None or len(previous) != len(current) or not previous:
        return 0.0
    return sum(abs(a - b) for a, b in zip(previous, current)) / len(previous)


def wait_for_article_content_change(
    window: Any,
    previous: tuple[int, ...] | None,
    timeout_sec: float,
    label: str = "open",
) -> bool:
    if previous is None:
        return True
    deadline = time.monotonic() + max(timeout_sec, 0.5)
    last_delta = 0.0
    while time.monotonic() < deadline:
        current = _window_visual_signature(window)
        last_delta = _signature_delta(previous, current)
        # A hover or tab animation is a small flicker. A real page change moves
        # the sampled brightness by much more than that.
        if current is not None and last_delta >= 8:
            debug_log(f"article content changed after {label} delta={last_delta:.1f}")
            return True
        time.sleep(0.4)
    debug_log(f"article content did not change after {label} delta={last_delta:.1f}")
    return False


def publisher_avatar_mean(window: Any) -> tuple[int, int, int] | None:
    captured = capture_window_samples(window)
    if captured is None:
        return None
    rgb, width, height = captured
    avatar = find_account_avatar(rgb, width, height)
    if avatar is None:
        return None
    mean = avatar["mean"]
    return int(mean[0]), int(mean[1]), int(mean[2])


def _publisher_matches(window: Any, seed_avatar: tuple[int, int, int], timeout_sec: float) -> bool:
    deadline = time.monotonic() + max(timeout_sec, 0.5)
    current: tuple[int, int, int] | None = None
    while time.monotonic() < deadline:
        current = publisher_avatar_mean(window)
        if current is not None and avatars_match(seed_avatar, current):
            debug_log(f"publisher avatar matches seed {seed_avatar} ~ {current}")
            return True
        time.sleep(0.4)
    debug_log(f"publisher avatar mismatch seed={seed_avatar} current={current}")
    return False


def window_looks_like_profile(window: Any) -> bool:
    captured = capture_window_samples(window)
    if captured is None:
        return False
    rgb, width, height = captured
    return looks_like_account_profile(rgb, width, height)


def _existing_article_reader(title_regex: str, target_app: str | None) -> MacWindow | None:
    """The native article window, not the main chat shell titled only 「微信」."""
    windows = list_mac_windows(title_regex, target_app)
    readers = [
        window
        for window in windows
        if window.title.strip() in {"微信 (窗口)", "WeChat (窗口)"}
        or (
            not is_generic_wechat_title(window.title)
            and window.title.strip() not in {"文件传输助手", "File Transfer Assistant"}
        )
    ]
    if not readers:
        return None
    return max(readers, key=_mac_window_area)


def wait_for_titled_article(title_regex: str, target_app: str | None, timeout_sec: float) -> MacWindow | None:
    deadline = time.monotonic() + timeout_sec
    started = time.monotonic()
    while time.monotonic() < deadline:
        # Window IDs are sufficient here and avoid repeatedly invoking the
        # slower Accessibility title lookup while an article is loading.
        windows = list_mac_windows(title_regex, target_app, attach_titles=False)
        # A detached chat window can have a non-generic title such as
        # "文件传输助手".  It must not be mistaken for the article reader
        # merely because it has a title.  When the click should open a new
        # reader, prefer a window that did not exist before the click.
        launched = windows_opened_since_launch(windows)
        if launched:
            # Some WeChat builds give the native article reader the generic
            # title "微信 (窗口)".  A new window ID is stronger evidence than
            # its title, so accept the newly opened article even when the
            # Accessibility title is generic.
            article_like = pick_article_like_windows(launched)
            return article_like[0] if article_like else launched[0]
        if not _launch_baseline_window_ids:
            titled = titled_article_windows(list_mac_windows(title_regex, target_app))
            titled = [window for window in titled if window.title.strip() not in {"文件传输助手", "File Transfer Assistant"}]
            if titled:
                return titled[0]
        # Mac WeChat loads the article into the already-open 「微信 (窗口)」
        # instead of creating a new window id. Accept that reader once the
        # link has had a moment to replace the previous page.
        if time.monotonic() >= started + 4:
            reader = _existing_article_reader(title_regex, target_app)
            if reader is not None:
                return reader
        time.sleep(0.4)
    return _existing_article_reader(title_regex, target_app)


def open_article_from_file_transfer(
    pyautogui: Any,
    pyperclip: Any,
    title_regex: str,
    target_app: str | None,
    wait_sec: float,
    timeout_sec: float,
    initial_link: str,
) -> MacWindow:
    """Send ``initial_link`` to 文件传输助手 and open the sent article.

    This is deliberately a clipboard/UI-only path.  The initial article URL is
    pasted into the chat input using the calibrated input point, sent with the
    normal chat shortcut, and then the newly sent bubble is clicked.  It is
    useful when the local WeChat build does not accept an ``open -a`` URL or
    when the operator wants the whole navigation to happen inside WeChat.

    The chat/search and link click points are intentionally best-effort.  The
    only required calibration for this flow is
    ``WECHAT_SIM_FILE_TRANSFER_INPUT_POINT`` once 文件传输助手 is selected.
    ``WECHAT_SIM_FILE_TRANSFER_CHAT_POINT`` remains an optional fallback for
    installations where the search step does not select the conversation.
    """
    windows = list_mac_windows(title_regex, target_app)
    if not windows:
        raise AutomationError("window_not_found", f"target window not found: {title_regex}", True)
    # Record the visible windows before clicking the sent link.  Without this
    # baseline, the already-open File Transfer Assistant chat is returned as
    # if it were the newly opened article reader.
    remember_launch_baseline(title_regex, target_app)
    shell = pick_chat_shell(windows)
    rect = shell.rectangle()
    debug_log(
        f"file transfer shell title={shell.title!r} "
        f"window=({rect.left},{rect.top},{rect.width}x{rect.height})"
    )
    # Raise WeChat before any click. Otherwise the click lands on whatever app
    # is in front (the editor) and the search text is pasted there.
    subprocess.run(["open", "-a", "WeChat"], check=False)
    time.sleep(0.8)
    shell.set_focus(pyautogui)
    time.sleep(0.4)
    query = os.getenv("WECHAT_SIM_FILE_TRANSFER_QUERY", "文件传输助手").strip() or "文件传输助手"
    pyperclip.copy(query)
    modifier = "command" if sys.platform == "darwin" else "ctrl"
    pyautogui.hotkey(modifier, "f")
    time.sleep(0.4)
    pyautogui.hotkey(modifier, "a")
    pyautogui.hotkey(modifier, "v")
    time.sleep(0.8)
    pyautogui.press("enter")
    time.sleep(max(wait_sec, 1.0))

    last_windows = list_mac_windows(title_regex, target_app)
    shell = pick_chat_shell(last_windows) if last_windows else shell

    # The search result should already select 文件传输助手.  Paste and send
    # the seed article before attempting to click a message.  Keeping this
    # operation in one helper also preserves the existing optional
    # ``send_to_file_transfer`` result/error handling used by normal flows.
    send_link_to_file_transfer(
        pyautogui,
        pyperclip,
        initial_link,
        wait_sec,
        timeout_sec,
        window=shell,
    )
    time.sleep(max(wait_sec, 0.2))

    for point in file_transfer_link_points(shell):
        debug_log(f"file transfer link click rel={point} abs={absolute_point(shell, point)}")
        ui_click(pyautogui, *absolute_point(shell, point))
        article = wait_for_titled_article(title_regex, target_app, min(timeout_sec, 6))
        if article is not None:
            article_rect = article.rectangle()
            debug_log(
                f"article window title={article.title!r} "
                f"window=({article_rect.left},{article_rect.top},{article_rect.width}x{article_rect.height})"
            )
            return article
    raise AutomationError(
        "article_window_not_found",
        "clicked File Transfer Assistant but no titled article window opened",
        True,
    )


def send_link_to_file_transfer(
    pyautogui: Any,
    pyperclip: Any,
    article_link: str,
    wait_sec: float,
    timeout_sec: float,
    window: Any | None = None,
) -> None:
    """Paste one already-validated URL into WeChat File Transfer Assistant.

    ``window`` is supplied by the ``file_transfer`` navigation flow after it
    has selected the conversation through the search box. In that case the
    chat point is optional and only the input point must be calibrated. The
    standalone post-copy send flow keeps its historical behavior and still
    uses ``WECHAT_SIM_FILE_TRANSFER_CHAT_POINT`` when no window is supplied.
    """
    title_regex = os.getenv("WECHAT_SIM_SEND_WINDOW_TITLE_REGEX", "微信.*").strip()
    target_app = os.getenv("WECHAT_SIM_SEND_TARGET_APP", "微信").strip()
    chat_point = optional_point("WECHAT_SIM_FILE_TRANSFER_CHAT_POINT")
    input_point = optional_point("WECHAT_SIM_FILE_TRANSFER_INPUT_POINT")
    if input_point is None:
        if sys.platform == "darwin" and window is not None:
            # The Mac chat composer occupies the lower-right portion of the
            # shell.  This fallback keeps the simple four-step path usable
            # before a per-machine calibration is available; a configured
            # point still takes precedence.
            rect = window.rectangle()
            input_point = (max(80, int(rect.width * 0.72)), max(80, int(rect.height * 0.88)))
            debug_log(f"using default Mac file-transfer input point={input_point}")
        else:
            raise AutomationError(
                "coordinates_not_configured",
                "WECHAT_SIM_FILE_TRANSFER_INPUT_POINT is required for File Transfer Assistant",
                True,
            )
    paste_shortcut = hotkey(
        os.getenv(
            "WECHAT_SIM_SEND_PASTE_HOTKEY",
            "command,v" if sys.platform == "darwin" else "ctrl,v",
        )
    )
    send_hotkey = hotkey(os.getenv("WECHAT_SIM_SEND_HOTKEY", "enter"))
    try:
        if window is None:
            window = wait_for_window(title_regex, timeout_sec, target_app, pyautogui)
        else:
            window.set_focus(pyautogui)
        if chat_point is not None:
            ui_click(pyautogui, *absolute_point(window, chat_point))
            time.sleep(max(wait_sec, 0.1))
        ui_click(pyautogui, *absolute_point(window, input_point))
        pyperclip.copy(article_link)
        pyautogui.hotkey(*paste_shortcut)
        time.sleep(max(wait_sec, 0.1))
        press_keys(pyautogui, send_hotkey)
        time.sleep(max(wait_sec, 0.1))
    except AutomationError:
        raise
    except Exception as exc:
        raise AutomationError(
            "file_transfer_send_failed",
            f"failed to send article link to File Transfer Assistant: {exc}",
            True,
        ) from exc


_CHAT_SHELL_TITLES = {"微信", "WeChat"}
_SESSION_WINDOW_TITLES = {"微信 (窗口)", "WeChat (窗口)"}
_LOGIN_TITLE_MARKERS = ("登录", "扫码", "log in", "login", "scan")
# The logged-in Mac chat shell observed via Accessibility is about 880x640.
# The QR login window is smaller and must not match on the shared "微信" title.
_MIN_CHAT_SHELL_WIDTH = 640
_MIN_CHAT_SHELL_HEIGHT = 480
_STATUS_MESSAGES = {
    "logged_in": "微信已登录",
    "logged_out": "微信未登录",
}


def _status_payload(status: str, window_title: str = "") -> dict[str, Any]:
    logged_in = status == "logged_in"
    return {
        "ok": True,
        "status": status,
        "loggedIn": logged_in,
        "windowTitle": window_title if logged_in else "",
        "message": _STATUS_MESSAGES[status],
    }


def _has_login_marker(title: str) -> bool:
    text = title.strip().lower()
    if not text:
        return False
    return any(marker in text for marker in _LOGIN_TITLE_MARKERS)


def _is_logged_in_window(title: str, width: int, height: int) -> bool:
    """Exact Accessibility/UIA titles only. Empty titles are not logged in."""
    text = title.strip()
    if text in _SESSION_WINDOW_TITLES:
        return True
    if text not in _CHAT_SHELL_TITLES:
        return False
    return width >= _MIN_CHAT_SHELL_WIDTH and height >= _MIN_CHAT_SHELL_HEIGHT


def classify_wechat_login(
    process_running: bool,
    windows: list[tuple[str, int, int]],
) -> dict[str, Any]:
    """Classify the local WeChat client from process presence and window titles.

    This does not read protocol data, cookies, or chat content.
    """
    for title, width, height in windows:
        if process_running and _is_logged_in_window(title, width, height):
            return _status_payload("logged_in", title.strip())
    return _status_payload("logged_out")


def read_mac_wechat_windows() -> tuple[bool, list[tuple[str, int, int]]]:
    """Return whether WeChat is running and each window title plus size."""
    script = r"""
tell application "System Events"
  if not (exists process "WeChat") and not (exists process "微信") then
    return "absent"
  end if
  set procName to "WeChat"
  if not (exists process "WeChat") then set procName to "微信"
  tell process procName
    set out to "present" & linefeed
    repeat with w in windows
      try
        set n to name of w
        set s to size of w
        set out to out & n & tab & (item 1 of s as text) & tab & (item 2 of s as text) & linefeed
      end try
    end repeat
    return out
  end tell
end tell
"""
    try:
        result = subprocess.run(
            ["osascript", "-e", script],
            capture_output=True,
            text=True,
            check=False,
            timeout=8,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise AutomationError("wechat_status_unavailable", f"无法读取微信窗口: {exc}", True) from exc
    if result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip()
        raise AutomationError("wechat_status_unavailable", detail or "无法读取微信窗口", True)
    text = (result.stdout or "").strip()
    if not text or text == "absent" or text.startswith("absent"):
        return False, []
    windows: list[tuple[str, int, int]] = []
    for line in text.splitlines():
        if line.strip() == "present":
            continue
        parts = line.split("\t")
        if len(parts) != 3:
            continue
        try:
            windows.append((parts[0], int(float(parts[1])), int(float(parts[2]))))
        except ValueError:
            continue
    return True, windows


def _windows_wechat_process_running() -> bool:
    for image in ("WeChat.exe", "Weixin.exe"):
        result = subprocess.run(
            ["tasklist", "/FI", f"IMAGENAME eq {image}", "/NH"],
            capture_output=True,
            text=True,
            check=False,
            timeout=8,
        )
        if image.lower() in (result.stdout or "").lower():
            return True
    return False


def read_windows_wechat_windows() -> tuple[bool, list[tuple[str, int, int]]]:
    from pywinauto import Desktop  # type: ignore

    found: list[tuple[str, int, int]] = []
    raw_windows = Desktop(backend="uia").windows(visible_only=True)
    for raw in raw_windows:
        try:
            window = WindowsWindow(raw)
            rect = window.rectangle()
        except Exception:
            continue
        title = window.title.strip()
        if title in _CHAT_SHELL_TITLES or title in _SESSION_WINDOW_TITLES or _has_login_marker(title):
            found.append((title, int(rect.width), int(rect.height)))
    if found:
        return True, found
    return _windows_wechat_process_running(), []


def wechat_login_status(platform_name: str | None = None) -> dict[str, Any]:
    platform_name = platform_name or sys.platform
    if platform_name == "darwin":
        running, windows = read_mac_wechat_windows()
    elif platform_name == "win32":
        running, windows = read_windows_wechat_windows()
    else:
        raise AutomationError("unsupported_platform", f"unsupported platform: {platform_name}", True)
    return classify_wechat_login(running, windows)


def main() -> int:
    if "--wechat-status" in sys.argv[1:]:
        try:
            result = wechat_login_status()
        except AutomationError as exc:
            response(False, status="logged_out", loggedIn=False, message="微信未登录", code=exc.code, error=exc.message, manualIntervention=exc.manual)
            return 0
        except Exception as exc:
            response(False, status="logged_out", loggedIn=False, message="微信未登录", code="wechat_status_failed", error=str(exc))
            return 0
        ok = bool(result.pop("ok", True))
        response(ok, **result)
        return 0
    raw = sys.stdin.read()
    try:
        account = json.loads(raw)
    except json.JSONDecodeError as exc:
        response(False, code="invalid_request", error=f"invalid account JSON: {exc}")
        return 0
    initial_link = str(account.get("initialLink", "")).strip()
    if not initial_link:
        response(False, code="invalid_request", error="initialLink is required")
        return 0

    try:
        validate_seed_initial_link(initial_link)
    except AutomationError as exc:
        response(False, code=exc.code, error=exc.message, manualIntervention=exc.manual)
        return 0

    try:
        # Resolve the business flow before loading desktop dependencies. This
        # keeps the four-step contract explicit and rejects a profile seed
        # immediately when WECHAT_SIM_FLOW=four_step is selected.
        navigation_flow = resolve_navigation_flow(initial_link)
        pyautogui, pyperclip = require_automation_modules()
        link_mode = os.getenv("WECHAT_SIM_LINK_MODE", "auto").strip().lower()
        if link_mode == "share":
            # share was a misnomer for the article "⋯" menu on macOS WeChat.
            link_mode = "menu"
        if link_mode not in {"auto", "direct", "shortcut", "current", "menu", "clipboard"}:
            raise AutomationError(
                "invalid_link_mode",
                "WECHAT_SIM_LINK_MODE must be auto, direct, shortcut, current, menu, or clipboard",
                True,
            )
        title_regex = os.getenv("WECHAT_SIM_WINDOW_TITLE_REGEX", ".*").strip()
        if link_mode != "clipboard" and (not title_regex or title_regex == ".*"):
            raise AutomationError(
                "window_title_not_configured",
                "Set WECHAT_SIM_WINDOW_TITLE_REGEX to the browser or WeChat window title",
                True,
            )
        reuse_current_article = os.getenv("WECHAT_SIM_REUSE_CURRENT_ARTICLE", "").strip().lower() in {
            "1",
            "true",
            "yes",
        }
        skip_article_navigation = os.getenv("WECHAT_SIM_SKIP_ARTICLE_NAV", "").strip().lower() in {
            "1",
            "true",
            "yes",
        } or link_mode == "clipboard"
        if reuse_current_article and link_mode == "clipboard":
            raise AutomationError(
                "invalid_start_mode",
                "WECHAT_SIM_REUSE_CURRENT_ARTICLE cannot be combined with clipboard link mode",
                True,
            )
        # Reusing an already-open article is an explicit start mode, not the
        # old debug shortcut: steps 2 and 3 must still run in this mode.
        if reuse_current_article:
            skip_article_navigation = False
        if skip_article_navigation:
            account_point = None
            list_point = None
            first_point = None
        elif navigation_flow == "article_to_account":
            # Vision-only Windows calibration is supported.  Keep a hand
            # recorded point when present, but do not fail before the
            # screenshot locator has a chance to identify the control.
            account_point = optional_point("WECHAT_SIM_ARTICLE_ACCOUNT_POINT")
            list_point = None
            # Keep the shorter legacy name as an alias so existing calibration
            # files can be reused for the account page's first article.
            first_point = optional_point("WECHAT_SIM_ACCOUNT_FIRST_ARTICLE_POINT") if os.getenv(
                "WECHAT_SIM_ACCOUNT_FIRST_ARTICLE_POINT", ""
            ).strip() else optional_point("WECHAT_SIM_FIRST_ARTICLE_POINT")
        else:
            account_point = None
            list_point = optional_point("WECHAT_SIM_ARTICLE_LIST_POINT")
            first_point = optional_point("WECHAT_SIM_FIRST_ARTICLE_POINT")
        wait_sec = float(os.getenv("WECHAT_SIM_WAIT_SEC", "3"))
        timeout_sec = float(os.getenv("WECHAT_SIM_WINDOW_TIMEOUT_SEC", "20"))
        clipboard_timeout_sec = float(os.getenv("WECHAT_SIM_CLIPBOARD_TIMEOUT_SEC", "3"))
        modifier = "command" if sys.platform == "darwin" else "ctrl"
        close_shortcut = hotkey(os.getenv("WECHAT_SIM_CLOSE_HOTKEY", f"{modifier},w"))
        address_shortcut = hotkey(os.getenv("WECHAT_SIM_ADDRESS_HOTKEY", f"{modifier},l"))
        copy_shortcut = hotkey(os.getenv("WECHAT_SIM_COPY_HOTKEY", f"{modifier},c"))
        copy_link_point = optional_point("WECHAT_SIM_COPY_LINK_POINT")
        send_to_file_transfer = os.getenv(
            "WECHAT_SIM_SEND_TO_FILE_TRANSFER", ""
        ).strip().lower() in {"1", "true", "yes"}
        if link_mode == "menu" and copy_link_point is None and not menu_use_keyboard():
            raise AutomationError(
                "coordinates_not_configured",
                "WECHAT_SIM_COPY_LINK_POINT is required for menu mode when keyboard navigation is disabled",
                True,
            )

        old_clipboard = pyperclip.paste()
        opened = False
        sent_to_file_transfer = False
        send_error: AutomationError | None = None
        try:
            target_app = os.getenv("WECHAT_SIM_TARGET_APP", "").strip() or None
            if link_mode == "clipboard":
                # The operator has already copied a canonical WeChat article
                # URL from the native client.  Do not open a window or clear
                # the pasteboard; just validate and return the existing value.
                window = None
            elif reuse_current_article:
                # The operator has explicitly confirmed that the target
                # article is already open. Keep the window and continue with
                # the normal account -> first article -> copy-link steps.
                if navigation_flow == "article_to_account":
                    window = find_current_article_window(title_regex, target_app, pyautogui)
                else:
                    window = wait_for_window(title_regex, timeout_sec, target_app, pyautogui)
                time.sleep(wait_sec)
            elif skip_article_navigation:
                # Debug/smoke mode operates on an article that the operator
                # already opened. Do not relaunch or close that manual page.
                if navigation_flow == "article_to_account":
                    window = find_current_article_window(title_regex, target_app, pyautogui)
                else:
                    window = wait_for_window(title_regex, timeout_sec, target_app, pyautogui)
                time.sleep(wait_sec)
            else:
                if sys.platform == "darwin":
                    remember_launch_baseline(title_regex, target_app)
                debug_log("step=1 open initial article link")
                previous_article = _window_visual_signature(_existing_article_reader(title_regex, target_app)) if sys.platform == "darwin" else None
                launch_initial_link(initial_link)
                opened = True
                if sys.platform == "darwin" and navigation_flow == "article_to_account":
                    # The four-step contract starts from an article reader.
                    # Do not silently fall back to the already-open chat shell
                    # or File Transfer Assistant: that would send the account
                    # click to the wrong window.  A newly created window ID is
                    # the reliable signal that the initial article actually
                    # opened on macOS.
                    window = wait_for_titled_article(title_regex, target_app, timeout_sec)
                    if window is None:
                        raise AutomationError(
                            "initial_article_not_opened",
                            "initialLink did not open a WeChat article window; check the WeChat launch command and window permissions",
                            True,
                        )
                    window.set_focus(pyautogui)
                    wait_for_article_content_change(window, previous_article, timeout_sec)
                else:
                    window = wait_for_window(title_regex, timeout_sec, target_app, pyautogui)
                time.sleep(wait_sec)

            if not skip_article_navigation:
                debug_log("step=2 enter official account; step=3 open first article")
                if navigation_flow == "article_to_account" and sys.platform == "darwin":
                    # The window handle only supplies position and focus.
                    # Publisher and first article have no shortcut: color
                    # first, then the hand-recorded point. A miss is re-recorded,
                    # not guessed.
                    window.set_focus(pyautogui)
                    seed_avatar = publisher_avatar_mean(window)
                    if seed_avatar is None:
                        debug_log("publisher color was not seen; the hand-recorded account point will be used")
                    else:
                        debug_log(f"seed publisher avatar={seed_avatar}")
                    account_rel = choose_relative_point(window, account_point, find_account_click, "article_account")
                    before_account = _window_visual_signature(window)
                    window.set_focus(pyautogui)
                    # Keep 飞连 and the chat shell aside until the new page has painted.
                    # Restoring them in the same instant as the click drops the event.
                    _begin_overlay_hold()
                    try:
                        ui_click(pyautogui, *absolute_point(window, account_rel))
                        account_opened = wait_for_article_content_change(window, before_account, timeout_sec, "account click")
                    finally:
                        _end_overlay_hold()
                    if not account_opened:
                        raise AutomationError(
                            "account_page_not_opened",
                            "clicking the publisher did not leave the seed article",
                            True,
                        )
                    time.sleep(wait_sec)
                    if not window_looks_like_profile(window):
                        raise AutomationError(
                            "account_page_not_opened",
                            "publisher click did not open the official-account home",
                            True,
                        )
                    debug_log("official account home is open")
                elif navigation_flow == "article_to_account":
                    window.set_focus(pyautogui)
                    account_rel = choose_relative_point(window, account_point, find_account_click, "article_account")
                    window.set_focus(pyautogui)
                    ui_click(pyautogui, *absolute_point(window, account_rel))
                    time.sleep(wait_sec)
                else:
                    assert list_point is not None
                    ui_click(pyautogui, *absolute_point(window, maybe_scale_point(window, list_point)))
                    time.sleep(wait_sec)
                before_first = _window_visual_signature(window)
                window.set_focus(pyautogui)
                first_rel = choose_relative_point(window, first_point, find_first_article_click, "first_article")
                window.set_focus(pyautogui)
                _begin_overlay_hold()
                try:
                    ui_click(pyautogui, *absolute_point(window, first_rel))
                    if navigation_flow == "article_to_account" and sys.platform == "darwin":
                        first_opened = wait_for_article_content_change(window, before_first, timeout_sec, "first article")
                    else:
                        first_opened = True
                finally:
                    _end_overlay_hold()
                if navigation_flow == "article_to_account" and sys.platform == "darwin":
                    if not first_opened:
                        raise AutomationError(
                            "first_article_not_opened",
                            "clicking the first card did not open an article",
                            True,
                        )
                    time.sleep(min(wait_sec, 2.0))
                    if seed_avatar is not None and not _publisher_matches(window, seed_avatar, timeout_sec):
                        raise AutomationError(
                            "article_account_mismatch",
                            "opened article publisher does not match the seed account",
                            False,
                        )
                else:
                    time.sleep(wait_sec)

            # step=4 copy the newly opened article URL. menu = WeChat \"⋯\" → \"复制链接\"; direct/shortcut = browser address bar;
            # current = native article reader's Ctrl/Command-C shortcut.
            if link_mode == "clipboard":
                article_link = read_article_link(pyperclip, clipboard_timeout_sec)
            elif link_mode == "menu":
                menu_windows = menu_target_windows(window, title_regex, target_app)
                article_link = ""
                last_menu_error: AutomationError | None = None
                for menu_window in menu_windows:
                    rect = menu_window.rectangle()
                    more_menu_point = resolve_more_menu_point(menu_window)
                    debug_log(
                        f"menu attempt title={getattr(menu_window, 'title', '')!r} "
                        f"window=({rect.left},{rect.top},{rect.width}x{rect.height}) "
                        f"more_menu={more_menu_point} abs={absolute_point(menu_window, more_menu_point)}"
                    )
                    try:
                        article_link = copy_link_from_mac_wechat_menu(
                            pyautogui,
                            pyperclip,
                            menu_window,
                            more_menu_point,
                            copy_link_point,
                            wait_sec,
                            clipboard_timeout_sec,
                        )
                        break
                    except AutomationError as exc:
                        last_menu_error = exc
                if not article_link:
                    assert last_menu_error is not None
                    raise last_menu_error
            elif link_mode == "current":
                article_link = copy_current_article_link(
                    pyautogui,
                    pyperclip,
                    window,
                    copy_shortcut,
                    clipboard_timeout_sec,
                )
            else:
                article_link = copy_link_from_shortcut(
                    pyautogui,
                    pyperclip,
                    window,
                    address_shortcut,
                    copy_shortcut,
                    clipboard_timeout_sec,
                )
            if send_to_file_transfer:
                try:
                    send_link_to_file_transfer(
                        pyautogui,
                        pyperclip,
                        article_link,
                        wait_sec,
                        float(os.getenv("WECHAT_SIM_SEND_WINDOW_TIMEOUT_SEC", str(timeout_sec))),
                    )
                    sent_to_file_transfer = True
                except AutomationError as exc:
                    send_error = exc
                finally:
                    # The final close action belongs to the source article
                    # window, not the File Transfer Assistant chat.
                    try:
                        window.set_focus(pyautogui)
                    except Exception:
                        pass
        finally:
            if opened:
                try:
                    pyautogui.hotkey(*close_shortcut)
                except Exception:
                    pass
            # Do not leave automation data in the operator's clipboard.
            try:
                pyperclip.copy(old_clipboard)
            except Exception:
                pass

        if send_error is not None:
            response(
                False,
                code=send_error.code,
                error=send_error.message,
                articleLink=article_link,
                sentToFileTransfer=False,
                manualIntervention=send_error.manual,
            )
            return 0
        response(
            True,
            articleLink=article_link,
            sentToFileTransfer=sent_to_file_transfer,
        )
        return 0
    except AutomationError as exc:
        response(False, code=exc.code, error=exc.message, manualIntervention=exc.manual)
        return 0
    except Exception as exc:  # Keep stdout machine-readable for the Go driver.
        response(False, code="automation_unexpected_error", error=str(exc), manualIntervention=True)
        return 0


if __name__ == "__main__":
    raise SystemExit(main())

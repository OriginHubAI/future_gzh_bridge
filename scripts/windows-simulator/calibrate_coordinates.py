
#!/usr/bin/env python3

"""Interactively calib

rate UI points used by ``latest_link_helper.py``.


The helper uses points relative to the *outer* target-window rectangle.  This
small utility deliberately does not click anything: the operator moves the
mouse to a control and presses Enter, then the utility records the mouse
position and prints the relative point.  It supports the same two window
backends as the production helper (pywinauto on Windows and Quartz on macOS).

Examples::

    # Windows / PowerShell (browser, account page -> first article)
    py scripts/windows-simulator/calibrate_coordinates.py \
        --title-regex "Microsoft Edge.*" --target-app "Microsoft Edge" \
        --format powershell

    # macOS / zsh (native WeChat share flow)
    python3 scripts/windows-simulator/calibrate_coordinates.py \
        --title-regex '微信.*' --target-app '微信' --mode share

The output is copy/paste-ready for the environment in which the bridge is
started.  No production configuration is changed automatically.
"""

from __future__ import annotations

import argparse
import json
import os
import platform
import re
import shlex
import sys
import time
from dataclasses import dataclass
from types import SimpleNamespace
from typing import Any, Iterable, Mapping, Sequence


class CalibrationError(RuntimeError):
    """An actionable calibration/setup error."""


@dataclass(frozen=True)
class WindowRect:
    """The outer screen rectangle used by the production helper."""

    left: int
    top: int
    width: int
    height: int

    @property
    def right(self) -> int:
        return self.left + self.width

    @property
    def bottom(self) -> int:
        return self.top + self.height


LABEL_ENV: Mapping[str, str] = {
    "article_list": "WECHAT_SIM_ARTICLE_LIST_POINT",
    "first_article": "WECHAT_SIM_FIRST_ARTICLE_POINT",
    "article_account": "WECHAT_SIM_ARTICLE_ACCOUNT_POINT",
    "account_first_article": "WECHAT_SIM_ACCOUNT_FIRST_ARTICLE_POINT",
    "share": "WECHAT_SIM_SHARE_POINT",
    "more_menu": "WECHAT_SIM_MORE_MENU_POINT",
    "copy_link": "WECHAT_SIM_COPY_LINK_POINT",
}


FLOW_LABELS: Mapping[str, tuple[str, ...]] = {
    "account_to_article": ("article_list", "first_article"),
    "article_to_account": ("article_account", "account_first_article"),
}


def parse_point(value: str) -> tuple[int, int]:
    """Parse an ``x,y`` point and reject malformed/negative values."""

    try:
        x_raw, y_raw = value.split(",", 1)
        x, y = int(x_raw.strip()), int(y_raw.strip())
    except (AttributeError, ValueError) as exc:
        raise CalibrationError(f"point must be x,y: {value!r}") from exc
    if x < 0 or y < 0:
        raise CalibrationError(f"point must be non-negative: {value!r}")
    return x, y


def relative_point(rect: WindowRect, screen_point: tuple[int, int]) -> tuple[int, int]:
    """Convert a screen coordinate to the convention used by the helper."""

    x, y = screen_point
    relative = (x - rect.left, y - rect.top)
    if relative[0] < 0 or relative[1] < 0:
        raise CalibrationError(
            f"mouse point {screen_point} is above/left of window "
            f"({rect.left},{rect.top})"
        )
    if relative[0] > rect.width or relative[1] > rect.height:
        raise CalibrationError(
            f"mouse point {screen_point} is outside window "
            f"({rect.left},{rect.top},{rect.width},{rect.height})"
        )
    return relative


def point_text(point: tuple[int, int]) -> str:
    return f"{point[0]},{point[1]}"


def env_lines(points: Mapping[str, tuple[int, int]], output_format: str) -> str:
    """Render captured points as shell commands or machine-readable JSON."""

    ordered = [(LABEL_ENV[label], point_text(points[label])) for label in points]
    if output_format == "json":
        return json.dumps({key: value for key, value in ordered}, ensure_ascii=False, indent=2)
    if output_format == "dotenv":
        return "\n".join(f"{key}={shlex.quote(value)}" for key, value in ordered)
    if output_format == "powershell":
        return "\n".join(f"$env:{key} = '{value}'" for key, value in ordered)
    # bash/zsh syntax is intentionally POSIX-compatible.
    return "\n".join(f"export {key}={shlex.quote(value)}" for key, value in ordered)


def yaml_lines(
    points: Mapping[str, tuple[int, int]], flow: str, mode: str
) -> str:
    """Render a mergeable ``simulator:`` YAML fragment.

    The bridge accepts these fields directly in ``configs/config.yaml``.  We
    keep this renderer dependency-free instead of requiring PyYAML in the
    calibration environment.
    """

    values: list[tuple[str, str]] = [("flow", flow), ("link_mode", mode)]
    for label, point in points.items():
        # LABEL_ENV is intentionally env-oriented; the bridge YAML uses the
        # same suffix without the WECHAT_SIM_ prefix.
        env_name = LABEL_ENV[label]
        yaml_name = env_name.removeprefix("WECHAT_SIM_").lower()
        values.append((yaml_name, point_text(point)))
    lines = ["simulator:"]
    lines.extend(f'  {key}: "{value}"' for key, value in values)
    return "\n".join(lines)


def simulator_updates(
    points: Mapping[str, tuple[int, int]], rect: WindowRect
) -> dict[str, str]:
    """YAML keys written back into an existing ``configs/config.yaml``."""

    updates = {
        LABEL_ENV[label].removeprefix("WECHAT_SIM_").lower(): point_text(point)
        for label, point in points.items()
    }
    updates["coord_ref_size"] = f"{rect.width},{rect.height}"
    return updates


def update_config_file(path: str, updates: Mapping[str, str]) -> list[str]:
    """Replace simulator point values in place. The rest of the file stays."""

    with open(path, encoding="utf-8") as stream:
        text = stream.read()
    newline = "\r\n" if "\r\n" in text else "\n"
    written: list[str] = []
    for key, value in updates.items():
        pattern = re.compile(rf"^([ \t]*{re.escape(key)}:[ \t]*).*$", re.MULTILINE)
        replacement = rf'\1"{value}"'
        text, count = pattern.subn(replacement, text, count=1)
        if count == 0:
            inserted = f'  {key}: "{value}"'
            text, count = re.subn(
                r"^(simulator:[ \t]*)$",
                rf"\1{newline}{inserted}",
                text,
                count=1,
                flags=re.MULTILINE,
            )
            if count == 0:
                raise CalibrationError(f"{path} has no simulator: section for {key}")
        written.append(f"{key}={value}")
    with open(path, "w", encoding="utf-8", newline="") as stream:
        stream.write(text)
    return written


def labels_for(flow: str, mode: str) -> tuple[str, ...]:
    if flow not in FLOW_LABELS:
        raise CalibrationError("flow must be account_to_article or article_to_account")
    if mode not in {"direct", "shortcut", "current", "share", "menu"}:
        raise CalibrationError("mode must be direct, shortcut, current, share, or menu")
    labels = list(FLOW_LABELS[flow])
    if mode == "menu":
        labels.extend(("more_menu", "copy_link"))
    elif mode == "share":
        labels.extend(("share", "copy_link"))
    return tuple(labels)


def _rect_from_any(rect: Any) -> WindowRect:
    """Normalize pywinauto/Quartz rectangle objects and dictionaries."""

    def get(name: str, default: int = 0) -> int:
        if isinstance(rect, Mapping):
            value = rect.get(name, default)
        else:
            value = getattr(rect, name, default)
        # pywinauto versions have exposed some RECT members as properties and
        # others as zero-argument methods.  Accept either shape.
        if callable(value):
            value = value()
        return int(value)

    left = get("left", get("X"))
    top = get("top", get("Y"))
    width = get("width", get("Width"))
    height = get("height", get("Height"))
    # Some APIs expose right/bottom but not width/height.
    if width <= 0:
        right = get("right", get("MaxX"))
        width = right - left
    if height <= 0:
        bottom = get("bottom", get("MaxY"))
        height = bottom - top
    if width <= 0 or height <= 0:
        raise CalibrationError(f"target window has invalid bounds: {rect!r}")
    return WindowRect(left, top, width, height)


class WindowAdapter:
    """Minimal window abstraction shared by Windows and macOS backends."""

    def __init__(self, raw: Any, owner: str = ""):
        self.raw = raw
        self.owner = owner

    def rectangle(self) -> WindowRect:
        raw_rect = self.raw.rectangle()
        return _rect_from_any(raw_rect)

    def focus(self) -> None:
        focus = getattr(self.raw, "set_focus", None)
        if callable(focus):
            focus()


class MacWindowAdapter(WindowAdapter):
    def rectangle(self) -> WindowRect:
        return _rect_from_any(self.raw.get("kCGWindowBounds", {}))

    def focus(self) -> None:
        # Activating by bundle/app name is intentionally best-effort.  Quartz
        # window enumeration remains useful even when Accessibility is absent.
        app = os.getenv("WECHAT_SIM_MAC_APP", "").strip() or self.owner
        if app:
            import subprocess

            subprocess.run(["open", "-a", app], check=False, capture_output=True)


def find_window(title_regex: str, target_app: str = "") -> WindowAdapter | None:
    """Find a visible matching window using the platform-native backend."""

    try:
        pattern = re.compile(title_regex, re.IGNORECASE)
    except re.error as exc:
        raise CalibrationError(f"invalid window title regex: {exc}") from exc
    if sys.platform == "win32":
        try:
            from pywinauto import Desktop  # type: ignore
        except ImportError as exc:
            raise CalibrationError(
                "Install pywinauto (py -m pip install -r scripts/windows-simulator/requirements.txt)"
            ) from exc
        windows = Desktop(backend="uia").windows(title_re=title_regex, visible_only=True)
        matches: list[WindowAdapter] = []
        for window in windows:
            try:
                rect = _rect_from_any(window.rectangle())
                if rect.width >= 200 and rect.height >= 100:
                    matches.append(WindowAdapter(window))
            except Exception:
                continue
        if len(matches) > 1:
            raise CalibrationError(
                f"{len(matches)} visible Windows windows match {title_regex!r}; "
                "narrow --title-regex before calibrating"
            )
        return matches[0] if matches else None

    if sys.platform == "darwin":
        try:
            from Quartz import (  # type: ignore
                CGWindowListCopyWindowInfo,
                kCGNullWindowID,
                kCGWindowListOptionOnScreenOnly,
            )
        except ImportError as exc:
            raise CalibrationError(
                "Install pyobjc-framework-Quartz (python3 -m pip install -r scripts/windows-simulator/requirements.txt)"
            ) from exc
        target = target_app.strip().lower()
        windows = CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly, kCGNullWindowID) or []
        for info in windows:
            owner = str(info.get("kCGWindowOwnerName", ""))
            title = str(info.get("kCGWindowName", ""))
            bounds = info.get("kCGWindowBounds", {}) or {}
            if target and target not in owner.lower():
                continue
            if int(info.get("kCGWindowLayer", 0)) != 0:
                continue
            try:
                rect = _rect_from_any(bounds)
            except CalibrationError:
                continue
            if rect.width >= 200 and rect.height >= 100 and pattern.search(f"{owner} {title}"):
                return MacWindowAdapter(info, owner)
        return None

    raise CalibrationError(f"unsupported platform: {platform.system()}")


def wait_for_window(title_regex: str, target_app: str, timeout_sec: float) -> WindowAdapter:
    deadline = time.monotonic() + max(timeout_sec, 0.1)
    while time.monotonic() < deadline:
        window = find_window(title_regex, target_app)
        if window is not None:
            try:
                window.focus()
            except Exception:
                pass
            return window
        time.sleep(0.25)
    raise CalibrationError(f"window not found before timeout: {title_regex!r}")


def mouse_position() -> tuple[int, int]:
    try:
        import pyautogui  # type: ignore
    except ImportError as exc:
        raise CalibrationError(
            "Install pyautogui (python -m pip install -r scripts/windows-simulator/requirements.txt)"
        ) from exc
    position = pyautogui.position()
    return int(position[0]), int(position[1])


def capture_interactively(
    labels: Iterable[str],
    rect: WindowRect,
    input_stream: Any | None = None,
    output_stream: Any | None = None,
) -> dict[str, tuple[int, int]]:
    """Prompt for each label and capture the current mouse position.

    The function is kept separate from platform discovery so it can be tested
    without a desktop session by supplying a fake ``mouse_position``.
    """

    input_stream = sys.stdin if input_stream is None else input_stream
    output_stream = sys.stdout if output_stream is None else output_stream
    points: dict[str, tuple[int, int]] = {}
    print(
        f"Window: left={rect.left}, top={rect.top}, width={rect.width}, height={rect.height}",
        file=output_stream,
    )
    for label in labels:
        env_name = LABEL_ENV[label]
        print(
            f"Move the mouse to {label} ({env_name}), then press Enter "
            "(no click will be sent).",
            file=output_stream,
            flush=True,
        )
        input_stream.readline()
        absolute = mouse_position()
        points[label] = relative_point(rect, absolute)
        print(
            f"  screen=({absolute[0]},{absolute[1]}) -> {env_name}={point_text(points[label])}",
            file=output_stream,
            flush=True,
        )
    return points


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--title-regex",
        default=os.getenv("WECHAT_SIM_WINDOW_TITLE_REGEX", ""),
        help="visible target-window title regex (also reads WECHAT_SIM_WINDOW_TITLE_REGEX)",
    )
    parser.add_argument(
        "--target-app",
        default=os.getenv("WECHAT_SIM_TARGET_APP", ""),
        help="macOS owner/app name filter (also reads WECHAT_SIM_TARGET_APP)",
    )
    parser.add_argument(
        "--flow",
        choices=tuple(FLOW_LABELS),
        default="account_to_article",
        help="navigation flow to calibrate",
    )
    parser.add_argument(
        "--mode",
        choices=("direct", "shortcut", "current", "share", "menu"),
        default="direct",
        help="direct/shortcut=address bar; current=Ctrl/Cmd+C; menu=WeChat ⋯→复制链接; share=legacy alias",
    )
    parser.add_argument(
        "--format",
        choices=("bash", "powershell", "dotenv", "json", "yaml"),
        default="bash" if os.name != "nt" else "powershell",
        help="format for the final copy/paste configuration",
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=float(os.getenv("WECHAT_SIM_WINDOW_TIMEOUT_SEC", "20")),
        help="seconds to wait for the target window",
    )
    parser.add_argument(
        "--output",
        help="optional file to write the final configuration; stdout always receives it",
    )
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    title_regex = args.title_regex.strip()
    if not title_regex or title_regex == ".*":
        print(
            "Set --title-regex (or WECHAT_SIM_WINDOW_TITLE_REGEX) to a specific browser/WeChat window",
            file=sys.stderr,
        )
        return 2
    try:
        labels = labels_for(args.flow, args.mode)
        window = wait_for_window(title_regex, args.target_app, args.timeout)
        rect = window.rectangle()
        points = capture_interactively(labels, rect)
        rendered = yaml_lines(points, args.flow, args.mode) if args.format == "yaml" else env_lines(points, args.format)
        print("\n# Copy the following into the same shell that starts bridge:")
        print(rendered)
        config_path = args.output or "configs/config.yaml"
        if args.format == "yaml" and os.path.isfile(config_path):
            written = update_config_file(config_path, simulator_updates(points, rect))
            print(f"\nUpdated {config_path}")
            for line in written:
                print(f"  {line}")
        elif args.output:
            with open(args.output, "w", encoding="utf-8") as stream:
                stream.write(rendered)
                stream.write("\n")
            print(f"\nWrote {args.output}")
        return 0
    except (CalibrationError, KeyboardInterrupt) as exc:
        print(f"Calibration failed: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())

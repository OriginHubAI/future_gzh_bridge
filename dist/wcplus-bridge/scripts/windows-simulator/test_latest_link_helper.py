#!/usr/bin/env python3
"""Pure-Python regression test for the helper's Windows branch.

The project is developed on macOS, so this test supplies tiny stand-ins for
pyautogui, pyperclip and pywinauto and executes the real helper with
``sys.platform == 'win32'``.  It checks the protocol and coordinate handling;
it does not claim that a real Windows WeChat window has been accepted.
"""

from __future__ import annotations

import importlib.util
import io
import json
import os
import sys
import types
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import patch


HELPER_PATH = Path(__file__).with_name("latest_link_helper.py")


class WindowsBranchTest(unittest.TestCase):
    def test_article_enters_account_then_copies_url(self) -> None:
        sentinel = "WCPLUS_TEST_SENTINEL"
        article = "https://mp.weixin.qq.com/s/windows-branch-test"
        clipboard = {"value": sentinel}
        actions: list[tuple[str, object]] = []

        pyautogui = types.ModuleType("pyautogui")

        def click(x: int, y: int) -> None:
            actions.append(("click", (x, y)))

        def hotkey(*keys: str) -> None:
            actions.append(("hotkey", keys))
            if keys == ("ctrl", "c"):
                clipboard["value"] = article

        pyautogui.click = click  # type: ignore[attr-defined]
        pyautogui.hotkey = hotkey  # type: ignore[attr-defined]

        pyperclip = types.ModuleType("pyperclip")
        pyperclip.paste = lambda: clipboard["value"]  # type: ignore[attr-defined]
        pyperclip.copy = lambda value: clipboard.__setitem__("value", value)  # type: ignore[attr-defined]

        class FakeWindow:
            def set_focus(self) -> None:
                actions.append(("focus", None))

            def rectangle(self) -> types.SimpleNamespace:
                return types.SimpleNamespace(
                    left=lambda: 100,
                    top=lambda: 200,
                    right=lambda: 1300,
                    bottom=lambda: 1000,
                )

            def window_text(self) -> str:
                return "WeChat article"

        class FakeDesktop:
            def __init__(self, backend: str) -> None:
                self.backend = backend

            def windows(self, **kwargs: object) -> list[FakeWindow]:
                self.kwargs = kwargs
                return [FakeWindow()]

        pywinauto = types.ModuleType("pywinauto")
        pywinauto.Desktop = FakeDesktop  # type: ignore[attr-defined]

        # Load the helper as a module so its subprocess/time calls can be
        # patched without changing the production script.
        spec = importlib.util.spec_from_file_location("latest_link_helper_test", HELPER_PATH)
        self.assertIsNotNone(spec)
        assert spec is not None and spec.loader is not None
        helper = importlib.util.module_from_spec(spec)

        env = {
            "WECHAT_SIM_WINDOW_TITLE_REGEX": "WeChat.*",
            "WECHAT_SIM_ARTICLE_ACCOUNT_POINT": "10,20",
            "WECHAT_SIM_ACCOUNT_FIRST_ARTICLE_POINT": "30,40",
            "WECHAT_SIM_WAIT_SEC": "0",
            "WECHAT_SIM_WINDOW_TIMEOUT_SEC": "1",
            "WECHAT_SIM_LAUNCH_COMMAND": "true {initial_link}",
            "WECHAT_SIM_CLOSE_HOTKEY": "ctrl,w",
        }
        request = json.dumps(
            {
                "id": "windows-test",
                "initialLink": "https://mp.weixin.qq.com/s/source-article",
            }
        )
        output = io.StringIO()

        with patch.dict(
            sys.modules,
            {
                "pyautogui": pyautogui,
                "pyperclip": pyperclip,
                "pywinauto": pywinauto,
                spec.name: helper,
            },
        ):
            spec.loader.exec_module(helper)
            with patch.dict(os.environ, env, clear=False):
                with patch.object(sys, "platform", "win32"):
                    with patch.object(helper.subprocess, "Popen"):
                        with patch.object(helper.time, "sleep"):
                            with patch.object(sys, "stdin", io.StringIO(request)):
                                with redirect_stdout(output):
                                    self.assertEqual(helper.main(), 0)

        response = json.loads(output.getvalue())
        self.assertEqual(
            response,
            {"ok": True, "articleLink": article, "sentToFileTransfer": False},
        )
        self.assertEqual(clipboard["value"], sentinel)
        # Calibration points are relative to the outer target-window origin;
        # a pane offset is only applied when explicitly configured.
        self.assertIn(("click", (110, 220)), actions)
        self.assertIn(("click", (130, 240)), actions)
        self.assertIn(("hotkey", ("ctrl", "l")), actions)
        self.assertIn(("hotkey", ("ctrl", "c")), actions)
        self.assertIn(("hotkey", ("ctrl", "w")), actions)

    def test_windows_adapter_normalizes_callable_rect_and_focus_signature(self) -> None:
        spec = importlib.util.spec_from_file_location("latest_windows_adapter_test", HELPER_PATH)
        self.assertIsNotNone(spec)
        assert spec is not None and spec.loader is not None
        helper = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = helper
        spec.loader.exec_module(helper)

        actions: list[str] = []

        class RawWindow:
            def rectangle(self) -> types.SimpleNamespace:
                return types.SimpleNamespace(
                    left=lambda: 10,
                    top=lambda: 20,
                    right=lambda: 650,
                    bottom=lambda: 500,
                )

            def set_focus(self) -> None:
                actions.append("focus")

            def window_text(self) -> str:
                return "WeChat article"

        window = helper.WindowsWindow(RawWindow())
        rect = window.rectangle()
        self.assertEqual((rect.left, rect.top, rect.width, rect.height), (10, 20, 640, 480))
        window.set_focus(object())
        self.assertEqual(actions, ["focus"])

    def test_windows_adapter_rejects_ambiguous_matches(self) -> None:
        spec = importlib.util.spec_from_file_location("latest_windows_ambiguity_test", HELPER_PATH)
        self.assertIsNotNone(spec)
        assert spec is not None and spec.loader is not None
        helper = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = helper
        spec.loader.exec_module(helper)

        class RawWindow:
            def __init__(self, title: str) -> None:
                self.title = title

            def rectangle(self) -> types.SimpleNamespace:
                return types.SimpleNamespace(left=0, top=0, width=1200, height=800)

            def set_focus(self) -> None:
                return None

            def window_text(self) -> str:
                return self.title

        class FakeDesktop:
            def __init__(self, backend: str) -> None:
                self.backend = backend

            def windows(self, **kwargs: object) -> list[RawWindow]:
                return [RawWindow("WeChat article 1"), RawWindow("WeChat article 2")]

        pywinauto = types.ModuleType("pywinauto")
        pywinauto.Desktop = FakeDesktop  # type: ignore[attr-defined]
        with patch.dict(sys.modules, {"pywinauto": pywinauto}):
            with patch.object(sys, "platform", "win32"):
                with self.assertRaises(helper.AutomationError) as context:
                    helper.find_windows_window("WeChat.*")
        self.assertEqual(context.exception.code, "ambiguous_window")
        self.assertIn("narrow WECHAT_SIM_WINDOW_TITLE_REGEX", context.exception.message)

    def test_windows_reuse_mode_excludes_chat_shell_and_file_transfer(self) -> None:
        spec = importlib.util.spec_from_file_location("latest_windows_article_pick_test", HELPER_PATH)
        self.assertIsNotNone(spec)
        assert spec is not None and spec.loader is not None
        helper = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = helper
        spec.loader.exec_module(helper)

        focused: list[str] = []

        class RawWindow:
            def __init__(self, title: str, width: int, height: int) -> None:
                self.title = title
                self.width = width
                self.height = height

            def rectangle(self) -> types.SimpleNamespace:
                return types.SimpleNamespace(left=10, top=20, right=10 + self.width, bottom=20 + self.height)

            def set_focus(self) -> None:
                focused.append(self.title)

            def window_text(self) -> str:
                return self.title

        class FakeDesktop:
            def __init__(self, backend: str) -> None:
                self.backend = backend

            def windows(self, **kwargs: object) -> list[RawWindow]:
                return [
                    RawWindow("微信", 880, 640),
                    RawWindow("文件传输助手", 880, 640),
                    RawWindow("微信 (窗口)", 708, 707),
                ]

        pywinauto = types.ModuleType("pywinauto")
        pywinauto.Desktop = FakeDesktop  # type: ignore[attr-defined]
        with patch.dict(sys.modules, {"pywinauto": pywinauto}):
            with patch.object(sys, "platform", "win32"):
                selected = helper.find_current_article_window("微信.*", "微信", object())
        self.assertEqual(selected.title, "微信 (窗口)")
        self.assertEqual(focused, ["微信 (窗口)"])

    def test_profile_link_keeps_legacy_account_flow(self) -> None:
        # The profile-link path remains supported for callers that already
        # resolve the official-account page before invoking the helper.
        spec = importlib.util.spec_from_file_location("latest_link_helper_flow_test", HELPER_PATH)
        self.assertIsNotNone(spec)
        assert spec is not None and spec.loader is not None
        helper = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = helper
        spec.loader.exec_module(helper)
        self.assertEqual(
            helper.resolve_navigation_flow(
                "https://mp.weixin.qq.com/mp/profile_ext?action=home&__biz=MzA=="
            ),
            "account_to_article",
        )

    def test_four_step_flow_requires_an_article_seed(self) -> None:
        spec = importlib.util.spec_from_file_location("latest_four_step_flow_test", HELPER_PATH)
        self.assertIsNotNone(spec)
        assert spec is not None and spec.loader is not None
        helper = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = helper
        spec.loader.exec_module(helper)
        with patch.dict(os.environ, {"WECHAT_SIM_FLOW": "four_step"}, clear=False):
            self.assertEqual(
                helper.resolve_navigation_flow("https://mp.weixin.qq.com/s/source-article"),
                "article_to_account",
            )
            with self.assertRaises(helper.AutomationError) as context:
                helper.resolve_navigation_flow(
                    "https://mp.weixin.qq.com/mp/profile_ext?action=home&__biz=MzA=="
                )
        self.assertEqual(context.exception.code, "invalid_four_step_seed")

    def test_file_transfer_search_is_not_a_supported_navigation_flow(self) -> None:
        spec = importlib.util.spec_from_file_location("latest_removed_file_transfer_flow_test", HELPER_PATH)
        self.assertIsNotNone(spec)
        assert spec is not None and spec.loader is not None
        helper = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = helper
        spec.loader.exec_module(helper)
        with patch.dict(os.environ, {"WECHAT_SIM_FLOW": "file_transfer"}, clear=False):
            with self.assertRaises(helper.AutomationError) as context:
                helper.resolve_navigation_flow("https://mp.weixin.qq.com/s/source-article")
        self.assertEqual(context.exception.code, "invalid_flow")

    def test_clipboard_validation_rejects_profile_and_foreign_urls(self) -> None:
        spec = importlib.util.spec_from_file_location("latest_article_link_validation_test", HELPER_PATH)
        self.assertIsNotNone(spec)
        assert spec is not None and spec.loader is not None
        helper = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = helper
        spec.loader.exec_module(helper)
        self.assertTrue(helper.valid_article_url("https://mp.weixin.qq.com/s/article-id"))
        self.assertFalse(helper.valid_article_url("https://mp.weixin.qq.com/mp/profile_ext?action=home"))
        self.assertFalse(helper.valid_article_url("https://example.test/s/article-id"))
        helper.validate_seed_initial_link("https://mp.weixin.qq.com/article/abc")
        self.assertEqual(
            helper.resolve_navigation_flow(
                "https://mp.weixin.qq.com/s?__biz=MzA==&mid=1&idx=1"
            ),
            "article_to_account",
        )

    def test_current_mode_copies_native_article_url_without_address_bar(self) -> None:
        spec = importlib.util.spec_from_file_location("latest_current_copy_test", HELPER_PATH)
        self.assertIsNotNone(spec)
        assert spec is not None and spec.loader is not None
        helper = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = helper
        spec.loader.exec_module(helper)

        clipboard = {"value": "stale"}
        actions: list[tuple[str, object]] = []
        pyautogui = types.SimpleNamespace(
            hotkey=lambda *keys: (actions.append(("hotkey", keys)), clipboard.__setitem__(
                "value", "https://mp.weixin.qq.com/s/native-copy-test"
            )),
        )
        pyperclip = types.SimpleNamespace(
            copy=lambda value: clipboard.__setitem__("value", value),
            paste=lambda: clipboard["value"],
        )
        window = types.SimpleNamespace(set_focus=lambda _pyautogui: actions.append(("focus", None)))

        got = helper.copy_current_article_link(
            pyautogui,
            pyperclip,
            window,
            ["ctrl", "c"],
            1,
        )
        self.assertEqual(got, "https://mp.weixin.qq.com/s/native-copy-test")
        self.assertEqual(actions, [("focus", None), ("hotkey", ("ctrl", "c"))])

    def test_clipboard_mode_reads_existing_article_url_without_ui(self) -> None:
        spec = importlib.util.spec_from_file_location("latest_clipboard_mode_test", HELPER_PATH)
        self.assertIsNotNone(spec)
        assert spec is not None and spec.loader is not None
        helper = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = helper
        spec.loader.exec_module(helper)

        article = "https://mp.weixin.qq.com/s/already-copied-link"
        pyperclip = types.SimpleNamespace(copy=lambda _value: None, paste=lambda: article)
        pyautogui = types.SimpleNamespace()
        request = json.dumps({"id": "clipboard-test", "initialLink": article})
        output = io.StringIO()
        env = {
            "WECHAT_SIM_LINK_MODE": "clipboard",
            "WECHAT_SIM_CLIPBOARD_TIMEOUT_SEC": "1",
        }
        with patch.object(helper, "require_automation_modules", return_value=(pyautogui, pyperclip)):
            with patch.dict(os.environ, env, clear=False):
                with patch.object(sys, "stdin", io.StringIO(request)):
                    with redirect_stdout(output):
                        self.assertEqual(helper.main(), 0)

        self.assertEqual(
            json.loads(output.getvalue()),
            {"ok": True, "articleLink": article, "sentToFileTransfer": False},
        )

    def test_file_transfer_flow_sends_seed_link_before_clicking_message(self) -> None:
        """The file-transfer path pastes the supplied seed before opening it."""
        spec = importlib.util.spec_from_file_location("latest_file_transfer_flow_test", HELPER_PATH)
        self.assertIsNotNone(spec)
        assert spec is not None and spec.loader is not None
        helper = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = helper
        spec.loader.exec_module(helper)

        class Window:
            title = "微信"

            def rectangle(self) -> types.SimpleNamespace:
                return types.SimpleNamespace(left=10, top=20, width=920, height=789)

            def set_focus(self, _pyautogui: object | None = None) -> None:
                return None

        shell = Window()
        article = types.SimpleNamespace(
            title="测试文章",
            rectangle=lambda: types.SimpleNamespace(left=10, top=20, width=600, height=700),
        )
        actions: list[tuple[str, object]] = []
        pyautogui = types.SimpleNamespace(
            hotkey=lambda *keys: actions.append(("hotkey", keys)),
            press=lambda key: actions.append(("press", key)),
        )
        pyperclip = types.SimpleNamespace(
            copy=lambda value: actions.append(("copy", value)),
        )
        sent: list[tuple[str, object]] = []

        def fake_send(*args: object, **kwargs: object) -> None:
            sent.append((str(args[2]), kwargs.get("window")))

        with patch.dict(
            os.environ,
            {"WECHAT_SIM_FILE_TRANSFER_LINK_POINT": "300,400"},
            clear=True,
        ):
            with patch.object(helper, "list_mac_windows", return_value=[shell]):
                with patch.object(helper.subprocess, "run"):
                    with patch.object(helper.time, "sleep"):
                        with patch.object(helper, "send_link_to_file_transfer", side_effect=fake_send):
                            with patch.object(helper, "ui_click", side_effect=lambda *_args: actions.append(("click", _args[1:]))):
                                with patch.object(helper, "wait_for_titled_article", return_value=article):
                                    got = helper.open_article_from_file_transfer(
                                        pyautogui,
                                        pyperclip,
                                        "微信.*",
                                        "微信",
                                        0,
                                        1,
                                        "https://mp.weixin.qq.com/s/seed-link",
                                    )

        self.assertIs(got, article)
        self.assertEqual(sent, [("https://mp.weixin.qq.com/s/seed-link", shell)])
        self.assertTrue(any(kind == "click" for kind, _ in actions))

    def test_file_transfer_send_only_requires_input_point_for_selected_chat(self) -> None:
        spec = importlib.util.spec_from_file_location("latest_file_transfer_send_test", HELPER_PATH)
        self.assertIsNotNone(spec)
        assert spec is not None and spec.loader is not None
        helper = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = helper
        spec.loader.exec_module(helper)

        class Window:
            def set_focus(self, _pyautogui: object | None = None) -> None:
                return None

            def rectangle(self) -> types.SimpleNamespace:
                return types.SimpleNamespace(left=100, top=200, width=920, height=789)

        actions: list[tuple[str, object]] = []
        clipboard = {"value": ""}
        pyautogui = types.SimpleNamespace(
            hotkey=lambda *keys: actions.append(("hotkey", keys)),
            press=lambda key: actions.append(("press", key)),
        )
        pyperclip = types.SimpleNamespace(
            copy=lambda value: clipboard.__setitem__("value", value),
        )
        with patch.dict(
            os.environ,
            {
                "WECHAT_SIM_FILE_TRANSFER_INPUT_POINT": "520,700",
                "WECHAT_SIM_SEND_PASTE_HOTKEY": "command,v",
                "WECHAT_SIM_SEND_HOTKEY": "enter",
            },
            clear=True,
        ):
            with patch.object(helper, "ui_click", side_effect=lambda *_args: actions.append(("click", _args[1:]))):
                helper.send_link_to_file_transfer(
                    pyautogui,
                    pyperclip,
                    "https://mp.weixin.qq.com/s/seed-link",
                    0,
                    1,
                    window=Window(),
                )

        self.assertEqual(clipboard["value"], "https://mp.weixin.qq.com/s/seed-link")
        self.assertIn(("click", (620, 900)), actions)
        self.assertIn(("hotkey", ("command", "v")), actions)
        self.assertIn(("press", "enter"), actions)



class WeChatLoginStatusTest(unittest.TestCase):
    def test_classifies_process_window_and_login_screen(self) -> None:
        helper = self._load()
        self.assertEqual(helper.classify_wechat_login(False, [])["status"], "logged_out")
        logged_in = helper.classify_wechat_login(True, [("微信", 880, 640), ("微信 (窗口)", 920, 789)])
        self.assertEqual(logged_in["status"], "logged_in")
        self.assertTrue(logged_in["loggedIn"])
        self.assertEqual(logged_in["windowTitle"], "微信")
        session_only = helper.classify_wechat_login(True, [("微信 (窗口)", 920, 789)])
        self.assertEqual(session_only["status"], "logged_in")
        self.assertEqual(session_only["windowTitle"], "微信 (窗口)")
        small_shell = helper.classify_wechat_login(True, [("微信", 380, 520)])
        self.assertEqual(small_shell["status"], "logged_out")
        self.assertFalse(small_shell["loggedIn"])
        qr = helper.classify_wechat_login(True, [("扫码登录", 380, 520)])
        self.assertEqual(qr["status"], "logged_out")
        article_only = helper.classify_wechat_login(True, [("滴滴出行", 920, 789)])
        self.assertEqual(article_only["status"], "logged_out")
        empty = helper.classify_wechat_login(True, [("", 880, 640)])
        self.assertEqual(empty["status"], "logged_out")

    def test_status_command_prints_json_without_reading_stdin(self) -> None:
        helper = self._load()
        with patch.object(helper, "read_mac_wechat_windows", return_value=(True, [("微信", 880, 640)])):
            with patch.object(sys, "argv", ["latest_link_helper.py", "--wechat-status"]):
                with patch.object(helper, "sys", helper.sys):
                    stdout = io.StringIO()
                    with redirect_stdout(stdout):
                        code = helper.main()
        self.assertEqual(code, 0)
        payload = json.loads(stdout.getvalue())
        self.assertTrue(payload["ok"])
        self.assertEqual(payload["status"], "logged_in")
        self.assertEqual(payload["windowTitle"], "微信")
        self.assertEqual(payload["message"], "微信已登录")

    def _load(self):
        spec = importlib.util.spec_from_file_location("latest_link_helper_status_test", HELPER_PATH)
        self.assertIsNotNone(spec)
        assert spec is not None and spec.loader is not None
        module = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = module
        spec.loader.exec_module(module)
        return module



class WindowsWeChatLoginStatusTest(unittest.TestCase):
    """Drive the real Windows reader with a stand-in desktop and task list."""

    def test_visible_wechat_shell_is_logged_in_without_tasklist(self) -> None:
        payload = self._status([self._window("微信", 880, 640)], wechat_exe=False)
        self.assertEqual(payload["status"], "logged_in")
        self.assertTrue(payload["loggedIn"])
        self.assertEqual(payload["windowTitle"], "微信")
        self.assertEqual(self.tasklist_calls, [])

    def test_session_window_title_is_logged_in(self) -> None:
        payload = self._status([self._window("微信 (窗口)", 920, 789)])
        self.assertEqual(payload["status"], "logged_in")
        self.assertEqual(payload["windowTitle"], "微信 (窗口)")

    def test_small_wechat_window_requires_login(self) -> None:
        payload = self._status([self._window("微信", 380, 520)])
        self.assertEqual(payload["status"], "logged_out")
        self.assertFalse(payload["loggedIn"])

    def test_scan_title_requires_login(self) -> None:
        payload = self._status([self._window("扫码登录", 380, 520)])
        self.assertEqual(payload["status"], "logged_out")

    def test_unrelated_window_and_running_process_is_unknown(self) -> None:
        payload = self._status(
            [self._window("Microsoft Edge", 1400, 900)],
            wechat_exe=False,
            weixin_exe=True,
        )
        self.assertEqual(payload["status"], "logged_out")
        self.assertIn("WeChat.exe", self.tasklist_calls[0])
        self.assertIn("Weixin.exe", self.tasklist_calls[1])

    def test_no_process_is_not_running(self) -> None:
        payload = self._status([self._window("Microsoft Edge", 1400, 900)], wechat_exe=False, weixin_exe=False)
        self.assertEqual(payload["status"], "logged_out")
        self.assertFalse(payload["loggedIn"])

    def test_status_command_uses_windows_reader(self) -> None:
        helper = self._load()
        desktop = self._desktop([self._window("WeChat", 960, 700)])
        pywinauto = types.ModuleType("pywinauto")
        pywinauto.Desktop = desktop  # type: ignore[attr-defined]
        stdout = io.StringIO()
        with patch.dict(sys.modules, {"pywinauto": pywinauto}):
            with patch.object(sys, "platform", "win32"):
                with patch.object(sys, "argv", ["latest_link_helper.py", "--wechat-status"]):
                    with redirect_stdout(stdout):
                        code = helper.main()
        self.assertEqual(code, 0)
        payload = json.loads(stdout.getvalue())
        self.assertTrue(payload["ok"])
        self.assertEqual(payload["status"], "logged_in")
        self.assertEqual(payload["windowTitle"], "WeChat")

    def _status(self, windows: list[object], wechat_exe: bool = True, weixin_exe: bool = False) -> dict:
        helper = self._load()
        desktop = self._desktop(windows)
        pywinauto = types.ModuleType("pywinauto")
        pywinauto.Desktop = desktop  # type: ignore[attr-defined]
        self.tasklist_calls = []

        def fake_run(args, **_kwargs):
            command = " ".join(args) if isinstance(args, list) else str(args)
            self.tasklist_calls.append(command)
            image = "Weixin.exe" if "Weixin.exe" in command else "WeChat.exe"
            present = weixin_exe if image == "Weixin.exe" else wechat_exe
            stdout = f"{image}                     4242 Console                    1     100,000 K" if present else "INFO: No tasks are running matching the specified criteria."
            return types.SimpleNamespace(returncode=0, stdout=stdout, stderr="")

        with patch.dict(sys.modules, {"pywinauto": pywinauto}):
            with patch.object(sys, "platform", "win32"):
                with patch.object(helper.subprocess, "run", side_effect=fake_run):
                    return helper.wechat_login_status()

    def _desktop(self, windows: list[object]):
        class FakeDesktop:
            def __init__(self, backend: str) -> None:
                if backend != "uia":
                    raise AssertionError(backend)

            def windows(self, **kwargs: object) -> list[object]:
                if kwargs.get("visible_only") is not True:
                    raise AssertionError(kwargs)
                return list(windows)

        return FakeDesktop

    def _window(self, title: str, width: int, height: int) -> object:
        class RawWindow:
            def window_text(self) -> str:
                return title

            def rectangle(self) -> types.SimpleNamespace:
                return types.SimpleNamespace(
                    left=lambda: 80,
                    top=lambda: 60,
                    right=lambda: 80 + width,
                    bottom=lambda: 60 + height,
                )

        return RawWindow()

    def _load(self):
        spec = importlib.util.spec_from_file_location("latest_link_helper_windows_status_test", HELPER_PATH)
        self.assertIsNotNone(spec)
        assert spec is not None and spec.loader is not None
        module = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = module
        spec.loader.exec_module(module)
        return module


class VisionMatchTest(unittest.TestCase):
    def _load(self):
        spec = importlib.util.spec_from_file_location("latest_link_helper_vision_test", HELPER_PATH)
        self.assertIsNotNone(spec)
        assert spec is not None and spec.loader is not None
        module = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = module
        spec.loader.exec_module(module)
        return module

    def _blank(self, width: int, height: int) -> list[tuple[int, int, int]]:
        return [(255, 255, 255)] * (width * height)

    def test_yellow_avatar_clicks_the_name_to_its_right(self) -> None:
        helper = self._load()
        width, height = 200, 120
        rgb = self._blank(width, height)
        for y in range(28, 44):
            for x in range(30, 46):
                rgb[y * width + x] = (230, 190, 40)
        hit = helper.find_account_click(rgb, width, height)
        self.assertIsNotNone(hit)
        assert hit is not None
        self.assertGreater(hit[0], 46)
        self.assertGreater(hit[1], 20)
        self.assertLess(hit[1], 55)

    def test_plain_page_does_not_invent_an_account_click(self) -> None:
        helper = self._load()
        self.assertIsNone(helper.find_account_click(self._blank(200, 120), 200, 120))

    def test_more_menu_is_the_dark_cluster_at_the_top_right(self) -> None:
        helper = self._load()
        width, height = 200, 120
        rgb = self._blank(width, height)
        for y in range(4, 8):
            for x in range(184, 192):
                rgb[y * width + x] = (30, 30, 30)
        hit = helper.find_more_menu_click(rgb, width, height)
        self.assertIsNotNone(hit)
        assert hit is not None
        self.assertGreater(hit[0], 180)
        self.assertLess(hit[1], 12)

    def test_first_article_is_the_cover_card_below_the_profile(self) -> None:
        helper = self._load()
        width, height = 200, 160
        rgb = self._blank(width, height)
        # A one-line label in the header must not win over the article card.
        for x in range(50, 130):
            rgb[70 * width + x] = (20, 20, 20)
        for y in range(96, 130):
            for x in range(30, 170):
                rgb[y * width + x] = (30, 70, 140)
        hit = helper.find_first_article_click(rgb, width, height)
        self.assertIsNotNone(hit)
        assert hit is not None
        self.assertGreater(hit[0], 40)
        self.assertLess(hit[0], 160)
        self.assertGreater(hit[1], 90)
        self.assertLess(hit[1], 130)

    def test_byline_name_is_preferred_over_the_bottom_avatar(self) -> None:
        helper = self._load()
        width, height = 200, 160
        rgb = self._blank(width, height)
        for y in range(22, 30):
            for x in range(24, 70):
                rgb[y * width + x] = (87, 107, 149)
        for y in range(130, 146):
            for x in range(20, 36):
                rgb[y * width + x] = (24, 24, 24)
        hit = helper.find_account_click(rgb, width, height)
        self.assertIsNotNone(hit)
        assert hit is not None
        self.assertGreater(hit[0], 20)
        self.assertLess(hit[0], 80)
        self.assertGreater(hit[1], 18)
        self.assertLess(hit[1], 40)

    def test_deleted_post_veil_is_skipped_for_the_next_article(self) -> None:
        helper = self._load()
        width, height = 200, 220
        rgb = self._blank(width, height)
        for y in range(100, 140):
            for x in range(30, 170):
                rgb[y * width + x] = (214, 214, 216)
        for y in range(160, 185):
            for x in range(30, 140):
                rgb[y * width + x] = (20, 20, 20)
        hit = helper.find_first_article_click(rgb, width, height)
        self.assertIsNotNone(hit)
        assert hit is not None
        self.assertGreater(hit[1], 150)

    def test_blue_avatar_clicks_the_name_to_its_right(self) -> None:
        helper = self._load()
        width, height = 200, 160
        rgb = self._blank(width, height)
        for y in range(100, 116):
            for x in range(40, 56):
                rgb[y * width + x] = (40, 90, 220)
        hit = helper.find_account_click(rgb, width, height)
        self.assertIsNotNone(hit)
        assert hit is not None
        self.assertGreater(hit[0], 56)
        self.assertGreater(hit[1], 90)
        self.assertLess(hit[1], 130)

    def test_dark_avatar_clicks_the_name_to_its_right(self) -> None:
        helper = self._load()
        width, height = 200, 160
        rgb = self._blank(width, height)
        for y in range(120, 136):
            for x in range(36, 52):
                rgb[y * width + x] = (24, 24, 24)
        hit = helper.find_account_click(rgb, width, height)
        self.assertIsNotNone(hit)
        assert hit is not None
        self.assertGreater(hit[0], 52)
        self.assertGreater(hit[1], 110)

    def test_blue_and_black_avatars_do_not_match(self) -> None:
        helper = self._load()
        self.assertTrue(helper.avatars_match((40, 90, 220), (48, 96, 210)))
        self.assertFalse(helper.avatars_match((40, 90, 220), (24, 24, 24)))

    def test_profile_header_is_not_an_article_cover(self) -> None:
        helper = self._load()
        width, height = 200, 160
        rgb = self._blank(width, height)
        for y in range(18, 48):
            for x in range(20, 50):
                rgb[y * width + x] = (30, 30, 30)
        self.assertTrue(helper.looks_like_account_profile(rgb, width, height))
        article = self._blank(width, height)
        for y in range(40, 110):
            for x in range(20, 180):
                article[y * width + x] = (20, 40, 90)
        self.assertFalse(helper.looks_like_account_profile(article, width, height))


if __name__ == "__main__":
    unittest.main()

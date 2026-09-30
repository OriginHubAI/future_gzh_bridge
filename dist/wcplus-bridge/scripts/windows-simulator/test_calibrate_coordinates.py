#!/usr/bin/env python3
"""Unit tests for the desktop coordinate calibration utility.

These tests are intentionally desktop-free.  Real window discovery remains a
small platform adapter around pywinauto/Quartz and is exercised on the target
Windows/macOS machine during operator calibration.
"""

from __future__ import annotations

import importlib.util
import io
import os
import sys
import tempfile
import types
import unittest
from pathlib import Path
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("calibrate_coordinates.py")
SPEC = importlib.util.spec_from_file_location("calibrate_coordinates_test_module", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
calibrator = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = calibrator
SPEC.loader.exec_module(calibrator)


class CalibrationUtilityTest(unittest.TestCase):
    def test_relative_point_uses_outer_window_origin(self) -> None:
        rect = calibrator.WindowRect(left=100, top=200, width=1200, height=800)
        self.assertEqual(calibrator.relative_point(rect, (360, 550)), (260, 350))

    def test_relative_point_rejects_point_outside_window(self) -> None:
        rect = calibrator.WindowRect(left=100, top=200, width=1200, height=800)
        with self.assertRaises(calibrator.CalibrationError):
            calibrator.relative_point(rect, (99, 550))

    def test_labels_and_shell_rendering_match_helper_names(self) -> None:
        labels = calibrator.labels_for("article_to_account", "share")
        self.assertEqual(
            labels,
            ("article_account", "account_first_article", "share", "copy_link"),
        )
        menu_labels = calibrator.labels_for("article_to_account", "menu")
        self.assertEqual(
            menu_labels,
            ("article_account", "account_first_article", "more_menu", "copy_link"),
        )
        points = {label: (index + 1, index + 2) for index, label in enumerate(labels)}
        output = calibrator.env_lines(points, "powershell")
        self.assertIn("$env:WECHAT_SIM_ARTICLE_ACCOUNT_POINT = '1,2'", output)
        self.assertIn("$env:WECHAT_SIM_COPY_LINK_POINT = '4,5'", output)

    def test_yaml_rendering_matches_bridge_simulator_fields(self) -> None:
        points = {"article_list": (260, 380), "first_article": (320, 470)}
        output = calibrator.yaml_lines(points, "account_to_article", "direct")
        self.assertIn('simulator:', output)
        self.assertIn('  flow: "account_to_article"', output)
        self.assertIn('  link_mode: "direct"', output)
        self.assertIn('  article_list_point: "260,380"', output)
        self.assertIn('  first_article_point: "320,470"', output)

    def test_update_config_file_replaces_points_and_keeps_other_lines(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = os.path.join(directory, "config.yaml")
            with open(path, "w", encoding="utf-8") as stream:
                stream.write(
                    "listen: \"127.0.0.1:19090\"\n"
                    "simulator:\n"
                    "  enabled: true\n"
                    "  article_account_point: \"\"\n"
                    "  coord_ref_size: \"\"\n"
                )
            written = calibrator.update_config_file(
                path,
                {"article_account_point": "10,20", "coord_ref_size": "823,707"},
            )
            with open(path, encoding="utf-8") as stream:
                text = stream.read()
        self.assertIn('listen: "127.0.0.1:19090"', text)
        self.assertIn("enabled: true", text)
        self.assertIn('article_account_point: "10,20"', text)
        self.assertIn('coord_ref_size: "823,707"', text)
        self.assertEqual(
            written,
            ["article_account_point=10,20", "coord_ref_size=823,707"],
        )

    def test_interactive_capture_records_relative_points_without_clicking(self) -> None:
        rect = calibrator.WindowRect(left=100, top=200, width=1200, height=800)
        fake_input = io.StringIO("\n\n")
        fake_output = io.StringIO()
        positions = iter(((360, 550), (420, 620)))
        with patch.object(calibrator, "mouse_position", side_effect=lambda: next(positions)):
            points = calibrator.capture_interactively(
                ("article_list", "first_article"),
                rect,
                input_stream=fake_input,
                output_stream=fake_output,
            )
        self.assertEqual(points, {"article_list": (260, 350), "first_article": (320, 420)})
        self.assertIn("no click will be sent", fake_output.getvalue())

    def test_rect_normalizes_quartz_bounds(self) -> None:
        rect = calibrator._rect_from_any({"X": 10, "Y": 20, "Width": 640, "Height": 480})
        self.assertEqual(rect, calibrator.WindowRect(10, 20, 640, 480))

    def test_rect_accepts_callable_window_members(self) -> None:
        rect = calibrator._rect_from_any(
            types.SimpleNamespace(
                left=lambda: 10,
                top=lambda: 20,
                right=lambda: 650,
                bottom=lambda: 500,
            )
        )
        self.assertEqual(rect, calibrator.WindowRect(10, 20, 640, 480))

    def test_windows_find_rejects_ambiguous_matches(self) -> None:
        class RawWindow:
            def rectangle(self) -> types.SimpleNamespace:
                return types.SimpleNamespace(left=0, top=0, width=1200, height=800)

        class FakeDesktop:
            def __init__(self, backend: str) -> None:
                self.backend = backend

            def windows(self, **kwargs: object) -> list[RawWindow]:
                return [RawWindow(), RawWindow()]

        pywinauto = types.ModuleType("pywinauto")
        pywinauto.Desktop = FakeDesktop  # type: ignore[attr-defined]
        with patch.dict(sys.modules, {"pywinauto": pywinauto}):
            with patch.object(sys, "platform", "win32"):
                with self.assertRaises(calibrator.CalibrationError) as context:
                    calibrator.find_window("WeChat.*")
        self.assertIn("narrow --title-regex", str(context.exception))


if __name__ == "__main__":
    unittest.main()

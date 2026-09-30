#!/usr/bin/env python3
"""Best-effort Mac Chrome calibration from live window geometry.

Interactive ``calibrate_coordinates.py`` needs an operator at the screen.
This script opens a sample page in Chrome, reads the outer window bounds via
Quartz, and derives relative click points for the account->article flow.
Re-run ``calibrate_coordinates.py`` if clicks still miss.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
import time
from pathlib import Path


def find_chrome_window(title_regex: str, target_app: str, timeout: float):
    from Quartz import CGWindowListCopyWindowInfo, kCGNullWindowID, kCGWindowListOptionOnScreenOnly

    pattern = re.compile(title_regex, re.I)
    target = target_app.strip().lower()
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        for info in CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly, kCGNullWindowID) or []:
            owner = str(info.get("kCGWindowOwnerName", ""))
            if target and owner.lower() != target:
                continue
            if int(info.get("kCGWindowLayer", 0)) != 0:
                continue
            bounds = info.get("kCGWindowBounds") or {}
            w, h = int(bounds.get("Width", 0)), int(bounds.get("Height", 0))
            if w < 400 or h < 300:
                continue
            title = str(info.get("kCGWindowName", ""))
            if pattern.search(f"{owner} {title}"):
                return int(bounds["X"]), int(bounds["Y"]), w, h, title
        time.sleep(0.3)
    return None


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--initial-link",
        default="https://mp.weixin.qq.com/mp/profile_ext?action=home&__biz=MjM5OTAwODk4MA==",
    )
    parser.add_argument("--title-regex", default="Google Chrome.*")
    parser.add_argument("--target-app", default="Google Chrome")
    parser.add_argument("--wait", type=float, default=6.0)
    parser.add_argument("--output", type=Path, default=Path("configs/calibrated.yaml"))
    args = parser.parse_args()

    subprocess.run(
        ["open", "-a", "Google Chrome", args.initial_link],
        check=False,
    )
    time.sleep(1.0)
    found = find_chrome_window(args.title_regex, args.target_app, args.wait)
    if not found:
        print("Chrome window not found; open Chrome manually and retry.", file=sys.stderr)
        return 1
    _x, _y, width, height, title = found
    # Heuristic for centered mobile-style MP layout inside Chrome.
    list_x = max(80, width // 2)
    list_y = max(120, int(height * 0.42))
    first_x = list_x
    first_y = max(list_y + 60, int(height * 0.52))
    fragment = "\n".join(
        [
            "simulator:",
            '  flow: "account_to_article"',
            '  link_mode: "direct"',
            f'  article_list_point: "{list_x},{list_y}"',
            f'  first_article_point: "{first_x},{first_y}"',
        ]
    )
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(fragment + "\n", encoding="utf-8")
    print(f"Window: {title!r} size={width}x{height}")
    print(f"article_list_point={list_x},{list_y}")
    print(f"first_article_point={first_x},{first_y}")
    print(f"Wrote {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

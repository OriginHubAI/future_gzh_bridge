#!/usr/bin/env python3
"""Heuristic Mac WeChat share-mode calibration (share + copy link points).

For precise control points, prefer interactive ``calibrate_coordinates.py
--mode share --flow article_to_account``.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
import time
from pathlib import Path


def find_wechat_window(title_regex: str, target_app: str, timeout: float):
    from Quartz import CGWindowListCopyWindowInfo, kCGNullWindowID, kCGWindowListOptionOnScreenOnly

    pattern = re.compile(title_regex, re.I)
    target = target_app.strip().lower()
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        for info in CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly, kCGNullWindowID) or []:
            owner = str(info.get("kCGWindowOwnerName", ""))
            if target and target not in owner.lower():
                continue
            if int(info.get("kCGWindowLayer", 0)) != 0:
                continue
            bounds = info.get("kCGWindowBounds") or {}
            w, h = int(bounds.get("Width", 0)), int(bounds.get("Height", 0))
            if w < 400 or h < 300:
                continue
            title = str(info.get("kCGWindowName", ""))
            if pattern.search(f"{owner} {title}"):
                return w, h, owner, title
        time.sleep(0.3)
    return None


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--initial-link",
        default="https://mp.weixin.qq.com/s/example",
        help="optional; WeChat may ignore URL when only calibrating an open window",
    )
    parser.add_argument("--title-regex", default="微信.*")
    parser.add_argument("--target-app", default="微信")
    parser.add_argument("--wait", type=float, default=8.0)
    parser.add_argument("--output", type=Path, default=Path("configs/calibrated.yaml"))
    args = parser.parse_args()

    if args.initial_link.strip():
        subprocess.run(["open", "-a", "WeChat", args.initial_link], check=False)
        time.sleep(2.0)
    subprocess.run(["open", "-a", "WeChat"], check=False)
    time.sleep(1.0)

    found = find_wechat_window(args.title_regex, args.target_app, args.wait)
    if not found:
        print("WeChat window not found; open WeChat on an article page and retry.", file=sys.stderr)
        return 1
    width, height, owner, title = found
    more_x, more_y = max(40, width - 48), max(40, int(height * 0.066))
    copy_x, copy_y = max(40, width - 110), max(80, int(height * 0.137))
    account_x, account_y = max(60, int(width * 0.18)), max(80, int(height * 0.247))
    first_x, first_y = max(60, int(width * 0.50)), max(120, int(height * 0.336))

    fragment = "\n".join(
        [
            "simulator:",
            '  launch_command: \'open -a WeChat {initial_link}\'',
            '  mac_app: "WeChat"',
            '  target_app: "微信"',
            '  window_title_regex: "微信.*"',
            '  flow: "article_to_account"',
            '  link_mode: "menu"',
            '  skip_article_nav: false',
            f'  article_account_point: "{account_x},{account_y}"',
            f'  account_first_article_point: "{first_x},{first_y}"',
            f'  more_menu_point: "{more_x},{more_y}"',
            f'  copy_link_point: "{copy_x},{copy_y}"',
        ]
    )
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(fragment + "\n", encoding="utf-8")
    print(f"Window: owner={owner!r} title={title!r} size={width}x{height}")
    print(fragment)
    print(f"Wrote {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

"""Desktop WeChat article automation."""

import logging
import pathlib
import re
import sys
import time

from PIL import Image

from discern_tools import same_pos_picture
from find_tools import find_template_bottom_up
from identify_tools import anchor_identify
from keyword_tools import delay, get_clipboard_text, send_ctrl_a, send_ctrl_c, send_ctrl_v, send_enter, set_clipboard_text
from mouse_tools import click_screen, move_screen, scroll_up
from screen_tools import find_wechat_hwnd, get_window_scale, screenshot_by_hwnd, set_foreground_window, set_window_size_ex

logging.basicConfig(level=logging.WARN)
KEYWORD_DELAY_SECONDS = 2
WECHAT_INIT_POS = (1024, 800, -10, 0)
ANCHOR_POINT_ROOT = pathlib.Path(__file__).resolve().parent / "anchor_point"
ANCHOR_POINT_ROOT.mkdir(exist_ok=True)


def collect(keyword: str, copy_mode: str = "link") -> dict:
    """Run the calibrated UI flow and return the final clipboard value."""
    keyword = str(keyword or "").strip()
    if not keyword:
        raise ValueError("keyword is required")
    hwnd = find_wechat_hwnd()
    if not hwnd:
        raise RuntimeError("微信窗口不存在")
    scale = get_window_scale(hwnd=hwnd)
    set_foreground_window(hwnd)
    set_window_size_ex(hwnd, *WECHAT_INIT_POS)
    captured_avatar = ""

    def click_checked(click_pos, check_pos, try_img, tries=0):
        if same_pos_picture(screenshot_by_hwnd(hwnd), check_pos, ANCHOR_POINT_ROOT / try_img, catch_mode=False):
            return True
        click_screen(hwnd, *click_pos)
        time.sleep(1)
        if same_pos_picture(screenshot_by_hwnd(hwnd), check_pos, ANCHOR_POINT_ROOT / try_img, catch_mode=False):
            return True
        if tries < 3:
            time.sleep(1)
            return click_checked(click_pos, check_pos, try_img, tries + 1)
        raise RuntimeError("定位失败")

    def click_link_package(horizontal_x):
        nonlocal captured_avatar
        set_window_size_ex(hwnd, *WECHAT_INIT_POS)
        same_pos_picture(screenshot_by_hwnd(hwnd), (horizontal_x, 0, 40, 800), ANCHOR_POINT_ROOT / "large.png", catch_mode=True)
        avatar_pos, confidence = find_template_bottom_up(
            ANCHOR_POINT_ROOT / "large.png", ANCHOR_POINT_ROOT / "avatar.png", threshold=0.60
        )
        if avatar_pos is None:
            raise RuntimeError(f"avatar不存在，最高匹配度={confidence:.3f}")
        avatar_dir = pathlib.Path(__file__).resolve().parent / "data" / "simulator-images"
        avatar_dir.mkdir(parents=True, exist_ok=True)
        template_size = Image.open(ANCHOR_POINT_ROOT / "avatar.png").size
        large_image = Image.open(ANCHOR_POINT_ROOT / "large.png")
        avatar_file = avatar_dir / "avatar.png"
        large_image.crop((avatar_pos[0], avatar_pos[1], avatar_pos[0] + template_size[0], avatar_pos[1] + template_size[1])).save(avatar_file)
        captured_avatar = str(avatar_file)
        logging.debug("avatar_pos=%s confidence=%s", avatar_pos, confidence)
        delay(KEYWORD_DELAY_SECONDS)(click_screen)(hwnd, horizontal_x - 50, int((avatar_pos[1] + 20) / scale))

    anchor_identify.n = 0
    click_checked((130, 120), (300, 40, 120, 40), anchor_identify())
    click_checked((530, 700), (300, 740, 200, 40), anchor_identify())
    set_clipboard_text(keyword)
    delay(KEYWORD_DELAY_SECONDS)(send_ctrl_v)()
    delay(KEYWORD_DELAY_SECONDS)(send_enter)()
    try:
        click_link_package(940)
    except RuntimeError:
        click_link_package(576)
    delay(KEYWORD_DELAY_SECONDS)(set_window_size_ex)(hwnd, *WECHAT_INIT_POS)
    click_screen(hwnd, 710, 756)
    move_screen(hwnd, 821, 405)
    delay(KEYWORD_DELAY_SECONDS)(scroll_up)(100)
    click_screen(hwnd, 866, 593)
    delay(KEYWORD_DELAY_SECONDS)(click_screen)(hwnd, 916, 45)
    if copy_mode == "content":
        # Article page: select/copy the visible article text.
        delay(KEYWORD_DELAY_SECONDS)(click_screen)(hwnd, 820, 405)
        delay(0.2)(send_ctrl_a)()
        delay(0.2)(send_ctrl_c)()
    else:
        set_clipboard_text("")
        delay(KEYWORD_DELAY_SECONDS)(click_screen)(hwnd, 873, 158)
        delay(KEYWORD_DELAY_SECONDS)(click_screen)(hwnd, 873, 120)
    copied = delay(KEYWORD_DELAY_SECONDS)(get_clipboard_text)()
    if copy_mode == "both":
        if not is_article_link(copied):
            raise RuntimeError("未复制到有效文章链接")
        click_screen(hwnd, 820, 405)
        set_clipboard_text("")
        delay(0.2)(send_ctrl_a)()
        delay(0.2)(send_ctrl_c)()
        content = delay(KEYWORD_DELAY_SECONDS)(get_clipboard_text)()
        if not content or content == copied:
            raise RuntimeError("正文复制失败")
        return {"clipboard": copied, "content": content, "avatar": captured_avatar}
    return {"clipboard": copied, "avatar": captured_avatar}


def is_article_link(value: str) -> bool:
    from urllib.parse import urlsplit
    parsed = urlsplit(value.strip())
    return parsed.scheme in {"http", "https"} and parsed.hostname == "mp.weixin.qq.com" and (parsed.path in {"/s", "/article"} or parsed.path.startswith(("/s/", "/article/")))


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("用法: py main.py 公众号昵称或关键词")
    print(collect(sys.argv[1])["clipboard"])

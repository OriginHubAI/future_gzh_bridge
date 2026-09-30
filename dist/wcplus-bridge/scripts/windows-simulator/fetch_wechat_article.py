import base64
import datetime
import html
import json
import re
import sys
from urllib.parse import parse_qs, urlparse

import requests
from bs4 import BeautifulSoup


WECHAT_HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) "
        "AppleWebKit/605.1.15 (KHTML, like Gecko) "
        "Version/17.0 Mobile/15E148 Safari/604.1"
    ),
    "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
    "Accept-Language": "zh-CN,zh;q=0.9",
    "Referer": "https://mp.weixin.qq.com/",
}


def decode_biz(biz: str):
    """MzU5...== -> 数字账号 id"""
    if not biz:
        return None
    try:
        s = biz + "=" * (4 - len(biz) % 4)
        return base64.b64decode(s).decode("utf-8", errors="ignore")
    except Exception:
        return None


def _clean_js_string(raw: str) -> str:
    if not raw:
        return ""

    # 如果整体被引号包着，去掉
    if (raw.startswith('"') and raw.endswith('"')) or (raw.startswith("'") and raw.endswith("'")):
        raw = raw[1:-1]

    # 只替换 JS 常见转义，避免 codecs.decode 把正常中文再次错误编码。
    raw = re.sub(r"\\u([0-9a-fA-F]{4})", lambda m: chr(int(m.group(1), 16)), raw)
    raw = re.sub(r"\\x([0-9a-fA-F]{2})", lambda m: chr(int(m.group(1), 16)), raw)
    raw = raw.replace(r"\/", "/")
    return html.unescape(raw).strip()


def _match(patterns, text: str) -> str:
    for pattern in patterns:
        match = re.search(pattern, text, flags=re.IGNORECASE)
        if match:
            return _clean_js_string(match.group(1))
    return ""


def _meta(soup: BeautifulSoup, name: str) -> str:
    tag = soup.find("meta", attrs={"property": name})
    if tag is None:
        tag = soup.find("meta", attrs={"name": name})
    return html.unescape(str(tag.get("content", "")).strip()) if tag else ""


def fetch_wechat_article(short_url: str) -> dict:
    parsed_short = urlparse(short_url)
    if parsed_short.scheme not in {"http", "https"} or parsed_short.hostname != "mp.weixin.qq.com":
        raise ValueError("文章链接必须是 mp.weixin.qq.com")

    s = requests.Session()
    r = s.get(short_url, headers=WECHAT_HEADERS, allow_redirects=True, timeout=15)
    r.raise_for_status()

    # ✅ 让 requests 自己处理编码，不手动折腾
    r.encoding = "utf-8"
    html = r.text
    final_url = r.url
    final_host = urlparse(final_url).hostname
    if final_host != "mp.weixin.qq.com":
        raise ValueError("文章跳转到了非微信页面")
    soup = BeautifulSoup(html, "lxml")

    # ---------- 1. 从最终 URL 拿 __biz / mid / idx / sn ----------
    q = parse_qs(urlparse(final_url).query)
    biz = (q.get("__biz") or [None])[0]
    mid = (q.get("mid") or [None])[0]
    idx = (q.get("idx") or [None])[0]
    sn = (q.get("sn") or [None])[0]

    # ---------- 2. URL 没有就从 JS 变量里抠（正则收紧） ----------
    # biz
    if not biz:
        biz = _match(
            [
                r'\bbiz\s*[:=]\s*""\s*\|\|\s*"([^"]+)"',
                r'["\']biz["\']\s*:\s*["\']([^"\']+)',
                r'(?:var\s+)?biz\s*=\s*["\']([^"\']+)["\']',
            ],
            html,
        ) or None

    # mid
    if not mid:
        mid = _match([r'(?:var\s+)?mid\s*=\s*["\']?(\d+)["\']?'], html) or None

    # idx
    if not idx:
        idx = _match([r'(?:var\s+)?idx\s*=\s*["\']?(\d+)["\']?'], html) or None

    # sn
    if not sn:
        sn = _match([r'(?:var\s+)?sn\s*=\s*["\']([^"\']+)["\']'], html) or None

    # ---------- 3. title（正则收紧 + 不二次 decode） ----------
    title = None
    # 优先从 JS 变量拿
    title = _match(
        [r'(?:var\s+)?msg_title\s*=\s*["\']([^"\']*?)["\']\s*;?'],
        html,
    ) or None
    # 备选：og:title
    if not title:
        title = _meta(soup, "og:title") or None

    # ---------- 4. 描述 ----------
    desc = None
    desc = _match(
        [r'(?:var\s+)?msg_desc\s*=\s*htmlDecode\("([^"]*?)"\)'],
        html,
    ) or None
    if not desc:
        desc = _meta(soup, "og:description") or None

    # ---------- 5. 公众号昵称 ----------
    account = None
    account = _match(
        [r'(?:var\s+)?nickname\s*=\s*["\']([^"\']*?)["\']'],
        html,
    ) or None
    if not account:
        el = soup.select_one("#js_name") or soup.select_one(".profile_nickname")
        if el:
            account = el.get_text(strip=True)

    # ---------- 6. 发布时间 ----------
    publish_time = None
    published_at = 0
    raw_ct = _match(
        [
            r'(?:var\s+)?ct\s*=\s*["\']?(\d{9,})["\']?',
            r'["\']create_time["\']\s*:\s*["\']?(\d{9,})',
        ],
        html,
    )
    if raw_ct:
        published_at = int(raw_ct)
        publish_time = datetime.datetime.fromtimestamp(published_at).strftime(
            "%Y-%m-%d %H:%M:%S"
        )

    # ---------- 7. 封面图 ----------
    cover = None
    cover = _match(
        [r'(?:var\s+)?msg_cdn_url\s*=\s*["\']([^"\']*?)["\']'],
        html,
    ) or None
    if not cover:
        cover = _meta(soup, "og:image") or None

    # 公众号头像与文章封面是两个字段。页面可能把头像放在 JS 或 profile
    # 图片上，优先返回可直接访问的绝对 URL。
    avatar = _match(
        [
            r'(?:var\s+)?round_head_img\s*=\s*["\']([^"\']+)["\']',
            r'(?:var\s+)?hd_head_img\s*=\s*["\']([^"\']+)["\']',
            r'(?:var\s+)?head_img\s*=\s*["\']([^"\']+)["\']',
        ],
        html,
    ) or ""
    if not avatar:
        avatar_el = soup.select_one("#js_profile_head_img, .profile_avatar img")
        if avatar_el:
            avatar = avatar_el.get("data-src") or avatar_el.get("src") or ""
    if avatar.startswith("//"):
        avatar = "https:" + avatar

    # ---------- 8. 正文 ----------
    soup = BeautifulSoup(html, "lxml")
    content_div = soup.select_one("#js_content")
    if content_div:
        # 把图片的 data-src 换回 src（方便后续渲染）
        for img in content_div.find_all("img"):
            if img.get("data-src") and not img.get("src"):
                img["src"] = img["data-src"]
        content_html = content_div.decode_contents()
        content_text = content_div.get_text("\n", strip=True)
    else:
        content_html = ""
        content_text = ""

    return {
        "final_url": final_url,
        "biz": biz,
        "biz_decoded": decode_biz(biz),
        "mid": mid,
        "idx": idx,
        "sn": sn,
        "title": title,
        "desc": desc,
        "account": account,
        "published_at": published_at,
        "publish_time": publish_time,
        "cover": cover,
        "avatar": avatar,
        "content_html": content_html,
        "content_text": content_text,
    }


if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(description="通过微信文章链接解析文章元信息和正文")
    parser.add_argument("url", help="https://mp.weixin.qq.com/s/...")
    parser.add_argument("--preview", action="store_true", help="在 JSON 后附加正文预览（不适合重定向到 JSON 文件）")
    args = parser.parse_args()
    try:
        data = fetch_wechat_article(args.url)
    except (ValueError, requests.RequestException) as exc:
        raise SystemExit(f"文章解析失败: {exc}")
    output = {k: v for k, v in data.items() if k != "content_html"}
    print(json.dumps(output, ensure_ascii=False, indent=2))
    if args.preview:
        print("\n正文预览（前 800 字）:\n" + "=" * 60)
        print(data["content_text"][:800])

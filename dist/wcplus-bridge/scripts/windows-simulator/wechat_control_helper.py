"""JSON stdin/stdout adapter used by wcplus."""

import hashlib
import json
import re
import sys
from datetime import datetime

import contextlib
import html
from urllib.parse import urlsplit, parse_qs
from urllib.request import Request, urlopen, build_opener, HTTPRedirectHandler


def account_metadata(link):
    """Resolve identity from the public article itself, never from Max."""
    class WechatRedirect(HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):
            parsed = urlsplit(newurl)
            if parsed.scheme not in {"http", "https"} or parsed.hostname != "mp.weixin.qq.com":
                raise ValueError("unexpected article redirect")
            return super().redirect_request(req, fp, code, msg, headers, newurl)
    if urlsplit(link).hostname != "mp.weixin.qq.com":
        raise ValueError("invalid article host")
    with build_opener(WechatRedirect()).open(Request(link, headers={"User-Agent": "Mozilla/5.0"}), timeout=20) as response:
        page = response.read(4 * 1024 * 1024).decode("utf-8", "replace")
    biz = parse_qs(urlsplit(link).query).get("__biz", [""])[0]
    if not biz:
        patterns = [
            r'\bbiz\s*[:=]\s*""\s*\|\|\s*"([^"]+)"',
            r'["\']biz["\']\s*:\s*["\']([^"\']+)',
            r'(?:var\s+)?biz\s*=\s*["\']([^"\']+)',
        ]
        for pattern in patterns:
            match = re.search(pattern, page)
            if match:
                biz = match.group(1)
                break
    match = re.search(r"(?:var\s+)?nickname\s*=\s*['\"]([^'\"]+)", page)
    nickname = html.unescape(match.group(1)) if match else ""
    if not biz or not nickname:
        raise ValueError("无法从原文确认公众号身份，请检查文章访问状态")
    return biz, nickname



def extract_title(text: str) -> str:
    lines = [line.strip() for line in text.splitlines() if line.strip()]
    return lines[0] if lines else ""


def extract_published_at(text: str) -> int:
    match = re.search(r"(20\d{2})[-年](\d{1,2})[-月](\d{1,2})", text)
    if not match:
        return 0
    year, month, day = map(int, match.groups())
    return int(datetime(year, month, day).timestamp())


def stable_article_id(biz: str, link: str) -> str:
    """Use the same deterministic identity as the bridge article warehouse."""
    return hashlib.sha1(f"{biz}\x00{link}".encode("utf-8")).hexdigest()


def parse_article(link: str) -> dict:
    """Best-effort HTTP enrichment; the UI result remains the fallback source."""
    try:
        from fetch_wechat_article import fetch_wechat_article

        return fetch_wechat_article(link)
    except Exception:
        # Public article pages can be temporarily blocked or require a browser.
        # The desktop UI collector already has the link/content, so enrichment
        # must not turn a successful UI collection into a failed import.
        return {}


def article_content(parsed: dict, plain_text: str) -> str:
    content_html = str(parsed.get("content_html") or "").strip()
    if content_html:
        return content_html
    plain_text = str(plain_text or "").strip()
    return f"<pre>{html.escape(plain_text)}</pre>" if plain_text else ""


def main() -> None:
    request = json.load(sys.stdin)
    if request.get("action") == "status":
        from screen_tools import find_wechat_hwnd
        found = bool(find_wechat_hwnd())
        print(json.dumps({"ok": True, "collector": "wechat_control", "windowPresent": found,
                          "wechatReady": found, "status": "ready" if found else "window_missing",
                          "message": "检测到窗口，登录及采集能力需实际采集确认" if found else "未找到微信窗口"}, ensure_ascii=False))
        return
    from main import collect, is_article_link
    if request.get("action") == "collect_account":
        link = str(request.get("articleLink") or "").strip()
        nickname = str(request.get("nickname") or "").strip()
        expected_biz = str(request.get("biz") or "").strip()
        if link:
            if not is_article_link(link):
                raise ValueError("无效微信文章链接")
            expected_biz, nickname = account_metadata(link)
        if not nickname:
            raise ValueError("需要公众号昵称或有效文章链接")
        with contextlib.redirect_stdout(sys.stderr):
            result = collect(nickname, "both")
        actual_link = html.unescape(str(result["clipboard"]).strip())
        actual_biz, actual_name = account_metadata(actual_link)
        parsed = parse_article(actual_link)
        actual_biz = str(parsed.get("biz") or actual_biz).strip()
        actual_name = str(parsed.get("account") or actual_name).strip()
        if expected_biz and actual_biz != expected_biz:
            raise ValueError("采集到的文章不属于请求的公众号")
        if not expected_biz and actual_name != nickname:
            raise ValueError("采集公众号昵称与请求不一致")
        content = article_content(parsed, str(result.get("content") or ""))
        if not content:
            raise ValueError("未获取正文")
        title = str(parsed.get("title") or extract_title(str(result.get("content") or ""))).strip()
        published_at = int(parsed.get("published_at") or extract_published_at(str(result.get("content") or "")))
        print(json.dumps({
            "ok": True,
            "id": stable_article_id(actual_biz, actual_link),
            "biz": actual_biz,
            "nickname": actual_name,
            "articleLink": actual_link,
            "title": title,
            "publishedAt": published_at,
            "image": str(parsed.get("cover") or ""),
            "avatar": str(parsed.get("avatar") or result.get("avatar") or ""),
            "content": content,
        }, ensure_ascii=False))
        return
    keyword = request.get("keyword") or request.get("nickname") or request.get("initialLink")
    if not keyword:
        raise ValueError("keyword, nickname or initialLink is required")

    action = request.get("action", "get_latest_link")
    collected = collect(keyword, "content" if action == "get_article_info" else "link")
    copied = str(collected.get("clipboard", "")).strip()
    if action == "get_article_info":
        article_link = str(request.get("articleLink", "")).strip()
        parsed = parse_article(article_link) if article_link else {}
        plain_content = str(parsed.get("content_text") or copied).strip()
        print(json.dumps({
            "ok": True,
            "id": stable_article_id(str(parsed.get("biz") or ""), article_link) if article_link else "",
            "articleLink": article_link,
            "biz": str(parsed.get("biz") or ""),
            "nickname": str(parsed.get("account") or ""),
            "title": str(parsed.get("title") or extract_title(plain_content)),
            "publishedAt": int(parsed.get("published_at") or extract_published_at(plain_content)),
            "image": str(parsed.get("cover") or ""),
            "avatar": str(parsed.get("avatar") or collected.get("avatar") or ""),
            "content": article_content(parsed, plain_content),
        }, ensure_ascii=False), flush=True)
        return
    if not is_article_link(copied):
        print(json.dumps({
            "ok": False,
            "code": "article_link_not_copied",
            "error": "微信流程完成，但剪贴板不是文章链接",
            "content": copied,
            "manualIntervention": True,
        }, ensure_ascii=False), flush=True)
        return

    print(json.dumps({
        "ok": True,
        "id": hashlib.sha1(copied.encode("utf-8")).hexdigest(),
        "articleLink": copied,
        "title": "",
        "publishedAt": 0,
        "image": "",
        "avatar": collected.get("avatar", ""),
        "content": "",
    }, ensure_ascii=False), flush=True)


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(json.dumps({
            "ok": False,
            "code": "wechat_helper_failed",
            "error": str(exc),
            "manualIntervention": True,
        }, ensure_ascii=False), flush=True)

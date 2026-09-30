package service

import (
	"net/url"
	"strings"
)

// BizFromWechatURL extracts __biz from mp.weixin.qq.com profile/home links.
func BizFromWechatURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if biz := strings.TrimSpace(u.Query().Get("__biz")); biz != "" {
		return biz, true
	}
	// Some links embed __biz in fragment or path query-like segments.
	if i := strings.Index(raw, "__biz="); i >= 0 {
		rest := raw[i+len("__biz="):]
		if j := strings.IndexAny(rest, "&# "); j >= 0 {
			rest = rest[:j]
		}
		rest = strings.TrimSpace(rest)
		if rest != "" {
			return rest, true
		}
	}
	return "", false
}

func enrichImportRequest(req ImportAccountRequest) ImportAccountRequest {
	link := strings.TrimSpace(req.Link)
	if link == "" {
		return req
	}
	if strings.TrimSpace(req.Biz) == "" {
		if b, ok := BizFromWechatURL(link); ok {
			req.Biz = b
		}
	}
	return req
}

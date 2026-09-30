package service

import (
	"regexp"
	"strings"
)

var firstImgSrcRe = regexp.MustCompile(`(?i)<img[^>]+src\s*=\s*["']([^"']+)`)

func firstImageFromHTML(html string) string {
	html = strings.TrimSpace(html)
	if html == "" {
		return ""
	}
	if m := firstImgSrcRe.FindStringSubmatch(html); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func resolveArticleImage(cover, contentHTML, gzhAvatar string) string {
	if s := strings.TrimSpace(cover); s != "" {
		return s
	}
	if s := firstImageFromHTML(contentHTML); s != "" {
		return s
	}
	return strings.TrimSpace(gzhAvatar)
}

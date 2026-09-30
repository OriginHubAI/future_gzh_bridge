package wcplus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// ImportByArticleURL mirrors wcplus UI「通过文章链接导入公众号」.
// wcplus versions may use different paths; we try documented-style endpoints in order.
func (c *Client) ImportByArticleURL(ctx context.Context, articleURL string) (json.RawMessage, error) {
	articleURL = strings.TrimSpace(articleURL)
	if articleURL == "" {
		return nil, fmt.Errorf("article url is empty")
	}
	body := map[string]any{
		"url":         articleURL,
		"article_url": articleURL,
		"link":        articleURL,
	}
	var out json.RawMessage
	// Primary guess (confirm via wcplus DevTools if this fails on your build).
	if err := c.postJSON(ctx, "/api/gzh/import_by_article_url", body, &out); err == nil && len(out) > 0 {
		return out, nil
	}
	if err := c.postJSON(ctx, "/api/gzh/add_by_article_link", body, &out); err == nil && len(out) > 0 {
		return out, nil
	}
	q := url.Values{}
	q.Set("url", articleURL)
	if err := c.getJSON(ctx, "/api/article/gzh", q, &out); err == nil && len(out) > 0 {
		return out, nil
	}
	return nil, fmt.Errorf("wcplus article-link import API not available; use wcplus UI once or import with nickname/biz (verify POST path in DevTools when clicking 通过文章链接导入)")
}

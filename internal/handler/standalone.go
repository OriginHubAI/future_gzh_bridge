package handler

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gkx/wcplus/internal/alert"
	"gkx/wcplus/internal/simulatorstore"
)

// requestString returns the first non-empty string value for any of the
// supplied aliases. The article fetcher has used both snake_case and camelCase
// names over time, so the standalone endpoint accepts both forms.
func requestString(req map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := req[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func requestInt64(value any) int64 {
	switch value := value.(type) {
	case float64:
		return int64(value)
	case float32:
		return int64(value)
	case int:
		return int64(value)
	case int64:
		return value
	case json.Number:
		parsed, _ := value.Int64()
		return parsed
	case string:
		value = strings.TrimSpace(value)
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04"} {
			if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
				return parsed.Unix()
			}
		}
		return 0
	default:
		return 0
	}
}

func articleID(biz, link string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(biz) + "\x00" + strings.TrimSpace(link)))
	return hex.EncodeToString(sum[:])
}

// StandaloneMiddleware owns the RPC surface in wechat_control mode. No request
// can fall through to a Max handler, including login and legacy simulator RPCs.
func StandaloneMiddleware(store *simulatorstore.Store, command string, args []string, timeout time.Duration, notify *alert.Notifier) gin.HandlerFunc {
	gate := make(chan struct{}, 1)
	run := func(ctx context.Context, input any) (map[string]any, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		select {
		case gate <- struct{}{}:
			defer func() { <-gate }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		raw, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		// The adapter replaces itself with the helper so CommandContext cancellation
		// stops the actual GUI process, rather than leaving a child clicking.
		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Stdin = strings.NewReader(string(raw))
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("independent helper failed: %w", err)
		}
		var result map[string]any
		if err = json.Unmarshal(out, &result); err != nil {
			return nil, fmt.Errorf("invalid helper JSON: %w", err)
		}
		if result["ok"] != true {
			return nil, fmt.Errorf("%v: %v", result["code"], result["error"])
		}
		return result, nil
	}
	str := func(m map[string]any, key string) string { v, _ := m[key].(string); return v }
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if path == "/health" {
			c.AbortWithStatusJSON(200, gin.H{"ok": true, "collector": "wechat_control"})
			return
		}
		if !strings.HasPrefix(path, "/v1/rpc/") {
			c.Next()
			return
		}
		c.Abort()
		fail := func(err error) {
			if notify != nil {
				notify.Trigger(c.Request.Context(), "simulator.manual_intervention", err.Error(), nil)
			}
			c.JSON(503, gin.H{"code": 503001, "error": err.Error()})
		}
		success := func(data any) { c.JSON(200, gin.H{"code": 0, "data": data}) }
		switch {
		case c.Request.Method == http.MethodPost && path == "/v1/rpc/save-article-link":
			// Accept both the normalized helper response and the raw
			// fetch_wechat_article.py JSON, so a previously fetched article can be
			// persisted without running the desktop collector again.
			var req map[string]any
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(400, gin.H{"code": 100001, "error": "invalid JSON"})
				return
			}
			biz := requestString(req, "biz")
			nickname := requestString(req, "nickname", "account")
			link := requestString(req, "articleLink", "final_url", "link")
			title := requestString(req, "title")
			image := requestString(req, "image", "cover")
			avatar := requestString(req, "avatar")
			content := requestString(req, "content", "content_html")
			if content == "" {
				if text := requestString(req, "content_text"); text != "" {
					content = "<pre>" + html.EscapeString(text) + "</pre>"
				}
			}
			if biz == "" || link == "" || content == "" {
				c.JSON(400, gin.H{"code": 100001, "error": "biz、articleLink/final_url、正文不能为空"})
				return
			}
			publishedAt := requestInt64(req["publishedAt"])
			if publishedAt == 0 {
				publishedAt = requestInt64(req["published_at"])
			}
			if publishedAt == 0 {
				publishedAt = requestInt64(req["publish_time"])
			}
			if err := store.UpsertAccount(biz, nickname, avatar); err != nil {
				fail(err)
				return
			}
			if err := store.AddArticleFrom("external", biz, nickname, link, title, image, content, publishedAt); err != nil {
				fail(err)
				return
			}
			success(gin.H{"id": articleID(biz, link), "biz": biz, "nickname": nickname, "articleLink": link, "source": "external"})
		case c.Request.Method == http.MethodGet && (path == "/v1/rpc/status" || path == "/v1/rpc/wechat-login-status" || path == "/v1/rpc/login-status"):
			result, err := run(c.Request.Context(), gin.H{"action": "status"})
			if err != nil {
				fail(err)
				return
			}
			success(result)
		case c.Request.Method == http.MethodPost && (path == "/v1/rpc/import-official-account" || path == "/v1/rpc/sync-official-account"):
			var req map[string]any
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(400, gin.H{"code": 100001, "error": "invalid JSON"})
				return
			}
			biz, nickname, link := str(req, "biz"), str(req, "nickname"), str(req, "articleLink")
			if link == "" {
				link = str(req, "link")
			}
			if path == "/v1/rpc/sync-official-account" {
				account, found := store.FindAccount(biz, "")
				if !found {
					c.JSON(404, gin.H{"code": 404001, "error": "公众号不存在"})
					return
				}
				nickname = account.Nickname
			}
			if nickname == "" && link == "" {
				c.JSON(400, gin.H{"code": 100001, "error": "articleLink or nickname required"})
				return
			}
			result, err := run(c.Request.Context(), gin.H{"action": "collect_account", "nickname": nickname, "articleLink": link, "biz": biz})
			if err != nil {
				fail(err)
				return
			}
			actualBiz, actualName, actualLink := str(result, "biz"), str(result, "nickname"), str(result, "articleLink")
			if actualBiz == "" || actualName == "" || actualLink == "" || str(result, "content") == "" {
				fail(fmt.Errorf("helper returned incomplete account/article data"))
				return
			}
			if biz != "" && actualBiz != biz {
				fail(fmt.Errorf("collected account differs from requested biz"))
				return
			}
			avatar := str(result, "avatar")
			if err = store.UpsertAccount(actualBiz, actualName, avatar); err != nil {
				fail(err)
				return
			}
			stamp, _ := result["publishedAt"].(float64)
			if err = store.AddArticleFrom("wechat_control", actualBiz, actualName, actualLink, str(result, "title"), str(result, "image"), str(result, "content"), int64(stamp)); err != nil {
				fail(err)
				return
			}
			success(gin.H{"candidate": gin.H{"Biz": actualBiz, "Nickname": actualName, "Img": avatar}, "collector": "wechat_control"})
		case c.Request.Method == http.MethodGet && path == "/v1/rpc/export-latest-articles":
			account, articles, err := store.Latest(c.Query("biz"), queryInt(c, "limit", 10))
			if err != nil {
				c.JSON(404, gin.H{"code": 404001, "error": "公众号不存在"})
				return
			}
			rows := make([]gin.H, 0, len(articles))
			for _, a := range articles {
				content := a.Content
				if c.Query("withContent") == "false" {
					content = ""
				}
				rows = append(rows, gin.H{"id": a.ID, "name": a.Title, "link": a.Link, "time": a.PublishedAt, "imageUrl": a.Image, "content": content})
			}
			success(gin.H{"biz": account.Biz, "officialAccountName": account.Nickname, "avatar": account.Avatar, "articles": rows, "total": len(rows)})
		case c.Request.Method == http.MethodPost && path == "/v1/rpc/delete-official-account":
			var req struct {
				Biz string `json:"biz"`
			}
			if c.ShouldBindJSON(&req) != nil || req.Biz == "" {
				c.JSON(400, gin.H{"code": 100001, "error": "biz required"})
				return
			}
			if err := store.DeleteAccount(req.Biz); err != nil {
				c.JSON(404, gin.H{"code": 404001, "error": err.Error()})
				return
			}
			success(gin.H{"biz": req.Biz})
		default:
			c.JSON(400, gin.H{"code": 100001, "error": "RPC not supported by standalone collector; use save-article-link/import/sync/export/status"})
		}
	}
}

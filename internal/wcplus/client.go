package wcplus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client calls wcplusPro local HTTP API (default http://127.0.0.1:5001).
// See https://www.wcplus.cn/doc/data_export_api
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.ListGzh(ctx, 0, 1, "", "")
	return err
}

// ListGzh GET /api/gzh/list
func (c *Client) ListGzh(ctx context.Context, offset, num int, sort, direction string) (GzhListResponse, error) {
	q := url.Values{}
	q.Set("offset", fmt.Sprintf("%d", offset))
	q.Set("num", fmt.Sprintf("%d", num))
	if sort != "" {
		q.Set("sort", sort)
	}
	if direction != "" {
		q.Set("direction", direction)
	}
	var out GzhListResponse
	err := c.getJSON(ctx, "/api/gzh/list", q, &out)
	return out, err
}

// ListGzhArticles GET /api/report/gzh_articles
func (c *Client) ListGzhArticles(ctx context.Context, biz string, offset, num int, sort, direction string) (GzhArticlesResponse, error) {
	q := url.Values{}
	q.Set("biz", biz)
	q.Set("offset", fmt.Sprintf("%d", offset))
	q.Set("num", fmt.Sprintf("%d", num))
	if sort == "" {
		sort = "p_date"
	}
	if direction == "" {
		direction = "desc"
	}
	q.Set("sort", sort)
	q.Set("direction", direction)
	var out GzhArticlesResponse
	err := c.getJSON(ctx, "/api/report/gzh_articles", q, &out)
	return out, err
}

// GetArticleContent GET /api/article/content
func (c *Client) GetArticleContent(ctx context.Context, nickname, articleID string) (ArticleContentResponse, error) {
	q := url.Values{}
	q.Set("nickname", nickname)
	q.Set("id", articleID)
	var out ArticleContentResponse
	err := c.getJSON(ctx, "/api/article/content", q, &out)
	return out, err
}

func (c *Client) getJSON(ctx context.Context, path string, q url.Values, dest any) error {
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("wcplus request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("wcplus HTTP %d: %s", resp.StatusCode, truncate(string(body), 512))
	}
	if len(body) == 0 {
		return fmt.Errorf("wcplus empty response")
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("wcplus decode: %w (body=%s)", err, truncate(string(body), 256))
	}
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

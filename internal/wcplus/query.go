package wcplus

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
)

// SearchGzh GET /api/gzh/search — already imported accounts.
func (c *Client) SearchGzh(ctx context.Context, keyword string, offset, num int) (json.RawMessage, error) {
	q := url.Values{}
	q.Set("keyword", keyword)
	if offset > 0 {
		q.Set("offset", itoa(offset))
	}
	if num > 0 {
		q.Set("num", itoa(num))
	}
	var out json.RawMessage
	err := c.getJSON(ctx, "/api/gzh/search", q, &out)
	return out, err
}

// SearchGzhCandidates GET /api/search_gzh/search — import candidates.
func (c *Client) SearchGzhCandidates(ctx context.Context, keyword string) (json.RawMessage, error) {
	q := url.Values{}
	q.Set("keyword", keyword)
	var out json.RawMessage
	err := c.getJSON(ctx, "/api/search_gzh/search", q, &out)
	return out, err
}

// AllArticles GET /api/article/all_articles — same data as #/all_articles page.
func (c *Client) AllArticles(ctx context.Context, offset, num int, sort, direction string) (json.RawMessage, error) {
	q := url.Values{}
	q.Set("offset", itoa(offset))
	q.Set("num", itoa(num))
	if sort == "" {
		sort = "p_date"
	}
	if direction == "" {
		direction = "desc"
	}
	q.Set("sort", sort)
	q.Set("direction", direction)
	var out json.RawMessage
	err := c.getJSON(ctx, "/api/article/all_articles", q, &out)
	return out, err
}

// GetGzhReqData GET /api/req_data/get_gzh — WeChat / gzh request params (login state hint).
func (c *Client) GetGzhReqData(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.getJSON(ctx, "/api/req_data/get_gzh", nil, &out)
	return out, err
}

// MaxQueueStatus POST /api/task/control command=run — probes Max activation.
func (c *Client) MaxQueueStatus(ctx context.Context) (TaskControlResponse, error) {
	return c.ControlTasks(ctx, "run")
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

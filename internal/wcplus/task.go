package wcplus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// TaskNewResponse is returned by POST /api/task/new (Max).
type TaskNewResponse struct {
	Status string `json:"status"`
	TaskID int64  `json:"task_id"`
	Msg    string `json:"msg"`
	Tasks  any    `json:"tasks"`
}

// TaskControlResponse is returned by POST /api/task/control.
type TaskControlResponse struct {
	Status string `json:"status"`
	Data   any    `json:"data"`
	Msg    string `json:"msg"`
}

// TaskAllResponse is returned by GET /api/task/all (unfinished queue only).
type TaskAllResponse struct {
	Tasks []TaskRow `json:"Tasks"`
}

func (r *TaskAllResponse) UnmarshalJSON(b []byte) error {
	var raw struct {
		Upper []TaskRow `json:"Tasks"`
		Lower []TaskRow `json:"tasks"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	r.Tasks = raw.Upper
	if len(r.Tasks) == 0 {
		r.Tasks = raw.Lower
	}
	return nil
}

type TaskRow struct {
	ID          int64  `json:"ID"`
	Biz         string `json:"Biz"`
	Nickname    string `json:"Nickname"`
	CrawlerType string `json:"CrawlerType"`
	Status      string `json:"Status"`
	CreatedAt   string `json:"CreatedAt"`
	UpdatedAt   string `json:"UpdatedAt"`
}

// NewTask POST /api/task/new — pass wcplus crawlerType and related fields.
func (c *Client) NewTask(ctx context.Context, body map[string]any) (TaskNewResponse, error) {
	var out TaskNewResponse
	err := c.postJSON(ctx, "/api/task/new", body, &out)
	return out, err
}

// ControlTasks POST /api/task/control — e.g. command=run.
func (c *Client) ControlTasks(ctx context.Context, command string) (TaskControlResponse, error) {
	var out TaskControlResponse
	err := c.postJSON(ctx, "/api/task/control", map[string]any{"command": command}, &out)
	return out, err
}

// AllTasks GET /api/task/all — unfinished tasks in queue.
func (c *Client) AllTasks(ctx context.Context) (TaskAllResponse, error) {
	var out TaskAllResponse
	err := c.getJSON(ctx, "/api/task/all", nil, &out)
	return out, err
}

func (c *Client) postJSON(ctx context.Context, path string, body any, dest any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	u := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("wcplus request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("wcplus HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 512))
	}
	if err := json.Unmarshal(respBody, dest); err != nil {
		return fmt.Errorf("wcplus decode: %w (body=%s)", err, truncate(string(respBody), 256))
	}
	return nil
}

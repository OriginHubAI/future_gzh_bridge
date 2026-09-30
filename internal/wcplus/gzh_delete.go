package wcplus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// DeleteGzh removes an imported official account from wcplus local storage.
// wcplusPro UI supports delete; public API docs omit the path — confirm via DevTools if all attempts fail.
func (c *Client) DeleteGzh(ctx context.Context, biz, nickname string) error {
	biz = strings.TrimSpace(biz)
	nickname = strings.TrimSpace(nickname)
	if biz == "" {
		return fmt.Errorf("biz is required")
	}
	body := map[string]any{
		"biz":      biz,
		"Biz":      biz,
		"nickname": nickname,
		"Nickname": nickname,
	}
	candidates := []string{
		"/api/gzh/delete",
		"/api/gzh/remove",
		"/api/gzh/del",
	}
	var lastErr error
	for _, path := range candidates {
		if err := c.postStatusOK(ctx, path, body); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	q := url.Values{}
	q.Set("biz", biz)
	if nickname != "" {
		q.Set("nickname", nickname)
	}
	for _, path := range []string{"/api/gzh/delete", "/api/gzh/remove"} {
		if err := c.getStatusOK(ctx, path, q); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return fmt.Errorf("wcplus delete gzh API not available (verify POST path in DevTools when clicking 删除): %w", lastErr)
	}
	return fmt.Errorf("wcplus delete gzh API not available; verify path in DevTools when clicking 删除")
}

type wcplusStatusReply struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Msg     string `json:"msg"`
}

func (c *Client) postStatusOK(ctx context.Context, path string, body map[string]any) error {
	var raw json.RawMessage
	if err := c.postJSON(ctx, path, body, &raw); err != nil {
		return err
	}
	return interpretWCPlusStatus(raw)
}

func (c *Client) getStatusOK(ctx context.Context, path string, q url.Values) error {
	var raw json.RawMessage
	if err := c.getJSON(ctx, path, q, &raw); err != nil {
		return err
	}
	return interpretWCPlusStatus(raw)
}

func interpretWCPlusStatus(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var st wcplusStatusReply
	if err := json.Unmarshal(raw, &st); err != nil {
		// Non-JSON or scalar success bodies are treated as OK when HTTP already succeeded.
		return nil
	}
	msg := strings.TrimSpace(st.Message)
	if msg == "" {
		msg = strings.TrimSpace(st.Msg)
	}
	switch strings.ToLower(strings.TrimSpace(st.Status)) {
	case "", "ok", "success":
		return nil
	case "error", "fail", "failed":
		if msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return fmt.Errorf("wcplus returned status=%s", st.Status)
	default:
		if msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return nil
	}
}

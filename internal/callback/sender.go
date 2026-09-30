package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Event is POSTed to upstream (e.g. Django) when configured.
type Event struct {
	Type      string         `json:"type"`
	OccurredAt time.Time     `json:"occurredAt"`
	Payload   map[string]any `json:"payload"`
}

type Sender struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewSender(baseURL, token string) *Sender {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return &Sender{
		baseURL: baseURL,
		token:   strings.TrimSpace(token),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (s *Sender) Enabled() bool {
	return s != nil && s.baseURL != ""
}

func (s *Sender) Send(ctx context.Context, ev Event) error {
	if !s.Enabled() {
		return nil
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("callback POST: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("callback HTTP %d", resp.StatusCode)
	}
	return nil
}

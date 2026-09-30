package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gkx/wcplus/internal/callback"
)

// Notifier sends alert.triggered to callback URL and/or alert webhook.
type Notifier struct {
	callback   *callback.Sender
	webhook    string
	client     *http.Client
	manualOnly bool
}

func New(callback *callback.Sender, webhookURL string, manualInterventionOnly bool) *Notifier {
	return &Notifier{
		callback:   callback,
		webhook:    strings.TrimSpace(webhookURL),
		client:     &http.Client{Timeout: 15 * time.Second},
		manualOnly: manualInterventionOnly,
	}
}

func manualInterventionAlertType(alertType string) bool {
	switch alertType {
	case "collector.not_ready", "collector.manual_required", "login.still_required", "wechat.content_restricted", "simulator.manual_intervention", "simulator.job_failed":
		return true
	default:
		return false
	}
}

func (n *Notifier) Trigger(ctx context.Context, alertType, message string, payload map[string]any) {
	if n.manualOnly && !manualInterventionAlertType(alertType) {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["alertType"] = alertType
	payload["message"] = message
	_ = n.callback.Send(ctx, callback.Event{
		Type:       "alert.triggered",
		OccurredAt: time.Now().UTC(),
		Payload:    payload,
	})
	if n.webhook == "" {
		return
	}
	body, _ := json.Marshal(map[string]any{
		"type":    alertType,
		"message": message,
		"payload": payload,
		"at":      time.Now().UTC().Format(time.RFC3339),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhook, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

func (n *Notifier) Triggerf(ctx context.Context, alertType, format string, args ...any) {
	n.Trigger(ctx, alertType, fmt.Sprintf(format, args...), nil)
}

// Success posts a non-alert event to callback only (e.g. sync.succeeded).
func (n *Notifier) Success(ctx context.Context, eventType string, payload map[string]any) {
	if n.manualOnly {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	_ = n.callback.Send(ctx, callback.Event{
		Type:       eventType,
		OccurredAt: time.Now().UTC(),
		Payload:    payload,
	})
}

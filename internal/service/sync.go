package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gkx/wcplus/internal/wcplus"
)

const taskCreateGap = 3 * time.Second

// SyncStep names accepted in SyncStartRequest.Steps.
const (
	StepLink    = "link"
	StepArticle = "article"
	StepReading = "reading"
)

// SyncStartRequest orchestrates Max task creation + queue run.
type SyncStartRequest struct {
	Biz      string   `json:"biz"`
	Nickname string   `json:"nickname"`
	Img      string   `json:"img,omitempty"`
	Steps    []string `json:"steps"`
	// Omitted defaults to true; a pointer preserves an explicit false from
	// callers that want to create tasks without immediately starting them.
	RunQueue *bool `json:"runQueue,omitempty"`
}

// StepResult is one wcplus task/new outcome.
type StepResult struct {
	Step        string `json:"step"`
	CrawlerType string `json:"crawlerType"`
	Status      string `json:"status"`
	TaskID      int64  `json:"taskId,omitempty"`
	Message     string `json:"message,omitempty"`
}

// SyncStartResult aggregates created tasks and optional queue start.
type SyncStartResult struct {
	Steps      []StepResult                `json:"steps"`
	QueueStart *wcplus.TaskControlResponse `json:"queueStart,omitempty"`
}

type SyncService struct {
	wc *wcplus.Client
}

func NewSyncService(wc *wcplus.Client) *SyncService {
	return &SyncService{wc: wc}
}

func (s *SyncService) Start(ctx context.Context, req SyncStartRequest) (SyncStartResult, error) {
	biz := strings.TrimSpace(req.Biz)
	nickname := strings.TrimSpace(req.Nickname)
	if biz == "" || nickname == "" {
		return SyncStartResult{}, fmt.Errorf("biz and nickname are required")
	}
	steps := normalizeSteps(req.Steps)
	var out SyncStartResult
	for i, step := range steps {
		if i > 0 {
			select {
			case <-ctx.Done():
				return out, ctx.Err()
			case <-time.After(taskCreateGap):
			}
		}
		body, crawlerType := taskBodyForStep(step, biz, nickname, req.Img)
		resp, err := s.wc.NewTask(ctx, body)
		sr := StepResult{
			Step:        step,
			CrawlerType: crawlerType,
			Status:      resp.Status,
			TaskID:      resp.TaskID,
			Message:     resp.Msg,
		}
		out.Steps = append(out.Steps, sr)
		if err != nil {
			return out, err
		}
		if strings.EqualFold(resp.Status, "error") {
			return out, fmt.Errorf("wcplus task/new %s: %s", crawlerType, resp.Msg)
		}
	}
	if runQueueOrDefault(req.RunQueue) {
		ctrl, err := s.wc.ControlTasks(ctx, "run")
		if err != nil {
			return out, err
		}
		out.QueueStart = &ctrl
		if strings.EqualFold(ctrl.Status, "not_max_version") || strings.EqualFold(ctrl.Status, "unactivated") {
			return out, fmt.Errorf("wcplus queue: status=%s msg=%s", ctrl.Status, ctrl.Msg)
		}
	}
	return out, nil
}

func normalizeSteps(steps []string) []string {
	if len(steps) == 0 {
		return []string{StepLink, StepArticle}
	}
	var out []string
	seen := map[string]struct{}{}
	order := []string{StepLink, StepArticle, StepReading}
	want := map[string]struct{}{}
	for _, s := range steps {
		want[strings.ToLower(strings.TrimSpace(s))] = struct{}{}
	}
	for _, s := range order {
		if _, ok := want[s]; ok {
			if _, dup := seen[s]; !dup {
				seen[s] = struct{}{}
				out = append(out, s)
			}
		}
	}
	if len(out) == 0 {
		return []string{StepLink, StepArticle}
	}
	return out
}

func taskBodyForStep(step, biz, nickname, img string) (map[string]any, string) {
	base := map[string]any{
		"biz":      biz,
		"nickname": nickname,
		"img":      img,
	}
	switch step {
	case StepReading:
		base["crawlerType"] = "reading_data"
		base["readingDataType"] = "all"
		base["readingDataOnlyMain"] = true
		base["readingDataRefresh"] = false
		return base, "reading_data"
	case StepArticle:
		base["crawlerType"] = "article"
		base["articleRefresh"] = false
		base["articleImgDownload"] = false
		return base, "article"
	default:
		base["crawlerType"] = "gzh_article_link"
		base["articleListType"] = "all"
		base["articleListAmount"] = 1000
		base["articleListOffset"] = 0
		return base, "gzh_article_link"
	}
}

package service

import (
	"context"
	"fmt"
	"strings"

	"gkx/wcplus/internal/wcplus"
)

// ImportAccountRequest imports by nickname and/or biz: resolve account then create link task.
type ImportAccountRequest struct {
	Biz      string `json:"biz"`
	Nickname string `json:"nickname"`
	// Link: mp.weixin.qq.com URL with __biz (profile/home).
	Link string `json:"link,omitempty"`
	// ArticleLink: mp.weixin.qq.com/s/... — same as wcplus UI「通过文章链接导入公众号」.
	ArticleLink string `json:"articleLink,omitempty"`
	// RunQueue defaults to true when omitted. A pointer preserves an explicit false.
	RunQueue *bool `json:"runQueue,omitempty"`
}

type ImportAccountResult struct {
	Source     string                      `json:"source"`
	Candidate  wcplus.GzhCandidate         `json:"candidate"`
	LinkTask   wcplus.TaskNewResponse      `json:"linkTask"`
	TaskReused bool                        `json:"taskReused,omitempty"`
	Queue      *wcplus.TaskControlResponse `json:"queueStart,omitempty"`
}

type ImportService struct {
	wc *wcplus.Client
}

func NewImportService(wc *wcplus.Client) *ImportService {
	return &ImportService{wc: wc}
}

func (s *ImportService) ImportByNickname(ctx context.Context, req ImportAccountRequest) (ImportAccountResult, error) {
	runQueue := runQueueOrDefault(req.RunQueue)
	articleLink := strings.TrimSpace(req.ArticleLink)
	if articleLink == "" {
		articleLink = strings.TrimSpace(req.Link)
		if !isWechatArticleLink(articleLink) {
			articleLink = ""
		}
	}
	if articleLink != "" {
		return s.ImportByArticleLink(ctx, articleLink, runQueue)
	}
	req = enrichImportRequest(req)
	nickname := strings.TrimSpace(req.Nickname)
	biz := strings.TrimSpace(req.Biz)
	link := strings.TrimSpace(req.Link)
	if nickname == "" && biz == "" {
		if link != "" {
			return ImportAccountResult{}, fmt.Errorf("link must contain __biz or provide nickname/biz")
		}
		return ImportAccountResult{}, fmt.Errorf("nickname, biz, or link is required")
	}
	if err := s.guardDuplicate(ctx, biz, nickname); err != nil {
		return ImportAccountResult{}, err
	}
	candidate, source, err := s.resolveCandidate(ctx, biz, nickname)
	if err != nil {
		return ImportAccountResult{}, err
	}
	candidate, err = s.completeCandidate(ctx, candidate)
	if err != nil {
		return ImportAccountResult{}, err
	}
	linkTask, queue, reused, err := s.createLinkTaskAndRun(ctx, candidate, runQueue)
	if err != nil {
		return ImportAccountResult{}, err
	}
	out := ImportAccountResult{Source: source, Candidate: candidate, LinkTask: linkTask, TaskReused: reused, Queue: queue}
	InvalidateGzhBizCache(candidate.Biz)
	return out, nil
}

func runQueueOrDefault(value *bool) bool {
	return value == nil || *value
}

// createLinkTaskAndRun creates gzh_article_link then optionally starts the wcplus queue (immediate crawl).
func (s *ImportService) createLinkTaskAndRun(ctx context.Context, candidate wcplus.GzhCandidate, runQueue bool) (wcplus.TaskNewResponse, *wcplus.TaskControlResponse, bool, error) {
	if strings.TrimSpace(candidate.Biz) == "" {
		return wcplus.TaskNewResponse{}, nil, false, fmt.Errorf("resolved account missing biz")
	}
	if strings.TrimSpace(candidate.Nickname) == "" {
		return wcplus.TaskNewResponse{}, nil, false, fmt.Errorf("resolved account missing nickname")
	}
	if existing, found, err := s.findUnfinishedLinkTask(ctx, candidate.Biz); err != nil {
		return wcplus.TaskNewResponse{}, nil, false, fmt.Errorf("check existing link task: %w", err)
	} else if found {
		queue, err := s.startQueue(ctx, runQueue)
		if err != nil {
			return existing, nil, true, err
		}
		return existing, queue, true, nil
	}
	body := map[string]any{
		"biz":               candidate.Biz,
		"nickname":          candidate.Nickname,
		"img":               candidate.Img,
		"crawlerType":       "gzh_article_link",
		"articleListType":   "all",
		"articleListAmount": 1000,
		"articleListOffset": 0,
	}
	linkTask, err := s.wc.NewTask(ctx, body)
	if err != nil {
		return wcplus.TaskNewResponse{}, nil, false, err
	}
	if strings.EqualFold(linkTask.Status, "error") {
		return linkTask, nil, false, fmt.Errorf("wcplus link task: %s", linkTask.Msg)
	}
	queue, err := s.startQueue(ctx, runQueue)
	if err != nil {
		return linkTask, nil, false, err
	}
	return linkTask, queue, false, nil
}

func (s *ImportService) findUnfinishedLinkTask(ctx context.Context, biz string) (wcplus.TaskNewResponse, bool, error) {
	tasks, err := s.wc.AllTasks(ctx)
	if err != nil {
		return wcplus.TaskNewResponse{}, false, err
	}
	for _, task := range tasks.Tasks {
		if strings.TrimSpace(task.Biz) == strings.TrimSpace(biz) && strings.EqualFold(strings.TrimSpace(task.CrawlerType), "gzh_article_link") {
			return wcplus.TaskNewResponse{
				Status: "existing",
				TaskID: task.ID,
				Msg:    "reused unfinished gzh_article_link task",
			}, true, nil
		}
	}
	return wcplus.TaskNewResponse{}, false, nil
}

func (s *ImportService) startQueue(ctx context.Context, runQueue bool) (*wcplus.TaskControlResponse, error) {
	if !runQueue {
		return nil, nil
	}
	ctrl, err := s.wc.ControlTasks(ctx, "run")
	if err != nil {
		return nil, err
	}
	return &ctrl, nil
}

func (s *ImportService) guardDuplicate(ctx context.Context, biz, nickname string) error {
	if biz != "" {
		if c, ok := FindImportedGzhByBiz(ctx, s.wc, biz); ok {
			return &OfficialAccountExistsError{Biz: c.Biz, Nickname: c.Nickname}
		}
	}
	if nickname != "" {
		if c, ok := s.findImportedByNickname(ctx, nickname); ok {
			return &OfficialAccountExistsError{Biz: c.Biz, Nickname: c.Nickname}
		}
	}
	return nil
}

func (s *ImportService) resolveCandidate(ctx context.Context, biz, nickname string) (wcplus.GzhCandidate, string, error) {
	if biz != "" {
		if nickname != "" {
			return wcplus.GzhCandidate{Biz: biz, Nickname: nickname}, "request_biz", nil
		}
		candidate, err := s.findCandidateByBiz(ctx, biz)
		if err != nil {
			return wcplus.GzhCandidate{}, "", err
		}
		return candidate, "gzh_search_by_biz", nil
	}
	if c, ok := s.findImportedByNickname(ctx, nickname); ok {
		return c, "already_imported", nil
	}
	if raw, err := s.wc.SearchGzh(ctx, nickname, 0, 20); err == nil {
		if list, err := wcplus.ParseSearchCandidates(raw); err == nil {
			if c, ok := wcplus.MatchNicknameExact(list, nickname); ok {
				return c, "gzh_search", nil
			}
		}
	}
	raw, err := s.wc.SearchGzhCandidates(ctx, nickname)
	if err != nil {
		return wcplus.GzhCandidate{}, "", err
	}
	candidates, err := wcplus.ParseSearchCandidates(raw)
	if err != nil {
		return wcplus.GzhCandidate{}, "", err
	}
	candidate, ok := wcplus.MatchNicknameExact(candidates, nickname)
	if !ok {
		return wcplus.GzhCandidate{}, "", fmt.Errorf(
			"no exact nickname match for %q (已入库请直接 sync/export；新号请在 wcplus 搜索确认昵称完全一致)",
			nickname,
		)
	}
	return candidate, "search_gzh", nil
}

func (s *ImportService) completeCandidate(ctx context.Context, candidate wcplus.GzhCandidate) (wcplus.GzhCandidate, error) {
	candidate.Biz = strings.TrimSpace(candidate.Biz)
	candidate.Nickname = strings.TrimSpace(candidate.Nickname)
	if candidate.Biz == "" {
		return wcplus.GzhCandidate{}, fmt.Errorf("resolved account missing biz")
	}
	if candidate.Nickname != "" {
		return candidate, nil
	}
	return s.findCandidateByBiz(ctx, candidate.Biz)
}

func (s *ImportService) findCandidateByBiz(ctx context.Context, biz string) (wcplus.GzhCandidate, error) {
	raw, err := s.wc.SearchGzhCandidates(ctx, biz)
	if err != nil {
		return wcplus.GzhCandidate{}, err
	}
	candidates, err := wcplus.ParseSearchCandidates(raw)
	if err != nil {
		return wcplus.GzhCandidate{}, err
	}
	candidate, ok := wcplus.MatchBizExact(candidates, biz)
	if !ok || strings.TrimSpace(candidate.Nickname) == "" {
		return wcplus.GzhCandidate{}, fmt.Errorf("could not resolve nickname for biz %q; provide nickname with biz", biz)
	}
	return candidate, nil
}

func (s *ImportService) findImportedByNickname(ctx context.Context, nickname string) (wcplus.GzhCandidate, bool) {
	want := strings.TrimSpace(nickname)
	for offset := 0; offset < 2000; offset += 100 {
		page, err := s.wc.ListGzh(ctx, offset, 100, "updated_at", "desc")
		if err != nil {
			return wcplus.GzhCandidate{}, false
		}
		for _, g := range page.Gzhs {
			if strings.TrimSpace(g.Nickname) == want && strings.TrimSpace(g.Biz) != "" {
				return wcplus.GzhSummaryToCandidate(g), true
			}
		}
		if len(page.Gzhs) < 100 {
			break
		}
	}
	return wcplus.GzhCandidate{}, false
}

func (s *ImportService) findByBiz(ctx context.Context, biz string) (wcplus.GzhCandidate, bool) {
	want := strings.TrimSpace(biz)
	for offset := 0; offset < 2000; offset += 100 {
		page, err := s.wc.ListGzh(ctx, offset, 100, "", "")
		if err != nil {
			return wcplus.GzhCandidate{}, false
		}
		for _, g := range page.Gzhs {
			if strings.TrimSpace(g.Biz) == want {
				return wcplus.GzhSummaryToCandidate(g), true
			}
		}
		if len(page.Gzhs) < 100 {
			break
		}
	}
	return wcplus.GzhCandidate{}, false
}

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gkx/wcplus/internal/wcplus"
)

func isWechatArticleLink(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	return strings.Contains(raw, "mp.weixin.qq.com/s/") ||
		strings.Contains(raw, "mp.weixin.qq.com/s?")
}

// ImportByArticleLink uses wcplus native「文章链接导入」when link is mp.weixin.qq.com/s/...
func (s *ImportService) ImportByArticleLink(ctx context.Context, articleLink string, runQueue bool) (ImportAccountResult, error) {
	articleLink = strings.TrimSpace(articleLink)
	if !isWechatArticleLink(articleLink) {
		return ImportAccountResult{}, fmt.Errorf("articleLink must be mp.weixin.qq.com/s/... ")
	}
	raw, err := s.wc.ImportByArticleURL(ctx, articleLink)
	if err != nil {
		return ImportAccountResult{}, err
	}
	candidate, err := parseImportArticleResponse(raw)
	if err != nil {
		return ImportAccountResult{}, err
	}
	candidate, err = s.completeCandidate(ctx, candidate)
	if err != nil {
		return ImportAccountResult{}, err
	}
	linkTask, queue, reused, err := s.createLinkTaskAndRun(ctx, candidate, runQueue)
	if err != nil {
		return ImportAccountResult{Source: "article_link_import", Candidate: candidate, LinkTask: linkTask}, err
	}
	out := ImportAccountResult{
		Source:     "article_link_import",
		Candidate:  candidate,
		LinkTask:   linkTask,
		TaskReused: reused,
		Queue:      queue,
	}
	InvalidateGzhBizCache(candidate.Biz)
	return out, nil
}

func parseImportArticleResponse(raw json.RawMessage) (wcplus.GzhCandidate, error) {
	if list, err := wcplus.ParseSearchCandidates(raw); err == nil && len(list) > 0 {
		return list[0], nil
	}
	var wrap struct {
		Gzh      wcplus.GzhSummary `json:"Gzh"`
		Data     wcplus.GzhSummary `json:"data"`
		Biz      string            `json:"biz"`
		Nickname string            `json:"nickname"`
		Status   string            `json:"status"`
		Msg      string            `json:"msg"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return wcplus.GzhCandidate{}, fmt.Errorf("decode wcplus import response: %w", err)
	}
	if st := strings.ToLower(strings.TrimSpace(wrap.Status)); st == "error" && wrap.Msg != "" {
		return wcplus.GzhCandidate{}, fmt.Errorf("wcplus: %s", wrap.Msg)
	}
	if strings.TrimSpace(wrap.Gzh.Biz) != "" {
		return wcplus.GzhSummaryToCandidate(wrap.Gzh), nil
	}
	if strings.TrimSpace(wrap.Data.Biz) != "" {
		return wcplus.GzhSummaryToCandidate(wrap.Data), nil
	}
	if wrap.Biz != "" {
		return wcplus.GzhCandidate{Biz: wrap.Biz, Nickname: wrap.Nickname}, nil
	}
	return wcplus.GzhCandidate{}, fmt.Errorf("no gzh in wcplus import response")
}

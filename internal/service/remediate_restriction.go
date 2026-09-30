package service

import (
	"context"
	"fmt"
	"strings"
)

// RemediateContentRestrictionRequest starts login/prepare flow for wcplus「正文受限」.
type RemediateContentRestrictionRequest struct {
	Biz         string `json:"biz"`
	ArticleURL  string `json:"articleURL,omitempty"`
}

type RemediateContentRestrictionResult struct {
	Prepare             LoginPrepareResult `json:"prepare"`
	RecommendedArticleURL string           `json:"recommendedArticleURL,omitempty"`
	RemediationSteps    []string           `json:"remediationSteps"`
}

type RemediateService struct {
	export *ExportService
	login  *LoginSessionService
}

func NewRemediateService(export *ExportService, login *LoginSessionService) *RemediateService {
	return &RemediateService{export: export, login: login}
}

func (s *RemediateService) Remediate(ctx context.Context, req RemediateContentRestrictionRequest) (RemediateContentRestrictionResult, error) {
	articleURL := strings.TrimSpace(req.ArticleURL)
	biz := strings.TrimSpace(req.Biz)
	if articleURL == "" && biz != "" {
		res, err := s.export.ExportLatest(ctx, ExportLatestRequest{
			Biz: biz, Limit: 3, WithContent: false,
		})
		if err == nil {
			articleURL = firstArticleLink(res.Articles)
		}
	}
	if articleURL == "" {
		return RemediateContentRestrictionResult{}, fmt.Errorf("articleURL or biz with listed articles is required")
	}
	prep, err := s.login.Prepare(ctx, LoginPrepareRequest{ArticleURL: articleURL})
	if err != nil {
		return RemediateContentRestrictionResult{}, err
	}
	steps := ContentRestrictionRemediationSteps(articleURL)
	return RemediateContentRestrictionResult{
		Prepare:               prep,
		RecommendedArticleURL: articleURL,
		RemediationSteps:      steps,
	}, nil
}

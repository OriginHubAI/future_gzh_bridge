package service

import (
	"context"
	"fmt"
	"strings"

	"gkx/wcplus/internal/wcplus"
)

type ExportLatestRequest struct {
	Biz         string `json:"biz"`
	Nickname    string `json:"nickname"`
	Limit       int    `json:"limit"`
	WithContent bool   `json:"withContent"`
}

// ArticleExportItem is the ADP-facing article row (query by biz).
type ArticleExportItem struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Link     string `json:"link"`
	Time     int64  `json:"time"`
	ImageURL string `json:"imageUrl"`
	Content  string `json:"content"`
}

type ExportLatestResult struct {
	Biz                 string              `json:"biz"`
	OfficialAccountName string              `json:"officialAccountName,omitempty"`
	Gzh                 *wcplus.GzhSummary  `json:"gzh,omitempty"`
	Total               int                 `json:"total"`
	Articles            []ArticleExportItem `json:"articles"`
}

type ExportService struct {
	wc *wcplus.Client
}

func NewExportService(wc *wcplus.Client) *ExportService {
	return &ExportService{wc: wc}
}

func (s *ExportService) ExportLatest(ctx context.Context, req ExportLatestRequest) (ExportLatestResult, error) {
	biz := strings.TrimSpace(req.Biz)
	nickname := strings.TrimSpace(req.Nickname)
	if biz == "" {
		return ExportLatestResult{}, fmt.Errorf("biz is required")
	}
	if _, ok := FindImportedGzhByBizCached(ctx, s.wc, biz); !ok {
		return ExportLatestResult{}, &OfficialAccountNotFoundError{Biz: biz, Nickname: nickname}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	list, err := s.wc.ListGzhArticles(ctx, biz, 0, limit, "p_date", "desc")
	if err != nil {
		return ExportLatestResult{}, err
	}
	gzhAvatar := ""
	officialName := nickname
	if list.Gzh != nil {
		gzhAvatar = strings.TrimSpace(list.Gzh.Img)
		if officialName == "" {
			officialName = strings.TrimSpace(list.Gzh.Nickname)
		}
	}
	out := ExportLatestResult{
		Biz:                 biz,
		OfficialAccountName: officialName,
		Gzh:                 list.Gzh,
		Total:               list.Total,
	}
	if nickname == "" {
		nickname = officialName
	}
	var contentErrors []error
	for _, a := range list.Articles {
		contentHTML := ""
		if req.WithContent {
			nn := nickname
			if strings.TrimSpace(a.Nickname) != "" {
				nn = strings.TrimSpace(a.Nickname)
			}
			if nn != "" && strings.TrimSpace(a.ID) != "" {
				content, err := s.wc.GetArticleContent(ctx, nn, a.ID)
				if err == nil {
					contentHTML = content.Content
				} else {
					contentErrors = append(contentErrors, err)
				}
			}
		}
		name := strings.TrimSpace(a.Title)
		if name == "" {
			name = strings.TrimSpace(a.Nickname)
		}
		item := ArticleExportItem{
			ID:       strings.TrimSpace(a.ID),
			Name:     name,
			Link:     a.BestLink(),
			Time:     a.PDate,
			ImageURL: resolveArticleImage(a.CoverImage(), contentHTML, gzhAvatar),
			Content:  contentHTML,
		}
		out.Articles = append(out.Articles, item)
	}
	if detectWechatContentRestriction(req.WithContent, out.Articles, contentErrors) {
		link := firstArticleLink(out.Articles)
		return out, &WechatContentRestrictedError{
			Biz:                   biz,
			Nickname:              nickname,
			Partial:               out,
			RecommendedArticleURL: link,
			RemediationSteps:      ContentRestrictionRemediationSteps(link),
		}
	}
	return out, nil
}

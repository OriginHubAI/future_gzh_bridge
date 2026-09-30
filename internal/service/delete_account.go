package service

import (
	"context"
	"fmt"
	"strings"

	"gkx/wcplus/internal/wcplus"
)

type DeleteAccountRequest struct {
	Biz      string `json:"biz"`
	Nickname string `json:"nickname"`
}

type DeleteAccountResult struct {
	Biz      string `json:"biz"`
	Nickname string `json:"nickname"`
}

type DeleteService struct {
	wc *wcplus.Client
}

func NewDeleteService(wc *wcplus.Client) *DeleteService {
	return &DeleteService{wc: wc}
}

func (s *DeleteService) Delete(ctx context.Context, req DeleteAccountRequest) (DeleteAccountResult, error) {
	biz := strings.TrimSpace(req.Biz)
	nickname := strings.TrimSpace(req.Nickname)
	if biz == "" {
		return DeleteAccountResult{}, fmt.Errorf("biz is required")
	}
	candidate, found := FindImportedGzhByBizCached(ctx, s.wc, biz)
	if !found {
		return DeleteAccountResult{}, &OfficialAccountNotFoundError{Biz: biz, Nickname: nickname}
	}
	if nickname == "" {
		nickname = strings.TrimSpace(candidate.Nickname)
	}
	if err := s.wc.DeleteGzh(ctx, biz, nickname); err != nil {
		return DeleteAccountResult{}, err
	}
	InvalidateGzhBizCache(biz)
	return DeleteAccountResult{Biz: biz, Nickname: nickname}, nil
}

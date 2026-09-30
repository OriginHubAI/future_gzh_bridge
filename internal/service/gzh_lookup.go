package service

import (
	"context"
	"strings"

	"gkx/wcplus/internal/wcplus"
)

// FindImportedGzhByBiz returns a gzh already stored in wcplus (paged list scan).
func FindImportedGzhByBiz(ctx context.Context, wc *wcplus.Client, biz string) (wcplus.GzhCandidate, bool) {
	want := strings.TrimSpace(biz)
	if want == "" {
		return wcplus.GzhCandidate{}, false
	}
	for offset := 0; offset < 2000; offset += 100 {
		page, err := wc.ListGzh(ctx, offset, 100, "", "")
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

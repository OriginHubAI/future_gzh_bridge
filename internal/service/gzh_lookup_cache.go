package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"gkx/wcplus/internal/wcplus"
)

const gzhBizCacheTTL = 2 * time.Minute

type gzhBizCacheEntry struct {
	found     bool
	candidate wcplus.GzhCandidate
	expires   time.Time
}

var gzhBizCache sync.Map // key: biz string

func FindImportedGzhByBizCached(ctx context.Context, wc *wcplus.Client, biz string) (wcplus.GzhCandidate, bool) {
	biz = strings.TrimSpace(biz)
	if biz == "" {
		return wcplus.GzhCandidate{}, false
	}
	if v, ok := gzhBizCache.Load(biz); ok {
		e := v.(gzhBizCacheEntry)
		if time.Now().Before(e.expires) {
			if !e.found {
				return wcplus.GzhCandidate{}, false
			}
			return e.candidate, true
		}
		gzhBizCache.Delete(biz)
	}
	c, found := FindImportedGzhByBiz(ctx, wc, biz)
	gzhBizCache.Store(biz, gzhBizCacheEntry{
		found:     found,
		candidate: c,
		expires:   time.Now().Add(gzhBizCacheTTL),
	})
	return c, found
}

// InvalidateGzhBizCache clears cached biz lookup after import.
func InvalidateGzhBizCache(biz string) {
	biz = strings.TrimSpace(biz)
	if biz != "" {
		gzhBizCache.Delete(biz)
	}
}

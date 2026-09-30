package watchdog

import (
	"context"
	"log"
	"time"

	"gkx/wcplus/internal/alert"
	"gkx/wcplus/internal/service"
)

// Run polls collector readiness and notifies ADP when manual intervention is needed (edge: ok -> not ok).
func Run(ctx context.Context, interval time.Duration, status *service.StatusService, notify *alert.Notifier) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	wasOK := true
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			st, err := status.ADPConnectivity(ctx)
			if err != nil {
				log.Printf("[watchdog] status error: %v", err)
				continue
			}
			if st.OK {
				wasOK = true
				continue
			}
			if wasOK && notify != nil {
				notify.Trigger(ctx, "collector.manual_required", st.Message, map[string]any{
					"maxActive": st.MaxActive, "wechatReady": st.WechatReady,
				})
			}
			wasOK = false
		}
	}
}

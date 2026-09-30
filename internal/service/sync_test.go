package service_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gkx/wcplus/internal/service"
	"gkx/wcplus/internal/wcplus"
)

func TestSyncHonorsExplicitRunQueueFalse(t *testing.T) {
	for _, tc := range []struct {
		name         string
		runQueue     *bool
		wantControls int
	}{
		{name: "omitted defaults to true", wantControls: 1},
		{name: "explicit false", runQueue: boolPtr(false), wantControls: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			controls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/task/new":
					writeJSON(t, w, map[string]any{"status": "ok", "task_id": 1})
				case "/api/task/control":
					controls++
					writeJSON(t, w, map[string]any{"status": "ok"})
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(srv.Close)

			svc := service.NewSyncService(wcplus.NewClient(srv.URL, 5*time.Second))
			_, err := svc.Start(context.Background(), service.SyncStartRequest{
				Biz: "MzDemo==", Nickname: "demo", Steps: []string{service.StepLink}, RunQueue: tc.runQueue,
			})
			if err != nil {
				t.Fatal(err)
			}
			if controls != tc.wantControls {
				t.Fatalf("controls=%d, want %d", controls, tc.wantControls)
			}
		})
	}
}

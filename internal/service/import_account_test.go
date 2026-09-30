package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gkx/wcplus/internal/service"
	"gkx/wcplus/internal/wcplus"
)

func boolPtr(value bool) *bool { return &value }

func TestImportAccountRequestPreservesExplicitFalse(t *testing.T) {
	var req service.ImportAccountRequest
	if err := json.Unmarshal([]byte(`{"nickname":"demo","runQueue":false}`), &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if req.RunQueue == nil || *req.RunQueue {
		t.Fatalf("runQueue=%v, want explicit false", req.RunQueue)
	}
}

func TestImportHonorsRunQueue(t *testing.T) {
	for _, tc := range []struct {
		name         string
		runQueue     *bool
		wantControls int
	}{
		{name: "default true", wantControls: 1},
		{name: "explicit false", runQueue: boolPtr(false), wantControls: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var newTasks, controls int
			srv := newImportTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/gzh/list":
					writeJSON(t, w, map[string]any{"Gzhs": []any{}, "Total": 0})
				case "/api/gzh/search":
					writeJSON(t, w, []map[string]any{{"Biz": "MzDemo==", "Nickname": "demo"}})
				case "/api/task/all":
					writeJSON(t, w, map[string]any{"Tasks": []any{}})
				case "/api/task/new":
					newTasks++
					writeJSON(t, w, map[string]any{"status": "ok", "task_id": 123})
				case "/api/task/control":
					controls++
					writeJSON(t, w, map[string]any{"status": "ok"})
				default:
					http.NotFound(w, r)
				}
			})

			svc := service.NewImportService(wcplus.NewClient(srv.URL, 5*time.Second))
			out, err := svc.ImportByNickname(context.Background(), service.ImportAccountRequest{
				Nickname: "demo",
				RunQueue: tc.runQueue,
			})
			if err != nil {
				t.Fatalf("ImportByNickname: %v", err)
			}
			if out.TaskReused {
				t.Fatal("new task unexpectedly marked as reused")
			}
			if newTasks != 1 || controls != tc.wantControls {
				t.Fatalf("newTasks=%d controls=%d, want 1/%d", newTasks, controls, tc.wantControls)
			}
		})
	}
}

func TestImportArticleLinkReusesUnfinishedTask(t *testing.T) {
	var newTasks, controls int
	srv := newImportTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/gzh/import_by_article_url":
			writeJSON(t, w, map[string]any{"biz": "MzDemo==", "nickname": "demo"})
		case "/api/task/all":
			writeJSON(t, w, map[string]any{"Tasks": []map[string]any{{
				"ID": 55, "Biz": "MzDemo==", "Nickname": "demo", "CrawlerType": "gzh_article_link", "Status": "queued",
			}}})
		case "/api/task/new":
			newTasks++
			writeJSON(t, w, map[string]any{"status": "ok", "task_id": 99})
		case "/api/task/control":
			controls++
			writeJSON(t, w, map[string]any{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	})

	svc := service.NewImportService(wcplus.NewClient(srv.URL, 5*time.Second))
	out, err := svc.ImportByArticleLink(context.Background(), "https://mp.weixin.qq.com/s/demo", false)
	if err != nil {
		t.Fatalf("ImportByArticleLink: %v", err)
	}
	if !out.TaskReused || out.LinkTask.TaskID != 55 {
		t.Fatalf("expected reused task 55, got %+v", out)
	}
	if newTasks != 0 || controls != 0 {
		t.Fatalf("newTasks=%d controls=%d, want 0/0", newTasks, controls)
	}
}

func TestImportByBizResolvesNicknameBeforeCreatingTask(t *testing.T) {
	var taskBody map[string]any
	srv := newImportTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/gzh/list":
			writeJSON(t, w, map[string]any{"Gzhs": []any{}, "Total": 0})
		case "/api/search_gzh/search":
			writeJSON(t, w, []map[string]any{{"Biz": "MzDemo==", "Nickname": "demo"}})
		case "/api/task/all":
			writeJSON(t, w, map[string]any{"Tasks": []any{}})
		case "/api/task/new":
			if err := json.NewDecoder(r.Body).Decode(&taskBody); err != nil {
				t.Fatalf("decode task body: %v", err)
			}
			writeJSON(t, w, map[string]any{"status": "ok", "task_id": 123})
		default:
			http.NotFound(w, r)
		}
	})

	svc := service.NewImportService(wcplus.NewClient(srv.URL, 5*time.Second))
	_, err := svc.ImportByNickname(context.Background(), service.ImportAccountRequest{
		Biz:      "MzDemo==",
		RunQueue: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("ImportByNickname: %v", err)
	}
	if got, _ := taskBody["nickname"].(string); got != "demo" {
		t.Fatalf("task nickname=%q, want demo", got)
	}
}

func newImportTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

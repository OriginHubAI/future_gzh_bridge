package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gkx/wcplus/internal/service"
	"gkx/wcplus/internal/wcplus"
)

func TestDeleteService_RemovesImportedGzh(t *testing.T) {
	const wantBiz = "MzTestBiz=="
	var deleted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/gzh/list":
			if deleted {
				_ = json.NewEncoder(w).Encode(map[string]any{"Gzhs": []any{}, "Total": 0})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Gzhs": []map[string]any{
					{"Biz": wantBiz, "Nickname": "demo", "Img": "", "Articles": 1},
				},
				"Total": 1,
			})
		case "/api/gzh/delete":
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			deleted = true
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client := wcplus.NewClient(srv.URL, 5*time.Second)
	del := service.NewDeleteService(client)
	out, err := del.Delete(context.Background(), service.DeleteAccountRequest{Biz: wantBiz})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if out.Biz != wantBiz || out.Nickname != "demo" {
		t.Fatalf("unexpected result: %+v", out)
	}
	if !deleted {
		t.Fatal("expected wcplus delete to be called")
	}
}

func TestDeleteService_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"Gzhs": []any{}, "Total": 0})
	}))
	t.Cleanup(srv.Close)

	client := wcplus.NewClient(srv.URL, 5*time.Second)
	del := service.NewDeleteService(client)
	_, err := del.Delete(context.Background(), service.DeleteAccountRequest{Biz: "missing"})
	if err == nil {
		t.Fatal("expected error")
	}
	var nf *service.OfficialAccountNotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("expected OfficialAccountNotFoundError, got %T: %v", err, err)
	}
}

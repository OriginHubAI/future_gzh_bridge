package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gkx/wcplus/internal/simulatorstore"
	"gkx/wcplus/internal/wcplus"
)

func TestStandaloneImportExportWithoutMax(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 required")
	}
	dir := t.TempDir()
	helper := filepath.Join(dir, "helper.py")
	if err := os.WriteFile(helper, []byte(`import json,sys
r=json.load(sys.stdin)
print(json.dumps({"ok":True,"biz":"verified-biz","nickname":"Example","articleLink":"https://mp.weixin.qq.com/s/example-real-link","title":"A title","content":"<p>正文</p>","publishedAt":1234,"image":"https://mmbiz.qpic.cn/cover.jpg","avatar":"https://mmbiz.qpic.cn/avatar.jpg"}))
`), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := simulatorstore.Open(filepath.Join(dir, "store.json"))
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	newRouter := func(articleStore *simulatorstore.Store) *gin.Engine {
		r := gin.New()
		r.Use(StandaloneMiddleware(articleStore, python, []string{helper}, time.Second*5, nil))
		// Deliberately unreachable Max: no request below may depend on it.
		h := New(wcplus.NewClient("http://127.0.0.1:1", time.Second), nil, "")
		h.Register(r)
		return r
	}
	r := newRouter(store)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	if w := call("GET", "/health", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "wechat_control") {
		t.Fatal(w.Body.String())
	}
	if w := call("GET", "/v1/rpc/export-latest-articles?biz=missing", ""); w.Code != 404 {
		t.Fatal(w.Body.String())
	}
	w := call("POST", "/v1/rpc/import-official-account", `{"articleLink":"https://mp.weixin.qq.com/s/example-real-link"}`)
	var imported struct {
		Data struct {
			Candidate struct {
				Biz      string `json:"Biz"`
				Nickname string `json:"Nickname"`
				Img      string `json:"Img"`
			} `json:"candidate"`
		} `json:"data"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &imported) != nil || imported.Data.Candidate.Biz != "verified-biz" || imported.Data.Candidate.Nickname != "Example" || imported.Data.Candidate.Img != "https://mmbiz.qpic.cn/avatar.jpg" {
		t.Fatal(w.Body.String())
	}
	checkExport := func(withContent bool) string {
		t.Helper()
		path := "/v1/rpc/export-latest-articles?biz=verified-biz"
		wantContent := "<p>正文</p>"
		if !withContent {
			path += "&withContent=false"
			wantContent = ""
		}
		w := call("GET", path, "")
		var payload struct {
			Data struct {
				Biz                 string `json:"biz"`
				OfficialAccountName string `json:"officialAccountName"`
				Avatar              string `json:"avatar"`
				Total               int    `json:"total"`
				Articles            []struct {
					ID       string `json:"id"`
					Name     string `json:"name"`
					Link     string `json:"link"`
					Time     int64  `json:"time"`
					ImageURL string `json:"imageUrl"`
					Content  string `json:"content"`
				} `json:"articles"`
			} `json:"data"`
		}
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &payload) != nil || payload.Data.Biz != "verified-biz" || payload.Data.OfficialAccountName != "Example" || payload.Data.Avatar != "https://mmbiz.qpic.cn/avatar.jpg" || payload.Data.Total != 1 || len(payload.Data.Articles) != 1 {
			t.Fatal(w.Body.String())
		}
		article := payload.Data.Articles[0]
		if article.ID == "" || article.Name != "A title" || article.Link != "https://mp.weixin.qq.com/s/example-real-link" || article.Time != 1234 || article.ImageURL != "https://mmbiz.qpic.cn/cover.jpg" || article.Content != wantContent {
			t.Fatal(w.Body.String())
		}
		return article.ID
	}
	articleID := checkExport(true)
	if w := call("POST", "/v1/rpc/import-official-account", `{"articleLink":"https://mp.weixin.qq.com/s/example-real-link"}`); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if w := call("POST", "/v1/rpc/sync-official-account", `{"biz":"verified-biz"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	reopened, err := simulatorstore.Open(filepath.Join(dir, "store.json"))
	if err != nil {
		t.Fatal(err)
	}
	account, rows, err := reopened.Latest("verified-biz", 10)
	if err != nil || len(rows) != 1 || account.Avatar != "https://mmbiz.qpic.cn/avatar.jpg" {
		t.Fatalf("dedup/persistence failed: account=%+v rows=%+v err=%v", account, rows, err)
	}
	// Serve from the reopened store so the export checks cannot pass using only memory.
	r = newRouter(reopened)
	if gotID := checkExport(true); gotID != articleID {
		t.Fatalf("article ID changed after repeated import and reopen: got %q want %q", gotID, articleID)
	}
	if gotID := checkExport(false); gotID != articleID {
		t.Fatalf("article ID changed when omitting content: got %q want %q", gotID, articleID)
	}
	if w := call("POST", "/v1/rpc/login/prepare", `{}`); w.Code != 400 {
		t.Fatal("Max-only route escaped standalone middleware")
	}
	if w := call("POST", "/v1/rpc/delete-official-account", `{"biz":"verified-biz"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := call("GET", "/v1/rpc/export-latest-articles?biz=verified-biz", ""); w.Code != 404 {
		t.Fatal(w.Body.String())
	}
}

func TestStandaloneSaveArticleLinkAcceptsFetcherJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := simulatorstore.Open(filepath.Join(t.TempDir(), "store.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(StandaloneMiddleware(store, "unused", nil, time.Second, nil))
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	raw := `{"biz":"MzA==","account":"量子位","final_url":"https://mp.weixin.qq.com/s/fetched-json","title":"解析标题","publish_time":"2026-09-29 17:01:37","cover":"https://mmbiz.qpic.cn/cover.jpg","avatar":"https://mmbiz.qpic.cn/avatar.jpg","content_text":"正文文本"}`
	if w := call(http.MethodPost, "/v1/rpc/save-article-link", raw); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"source":"external"`) {
		t.Fatalf("save fetcher JSON: %d %s", w.Code, w.Body.String())
	}
	w := call(http.MethodGet, "/v1/rpc/export-latest-articles?biz=MzA==&limit=10", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"name":"解析标题"`) || !strings.Contains(w.Body.String(), `"imageUrl":"https://mmbiz.qpic.cn/cover.jpg"`) || !strings.Contains(w.Body.String(), "正文文本") || !strings.Contains(w.Body.String(), `"avatar":"https://mmbiz.qpic.cn/avatar.jpg"`) {
		t.Fatalf("export fetcher JSON: %d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodPost, "/v1/rpc/save-article-link", strings.Replace(raw, "content_text", "content_html", 1)); w.Code != http.StatusOK {
		t.Fatalf("repeat save fetcher JSON: %d %s", w.Code, w.Body.String())
	}
	_, rows, err := store.Latest("MzA==", 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("repeat save created duplicate: rows=%d err=%v", len(rows), err)
	}
}

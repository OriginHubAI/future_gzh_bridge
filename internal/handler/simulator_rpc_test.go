package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"gkx/wcplus/internal/simulator"
	"gkx/wcplus/internal/simulatorstore"
	"gkx/wcplus/internal/wcplus"
)

type simulatorDriverFunc func(context.Context, simulator.Account) (string, error)

func (f simulatorDriverFunc) FetchLatest(ctx context.Context, account simulator.Account) (string, error) {
	return f(ctx, account)
}

func TestLatestArticleLinkJobEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := New(wcplus.NewClient("http://127.0.0.1:5001", time.Second), nil, "http://127.0.0.1:5001")
	h.ConfigureSimulator(simulator.Config{
		Enabled: true,
		Driver: simulatorDriverFunc(func(context.Context, simulator.Account) (string, error) {
			return "https://mp.weixin.qq.com/s/latest", nil
		}),
		DefaultRetries: 0,
		RetryDelay:     time.Millisecond,
	})
	r := gin.New()
	h.Register(r)

	request := httptest.NewRequest(http.MethodPost, "/v1/rpc/latest-article-link-jobs", strings.NewReader(`{
      "accounts": [{"id":"a","biz":"MzA==","initialLink":"https://mp.weixin.qq.com/mp/profile_ext?action=home"}]
    }`))
	request.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	r.ServeHTTP(writer, request)
	if writer.Code != http.StatusAccepted {
		t.Fatalf("create status = %d: %s", writer.Code, writer.Body.String())
	}
	var create struct {
		Data struct {
			JobID string `json:"jobId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(writer.Body.Bytes(), &create); err != nil {
		t.Fatal(err)
	}
	if create.Data.JobID == "" {
		t.Fatalf("missing jobId: %s", writer.Body.String())
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		statusRequest := httptest.NewRequest(http.MethodGet, "/v1/rpc/latest-article-link-jobs/"+create.Data.JobID, nil)
		statusWriter := httptest.NewRecorder()
		r.ServeHTTP(statusWriter, statusRequest)
		if strings.Contains(statusWriter.Body.String(), `"state":"completed"`) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	linksRequest := httptest.NewRequest(http.MethodGet, "/v1/rpc/latest-article-links?jobId="+create.Data.JobID, nil)
	linksWriter := httptest.NewRecorder()
	r.ServeHTTP(linksWriter, linksRequest)
	if linksWriter.Code != http.StatusOK {
		t.Fatalf("links status = %d: %s", linksWriter.Code, linksWriter.Body.String())
	}
	if !strings.Contains(linksWriter.Body.String(), `"articleLink":"https://mp.weixin.qq.com/s/latest"`) {
		t.Fatalf("links body = %s", linksWriter.Body.String())
	}
}

type wechatLoginDriver struct {
	status simulator.WeChatLoginStatus
}

func (d wechatLoginDriver) FetchLatest(context.Context, simulator.Account) (string, error) {
	return "", nil
}

func (d wechatLoginDriver) WeChatLoginStatus(context.Context) (simulator.WeChatLoginStatus, error) {
	return d.status, nil
}

func (d wechatLoginDriver) Ready() bool { return true }

func TestWeChatLoginStatusEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := New(wcplus.NewClient("http://127.0.0.1:5001", time.Second), nil, "http://127.0.0.1:5001")
	r := gin.New()
	h.Register(r)

	disabled := httptest.NewRecorder()
	r.ServeHTTP(disabled, httptest.NewRequest(http.MethodGet, "/v1/rpc/wechat-login-status", nil))
	if disabled.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled status = %d: %s", disabled.Code, disabled.Body.String())
	}

	h.ConfigureSimulator(simulator.Config{
		Enabled: true,
		Driver: wechatLoginDriver{status: simulator.WeChatLoginStatus{
			Status: "logged_in", LoggedIn: true, WindowTitle: "微信", Message: "微信已登录",
		}},
	})
	enabled := httptest.NewRecorder()
	r.ServeHTTP(enabled, httptest.NewRequest(http.MethodGet, "/v1/rpc/wechat-login-status", nil))
	if enabled.Code != http.StatusOK || !strings.Contains(enabled.Body.String(), `"status":"logged_in"`) || !strings.Contains(enabled.Body.String(), `"windowTitle":"微信"`) {
		t.Fatalf("enabled status = %d: %s", enabled.Code, enabled.Body.String())
	}
}

func TestSaveArticleLinkWritesWarehouse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := New(wcplus.NewClient("http://127.0.0.1:1", 50*time.Millisecond), nil, "")
	store, err := simulatorstore.Open(filepath.Join(t.TempDir(), "store.json"))
	if err != nil {
		t.Fatal(err)
	}
	h.ConfigureSimulatorStore(store)
	r := gin.New()
	h.Register(r)

	request := httptest.NewRequest(http.MethodPost, "/v1/rpc/save-article-link", strings.NewReader(`{
      "biz":"MzA==","nickname":"量子位","articleLink":"https://mp.weixin.qq.com/s/abc"
    }`))
	request.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	r.ServeHTTP(writer, request)
	if writer.Code != http.StatusOK || !strings.Contains(writer.Body.String(), `"source":"external"`) {
		t.Fatalf("save status = %d: %s", writer.Code, writer.Body.String())
	}

	exportReq := httptest.NewRequest(http.MethodGet, "/v1/rpc/export-latest-articles?biz=MzA==&limit=10", nil)
	exportWriter := httptest.NewRecorder()
	r.ServeHTTP(exportWriter, exportReq)
	if exportWriter.Code != http.StatusOK || !strings.Contains(exportWriter.Body.String(), "https://mp.weixin.qq.com/s/abc") || !strings.Contains(exportWriter.Body.String(), `"source":"warehouse"`) {
		t.Fatalf("export status = %d: %s", exportWriter.Code, exportWriter.Body.String())
	}
}

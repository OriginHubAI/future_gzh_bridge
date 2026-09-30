package simulator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeDriver struct {
	mu        sync.Mutex
	responses map[string]fakeResponse
	calls     []string
}

type fakeResponse struct {
	link string
	err  error
}

func (d *fakeDriver) FetchLatest(_ context.Context, account Account) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, account.ID)
	response := d.responses[account.ID]
	return response.link, response.err
}

func TestManagerReturnsOneNewLinkAndSkipsDuplicates(t *testing.T) {
	driver := &fakeDriver{responses: map[string]fakeResponse{
		"a": {link: "https://mp.weixin.qq.com/s/a#ignored"},
		"b": {link: "https://mp.weixin.qq.com/s/existing"},
		"c": {link: "https://mp.weixin.qq.com/s/a"},
	}}
	m := NewManager(Config{Enabled: true, Driver: driver, DefaultRetries: 0, RetryDelay: time.Millisecond})
	job, err := m.Create(context.Background(), CreateJobRequest{
		Accounts: []Account{
			{ID: "a", InitialLink: "https://mp.weixin.qq.com/mp/profile_ext?action=home"},
			{ID: "b", InitialLink: "https://mp.weixin.qq.com/mp/profile_ext?action=home"},
			{ID: "c", InitialLink: "https://mp.weixin.qq.com/mp/profile_ext?action=home"},
		},
		ExistingLinks: []string{"https://mp.weixin.qq.com/s/existing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForJob(t, m, job.ID, func(j Job) bool { return j.State == JobCompleted })
	if finished.Success != 1 || finished.Duplicate != 2 || finished.Failed != 0 {
		t.Fatalf("counts = success:%d duplicate:%d failed:%d, want 1/2/0", finished.Success, finished.Duplicate, finished.Failed)
	}
	if len(finished.Results) != 3 || finished.Results[0].ArticleLink != "https://mp.weixin.qq.com/s/a" {
		t.Fatalf("results = %#v", finished.Results)
	}
	driver.mu.Lock()
	calls := append([]string(nil), driver.calls...)
	driver.mu.Unlock()
	if strings.Join(calls, ",") != "a,b,c" {
		t.Fatalf("accounts were not processed serially: %v", calls)
	}
}

func TestManagerPausesForManualInterventionAndResumes(t *testing.T) {
	var calls int
	driver := driverFunc(func(_ context.Context, account Account) (string, error) {
		calls++
		if calls == 1 {
			return "", &ManualInterventionError{Code: "login_required", Message: "微信需要重新登录"}
		}
		return "https://mp.weixin.qq.com/s/after-login", nil
	})
	m := NewManager(Config{Enabled: true, Driver: driver, DefaultRetries: 0, RetryDelay: time.Millisecond})
	job, err := m.Create(context.Background(), CreateJobRequest{Accounts: []Account{{ID: "a", InitialLink: "https://mp.weixin.qq.com/mp/profile_ext?action=home"}}})
	if err != nil {
		t.Fatal(err)
	}
	paused := waitForJob(t, m, job.ID, func(j Job) bool { return j.State == JobPaused })
	if paused.ManualIntervention != 1 || len(paused.Results) != 1 || paused.Results[0].Status != ResultManualIntervention {
		t.Fatalf("paused job = %#v", paused)
	}
	if _, err := m.Resume(job.ID); err != nil {
		t.Fatal(err)
	}
	finished := waitForJob(t, m, job.ID, func(j Job) bool { return j.State == JobCompleted })
	if finished.Success != 1 || len(finished.Results) != 2 {
		t.Fatalf("resumed job = %#v", finished)
	}
	if finished.Results[1].ArticleLink != "https://mp.weixin.qq.com/s/after-login" {
		t.Fatalf("link = %q", finished.Results[1].ArticleLink)
	}
}

func TestManagerRetriesNonManualFailure(t *testing.T) {
	attempt := 0
	driver := driverFunc(func(_ context.Context, _ Account) (string, error) {
		attempt++
		if attempt == 1 {
			return "", errors.New("window did not load")
		}
		return "https://mp.weixin.qq.com/s/retried", nil
	})
	m := NewManager(Config{Enabled: true, Driver: driver, DefaultRetries: 1, RetryDelay: time.Millisecond})
	job, err := m.Create(context.Background(), CreateJobRequest{Accounts: []Account{{ID: "a", InitialLink: "https://mp.weixin.qq.com/mp/profile_ext?action=home"}}})
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForJob(t, m, job.ID, func(j Job) bool { return j.State == JobCompleted })
	if finished.Success != 1 || finished.Results[0].Attempts != 2 {
		t.Fatalf("result = %#v", finished.Results[0])
	}
}

func TestValidateSeedInitialLink(t *testing.T) {
	if err := ValidateSeedInitialLink("https://mp.weixin.qq.com/s/AbCdEfGh123"); err != nil {
		t.Fatalf("valid slug: %v", err)
	}
	if err := ValidateSeedInitialLink("https://mp.weixin.qq.com/mp/profile_ext?action=home"); err != nil {
		t.Fatalf("profile seed: %v", err)
	}
	if err := ValidateSeedInitialLink("https://mp.weixin.qq.com/s?__biz=x&mid=1&idx=1&sn=0"); err == nil {
		t.Fatal("expected sn=0 to be rejected")
	}
	if err := ValidateSeedInitialLink("https://mp.weixin.qq.com/s?__biz=x&sn=short"); err == nil {
		t.Fatal("expected short sn to be rejected")
	}
}

func TestNormalizeLink(t *testing.T) {
	got, err := NormalizeLink(" https://mp.weixin.qq.com/s/x?a=1#frag ")
	if err != nil || got != "https://mp.weixin.qq.com/s/x?a=1" {
		t.Fatalf("NormalizeLink() = %q, %v", got, err)
	}
	if _, err := NormalizeLink("file:///tmp/x"); err == nil {
		t.Fatal("file URL should fail")
	}
	if _, err := NormalizeLink("https://mp.weixin.qq.com/mp/profile_ext?action=home"); err == nil {
		t.Fatal("profile URL must not be accepted as an article link")
	}
	if _, err := NormalizeLink("https://example.test/s/not-wechat"); err == nil {
		t.Fatal("non-WeChat URL must not be accepted as an article link")
	}
}

func TestEnvWithDefaultsPreservesProcessEnvironment(t *testing.T) {
	t.Setenv("WECHAT_SIM_TARGET_APP", "from-process")
	env := envWithDefaults([]string{
		"WECHAT_SIM_TARGET_APP=from-config",
		"WECHAT_SIM_LINK_MODE=direct",
	})
	values := make(map[string]string)
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	if values["WECHAT_SIM_TARGET_APP"] != "from-process" {
		t.Fatalf("process environment was overwritten: %q", values["WECHAT_SIM_TARGET_APP"])
	}
	if values["WECHAT_SIM_LINK_MODE"] != "direct" {
		t.Fatalf("config default was not added: %q", values["WECHAT_SIM_LINK_MODE"])
	}
}

func TestManagerPersistsSeenLinks(t *testing.T) {
	stateFile := t.TempDir() + "/state.json"
	first := NewManager(Config{
		Enabled: true,
		Driver: driverFunc(func(context.Context, Account) (string, error) {
			return "https://mp.weixin.qq.com/s/persisted", nil
		}),
		StateFile:      stateFile,
		DefaultRetries: 0,
		RetryDelay:     time.Millisecond,
	})
	job, err := first.Create(context.Background(), CreateJobRequest{Accounts: []Account{{ID: "a", InitialLink: "https://mp.weixin.qq.com/mp/profile_ext?action=home"}}})
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, first, job.ID, func(j Job) bool { return j.State == JobCompleted })

	second := NewManager(Config{
		Enabled: true,
		Driver: driverFunc(func(context.Context, Account) (string, error) {
			return "https://mp.weixin.qq.com/s/persisted", nil
		}),
		StateFile:      stateFile,
		DefaultRetries: 0,
		RetryDelay:     time.Millisecond,
	})
	job, err = second.Create(context.Background(), CreateJobRequest{Accounts: []Account{{ID: "b", InitialLink: "https://mp.weixin.qq.com/mp/profile_ext?action=home"}}})
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForJob(t, second, job.ID, func(j Job) bool { return j.State == JobCompleted })
	if finished.Duplicate != 1 || finished.Success != 0 {
		t.Fatalf("persisted dedupe = %#v", finished)
	}
}

func TestManagerRestoresRequestOptionsForResume(t *testing.T) {
	stateFile := t.TempDir() + "/state.json"
	account := Account{ID: "resume", InitialLink: "https://mp.weixin.qq.com/mp/profile_ext?action=home"}
	job := Job{
		ID:        "sim-restored",
		State:     JobPaused,
		CreatedAt: time.Now().UTC(),
		Total:     1,
		Accounts:  []Account{account},
		Results:   []Result{},
	}
	state := persistedState{
		Jobs: []Job{job},
		Requests: map[string]persistedRequest{
			job.ID: {
				Accounts:          []Account{account},
				MaxRetries:        2,
				AccountTimeoutSec: 7,
			},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFile, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	attempts := 0
	timeoutRestored := true
	m := NewManager(Config{
		Enabled:        true,
		StateFile:      stateFile,
		DefaultRetries: 0,
		RetryDelay:     time.Millisecond,
		Driver: driverFunc(func(ctx context.Context, _ Account) (string, error) {
			attempts++
			if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) < 6*time.Second {
				timeoutRestored = false
			}
			return "", errors.New("temporary UI failure")
		}),
	})
	if _, err := m.Resume(job.ID); err != nil {
		t.Fatal(err)
	}
	finished := waitForJob(t, m, job.ID, func(j Job) bool { return j.State == JobCompleted })
	if !timeoutRestored || attempts != 3 || finished.Results[0].Attempts != 3 {
		t.Fatalf("restored retry options were lost: attempts=%d result=%#v", attempts, finished.Results[0])
	}
}

type driverFunc func(context.Context, Account) (string, error)

func (f driverFunc) FetchLatest(ctx context.Context, account Account) (string, error) {
	return f(ctx, account)
}

func TestCommandDriverWeChatLoginStatus(t *testing.T) {
	script := filepath.Join(t.TempDir(), "helper.sh")
	body := `#!/bin/sh
if [ "$1" = "--wechat-status" ]; then
  printf '%s\n' '{"ok":true,"status":"logged_in","loggedIn":true,"windowTitle":"微信","message":"微信已登录"}'
  exit 0
fi
printf '%s\n' '{"ok":false,"code":"unexpected"}'
exit 1
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := (CommandDriver{Command: script}).WeChatLoginStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.LoggedIn || got.Status != "logged_in" || got.WindowTitle != "微信" || got.Message != "微信已登录" {
		t.Fatalf("status = %#v", got)
	}
}

func waitForJob(t *testing.T, m *Manager, id string, done func(Job) bool) Job {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := m.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if done(job) {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	job, _ := m.Get(id)
	t.Fatalf("job %s did not finish: %#v", id, job)
	return Job{}
}

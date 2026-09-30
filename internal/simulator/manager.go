// Package simulator implements the vendor-independent, UI-driven latest-link
// collection flow.  It deliberately knows nothing about WeChat internals:
// the Windows driver receives an account seed and returns one copied URL.
package simulator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	JobQueued    = "queued"
	JobRunning   = "running"
	JobPaused    = "paused"
	JobCompleted = "completed"
	JobStopped   = "stopped"
	JobFailed    = "failed"

	ResultSuccess            = "success"
	ResultDuplicate          = "duplicate_skipped"
	ResultFailed             = "failed"
	ResultManualIntervention = "manual_intervention"
)

var (
	ErrDisabled       = errors.New("simulator is disabled")
	ErrJobNotFound    = errors.New("latest-link job not found")
	ErrInvalidRequest = errors.New("invalid latest-link job request")
)

// Account is the minimum seed needed by a UI driver. InitialLink is kept
// explicitly because the simulator must not discover accounts by crawling
// historical data.
type Account struct {
	ID          string `json:"id,omitempty"`
	Biz         string `json:"biz,omitempty"`
	Nickname    string `json:"nickname,omitempty"`
	InitialLink string `json:"initialLink"`
}

// CreateJobRequest starts one serial batch. ExistingLinks can be populated
// from the business database; the manager also remembers links returned by
// earlier jobs when a state file is configured.
type CreateJobRequest struct {
	Accounts          []Account `json:"accounts"`
	ExistingLinks     []string  `json:"existingLinks,omitempty"`
	MaxRetries        int       `json:"maxRetries,omitempty"`
	AccountTimeoutSec int       `json:"accountTimeoutSec,omitempty"`
}

type Result struct {
	Account     Account   `json:"account"`
	ArticleLink string    `json:"articleLink,omitempty"`
	ID          string    `json:"id,omitempty"`
	Title       string    `json:"title,omitempty"`
	PublishedAt int64     `json:"publishedAt,omitempty"`
	Image       string    `json:"image,omitempty"`
	Avatar      string    `json:"avatar,omitempty"`
	Content     string    `json:"content,omitempty"`
	Status      string    `json:"status"`
	Attempts    int       `json:"attempts"`
	Error       string    `json:"error,omitempty"`
	StartedAt   time.Time `json:"startedAt,omitempty"`
	FinishedAt  time.Time `json:"finishedAt,omitempty"`
}

type Current struct {
	Index     int       `json:"index"`
	Total     int       `json:"total"`
	Account   Account   `json:"account"`
	Step      string    `json:"step"`
	StartedAt time.Time `json:"startedAt"`
}

// Job is a JSON-safe snapshot. The Accounts field is retained so a persisted
// job can be inspected or resumed after a process restart.
type Job struct {
	ID                 string    `json:"id"`
	State              string    `json:"state"`
	CreatedAt          time.Time `json:"createdAt"`
	StartedAt          time.Time `json:"startedAt,omitempty"`
	FinishedAt         time.Time `json:"finishedAt,omitempty"`
	Total              int       `json:"total"`
	Success            int       `json:"success"`
	Duplicate          int       `json:"duplicate"`
	Failed             int       `json:"failed"`
	ManualIntervention int       `json:"manualIntervention"`
	NextIndex          int       `json:"nextIndex"`
	Current            *Current  `json:"current,omitempty"`
	Accounts           []Account `json:"accounts,omitempty"`
	Results            []Result  `json:"results"`
	LastError          string    `json:"lastError,omitempty"`
}

// Snapshot is returned by status endpoints. It is an alias-like wrapper so
// future internal fields can be added without exposing cancellation handles.
type Snapshot = Job

// Driver is implemented by the Windows automation process. A driver must
// return one article URL and must not return article content.
type Driver interface {
	FetchLatest(ctx context.Context, account Account) (string, error)
}

type ArticleInfo struct {
	ID          string `json:"id,omitempty"`
	Title       string `json:"title,omitempty"`
	PublishedAt int64  `json:"publishedAt,omitempty"`
	Image       string `json:"image,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
	Content     string `json:"content,omitempty"`
}

type ArticleInfoDriver interface {
	FetchArticleInfo(ctx context.Context, account Account, articleLink string) (ArticleInfo, error)
}

// WeChatLoginStatus is the local WeChat client's window state. It is not the
// wcplus req_data session.
type WeChatLoginStatus struct {
	Status      string `json:"status"`
	LoggedIn    bool   `json:"loggedIn"`
	WindowTitle string `json:"windowTitle,omitempty"`
	Message     string `json:"message,omitempty"`
}

// LoginStatusDriver reads the desktop WeChat window. Drivers that only copy
// article links do not have to implement it.
type LoginStatusDriver interface {
	WeChatLoginStatus(ctx context.Context) (WeChatLoginStatus, error)
}

// ReadyDriver lets the status endpoint distinguish a registered adapter from
// an adapter whose executable has not yet been configured.
type ReadyDriver interface {
	Ready() bool
}

// ManualInterventionError tells the manager to pause the batch and surface a
// human action (login, captcha, window focus, etc.).
type ManualInterventionError struct {
	Code    string
	Message string
}

func (e *ManualInterventionError) Error() string {
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if strings.TrimSpace(e.Code) != "" {
		return e.Code
	}
	return "manual intervention required"
}

// DriverError is a structured non-manual driver failure.
type DriverError struct {
	Code    string
	Message string
}

func (e *DriverError) Error() string {
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	return e.Code
}

type ManualEvent struct {
	JobID   string
	Account Account
	Code    string
	Message string
}

type Config struct {
	Enabled              bool
	Driver               Driver
	StateFile            string
	MaxAccounts          int
	DefaultRetries       int
	DefaultTimeout       time.Duration
	RetryDelay           time.Duration
	OnManualIntervention func(ManualEvent)
	OnCompleted          func(Job)
}

type Manager struct {
	mu       sync.Mutex
	cfg      Config
	driver   Driver
	jobs     map[string]*jobRuntime
	seen     map[string]struct{}
	sequence uint64
}

type jobRuntime struct {
	snapshot Job
	request  CreateJobRequest
	cancel   context.CancelFunc
	wake     chan struct{}
	paused   bool
	stopped  bool
	running  bool
}

type persistedState struct {
	Jobs      []Job                       `json:"jobs"`
	SeenLinks []string                    `json:"seenLinks"`
	Requests  map[string]persistedRequest `json:"requests,omitempty"`
}

// persistedRequest keeps resume behavior stable across a bridge restart. It
// is separate from Job so the public status payload does not need to expose
// the original batch options.
type persistedRequest struct {
	Accounts          []Account `json:"accounts"`
	ExistingLinks     []string  `json:"existingLinks,omitempty"`
	MaxRetries        int       `json:"maxRetries,omitempty"`
	AccountTimeoutSec int       `json:"accountTimeoutSec,omitempty"`
}

func NewManager(cfg Config) *Manager {
	if cfg.MaxAccounts <= 0 {
		cfg.MaxAccounts = 2000
	}
	if cfg.DefaultRetries < 0 {
		cfg.DefaultRetries = 1
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 90 * time.Second
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = 500 * time.Millisecond
	}
	m := &Manager{cfg: cfg, driver: cfg.Driver, jobs: make(map[string]*jobRuntime), seen: make(map[string]struct{})}
	m.load()
	return m
}

func (m *Manager) Enabled() bool { return m != nil && m.cfg.Enabled }

func (m *Manager) Create(ctx context.Context, req CreateJobRequest) (Job, error) {
	if !m.Enabled() {
		return Job{}, ErrDisabled
	}
	if len(req.Accounts) == 0 {
		return Job{}, fmt.Errorf("%w: accounts is empty", ErrInvalidRequest)
	}
	if len(req.Accounts) > m.cfg.MaxAccounts {
		return Job{}, fmt.Errorf("%w: accounts exceeds max %d", ErrInvalidRequest, m.cfg.MaxAccounts)
	}
	for i := range req.Accounts {
		req.Accounts[i].InitialLink = strings.TrimSpace(req.Accounts[i].InitialLink)
		if req.Accounts[i].InitialLink == "" {
			return Job{}, fmt.Errorf("%w: accounts[%d].initialLink is required", ErrInvalidRequest, i)
		}
		if err := ValidateSeedInitialLink(req.Accounts[i].InitialLink); err != nil {
			return Job{}, fmt.Errorf("%w: accounts[%d].initialLink: %v", ErrInvalidRequest, i, err)
		}
	}
	req = m.effectiveRequest(req)

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, link := range req.ExistingLinks {
		if normalized, err := NormalizeLink(link); err == nil {
			m.seen[normalized] = struct{}{}
		}
	}
	var id string
	for {
		m.sequence++
		id = fmt.Sprintf("sim-%s-%06d", time.Now().UTC().Format("20060102-150405"), m.sequence)
		if _, exists := m.jobs[id]; !exists {
			break
		}
	}
	now := time.Now().UTC()
	j := &jobRuntime{
		snapshot: Job{ID: id, State: JobQueued, CreatedAt: now, Total: len(req.Accounts), Accounts: append([]Account(nil), req.Accounts...), Results: []Result{}},
		request:  cloneCreateJobRequest(req),
		wake:     make(chan struct{}),
	}
	m.jobs[id] = j
	m.persistLocked()
	go m.run(id, req)
	return cloneJob(j.snapshot), nil
}

func (m *Manager) effectiveRequest(req CreateJobRequest) CreateJobRequest {
	if req.MaxRetries <= 0 {
		req.MaxRetries = m.cfg.DefaultRetries
	}
	if req.MaxRetries > 5 {
		req.MaxRetries = 5
	}
	if req.AccountTimeoutSec <= 0 {
		req.AccountTimeoutSec = int(m.cfg.DefaultTimeout / time.Second)
	}
	if req.AccountTimeoutSec <= 0 {
		req.AccountTimeoutSec = 1
	}
	return cloneCreateJobRequest(req)
}

func (m *Manager) Get(id string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return Job{}, ErrJobNotFound
	}
	return cloneJob(j.snapshot), nil
}

// GetStatus is intentionally lightweight for frequent front-end polling.
// Call Get when the caller needs completed rows (for example the link export
// endpoint) rather than repeatedly transferring the full batch input/output.
func (m *Manager) GetStatus(id string) (Job, error) {
	out, err := m.Get(id)
	if err != nil {
		return Job{}, err
	}
	out.Accounts = nil
	out.Results = nil
	return out, nil
}

func (m *Manager) Pause(id string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return Job{}, ErrJobNotFound
	}
	if j.snapshot.State == JobCompleted || j.snapshot.State == JobStopped || j.snapshot.State == JobFailed {
		return cloneJob(j.snapshot), nil
	}
	j.paused = true
	j.snapshot.State = JobPaused
	m.signalLocked(j)
	m.persistLocked()
	return cloneJob(j.snapshot), nil
}

func (m *Manager) Resume(id string) (Job, error) {
	m.mu.Lock()
	j, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return Job{}, ErrJobNotFound
	}
	if j.snapshot.State == JobCompleted || j.snapshot.State == JobStopped {
		out := cloneJob(j.snapshot)
		m.mu.Unlock()
		return out, nil
	}
	wasRunning := j.running
	j.paused = false
	if j.snapshot.State == JobPaused || j.snapshot.State == JobFailed {
		j.snapshot.State = JobRunning
	}
	m.signalLocked(j)
	request := j.request
	if !wasRunning {
		go m.run(id, request)
	}
	m.persistLocked()
	out := cloneJob(j.snapshot)
	m.mu.Unlock()
	return out, nil
}

func (m *Manager) Stop(id string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return Job{}, ErrJobNotFound
	}
	if j.snapshot.State == JobCompleted || j.snapshot.State == JobStopped {
		return cloneJob(j.snapshot), nil
	}
	j.stopped = true
	if j.cancel != nil {
		j.cancel()
	}
	j.snapshot.State = JobStopped
	j.snapshot.FinishedAt = time.Now().UTC()
	m.signalLocked(j)
	m.persistLocked()
	return cloneJob(j.snapshot), nil
}

func (m *Manager) Summary() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	counts := map[string]int{}
	for _, j := range m.jobs {
		counts[j.snapshot.State]++
	}
	configured := m.driver != nil
	if ready, ok := m.driver.(ReadyDriver); ok {
		configured = ready.Ready()
	}
	return map[string]any{"enabled": m.Enabled(), "driverConfigured": configured, "jobs": len(m.jobs), "states": counts}
}

// WeChatLoginStatus asks the configured helper to read the local WeChat window.
func (m *Manager) WeChatLoginStatus(ctx context.Context) (WeChatLoginStatus, error) {
	if !m.Enabled() {
		return WeChatLoginStatus{}, ErrDisabled
	}
	probe, ok := m.driver.(LoginStatusDriver)
	if !ok {
		return WeChatLoginStatus{}, &DriverError{Code: "driver_not_configured", Message: "当前模拟器驱动不能读取微信窗口"}
	}
	if ready, ok := m.driver.(ReadyDriver); ok && !ready.Ready() {
		return WeChatLoginStatus{}, &DriverError{Code: "driver_not_configured", Message: "模拟取链 helper 未配置，无法读取微信窗口"}
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}
	return probe.WeChatLoginStatus(ctx)
}

func (m *Manager) run(id string, req CreateJobRequest) {
	m.mu.Lock()
	j, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	if j.running {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	j.cancel = cancel
	j.running = true
	if j.paused {
		j.snapshot.State = JobPaused
	} else {
		j.snapshot.State = JobRunning
	}
	if j.snapshot.StartedAt.IsZero() {
		j.snapshot.StartedAt = time.Now().UTC()
	}
	startIndex := j.snapshot.NextIndex
	m.persistLocked()
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		if current, ok := m.jobs[id]; ok {
			current.cancel = nil
			current.running = false
		}
		m.mu.Unlock()
	}()

	maxRetries := req.MaxRetries
	if maxRetries <= 0 {
		maxRetries = m.cfg.DefaultRetries
	}
	if maxRetries > 5 {
		maxRetries = 5
	}
	timeout := time.Duration(req.AccountTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = m.cfg.DefaultTimeout
	}

	if startIndex < 0 || startIndex > len(req.Accounts) {
		startIndex = 0
	}
	for index := startIndex; index < len(req.Accounts); {
		if !m.waitUntilRunnable(ctx, id) {
			return
		}
		account := req.Accounts[index]
		m.setCurrent(id, index, account, "opening")
		result := m.fetchOne(ctx, id, account, maxRetries, timeout)
		m.finishOne(id, index, result)
		if result.Status == ResultManualIntervention {
			m.pauseForManual(id, result)
			// Resume continues with the same account. The link has not been
			// obtained yet, so advancing would silently lose it.
			continue
		}
		index++
	}
	var completed Job
	var onCompleted func(Job)
	m.mu.Lock()
	if j, ok := m.jobs[id]; ok && !j.stopped && !j.paused {
		j.snapshot.State = JobCompleted
		j.snapshot.Current = nil
		j.snapshot.FinishedAt = time.Now().UTC()
		m.persistLocked()
		completed = cloneJob(j.snapshot)
		onCompleted = m.cfg.OnCompleted
	}
	m.mu.Unlock()
	if onCompleted != nil {
		go onCompleted(completed)
	}
}

func (m *Manager) waitUntilRunnable(ctx context.Context, id string) bool {
	for {
		m.mu.Lock()
		j, ok := m.jobs[id]
		if !ok || j.stopped {
			m.mu.Unlock()
			return false
		}
		if !j.paused {
			m.mu.Unlock()
			return true
		}
		wake := j.wake
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return false
		case <-wake:
		}
	}
}

func (m *Manager) fetchOne(parent context.Context, id string, account Account, maxRetries int, timeout time.Duration) Result {
	result := Result{Account: account, Status: ResultFailed, StartedAt: time.Now().UTC()}
	if m.driver == nil {
		result.Status = ResultManualIntervention
		result.Error = "simulator driver is not configured"
		result.FinishedAt = time.Now().UTC()
		return result
	}
	for attempt := 1; attempt <= maxRetries+1; attempt++ {
		result.Attempts = attempt
		m.setCurrentStep(id, "opening")
		ctx, cancel := context.WithTimeout(parent, timeout)
		link, err := m.driver.FetchLatest(ctx, account)
		cancel()
		if err == nil {
			m.setCurrentStep(id, "validating")
			normalized, normalizeErr := NormalizeLink(link)
			if normalizeErr == nil {
				result.ArticleLink = normalized
				if infoDriver, ok := m.driver.(ArticleInfoDriver); ok {
					m.setCurrentStep(id, "collecting_article_info")
					infoCtx, infoCancel := context.WithTimeout(parent, timeout)
					info, infoErr := infoDriver.FetchArticleInfo(infoCtx, account, normalized)
					infoCancel()
					if infoErr == nil {
						result.ID = info.ID
						result.Title = info.Title
						result.PublishedAt = info.PublishedAt
						result.Image = info.Image
						result.Avatar = info.Avatar
						result.Content = info.Content
					}
				}
				result.Status = ResultSuccess
				result.FinishedAt = time.Now().UTC()
				return result
			}
			err = normalizeErr
		}
		var manual *ManualInterventionError
		if errors.As(err, &manual) {
			result.Status = ResultManualIntervention
			result.Error = manual.Error()
			result.FinishedAt = time.Now().UTC()
			return result
		}
		result.Error = err.Error()
		if attempt <= maxRetries {
			m.setCurrentStep(id, "retrying")
			select {
			case <-parent.Done():
				result.Error = parent.Err().Error()
				result.FinishedAt = time.Now().UTC()
				return result
			case <-time.After(m.cfg.RetryDelay):
			}
		}
	}
	result.FinishedAt = time.Now().UTC()
	return result
}

func (m *Manager) finishOne(id string, index int, result Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok || j.stopped {
		return
	}
	if result.Status == ResultSuccess {
		if _, exists := m.seen[result.ArticleLink]; exists {
			result.Status = ResultDuplicate
			result.Error = "article link already exists"
			j.snapshot.Duplicate++
		} else {
			m.seen[result.ArticleLink] = struct{}{}
			j.snapshot.Success++
		}
	} else if result.Status == ResultManualIntervention {
		j.snapshot.ManualIntervention++
	} else {
		j.snapshot.Failed++
	}
	j.snapshot.Results = append(j.snapshot.Results, result)
	// A manual interruption means the current account has not produced a
	// usable link. Resume must retry that same account instead of silently
	// advancing to the next one.
	if result.Status == ResultManualIntervention {
		j.snapshot.NextIndex = index
	} else {
		j.snapshot.NextIndex = index + 1
	}
	j.snapshot.Current = nil
	m.persistLocked()
}

func (m *Manager) pauseForManual(id string, result Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok || j.stopped {
		return
	}
	j.paused = true
	j.snapshot.State = JobPaused
	j.snapshot.LastError = result.Error
	m.persistLocked()
	event := ManualEvent{JobID: id, Account: result.Account, Code: "manual_intervention", Message: result.Error}
	if m.cfg.OnManualIntervention != nil {
		// Do not hold the manager lock while calling an external notifier.
		callback := m.cfg.OnManualIntervention
		go callback(event)
	}
}

func (m *Manager) setCurrent(id string, index int, account Account, step string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[id]; ok {
		j.snapshot.Current = &Current{Index: index + 1, Total: j.snapshot.Total, Account: account, Step: step, StartedAt: time.Now().UTC()}
		m.persistLocked()
	}
}

func (m *Manager) setCurrentStep(id, step string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[id]; ok && j.snapshot.Current != nil {
		j.snapshot.Current.Step = step
		m.persistLocked()
	}
}

func (m *Manager) signalLocked(j *jobRuntime) {
	close(j.wake)
	j.wake = make(chan struct{})
}

func (m *Manager) load() {
	path := strings.TrimSpace(m.cfg.StateFile)
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var state persistedState
	if json.Unmarshal(raw, &state) != nil {
		return
	}
	for _, link := range state.SeenLinks {
		if normalized, err := NormalizeLink(link); err == nil {
			m.seen[normalized] = struct{}{}
		}
	}
	for _, snapshot := range state.Jobs {
		// A process cannot safely resume a GUI action after a crash. Keep the
		// completed results and expose the job as paused for explicit resume.
		if snapshot.State == JobRunning || snapshot.State == JobQueued {
			snapshot.State = JobPaused
			snapshot.LastError = "process restarted; resume this job explicitly"
		}
		request := CreateJobRequest{Accounts: append([]Account(nil), snapshot.Accounts...)}
		if saved, ok := state.Requests[snapshot.ID]; ok {
			request = CreateJobRequest{
				Accounts:          append([]Account(nil), saved.Accounts...),
				ExistingLinks:     append([]string(nil), saved.ExistingLinks...),
				MaxRetries:        saved.MaxRetries,
				AccountTimeoutSec: saved.AccountTimeoutSec,
			}
		}
		if len(request.Accounts) == 0 {
			request.Accounts = append([]Account(nil), snapshot.Accounts...)
		}
		m.jobs[snapshot.ID] = &jobRuntime{snapshot: snapshot, request: request, wake: make(chan struct{}), paused: snapshot.State == JobPaused}
	}
}

func (m *Manager) persistLocked() {
	path := strings.TrimSpace(m.cfg.StateFile)
	if path == "" {
		return
	}
	state := persistedState{SeenLinks: make([]string, 0, len(m.seen)), Requests: make(map[string]persistedRequest, len(m.jobs))}
	for link := range m.seen {
		state.SeenLinks = append(state.SeenLinks, link)
	}
	for _, j := range m.jobs {
		state.Jobs = append(state.Jobs, cloneJob(j.snapshot))
		state.Requests[j.snapshot.ID] = persistedRequest{
			Accounts:          append([]Account(nil), j.request.Accounts...),
			ExistingLinks:     append([]string(nil), j.request.ExistingLinks...),
			MaxRetries:        j.request.MaxRetries,
			AccountTimeoutSec: j.request.AccountTimeoutSec,
		}
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".latest-link-state-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return
	}
	if err = tmp.Close(); err != nil {
		return
	}
	_ = os.Rename(tmpName, path)
}

func cloneCreateJobRequest(in CreateJobRequest) CreateJobRequest {
	out := in
	out.Accounts = append([]Account(nil), in.Accounts...)
	out.ExistingLinks = append([]string(nil), in.ExistingLinks...)
	return out
}

func cloneJob(in Job) Job {
	out := in
	out.Accounts = append([]Account(nil), in.Accounts...)
	out.Results = append([]Result(nil), in.Results...)
	if in.Current != nil {
		current := *in.Current
		out.Current = &current
	}
	return out
}

// ValidateSeedInitialLink rejects placeholder article URLs before UI automation.
// Profile/home links remain valid seeds for account_to_article.
func ValidateSeedInitialLink(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("initialLink is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid initialLink: %q", raw)
	}
	if !strings.EqualFold(u.Hostname(), "mp.weixin.qq.com") {
		return fmt.Errorf("initialLink must be on mp.weixin.qq.com")
	}
	path := strings.TrimRight(strings.ToLower(u.Path), "/")
	switch {
	case path == "/s" || strings.HasPrefix(path, "/s/"):
		return validateArticleSeedLink(u)
	case strings.Contains(path, "profile_ext"):
		return nil
	default:
		return fmt.Errorf("initialLink must be a WeChat article (/s/...) or mp/profile_ext home link")
	}
}

func validateArticleSeedLink(u *url.URL) error {
	path := strings.TrimRight(strings.ToLower(u.Path), "/")
	if strings.HasPrefix(path, "/s/") {
		slug := strings.TrimPrefix(path, "/s/")
		if len(slug) < 4 {
			return fmt.Errorf("initialLink article slug is too short")
		}
		return nil
	}
	sn := strings.TrimSpace(u.Query().Get("sn"))
	if sn == "" || sn == "0" {
		return fmt.Errorf("initialLink has invalid sn (WeChat shows 参数错误); copy a real /s/ link from 复制链接")
	}
	if len(sn) < 8 {
		return fmt.Errorf("initialLink sn looks like a placeholder")
	}
	return nil
}

// NormalizeLink validates and canonicalizes a public WeChat article URL
// without attempting to inspect private WeChat parameters. Fragments are
// irrelevant to article identity; query parameters are preserved.
func NormalizeLink(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("article link is empty")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid article link: %q", raw)
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return "", fmt.Errorf("unsupported article link scheme: %q", u.Scheme)
	}
	if !isWechatArticleURL(u) {
		return "", fmt.Errorf("not a WeChat article link: %q", raw)
	}
	u.Fragment = ""
	return strings.TrimRight(u.String(), "?"), nil
}

func isWechatArticleURL(u *url.URL) bool {
	if u == nil || !strings.EqualFold(u.Hostname(), "mp.weixin.qq.com") {
		return false
	}
	path := strings.TrimRight(strings.ToLower(u.Path), "/")
	return path == "/s" || strings.HasPrefix(path, "/s/") || path == "/article" || strings.HasPrefix(path, "/article/")
}

// CommandDriver invokes a local helper using a JSON-over-stdin protocol. It
// supports Python, C#, Java or a Windows Go executable without putting GUI
// code into the bridge process.
type CommandDriver struct {
	Command string
	Args    []string
	// Env contains configuration defaults for the helper in KEY=VALUE form.
	// Values already present in the bridge process environment win, preserving
	// backwards compatibility with the WECHAT_SIM_* export-based setup.
	Env []string
}

func (d CommandDriver) Ready() bool { return strings.TrimSpace(d.Command) != "" }

func (d CommandDriver) FetchLatest(ctx context.Context, account Account) (string, error) {
	if strings.TrimSpace(d.Command) == "" {
		return "", &ManualInterventionError{Code: "driver_not_configured", Message: "Windows simulator command is not configured"}
	}
	payload, err := json.Marshal(account)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, d.Command, d.Args...)
	cmd.Env = envWithDefaults(d.Env)
	cmd.Stdin = strings.NewReader(string(payload))
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return "", &DriverError{Code: "helper_failed", Message: message}
	}
	var response struct {
		OK          bool   `json:"ok"`
		ArticleLink string `json:"articleLink"`
		Link        string `json:"link"`
		Code        string `json:"code"`
		Error       string `json:"error"`
		Manual      bool   `json:"manualIntervention"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return "", &DriverError{Code: "helper_invalid_response", Message: fmt.Sprintf("%v: %s", err, strings.TrimSpace(string(out)))}
	}
	if response.Manual || strings.EqualFold(response.Code, "manual_intervention") || strings.EqualFold(response.Code, "login_required") {
		return "", &ManualInterventionError{Code: response.Code, Message: response.Error}
	}
	if !response.OK {
		message := response.Error
		if message == "" {
			message = response.Code
		}
		return "", &DriverError{Code: response.Code, Message: message}
	}
	if response.ArticleLink != "" {
		return response.ArticleLink, nil
	}
	return response.Link, nil
}

func (d CommandDriver) FetchArticleInfo(ctx context.Context, account Account, articleLink string) (ArticleInfo, error) {
	if strings.TrimSpace(d.Command) == "" {
		return ArticleInfo{}, &DriverError{Code: "driver_not_configured", Message: "Windows simulator command is not configured"}
	}
	payload, err := json.Marshal(map[string]any{
		"action": "get_article_info", "articleLink": articleLink,
		"keyword": account.Nickname, "biz": account.Biz,
	})
	if err != nil {
		return ArticleInfo{}, err
	}
	cmd := exec.CommandContext(ctx, d.Command, d.Args...)
	cmd.Env = envWithDefaults(d.Env)
	cmd.Stdin = strings.NewReader(string(payload))
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return ArticleInfo{}, &DriverError{Code: "article_info_helper_failed", Message: message}
	}
	var response struct {
		OK          bool   `json:"ok"`
		ID          string `json:"id"`
		Title       string `json:"title"`
		PublishedAt int64  `json:"publishedAt"`
		Image       string `json:"image"`
		Avatar      string `json:"avatar"`
		Content     string `json:"content"`
		Code        string `json:"code"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return ArticleInfo{}, &DriverError{Code: "article_info_invalid_response", Message: fmt.Sprintf("%v: %s", err, strings.TrimSpace(string(out)))}
	}
	if !response.OK {
		return ArticleInfo{}, &DriverError{Code: response.Code, Message: response.Error}
	}
	return ArticleInfo{ID: response.ID, Title: response.Title, PublishedAt: response.PublishedAt, Image: response.Image, Avatar: response.Avatar, Content: response.Content}, nil
}

func (d CommandDriver) WeChatLoginStatus(ctx context.Context) (WeChatLoginStatus, error) {
	if strings.TrimSpace(d.Command) == "" {
		return WeChatLoginStatus{}, &DriverError{Code: "driver_not_configured", Message: "模拟取链 helper 未配置，无法读取微信窗口"}
	}
	args := make([]string, 0, len(d.Args)+1)
	args = append(args, d.Args...)
	args = append(args, "--wechat-status")
	cmd := exec.CommandContext(ctx, d.Command, args...)
	cmd.Env = envWithDefaults(d.Env)
	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(string(out))
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			message = strings.TrimSpace(string(exitErr.Stderr))
		}
		if message == "" {
			message = err.Error()
		}
		return WeChatLoginStatus{}, &DriverError{Code: "helper_failed", Message: message}
	}
	var raw struct {
		OK          bool   `json:"ok"`
		Status      string `json:"status"`
		LoggedIn    bool   `json:"loggedIn"`
		WindowTitle string `json:"windowTitle"`
		Message     string `json:"message"`
		Code        string `json:"code"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return WeChatLoginStatus{}, &DriverError{Code: "helper_invalid_response", Message: fmt.Sprintf("%v: %s", err, strings.TrimSpace(string(out)))}
	}
	if !raw.OK || raw.Status == "" {
		message := raw.Error
		if message == "" {
			message = raw.Code
		}
		if message == "" {
			message = "微信窗口状态读取失败"
		}
		code := raw.Code
		if code == "" {
			code = "wechat_status_failed"
		}
		return WeChatLoginStatus{}, &DriverError{Code: code, Message: message}
	}
	return WeChatLoginStatus{
		Status:      raw.Status,
		LoggedIn:    raw.LoggedIn,
		WindowTitle: raw.WindowTitle,
		Message:     raw.Message,
	}, nil
}

func envWithDefaults(defaults []string) []string {
	env := os.Environ()
	seen := make(map[string]struct{}, len(env))
	for _, item := range env {
		key, _, ok := strings.Cut(item, "=")
		if ok && key != "" {
			seen[key] = struct{}{}
		}
	}
	for _, item := range defaults {
		key, _, ok := strings.Cut(item, "=")
		if !ok || key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		env = append(env, item)
		seen[key] = struct{}{}
	}
	return env
}

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gkx/wcplus/internal/wcplus"
)

// LoginPrepareRequest starts proxy for WeChat param capture (login forwarding step 1).
type LoginPrepareRequest struct {
	// ArticleURL is shown to the operator; wcplus still requires opening it in WeChat PC client.
	ArticleURL string `json:"articleURL,omitempty"`
}

type LoginPrepareResult struct {
	ProxySetResponse json.RawMessage `json:"proxySetResponse"`
	WcplusTaskURL    string          `json:"wcplusTaskURL"`
	UserSteps        []string        `json:"userSteps"`
	WeChatAutoHint   string          `json:"weChatAutoHint"`
}

type LoginFinishResult struct {
	ProxyUnsetResponse json.RawMessage `json:"proxyUnsetResponse"`
	LoginStatus        LoginStatusResult `json:"loginStatus"`
}

type LoginSessionService struct {
	wc     *wcplus.Client
	status *StatusService
	uiBase string
}

func NewLoginSessionService(wc *wcplus.Client, status *StatusService, wcplusBaseURL string) *LoginSessionService {
	base := strings.TrimRight(strings.TrimSpace(wcplusBaseURL), "/")
	return &LoginSessionService{wc: wc, status: status, uiBase: base}
}

func (s *LoginSessionService) Prepare(ctx context.Context, req LoginPrepareRequest) (LoginPrepareResult, error) {
	raw, err := s.wc.ProxySet(ctx)
	if err != nil {
		return LoginPrepareResult{}, fmt.Errorf("proxy set (Set Proxy): %w", err)
	}
	steps := []string{
		"1. Bridge 已调用 wcplus Set Proxy（本机代理 127.0.0.1:9090）。",
		"2. 在电脑微信（非浏览器）打开目标公众号任意一篇历史图文，等待加载完成。",
		"3. 打开 wcplus 任务页，确认出现公众号参数/绿色状态。",
		"4. 调用 POST /v1/rpc/login/finish 清除代理（Clear Proxy）。",
	}
	if u := strings.TrimSpace(req.ArticleURL); u != "" {
		steps = append(steps, "参考文章链接（请在微信内打开）："+u)
	}
	return LoginPrepareResult{
		ProxySetResponse: raw,
		WcplusTaskURL:    s.uiBase + "/#/task",
		UserSteps:        steps,
		WeChatAutoHint:   "Max 可安装 wcplus WeChat Auto（Windows）：自动 Set/Clear Proxy 并在微信内打开文章；Bridge 已提供相同 Proxy API。见 https://www.wcplus.cn/doc/auto_windows",
	}, nil
}

func (s *LoginSessionService) Finish(ctx context.Context) (LoginFinishResult, error) {
	raw, err := s.wc.ProxyUnset(ctx)
	if err != nil {
		return LoginFinishResult{}, fmt.Errorf("proxy unset (Clear Proxy): %w", err)
	}
	st, err := s.status.LoginStatus(ctx)
	if err != nil {
		return LoginFinishResult{ProxyUnsetResponse: raw}, err
	}
	return LoginFinishResult{ProxyUnsetResponse: raw, LoginStatus: st}, nil
}

type InitializeResult struct {
	License           json.RawMessage   `json:"license"`
	LoginStatus       LoginStatusResult `json:"loginStatus"`
	WeChatAutoAllowed bool              `json:"weChatAutoAllowedHint"`
	NextActions       []string          `json:"nextActions"`
	ForwardFlow       []string          `json:"forwardFlow"`
	WcplusTaskURL     string            `json:"wcplusTaskURL"`
	Docs              map[string]string `json:"docs"`
}

func (s *LoginSessionService) Initialize(ctx context.Context) (InitializeResult, error) {
	lic, _ := s.wc.LicenseSettings(ctx)
	st, err := s.status.LoginStatus(ctx)
	if err != nil {
		return InitializeResult{}, err
	}
	out := InitializeResult{
		License:     lic,
		LoginStatus: st,
		NextActions: []string{},
		WcplusTaskURL: s.uiBase + "/#/task",
		ForwardFlow: []string{
			"POST /v1/rpc/login/prepare",
			"用户在本机微信打开目标公众号文章（可配合 WeChat Auto）",
			"POST /v1/rpc/login/finish",
			"POST /v1/rpc/sync-official-account（exportAfter:true 可同步并导出最新）",
		},
		Docs: map[string]string{
			"usage":       "https://www.wcplus.cn/doc/usage",
			"autoWindows": "https://www.wcplus.cn/doc/auto_windows",
		},
	}
	licText := strings.ToLower(string(lic))
	out.WeChatAutoAllowed = strings.Contains(licText, "max") || strings.Contains(licText, "automation")
	if !st.MaxActive {
		out.NextActions = append(out.NextActions, "激活 Max 授权后再做 API 建任务。")
	}
	if st.NeedsManualLogin {
		out.NextActions = append(out.NextActions, "POST /v1/rpc/login/prepare → 微信开文 → POST /v1/rpc/login/finish")
	} else {
		out.NextActions = append(out.NextActions, "可直接 sync/export；参数过期时再走 login/prepare。")
	}
	if !out.WeChatAutoAllowed {
		out.NextActions = append(out.NextActions, "可选：安装 WeChat Auto 减少人工（Max + 自动化授权）。")
	}
	return out, nil
}

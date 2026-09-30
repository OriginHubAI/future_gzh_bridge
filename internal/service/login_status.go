package service

import (
	"context"
	"encoding/json"
	"strings"

	"gkx/wcplus/internal/wcplus"
)

type LoginStatusResult struct {
	MaxStatus         string          `json:"maxStatus"`
	MaxMessage        string          `json:"maxMessage,omitempty"`
	MaxActive         bool            `json:"maxActive"`
	License           json.RawMessage `json:"license,omitempty"`
	ReqData           json.RawMessage `json:"reqData,omitempty"`
	NeedsManualLogin  bool            `json:"needsManualLogin"`
	RecommendedAction string          `json:"recommendedAction"`
}

type StatusService struct {
	wc *wcplus.Client
}

func NewStatusService(wc *wcplus.Client) *StatusService {
	return &StatusService{wc: wc}
}

func (s *StatusService) LoginStatus(ctx context.Context) (LoginStatusResult, error) {
	out := LoginStatusResult{
		RecommendedAction: "若采集失败，请在 wcplus 打开「任务」→ Set Proxy → 电脑微信打开目标公众号任意图文 → 等待识别成功 → Clear Proxy。",
	}
	ctrl, err := s.wc.MaxQueueStatus(ctx)
	if err != nil {
		return out, err
	}
	out.MaxStatus = ctrl.Status
	out.MaxMessage = ctrl.Msg
	st := strings.ToLower(strings.TrimSpace(ctrl.Status))
	out.MaxActive = st != "not_max_version" && st != "unactivated" && st != "blocked"
	if !out.MaxActive {
		out.NeedsManualLogin = true
		out.RecommendedAction = "Max 未激活：请在 wcplus「设置/授权」确认 Max 有效后再调 API。"
		return out, nil
	}
	if lic, err := s.wc.LicenseSettings(ctx); err == nil && len(lic) > 0 {
		out.License = lic
	}
	raw, err := s.wc.GetGzhReqData(ctx)
	if err == nil && len(raw) > 0 {
		out.ReqData = raw
		if lacksWechatParams(raw) {
			out.NeedsManualLogin = true
			out.RecommendedAction = "调用 POST /v1/rpc/login/prepare，在微信 PC 客户端打开目标号文章，再 POST /v1/rpc/login/finish。"
		}
	}
	return out, nil
}

func lacksWechatParams(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "[]" || trimmed == "{}" || trimmed == "null" {
		return true
	}
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err == nil {
		return len(arr) == 0
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		if len(obj) == 0 {
			return true
		}
		if data, ok := obj["data"]; ok {
			switch v := data.(type) {
			case []any:
				return len(v) == 0
			case nil:
				return true
			}
		}
		return false
	}
	return false
}

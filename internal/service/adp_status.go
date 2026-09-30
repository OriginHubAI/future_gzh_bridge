package service

import (
	"context"
)

// ADPConnectivityStatus is the simplified status ADP needs (diagram: status 接口).
type ADPConnectivityStatus struct {
	OK              bool   `json:"ok"`
	WcplusReachable bool   `json:"wcplusReachable"`
	MaxActive       bool   `json:"maxActive"`
	WechatReady     bool   `json:"wechatReady"`
	Message         string `json:"message,omitempty"`
}

// ADPConnectivity evaluates whether the collector can serve export/import (internal detail hidden from ADP ops).
func (s *StatusService) ADPConnectivity(ctx context.Context) (ADPConnectivityStatus, error) {
	out := ADPConnectivityStatus{}
	if err := s.wc.Ping(ctx); err != nil {
		out.Message = "wcplus 不可达"
		return out, nil
	}
	out.WcplusReachable = true
	st, err := s.LoginStatus(ctx)
	if err != nil {
		out.Message = err.Error()
		return out, err
	}
	out.MaxActive = st.MaxActive
	out.WechatReady = st.MaxActive && !st.NeedsManualLogin
	out.OK = out.WcplusReachable && out.WechatReady
	if !out.MaxActive {
		out.Message = "Max 未激活或不可用"
		return out, nil
	}
	if st.NeedsManualLogin {
		out.Message = "微信参数未就绪，需采集机侧登录转发或 WeChat Auto"
		return out, nil
	}
	out.Message = "正常"
	return out, nil
}

// EnsureCollectorReady returns CollectorNotReadyError when ADP-facing operations should not proceed.
func (s *StatusService) EnsureCollectorReady(ctx context.Context) error {
	st, err := s.ADPConnectivity(ctx)
	if err != nil {
		return err
	}
	if st.OK {
		return nil
	}
	return &CollectorNotReadyError{Message: st.Message}
}

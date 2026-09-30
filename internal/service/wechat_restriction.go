package service

import (
	"strings"
)

// WechatContentRestrictedError is returned when wcplus/wechat blocks article body fetch (Set Proxy + open in WeChat PC).
type WechatContentRestrictedError struct {
	Biz                    string
	Nickname               string
	Partial                ExportLatestResult
	RecommendedArticleURL  string
	RemediationSteps       []string
}

func (e *WechatContentRestrictedError) Error() string {
	return "微信正文采集受限，需采集机 Set Proxy 并在 PC 微信内打开公众号文章"
}

// ContentRestrictionRemediationSteps matches wcplus log guidance (Set proxy → WeChat open article → Clear proxy).
func ContentRestrictionRemediationSteps(articleURL string) []string {
	steps := []string{
		"1. 调用 POST /v1/rpc/login/prepare（或 wcplus 任务页 Set Proxy）。",
		"2. 在 PC 微信（非浏览器）打开下方推荐文章或目标号任意图文，等待加载完成。",
		"3. 调用 POST /v1/rpc/login/finish（Clear Proxy）。",
		"4. 等待 wcplus 重采正文或 ADP 重新 export。",
	}
	if u := strings.TrimSpace(articleURL); u != "" {
		steps = append(steps, "推荐文章："+u)
	}
	return steps
}

func looksLikeWechatContentBlock(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "受限") ||
		strings.Contains(s, "限制") ||
		strings.Contains(s, "restrict") ||
		strings.Contains(s, "freq") ||
		strings.Contains(s, "verify")
}

// detectWechatContentRestriction after export when WithContent: all rows with id/link missing body.
func detectWechatContentRestriction(withContent bool, articles []ArticleExportItem, contentErrors []error) bool {
	if !withContent || len(articles) == 0 {
		return false
	}
	for _, err := range contentErrors {
		if looksLikeWechatContentBlock(err) {
			return true
		}
	}
	eligible := 0
	empty := 0
	for _, a := range articles {
		if strings.TrimSpace(a.ID) == "" && strings.TrimSpace(a.Link) == "" {
			continue
		}
		eligible++
		if strings.TrimSpace(a.Content) == "" {
			empty++
		}
	}
	return eligible >= 1 && empty == eligible
}

func firstArticleLink(articles []ArticleExportItem) string {
	for _, a := range articles {
		if u := strings.TrimSpace(a.Link); u != "" {
			return u
		}
	}
	return ""
}

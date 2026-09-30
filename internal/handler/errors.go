package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"gkx/wcplus/internal/service"
)

func replyServiceErr(c *gin.Context, err error) {
	var nf *service.OfficialAccountNotFoundError
	var ex *service.OfficialAccountExistsError
	var nr *service.CollectorNotReadyError
	var wr *service.WechatContentRestrictedError
	switch {
	case errors.As(err, &wr):
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code":    503002,
			"error":   "微信正文采集受限",
			"message": wr.Error(),
			"data":    wr.Partial,
			"remediation": gin.H{
				"recommendedArticleURL": wr.RecommendedArticleURL,
				"steps":                 wr.RemediationSteps,
				"prepare":               "POST /v1/rpc/login/prepare",
				"finish":                "POST /v1/rpc/login/finish",
				"remediate":             "POST /v1/rpc/remediate-wechat-content-restriction",
			},
		})
	case errors.As(err, &nf):
		c.JSON(http.StatusNotFound, gin.H{
			"code":  404001,
			"error": "公众号不存在",
			"biz":   nf.Biz,
		})
	case errors.As(err, &ex):
		c.JSON(http.StatusConflict, gin.H{
			"code":     409001,
			"error":    "公众号已存在",
			"biz":      ex.Biz,
			"nickname": ex.Nickname,
		})
	case errors.As(err, &nr):
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code":    503001,
			"error":   "采集机未就绪",
			"message": nr.Message,
		})
	default:
		replyErr(c, err)
	}
}

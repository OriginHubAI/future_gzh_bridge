package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"gkx/wcplus/internal/simulator"
)

func (h *Handler) RPCCreateLatestArticleLinkJob(c *gin.Context) {
	var req simulator.CreateJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 100001, "error": err.Error()})
		return
	}
	if h.simulator == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503004, "error": "模拟取链服务未初始化"})
		return
	}
	out, err := h.simulator.Create(c.Request.Context(), req)
	if err != nil {
		writeSimulatorError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"code": 0, "data": gin.H{
		"jobId":     out.ID,
		"state":     out.State,
		"total":     out.Total,
		"createdAt": out.CreatedAt,
	}})
}

func (h *Handler) RPCGetLatestArticleLinkJob(c *gin.Context) {
	out, err := h.getSimulatorJobStatus(c)
	if err != nil {
		writeSimulatorError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

func (h *Handler) RPCPauseLatestArticleLinkJob(c *gin.Context) {
	if h.simulator == nil {
		writeSimulatorError(c, simulator.ErrDisabled)
		return
	}
	out, err := h.simulator.Pause(strings.TrimSpace(c.Param("jobId")))
	if err != nil {
		writeSimulatorError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

func (h *Handler) RPCResumeLatestArticleLinkJob(c *gin.Context) {
	if h.simulator == nil {
		writeSimulatorError(c, simulator.ErrDisabled)
		return
	}
	out, err := h.simulator.Resume(strings.TrimSpace(c.Param("jobId")))
	if err != nil {
		writeSimulatorError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

func (h *Handler) RPCStopLatestArticleLinkJob(c *gin.Context) {
	if h.simulator == nil {
		writeSimulatorError(c, simulator.ErrDisabled)
		return
	}
	out, err := h.simulator.Stop(strings.TrimSpace(c.Param("jobId")))
	if err != nil {
		writeSimulatorError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

// RPCGetLatestArticleLinks deliberately exposes only links and account keys;
// article body/image fields belong to the existing export API.
func (h *Handler) RPCGetLatestArticleLinks(c *gin.Context) {
	if h.simulator == nil {
		writeSimulatorError(c, simulator.ErrDisabled)
		return
	}
	jobID := strings.TrimSpace(c.Query("jobId"))
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 100002, "error": "jobId is required"})
		return
	}
	out, err := h.simulator.Get(jobID)
	if err != nil {
		writeSimulatorError(c, err)
		return
	}
	type linkItem struct {
		ID          string `json:"id,omitempty"`
		Biz         string `json:"biz,omitempty"`
		Nickname    string `json:"nickname,omitempty"`
		ArticleLink string `json:"articleLink"`
	}
	links := make([]linkItem, 0, out.Success)
	for _, result := range out.Results {
		if result.Status != simulator.ResultSuccess {
			continue
		}
		links = append(links, linkItem{
			ID: result.Account.ID, Biz: result.Account.Biz, Nickname: result.Account.Nickname,
			ArticleLink: result.ArticleLink,
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"jobId":     out.ID,
		"state":     out.State,
		"total":     out.Total,
		"success":   out.Success,
		"duplicate": out.Duplicate,
		"failed":    out.Failed,
		"links":     links,
	}})
}

func (h *Handler) RPCSimulatorStatus(c *gin.Context) {
	if h.simulator == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"enabled": false}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": h.simulator.Summary()})
}

func (h *Handler) RPCWeChatLoginStatus(c *gin.Context) {
	if h.simulator == nil {
		writeSimulatorError(c, simulator.ErrDisabled)
		return
	}
	out, err := h.simulator.WeChatLoginStatus(c.Request.Context())
	if err != nil {
		writeSimulatorError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

func (h *Handler) getSimulatorJob(c *gin.Context) (simulator.Job, error) {
	if h.simulator == nil {
		return simulator.Job{}, simulator.ErrDisabled
	}
	return h.simulator.Get(strings.TrimSpace(c.Param("jobId")))
}

func (h *Handler) getSimulatorJobStatus(c *gin.Context) (simulator.Job, error) {
	if h.simulator == nil {
		return simulator.Job{}, simulator.ErrDisabled
	}
	return h.simulator.GetStatus(strings.TrimSpace(c.Param("jobId")))
}

func writeSimulatorError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, simulator.ErrDisabled):
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503004, "error": "模拟取链服务未启用", "message": err.Error()})
	case errors.Is(err, simulator.ErrJobNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": 404004, "error": "模拟取链任务不存在"})
	case errors.Is(err, simulator.ErrInvalidRequest):
		c.JSON(http.StatusBadRequest, gin.H{"code": 100002, "error": err.Error()})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"code": 502004, "error": err.Error()})
	}
}

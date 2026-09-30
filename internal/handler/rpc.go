package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"gkx/wcplus/internal/service"
)

// Contract RPC surface: wcplus import/sync/export plus an independent Windows
// latest-link simulator, login forwarding, initialization and status.
func (h *Handler) registerRPC(v1 *gin.RouterGroup) {
	rpc := v1.Group("/rpc")
	{
		rpc.POST("/import-official-account", h.RPCImportOfficialAccount)
		rpc.POST("/delete-official-account", h.RPCDeleteOfficialAccount)
		rpc.POST("/sync-official-account", h.RPCSyncOfficialAccount)
		rpc.GET("/export-latest-articles", h.RPCExportLatestArticles)
		rpc.POST("/save-article-link", h.RPCSaveArticleLink)
		rpc.GET("/status", h.RPCADPStatus)
		rpc.GET("/login-status", h.RPCLoginStatus)
		rpc.POST("/login/prepare", h.RPCLoginPrepare)
		rpc.POST("/login/finish", h.RPCLoginFinish)
		rpc.POST("/initialize", h.RPCInitialize)
		rpc.POST("/remediate-wechat-content-restriction", h.RPCRemediateWechatContentRestriction)
		// Vendor-independent Windows UI simulation. These endpoints only deal
		// with the latest article URL and never return article content.
		rpc.POST("/latest-article-link-jobs", h.RPCCreateLatestArticleLinkJob)
		rpc.GET("/latest-article-link-jobs/:jobId", h.RPCGetLatestArticleLinkJob)
		rpc.POST("/latest-article-link-jobs/:jobId/pause", h.RPCPauseLatestArticleLinkJob)
		rpc.POST("/latest-article-link-jobs/:jobId/resume", h.RPCResumeLatestArticleLinkJob)
		rpc.POST("/latest-article-link-jobs/:jobId/stop", h.RPCStopLatestArticleLinkJob)
		rpc.GET("/latest-article-links", h.RPCGetLatestArticleLinks)
		rpc.GET("/simulator-status", h.RPCSimulatorStatus)
		rpc.GET("/wechat-login-status", h.RPCWeChatLoginStatus)
	}
}

func (h *Handler) RPCDeleteOfficialAccount(c *gin.Context) {
	var req service.DeleteAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 100001, "error": err.Error()})
		return
	}
	out, err := h.deleteSvc.Delete(c.Request.Context(), req)
	if err != nil {
		var nf *service.OfficialAccountNotFoundError
		if errors.As(err, &nf) {
			replyServiceErr(c, err)
			return
		}
		h.alertFail(c, "delete_failed", err, map[string]any{"biz": req.Biz})
		return
	}
	h.notifySuccess(c, "delete.succeeded", map[string]any{"biz": out.Biz, "nickname": out.Nickname})
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

func (h *Handler) RPCImportOfficialAccount(c *gin.Context) {
	var req service.ImportAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 100001, "error": err.Error()})
		return
	}
	if h.simStore != nil {
		biz := strings.TrimSpace(req.Biz)
		nickname := strings.TrimSpace(req.Nickname)
		if existing, ok := h.simStore.FindAccount(biz, nickname); ok {
			c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
				"source": "simulator", "candidate": gin.H{
					"biz": existing.Biz, "nickname": existing.Nickname, "img": existing.Avatar,
				}, "taskReused": true,
			}})
			return
		}
		if biz == "" || nickname == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":  100002,
				"error": "模拟模式导入需要 biz 和 nickname；当前 helper 尚未能从文章页面自动解析 biz",
			})
			return
		}
		if err := h.simStore.UpsertAccount(biz, nickname, ""); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"code": 502001, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
			"source": "simulator", "candidate": gin.H{"biz": biz, "nickname": nickname},
		}})
		return
	}
	ctx := c.Request.Context()
	if err := h.status.EnsureCollectorReady(ctx); err != nil {
		h.maybeAlertCollector(ctx, err)
		replyServiceErr(c, err)
		return
	}
	out, err := h.importSvc.ImportByNickname(ctx, req)
	if err != nil {
		var ex *service.OfficialAccountExistsError
		if errors.As(err, &ex) {
			replyServiceErr(c, err)
			return
		}
		h.alertFail(c, "import_failed", err, map[string]any{"nickname": req.Nickname})
		return
	}
	h.notifySuccess(c, "import.succeeded", map[string]any{"nickname": req.Nickname, "biz": out.Candidate.Biz})
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

type rpcSyncBody struct {
	service.SyncStartRequest
	WaitQueue      bool `json:"waitQueue"`
	WaitTimeoutSec int  `json:"waitTimeoutSec"`
	// exportAfter: 对话「同步 + 导出最新」合并为一次 RPC（原 update-official-account 能力）
	ExportAfter bool `json:"exportAfter"`
	ExportLimit int  `json:"exportLimit"`
	WithContent bool `json:"withContent"`
}

func (h *Handler) RPCSyncOfficialAccount(c *gin.Context) {
	var req rpcSyncBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 100001, "error": err.Error()})
		return
	}
	syncRes, err := h.sync.Start(c.Request.Context(), req.SyncStartRequest)
	if err != nil {
		h.alertFail(c, "sync_failed", err, map[string]any{"biz": req.Biz, "nickname": req.Nickname})
		return
	}
	data := gin.H{"sync": syncRes}
	if req.WaitQueue {
		waitRes, werr := h.sync.WaitQueue(c.Request.Context(), service.WaitQueueRequest{TimeoutSec: req.WaitTimeoutSec})
		data["wait"] = waitRes
		if werr != nil {
			h.alertFail(c, "sync_wait_timeout", werr, map[string]any{"biz": req.Biz})
			c.JSON(http.StatusBadGateway, gin.H{"code": 502002, "error": werr.Error(), "data": data})
			return
		}
	}
	if req.ExportAfter {
		exportRes, eerr := h.export.ExportLatest(c.Request.Context(), service.ExportLatestRequest{
			Biz: req.Biz, Nickname: req.Nickname, Limit: req.ExportLimit, WithContent: req.WithContent,
		})
		if eerr == nil {
			h.rememberExport("wcplus", exportRes)
		}
		data["export"] = exportRes
		if eerr != nil {
			h.alertFail(c, "export_failed", eerr, map[string]any{"biz": req.Biz})
			c.JSON(http.StatusBadGateway, gin.H{"code": 502001, "error": eerr.Error(), "data": data})
			return
		}
		h.notifySuccess(c, "sync_export.succeeded", map[string]any{"biz": req.Biz, "total": exportRes.Total})
	} else {
		h.notifySuccess(c, "sync.succeeded", map[string]any{"biz": req.Biz, "nickname": req.Nickname})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": data})
}

func (h *Handler) RPCExportLatestArticles(c *gin.Context) {
	req := service.ExportLatestRequest{
		Biz:         c.Query("biz"),
		Nickname:    c.Query("nickname"),
		Limit:       queryInt(c, "limit", 10),
		WithContent: queryWithContentDefaultTrue(c),
	}
	// Prefer a live wcplus Pro read, then keep the rows in the bridge warehouse.
	// If Pro is down, return the warehouse so the export stays stable.
	ctx := c.Request.Context()
	out, err := h.export.ExportLatest(ctx, req)
	if err == nil {
		h.rememberExport("wcplus", out)
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
		return
	}
	var wr *service.WechatContentRestrictedError
	if errors.As(err, &wr) {
		h.rememberExport("wcplus", wr.Partial)
		if h.notify != nil {
			h.notify.Trigger(c.Request.Context(), "wechat.content_restricted", wr.Error(), map[string]any{
				"biz": wr.Biz, "recommendedArticleURL": wr.RecommendedArticleURL,
			})
		}
		replyServiceErr(c, err)
		return
	}
	if h.replyWarehouse(c, req) {
		return
	}
	var nf *service.OfficialAccountNotFoundError
	if errors.As(err, &nf) {
		replyServiceErr(c, err)
		return
	}
	h.alertFail(c, "export_failed", err, map[string]any{"biz": req.Biz})
}

type saveArticleLinkBody struct {
	Biz         string `json:"biz"`
	Nickname    string `json:"nickname"`
	ArticleLink string `json:"articleLink"`
	Title       string `json:"title"`
	Image       string `json:"image"`
	Avatar      string `json:"avatar"`
	Content     string `json:"content"`
	PublishedAt int64  `json:"publishedAt"`
}

func (h *Handler) RPCSaveArticleLink(c *gin.Context) {
	if h.simStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503001, "error": "文章库未配置 store_file"})
		return
	}
	var req saveArticleLinkBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 100001, "error": err.Error()})
		return
	}
	link := strings.TrimSpace(req.ArticleLink)
	if !isSavedArticleLink(link) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 100001, "error": "articleLink 必须是 mp.weixin.qq.com/s/..."})
		return
	}
	biz := strings.TrimSpace(req.Biz)
	if biz == "" {
		if parsed, ok := service.BizFromWechatURL(link); ok {
			biz = parsed
		}
	}
	nickname := strings.TrimSpace(req.Nickname)
	if biz == "" && nickname != "" {
		if account, ok := h.simStore.FindAccount("", nickname); ok {
			biz = account.Biz
		}
	}
	if biz == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 100001, "error": "缺少 biz。短链接不含 __biz 时，请传入 biz，或传入已经入库的 nickname"})
		return
	}
	if err := h.simStore.UpsertAccount(biz, nickname, req.Avatar); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502001, "error": err.Error()})
		return
	}
	if err := h.simStore.AddArticleFrom("external", biz, nickname, link, req.Title, req.Image, req.Content, req.PublishedAt); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502001, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"biz": biz, "nickname": nickname, "articleLink": link, "source": "external",
	}})
}

func isSavedArticleLink(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	return strings.Contains(raw, "mp.weixin.qq.com/s/") || strings.Contains(raw, "mp.weixin.qq.com/s?")
}

func (h *Handler) rememberExport(source string, out service.ExportLatestResult) {
	if h.simStore == nil || strings.TrimSpace(out.Biz) == "" {
		return
	}
	nickname := strings.TrimSpace(out.OfficialAccountName)
	avatar := ""
	if out.Gzh != nil {
		if nickname == "" {
			nickname = strings.TrimSpace(out.Gzh.Nickname)
		}
		avatar = strings.TrimSpace(out.Gzh.Img)
	}
	_ = h.simStore.UpsertAccount(out.Biz, nickname, avatar)
	for _, article := range out.Articles {
		if strings.TrimSpace(article.Link) == "" {
			continue
		}
		_ = h.simStore.AddArticleFrom(source, out.Biz, nickname, article.Link, article.Name, article.ImageURL, article.Content, article.Time)
	}
}

func (h *Handler) replyWarehouse(c *gin.Context, req service.ExportLatestRequest) bool {
	if h.simStore == nil || strings.TrimSpace(req.Biz) == "" {
		return false
	}
	account, articles, err := h.simStore.Latest(req.Biz, req.Limit)
	if err != nil {
		return false
	}
	rows := make([]gin.H, 0, len(articles))
	for _, article := range articles {
		rows = append(rows, gin.H{
			"id": article.ID, "name": article.Title, "title": article.Title,
			"link": article.Link, "time": article.PublishedAt,
			"imageUrl": article.Image, "image": article.Image,
			"avatar":  account.Avatar,
			"content": article.Content,
			"source":  article.Source,
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"biz": req.Biz, "officialAccountName": account.Nickname,
		"total": len(articles), "articles": rows, "source": "warehouse",
	}})
	return true
}

func (h *Handler) RPCRemediateWechatContentRestriction(c *gin.Context) {
	var req service.RemediateContentRestrictionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 100001, "error": err.Error()})
		return
	}
	out, err := h.remediate.Remediate(c.Request.Context(), req)
	if err != nil {
		h.alertFail(c, "remediate_content_failed", err, map[string]any{"biz": req.Biz})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

func (h *Handler) RPCADPStatus(c *gin.Context) {
	if h.simStore != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
			"ok": true, "wechatReady": h.simulator != nil && h.simulator.Enabled(),
			"message": "模拟后端已启用；微信窗口状态请调用 /v1/rpc/wechat-login-status",
		}})
		return
	}
	out, err := h.status.ADPConnectivity(c.Request.Context())
	if err != nil {
		replyErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

func (h *Handler) RPCLoginStatus(c *gin.Context) {
	out, err := h.status.LoginStatus(c.Request.Context())
	if err != nil {
		replyErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

func (h *Handler) RPCLoginPrepare(c *gin.Context) {
	var req service.LoginPrepareRequest
	_ = c.ShouldBindJSON(&req)
	out, err := h.loginSession.Prepare(c.Request.Context(), req)
	if err != nil {
		h.alertFail(c, "login_prepare_failed", err, nil)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

func (h *Handler) RPCLoginFinish(c *gin.Context) {
	out, err := h.loginSession.Finish(c.Request.Context())
	if err != nil {
		h.alertFail(c, "login_finish_failed", err, nil)
		return
	}
	if out.LoginStatus.NeedsManualLogin {
		h.notify.Trigger(c.Request.Context(), "login.still_required", out.LoginStatus.RecommendedAction, nil)
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

func (h *Handler) RPCInitialize(c *gin.Context) {
	out, err := h.loginSession.Initialize(c.Request.Context())
	if err != nil {
		replyErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

// queryWithContentDefaultTrue: ADP expects article body; pass withContent=false to skip.
func queryWithContentDefaultTrue(c *gin.Context) bool {
	v := strings.TrimSpace(c.Query("withContent"))
	if v == "" {
		return true
	}
	return v == "true" || v == "1"
}

func queryInt(c *gin.Context, key string, def int) int {
	v, err := strconv.Atoi(c.DefaultQuery(key, strconv.Itoa(def)))
	if err != nil {
		return def
	}
	return v
}

func (h *Handler) maybeAlertCollector(ctx context.Context, err error) {
	var nr *service.CollectorNotReadyError
	if h.notify != nil && errors.As(err, &nr) {
		h.notify.Trigger(ctx, "collector.not_ready", nr.Message, nil)
	}
}

func (h *Handler) alertFail(c *gin.Context, alertType string, err error, payload map[string]any) {
	if h.notify != nil {
		h.notify.Trigger(c.Request.Context(), alertType, err.Error(), payload)
	}
	replyErr(c, err)
}

func (h *Handler) notifySuccess(c *gin.Context, eventType string, payload map[string]any) {
	if h.notify == nil {
		return
	}
	h.notify.Success(c.Request.Context(), eventType, payload)
}

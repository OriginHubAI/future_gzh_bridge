package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"gkx/wcplus/internal/alert"
	"gkx/wcplus/internal/service"
	"gkx/wcplus/internal/simulator"
	"gkx/wcplus/internal/simulatorstore"
	"gkx/wcplus/internal/wcplus"
)

type Handler struct {
	wcplus       *wcplus.Client
	sync         *service.SyncService
	importSvc    *service.ImportService
	deleteSvc    *service.DeleteService
	export       *service.ExportService
	status       *service.StatusService
	loginSession *service.LoginSessionService
	remediate    *service.RemediateService
	simulator    *simulator.Manager
	simStore     *simulatorstore.Store
	notify       *alert.Notifier
}

func New(w *wcplus.Client, notify *alert.Notifier, wcplusBaseURL string) *Handler {
	syncSvc := service.NewSyncService(w)
	statusSvc := service.NewStatusService(w)
	exportSvc := service.NewExportService(w)
	loginSvc := service.NewLoginSessionService(w, statusSvc, wcplusBaseURL)
	return &Handler{
		wcplus:       w,
		sync:         syncSvc,
		importSvc:    service.NewImportService(w),
		deleteSvc:    service.NewDeleteService(w),
		export:       exportSvc,
		status:       statusSvc,
		loginSession: loginSvc,
		remediate:    service.NewRemediateService(exportSvc, loginSvc),
		simulator:    simulator.NewManager(simulator.Config{}),
		notify:       notify,
	}
}

// ConfigureSimulator replaces the default disabled manager with the runtime
// configuration. It is kept separate from New so existing callers and tests
// retain the original bridge constructor.
func (h *Handler) ConfigureSimulator(cfg simulator.Config) {
	h.simulator = simulator.NewManager(cfg)
}

func (h *Handler) ConfigureSimulatorStore(store *simulatorstore.Store) {
	h.simStore = store
}

func (h *Handler) Register(r *gin.Engine) {
	r.GET("/health", h.Health)
	v1 := r.Group("/v1")
	h.registerRPC(v1)
}

type healthResp struct {
	OK     bool   `json:"ok"`
	Wcplus string `json:"wcplus"`
}

func (h *Handler) Health(c *gin.Context) {
	ctx := c.Request.Context()
	status := "up"
	if err := h.wcplus.Ping(ctx); err != nil {
		status = "down: " + err.Error()
		c.JSON(http.StatusOK, healthResp{OK: false, Wcplus: status})
		return
	}
	c.JSON(http.StatusOK, healthResp{OK: true, Wcplus: status})
}

func replyErr(c *gin.Context, err error) {
	c.JSON(http.StatusBadGateway, gin.H{
		"code":  502001,
		"error": err.Error(),
	})
}

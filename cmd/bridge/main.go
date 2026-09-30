package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"gkx/wcplus/internal/alert"
	"gkx/wcplus/internal/callback"
	"gkx/wcplus/internal/config"
	"gkx/wcplus/internal/handler"
	"gkx/wcplus/internal/service"
	"gkx/wcplus/internal/simulator"
	"gkx/wcplus/internal/simulatorstore"
	"gkx/wcplus/internal/watchdog"
	"gkx/wcplus/internal/wcplus"
)

func main() {
	configPath := flag.String("config", "", "path to config YAML (optional)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger())

	client := wcplus.NewClient(cfg.Wcplus.BaseURL, cfg.Wcplus.HTTPTimeout())
	cb := callback.NewSender(cfg.Callback.BaseURL, cfg.Callback.Token)
	notify := alert.New(cb, cfg.Alert.WebhookURL, cfg.Callback.ManualInterventionOnly())
	h := handler.New(client, notify, cfg.Wcplus.BaseURL)
	var simStore *simulatorstore.Store
	if cfg.Simulator.StoreFile != "" {
		var storeErr error
		simStore, storeErr = simulatorstore.Open(cfg.Simulator.StoreFile)
		if storeErr != nil {
			log.Fatalf("article warehouse: %v", storeErr)
		}
		h.ConfigureSimulatorStore(simStore)
		defer simStore.Close()
	}
	if cfg.Simulator.Enabled {
		h.ConfigureSimulator(simulator.Config{
			Enabled:        true,
			Driver:         simulator.CommandDriver{Command: cfg.Simulator.HelperCommand, Args: cfg.Simulator.HelperArgs, Env: cfg.Simulator.HelperEnv()},
			StateFile:      cfg.Simulator.StateFile,
			MaxAccounts:    cfg.Simulator.MaxAccounts,
			DefaultRetries: cfg.Simulator.DefaultRetries,
			DefaultTimeout: cfg.Simulator.AccountTimeout(),
			RetryDelay:     cfg.Simulator.RetryDelay(),
			OnManualIntervention: func(event simulator.ManualEvent) {
				notify.Trigger(context.Background(), "simulator.manual_intervention", event.Message, map[string]any{
					"jobId": event.JobID, "accountId": event.Account.ID, "biz": event.Account.Biz,
					"nickname": event.Account.Nickname, "code": event.Code,
				})
			},
			OnCompleted: func(job simulator.Job) {
				if simStore != nil {
					for _, result := range job.Results {
						if result.Status != simulator.ResultSuccess || result.ArticleLink == "" {
							continue
						}
						_ = simStore.UpsertAccount(result.Account.Biz, result.Account.Nickname, result.Avatar)
						_ = simStore.AddArticleFrom("simulator", result.Account.Biz, result.Account.Nickname, result.ArticleLink, result.Title, result.Image, result.Content, result.PublishedAt)
					}
				}
				if job.Failed == 0 {
					notify.Success(context.Background(), "simulator.completed", map[string]any{
						"jobId": job.ID, "total": job.Total, "success": job.Success, "duplicate": job.Duplicate,
					})
					return
				}
				notify.Trigger(context.Background(), "simulator.job_failed", "模拟取链任务存在失败账号", map[string]any{
					"jobId": job.ID, "total": job.Total, "success": job.Success, "duplicate": job.Duplicate, "failed": job.Failed,
				})
			},
		})
	}

	r.Use(handler.AuthMiddleware(cfg.AuthToken))
	if cfg.Collector == "wechat_control" {
		if simStore == nil || !cfg.Simulator.Enabled {
			log.Fatal("wechat_control requires simulator.enabled and simulator.store_file")
		}
		r.Use(handler.StandaloneMiddleware(simStore, cfg.Simulator.HelperCommand, cfg.Simulator.HelperArgs, cfg.Simulator.AccountTimeout(), notify))
	} else if cfg.Collector != "" && cfg.Collector != "wcplus" {
		log.Fatal("unknown collector: ", cfg.Collector)
	}
	h.Register(r)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Watchdog.Enabled && cfg.Collector != "wechat_control" {
		statusSvc := service.NewStatusService(client)
		go watchdog.Run(ctx, cfg.Watchdog.Interval(), statusSvc, notify)
		log.Printf("[wcplus-bridge] watchdog enabled interval=%s", cfg.Watchdog.Interval())
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("[wcplus-bridge] listen=%s wcplus=%s callback=%v notify_mode=%s watchdog=%v alert_webhook=%v simulator=%v",
		cfg.Listen, cfg.Wcplus.BaseURL, cb.Enabled(), cfg.Callback.NotifyMode, cfg.Watchdog.Enabled, cfg.Alert.WebhookURL != "", cfg.Simulator.Enabled)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[wcplus-bridge] shutdown: %v", err)
	}
}

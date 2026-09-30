package config

import (
	"os"
	"strings"
	"testing"
)

func TestSimulatorHelperEnv(t *testing.T) {
	cfg := SimulatorConfig{
		LaunchCommand:            "msedge.exe {initial_link}",
		TargetApp:                "Microsoft Edge",
		WindowTitleRegex:         "Microsoft Edge.*",
		Flow:                     "article_to_account",
		LinkMode:                 "direct",
		ArticleListPoint:         "260,380",
		FirstArticlePoint:        "320,470",
		ArticleAccountPoint:      "260,220",
		AccountFirstArticlePoint: "320,470",
		ArticlePaneOrigin:        "240,0",
		WaitSec:                  2,
		SkipArticleNav:           true,
		SendToFileTransfer:       true,
		SendWindowTimeoutSec:     30,
		Vision:                   true,
		ReuseCurrentArticle:      true,
		RequireMenuVision:       true,
		RequireArticleVision:    true,
	}
	env := cfg.HelperEnv()
	joined := strings.Join(env, "\n")
	for _, want := range []string{
		"WECHAT_SIM_LAUNCH_COMMAND=msedge.exe {initial_link}",
		"WECHAT_SIM_TARGET_APP=Microsoft Edge",
		"WECHAT_SIM_WINDOW_TITLE_REGEX=Microsoft Edge.*",
		"WECHAT_SIM_FLOW=article_to_account",
		"WECHAT_SIM_LINK_MODE=direct",
		"WECHAT_SIM_ARTICLE_LIST_POINT=260,380",
		"WECHAT_SIM_FIRST_ARTICLE_POINT=320,470",
		"WECHAT_SIM_ARTICLE_ACCOUNT_POINT=260,220",
		"WECHAT_SIM_ACCOUNT_FIRST_ARTICLE_POINT=320,470",
		"WECHAT_SIM_ARTICLE_PANE_ORIGIN=240,0",
		"WECHAT_SIM_WAIT_SEC=2",
		"WECHAT_SIM_SKIP_ARTICLE_NAV=1",
		"WECHAT_SIM_SEND_TO_FILE_TRANSFER=1",
		"WECHAT_SIM_SEND_WINDOW_TIMEOUT_SEC=30",
		"WECHAT_SIM_VISION=1",
		"WECHAT_SIM_REUSE_CURRENT_ARTICLE=1",
		"WECHAT_SIM_REQUIRE_MENU_VISION=1",
		"WECHAT_SIM_REQUIRE_ARTICLE_VISION=1",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("HelperEnv missing %q: %v", want, env)
		}
	}
	if strings.Contains(joined, "WECHAT_SIM_SHARE_POINT=") {
		t.Fatal("empty optional values must be omitted")
	}
}

func TestLoadSimulatorCalibrationFromYAML(t *testing.T) {
	path := t.TempDir() + "/config.yaml"
	content := `
wcplus:
  base_url: http://127.0.0.1:5001
simulator:
  enabled: true
  helper_command: python
  launch_command: "msedge.exe {initial_link}"
  target_app: "Microsoft Edge"
  window_title_regex: "Microsoft Edge.*"
  flow: article_to_account
  link_mode: direct
  article_list_point: "260,380"
  first_article_point: "320,470"
  article_account_point: "260,220"
  account_first_article_point: "320,470"
  article_pane_origin: "240,0"
  vision: true
  wait_sec: 2
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Simulator.Enabled || cfg.Simulator.TargetApp != "Microsoft Edge" || cfg.Simulator.ArticleListPoint != "260,380" || cfg.Simulator.Flow != "article_to_account" || cfg.Simulator.ArticlePaneOrigin != "240,0" {
		t.Fatalf("simulator config = %#v", cfg.Simulator)
	}
	if cfg.Simulator.WaitSec != 2 {
		t.Fatalf("wait_sec = %v", cfg.Simulator.WaitSec)
	}
	if !cfg.Simulator.Vision {
		t.Fatal("vision = false, want true")
	}
}

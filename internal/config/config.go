package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds bridge runtime settings.
type Config struct {
	Collector string          `yaml:"collector"`
	Listen    string          `yaml:"listen"`
	AuthToken string          `yaml:"auth_token"`
	Wcplus    WcplusConfig    `yaml:"wcplus"`
	Simulator SimulatorConfig `yaml:"simulator"`
	Callback  CallbackConfig  `yaml:"callback"`
	Alert     AlertConfig     `yaml:"alert"`
	Watchdog  WatchdogConfig  `yaml:"watchdog"`
}

// SimulatorConfig controls the vendor-independent Windows UI automation
// worker. The bridge only invokes the configured local helper; it does not
// inspect private WeChat protocols or article content.
type SimulatorConfig struct {
	Enabled           bool     `yaml:"enabled"`
	StoreFile         string   `yaml:"store_file"`
	HelperCommand     string   `yaml:"helper_command"`
	HelperArgs        []string `yaml:"helper_args"`
	StateFile         string   `yaml:"state_file"`
	MaxAccounts       int      `yaml:"max_accounts"`
	DefaultRetries    int      `yaml:"default_retries"`
	AccountTimeoutSec int      `yaml:"account_timeout_sec"`
	RetryDelayMs      int      `yaml:"retry_delay_ms"`

	// The fields below are passed to the helper as WECHAT_SIM_* variables.
	// Keeping them in the bridge config makes UI calibration reproducible and
	// avoids requiring the operator to export a large set of variables before
	// starting the bridge.  Explicit process environment variables still take
	// precedence (see HelperEnv and the command driver).
	LaunchCommand            string  `yaml:"launch_command"`
	MacApp                   string  `yaml:"mac_app"`
	TargetApp                string  `yaml:"target_app"`
	WindowTitleRegex         string  `yaml:"window_title_regex"`
	Flow                     string  `yaml:"flow"`
	LinkMode                 string  `yaml:"link_mode"`
	ArticleListPoint         string  `yaml:"article_list_point"`
	FirstArticlePoint        string  `yaml:"first_article_point"`
	ArticleAccountPoint      string  `yaml:"article_account_point"`
	AccountFirstArticlePoint string  `yaml:"account_first_article_point"`
	ArticlePaneOrigin        string  `yaml:"article_pane_origin"`
	SharePoint               string  `yaml:"share_point"`
	MoreMenuPoint            string  `yaml:"more_menu_point"`
	CopyLinkPoint            string  `yaml:"copy_link_point"`
	WaitSec                  float64 `yaml:"wait_sec"`
	WindowTimeoutSec         float64 `yaml:"window_timeout_sec"`
	ClipboardTimeoutSec      float64 `yaml:"clipboard_timeout_sec"`
	CloseHotkey              string  `yaml:"close_hotkey"`
	AddressHotkey            string  `yaml:"address_hotkey"`
	CopyHotkey               string  `yaml:"copy_hotkey"`
	SkipArticleNav           bool    `yaml:"skip_article_nav"`
	SendToFileTransfer       bool    `yaml:"send_to_file_transfer"`
	SendTargetApp            string  `yaml:"send_target_app"`
	SendWindowTitleRegex     string  `yaml:"send_window_title_regex"`
	FileTransferChatPoint    string  `yaml:"file_transfer_chat_point"`
	FileTransferInputPoint   string  `yaml:"file_transfer_input_point"`
	SendPasteHotkey          string  `yaml:"send_paste_hotkey"`
	SendHotkey               string  `yaml:"send_hotkey"`
	SendWindowTimeoutSec     float64 `yaml:"send_window_timeout_sec"`
	CoordRefSize             string  `yaml:"coord_ref_size"`
	ClickDriver              string  `yaml:"click_driver"`
	TryAllMacWindows         bool    `yaml:"try_all_mac_windows"`
	MacWindowPick            string  `yaml:"mac_window_pick"`
	// ReuseCurrentArticle runs the account -> first article -> copy steps on
	// an article already open in the native WeChat window. It is explicit so
	// the normal launch-from-seed flow never silently controls a stale window.
	ReuseCurrentArticle  bool `yaml:"reuse_current_article"`
	RequireMenuVision    bool `yaml:"require_menu_vision"`
	RequireArticleVision bool `yaml:"require_article_vision"`
	// Vision enables the optional screenshot/color locator. Calibrated points
	// remain the fallback when a visual target is not found.
	Vision bool `yaml:"vision"`
}

// HelperEnv returns simulator settings in the helper's historical environment
// variable format.  Empty values are omitted so the helper's platform
// defaults remain effective.  The caller should only add entries that are not
// already present in its process environment; this preserves the long-standing
// WECHAT_SIM_* environment-variable override behavior.
func (s SimulatorConfig) HelperEnv() []string {
	values := []struct {
		name  string
		value string
	}{
		{"WECHAT_SIM_LAUNCH_COMMAND", s.LaunchCommand},
		{"WECHAT_SIM_MAC_APP", s.MacApp},
		{"WECHAT_SIM_TARGET_APP", s.TargetApp},
		{"WECHAT_SIM_WINDOW_TITLE_REGEX", s.WindowTitleRegex},
		{"WECHAT_SIM_FLOW", s.Flow},
		{"WECHAT_SIM_LINK_MODE", s.LinkMode},
		{"WECHAT_SIM_ARTICLE_LIST_POINT", s.ArticleListPoint},
		{"WECHAT_SIM_FIRST_ARTICLE_POINT", s.FirstArticlePoint},
		{"WECHAT_SIM_ARTICLE_ACCOUNT_POINT", s.ArticleAccountPoint},
		{"WECHAT_SIM_ACCOUNT_FIRST_ARTICLE_POINT", s.AccountFirstArticlePoint},
		{"WECHAT_SIM_ARTICLE_PANE_ORIGIN", s.ArticlePaneOrigin},
		{"WECHAT_SIM_SHARE_POINT", s.SharePoint},
		{"WECHAT_SIM_MORE_MENU_POINT", s.MoreMenuPoint},
		{"WECHAT_SIM_COPY_LINK_POINT", s.CopyLinkPoint},
		{"WECHAT_SIM_CLOSE_HOTKEY", s.CloseHotkey},
		{"WECHAT_SIM_ADDRESS_HOTKEY", s.AddressHotkey},
		{"WECHAT_SIM_COPY_HOTKEY", s.CopyHotkey},
		{"WECHAT_SIM_SEND_TARGET_APP", s.SendTargetApp},
		{"WECHAT_SIM_SEND_WINDOW_TITLE_REGEX", s.SendWindowTitleRegex},
		{"WECHAT_SIM_FILE_TRANSFER_CHAT_POINT", s.FileTransferChatPoint},
		{"WECHAT_SIM_FILE_TRANSFER_INPUT_POINT", s.FileTransferInputPoint},
		{"WECHAT_SIM_SEND_PASTE_HOTKEY", s.SendPasteHotkey},
		{"WECHAT_SIM_SEND_HOTKEY", s.SendHotkey},
		{"WECHAT_SIM_COORD_REF_SIZE", s.CoordRefSize},
		{"WECHAT_SIM_CLICK_DRIVER", s.ClickDriver},
		{"WECHAT_SIM_MAC_WINDOW_PICK", s.MacWindowPick},
	}
	if s.ReuseCurrentArticle {
		values = append(values, struct {
			name  string
			value string
		}{"WECHAT_SIM_REUSE_CURRENT_ARTICLE", "1"})
	}
	if s.RequireMenuVision {
		values = append(values, struct {
			name  string
			value string
		}{"WECHAT_SIM_REQUIRE_MENU_VISION", "1"})
	}
	if s.RequireArticleVision {
		values = append(values, struct {
			name  string
			value string
		}{"WECHAT_SIM_REQUIRE_ARTICLE_VISION", "1"})
	}
	if s.Vision {
		values = append(values, struct {
			name  string
			value string
		}{"WECHAT_SIM_VISION", "1"})
	}
	if s.WaitSec > 0 {
		values = append(values, struct {
			name  string
			value string
		}{"WECHAT_SIM_WAIT_SEC", strconv.FormatFloat(s.WaitSec, 'f', -1, 64)})
	}
	if s.WindowTimeoutSec > 0 {
		values = append(values, struct {
			name  string
			value string
		}{"WECHAT_SIM_WINDOW_TIMEOUT_SEC", strconv.FormatFloat(s.WindowTimeoutSec, 'f', -1, 64)})
	}
	if s.ClipboardTimeoutSec > 0 {
		values = append(values, struct {
			name  string
			value string
		}{"WECHAT_SIM_CLIPBOARD_TIMEOUT_SEC", strconv.FormatFloat(s.ClipboardTimeoutSec, 'f', -1, 64)})
	}
	if s.SendWindowTimeoutSec > 0 {
		values = append(values, struct {
			name  string
			value string
		}{"WECHAT_SIM_SEND_WINDOW_TIMEOUT_SEC", strconv.FormatFloat(s.SendWindowTimeoutSec, 'f', -1, 64)})
	}
	if s.SkipArticleNav {
		values = append(values, struct {
			name  string
			value string
		}{"WECHAT_SIM_SKIP_ARTICLE_NAV", "1"})
	}
	if s.SendToFileTransfer {
		values = append(values, struct {
			name  string
			value string
		}{"WECHAT_SIM_SEND_TO_FILE_TRANSFER", "1"})
	}
	if s.TryAllMacWindows {
		values = append(values, struct {
			name  string
			value string
		}{"WECHAT_SIM_TRY_ALL_MAC_WINDOWS", "1"})
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		if strings.TrimSpace(item.value) != "" {
			result = append(result, item.name+"="+item.value)
		}
	}
	return result
}

// WatchdogConfig optional background collector health notifier (ADP manual intervention).
type WatchdogConfig struct {
	Enabled     bool `yaml:"enabled"`
	IntervalSec int  `yaml:"interval_sec"`
}

type AlertConfig struct {
	WebhookURL string `yaml:"webhook_url"`
}

type WcplusConfig struct {
	BaseURL    string `yaml:"base_url"`
	TimeoutSec int    `yaml:"timeout_sec"`
}

type CallbackConfig struct {
	BaseURL string `yaml:"base_url"`
	Token   string `yaml:"token"`
	// NotifyMode: all (default) | manual_intervention — ADP §3: only push when ops must act.
	NotifyMode string `yaml:"notify_mode"`
}

// ManualInterventionOnly skips RPC success/failure callbacks; keeps collector + login alerts.
func (c CallbackConfig) ManualInterventionOnly() bool {
	return strings.EqualFold(strings.TrimSpace(c.NotifyMode), "manual_intervention")
}

// Load reads YAML from path; empty path uses defaults and env overrides.
func Load(path string) (Config, error) {
	cfg := Config{
		Listen:    "127.0.0.1:19090",
		AuthToken: "",
		Wcplus: WcplusConfig{
			BaseURL:    "http://127.0.0.1:5001",
			TimeoutSec: 120,
		},
		Simulator: SimulatorConfig{
			Enabled:           false,
			MaxAccounts:       2000,
			DefaultRetries:    1,
			AccountTimeoutSec: 90,
			RetryDelayMs:      500,
		},
	}
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return Config{}, err
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return Config{}, err
		}
	}
	applyEnv(&cfg)
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyEnv(cfg *Config) {
	if v := strings.TrimSpace(os.Getenv("WCPLUS_BRIDGE_LISTEN")); v != "" {
		cfg.Listen = v
	}
	if v := strings.TrimSpace(os.Getenv("WCPLUS_BRIDGE_AUTH_TOKEN")); v != "" {
		cfg.AuthToken = v
	}
	if v := strings.TrimSpace(os.Getenv("WCPLUS_BASE_URL")); v != "" {
		cfg.Wcplus.BaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv("WCPLUS_BRIDGE_CALLBACK_URL")); v != "" {
		cfg.Callback.BaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv("WCPLUS_BRIDGE_CALLBACK_TOKEN")); v != "" {
		cfg.Callback.Token = v
	}
	if v := strings.TrimSpace(os.Getenv("WCPLUS_ALERT_WEBHOOK_URL")); v != "" {
		cfg.Alert.WebhookURL = v
	}
	if v := strings.TrimSpace(os.Getenv("WCPLUS_HTTP_TIMEOUT_SEC")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Wcplus.TimeoutSec = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("WCPLUS_SIMULATOR_ENABLED")); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			cfg.Simulator.Enabled = enabled
		}
	}
	if v := strings.TrimSpace(os.Getenv("WCPLUS_SIMULATOR_HELPER_COMMAND")); v != "" {
		cfg.Simulator.HelperCommand = v
	}
	if v := strings.TrimSpace(os.Getenv("WCPLUS_SIMULATOR_STATE_FILE")); v != "" {
		cfg.Simulator.StateFile = v
	}
}

func (c Config) validate() error {
	if strings.TrimSpace(c.Wcplus.BaseURL) == "" {
		return fmt.Errorf("wcplus.base_url is required")
	}
	if c.Wcplus.TimeoutSec <= 0 {
		c.Wcplus.TimeoutSec = 120
	}
	return nil
}

func (w WatchdogConfig) Interval() time.Duration {
	sec := w.IntervalSec
	if sec <= 0 {
		sec = 300
	}
	return time.Duration(sec) * time.Second
}

func (c WcplusConfig) HTTPTimeout() time.Duration {
	return time.Duration(c.TimeoutSec) * time.Second
}

func (c SimulatorConfig) AccountTimeout() time.Duration {
	sec := c.AccountTimeoutSec
	if sec <= 0 {
		sec = 90
	}
	return time.Duration(sec) * time.Second
}

func (c SimulatorConfig) RetryDelay() time.Duration {
	ms := c.RetryDelayMs
	if ms <= 0 {
		ms = 500
	}
	return time.Duration(ms) * time.Millisecond
}

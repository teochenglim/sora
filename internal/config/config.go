// Package config defines and loads SORA's configuration.
package config

import "time"

type Config struct {
	Server        ServerConfig        `mapstructure:"server"`
	Mode          string              `mapstructure:"mode"`
	AI            AIConfig            `mapstructure:"ai"`
	Dedup         DedupConfig         `mapstructure:"dedup"`
	Notifications NotificationsConfig `mapstructure:"notifications"`
	WorkHours     WorkHoursConfig     `mapstructure:"work_hours"`
	BusinessOwners []BusinessOwner    `mapstructure:"business_owners"`
	BusinessMappings []BusinessMapping `mapstructure:"business_mappings"`
	Remediation   RemediationConfig   `mapstructure:"remediation"`
	FallbackRules map[string]string   `mapstructure:"fallback_rules"`
	SourceMappings map[string]map[string]string `mapstructure:"source_mappings"`

	RulesPath   string `mapstructure:"rules_path"`
	PromptsPath string `mapstructure:"prompts_path"`
}

type ServerConfig struct {
	Port            int           `mapstructure:"port"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
}

type AIConfig struct {
	Provider    string        `mapstructure:"provider"`
	APIKey      string        `mapstructure:"api_key"`
	BaseURL     string        `mapstructure:"base_url"`
	Model       string        `mapstructure:"model"`
	Temperature float64       `mapstructure:"temperature"`
	Timeout     time.Duration `mapstructure:"timeout"`
	MaxRetries  int           `mapstructure:"max_retries"`
}

type DedupConfig struct {
	WindowSeconds int    `mapstructure:"window_seconds"`
	RedisAddr     string `mapstructure:"redis_addr"`
	RedisPassword string `mapstructure:"redis_password"`
}

type NotificationsConfig struct {
	Slack     SlackConfig     `mapstructure:"slack"`
	Telegram  TelegramConfig  `mapstructure:"telegram"`
	PagerDuty PagerDutyConfig `mapstructure:"pagerduty"`
}

type SlackConfig struct {
	WebhookURL              string `mapstructure:"webhook_url"`
	InteractiveSigningSecret string `mapstructure:"interactive_signing_secret"`
}

type TelegramConfig struct {
	BotToken      string `mapstructure:"bot_token"`
	DefaultChatID string `mapstructure:"default_chat_id"`
}

type PagerDutyConfig struct {
	IntegrationKey string `mapstructure:"integration_key"`
}

type WorkHoursConfig struct {
	Start    string   `mapstructure:"start"`
	End      string   `mapstructure:"end"`
	Timezone string   `mapstructure:"timezone"`
	Days     []string `mapstructure:"days"`
}

type BusinessOwner struct {
	Name       string `mapstructure:"name"`
	SlackID    string `mapstructure:"slack_id"`
	TelegramID string `mapstructure:"telegram_id"`
}

type BusinessMapping struct {
	Pattern      string `mapstructure:"pattern"`
	BusinessLine string `mapstructure:"business_line"`
}

type RemediationConfig struct {
	DryRun             bool          `mapstructure:"dry_run"`
	Kubeconfig         string        `mapstructure:"kubeconfig"`
	Namespace          string        `mapstructure:"namespace"`
	ToolTimeout        time.Duration `mapstructure:"tool_timeout"`
	MaxTier2ToolCalls  int           `mapstructure:"max_tier2_tool_calls"`
	ApprovalTimeout    time.Duration `mapstructure:"approval_timeout"`
	VerificationWait   time.Duration `mapstructure:"verification_wait"`
	ServiceWhitelist   []string      `mapstructure:"service_whitelist"`
	ServiceBlacklist   []string      `mapstructure:"service_blacklist"`
}

// IsWhitelisted reports whether the given service is allowed to be auto-remediated.
func (r RemediationConfig) IsWhitelisted(service string) bool {
	for _, b := range r.ServiceBlacklist {
		if b == service {
			return false
		}
	}
	if len(r.ServiceWhitelist) == 0 {
		return true
	}
	for _, w := range r.ServiceWhitelist {
		if w == service {
			return true
		}
	}
	return false
}

// Default returns a configuration with sane defaults, used by demo mode
// and as a base before overlaying file/env values.
func Default() *Config {
	return &Config{
		Mode: "full",
		Server: ServerConfig{
			Port:            8080,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 15 * time.Second,
		},
		AI: AIConfig{
			Provider:    "anthropic",
			Model:       "claude-3-5-sonnet-20241022",
			Temperature: 0.1,
			Timeout:     8 * time.Second,
			MaxRetries:  2,
		},
		Dedup: DedupConfig{WindowSeconds: 300},
		Remediation: RemediationConfig{
			ToolTimeout:       30 * time.Second,
			MaxTier2ToolCalls: 3,
			ApprovalTimeout:   120 * time.Second,
			VerificationWait:  60 * time.Second,
		},
		FallbackRules: map[string]string{
			"critical": "P0",
			"warning":  "P1",
			"info":     "P2",
		},
		RulesPath:   "configs/rules.yaml",
		PromptsPath: "configs/prompts.yaml",
	}
}

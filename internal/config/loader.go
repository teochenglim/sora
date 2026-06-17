package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Load reads the config file at path, applying ${ENV_VAR} and
// ${ENV_VAR:-default} substitution, and returns a populated Config. An
// empty path falls back to Default().
func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	expanded := expandEnv(string(raw))

	v := viper.New()
	v.SetConfigType(strings.TrimPrefix(filepath.Ext(path), "."))
	if err := v.ReadConfig(bytes.NewBufferString(expanded)); err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config %s: %w", path, err)
	}
	if cfg.RulesPath == "" {
		cfg.RulesPath = "configs/rules.yaml"
	}
	if cfg.PromptsPath == "" {
		cfg.PromptsPath = "configs/prompts.yaml"
	}
	return cfg, nil
}

func envLookup(key string) string { return os.Getenv(key) }

var envDefaultPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*):-([^}]*)\}`)

// expandEnv supports both ${VAR} (empty if unset) and ${VAR:-default}
// (falls back to default if VAR is unset or empty), so config.yaml can
// ship sane defaults (e.g. model/provider/temperature) while still being
// overridable per-deployment via env vars like LLM_MODEL.
func expandEnv(s string) string {
	s = envDefaultPattern.ReplaceAllStringFunc(s, func(match string) string {
		groups := envDefaultPattern.FindStringSubmatch(match)
		if v := os.Getenv(groups[1]); v != "" {
			return v
		}
		return groups[2]
	})
	return os.Expand(s, envLookup)
}

// ClassifierRule is a single Tier-1 classification rule.
type ClassifierRule struct {
	Name  string `yaml:"name"`
	Match struct {
		AlertNameRegex string `yaml:"alertname_regex"`
		Severity       string `yaml:"severity"`
		Namespace      string `yaml:"namespace"`
	} `yaml:"match"`
	Result struct {
		Level         string   `yaml:"level"`
		BusinessLine  string   `yaml:"business_line"`
		RootCauseHint string   `yaml:"root_cause_hint"`
		Actions       []string `yaml:"actions"`
	} `yaml:"result"`
}

// RemediationRule is a single Tier-1 remediation rule.
type RemediationRule struct {
	Name  string `yaml:"name"`
	Match struct {
		AlertNameRegex string `yaml:"alertname_regex"`
		RestartCountLt *int   `yaml:"restart_count_lt"`
	} `yaml:"match"`
	Action          string `yaml:"action"`
	RequireApproval bool   `yaml:"require_approval"`
	MaxPerHour      int    `yaml:"max_per_hour"`
}

// RulesFile is the parsed content of configs/rules.yaml.
type RulesFile struct {
	ClassifierRules  []ClassifierRule  `yaml:"classifier_rules"`
	RemediationRules []RemediationRule `yaml:"remediation_rules"`
}

// RulesStore holds the currently active rule set and supports atomic
// hot-reload (e.g. triggered by SIGHUP) without disrupting in-flight reads.
type RulesStore struct {
	mu    sync.RWMutex
	rules RulesFile
	path  string
}

// NewRulesStore loads rules from path and returns a store ready for use.
func NewRulesStore(path string) (*RulesStore, error) {
	s := &RulesStore{path: path}
	if err := s.Reload(); err != nil {
		return nil, err
	}
	return s, nil
}

// Reload re-reads the rules file from disk and atomically swaps it in.
func (s *RulesStore) Reload() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("reading rules file %s: %w", s.path, err)
	}
	var rf RulesFile
	if err := yaml.Unmarshal(data, &rf); err != nil {
		return fmt.Errorf("parsing rules file %s: %w", s.path, err)
	}
	s.mu.Lock()
	s.rules = rf
	s.mu.Unlock()
	return nil
}

// Get returns a snapshot of the current rules.
func (s *RulesStore) Get() RulesFile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rules
}

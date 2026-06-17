package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teochenglim/sora/internal/config"
)

func TestLoad_EmptyPathReturnsDefaults(t *testing.T) {
	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, "full", cfg.Mode)
}

func TestLoad_ExpandsEnvVars(t *testing.T) {
	t.Setenv("SORA_TEST_API_KEY", "secret-123")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("ai:\n  api_key: ${SORA_TEST_API_KEY}\n  model: test-model\n"), 0o644))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "secret-123", cfg.AI.APIKey)
	assert.Equal(t, "test-model", cfg.AI.Model)
}

func TestLoad_EnvDefaultSyntaxUsesDefaultWhenUnset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(
		"ai:\n  provider: ${SORA_TEST_UNSET_PROVIDER:-ollama}\n  model: ${SORA_TEST_UNSET_MODEL:-gemma4-fast:latest}\n  temperature: ${SORA_TEST_UNSET_TEMP:-0.2}\n",
	), 0o644))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "ollama", cfg.AI.Provider)
	assert.Equal(t, "gemma4-fast:latest", cfg.AI.Model)
	assert.Equal(t, 0.2, cfg.AI.Temperature)
}

func TestLoad_EnvDefaultSyntaxPrefersSetValue(t *testing.T) {
	t.Setenv("SORA_TEST_SET_MODEL", "llama3:latest")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("ai:\n  model: ${SORA_TEST_SET_MODEL:-gemma4-fast:latest}\n"), 0o644))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "llama3:latest", cfg.AI.Model)
}

func TestRemediationConfig_IsWhitelisted(t *testing.T) {
	r := config.RemediationConfig{ServiceWhitelist: []string{"worker-service"}, ServiceBlacklist: []string{"payments-api"}}
	assert.True(t, r.IsWhitelisted("worker-service"))
	assert.False(t, r.IsWhitelisted("payments-api"), "blacklist always wins")
	assert.False(t, r.IsWhitelisted("unknown-service"), "not on whitelist when whitelist is non-empty")

	open := config.RemediationConfig{}
	assert.True(t, open.IsWhitelisted("anything"), "empty whitelist means everything is allowed")
}

func TestRulesStore_Reload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte("remediation_rules: []\n"), 0o644))

	store, err := config.NewRulesStore(path)
	require.NoError(t, err)
	assert.Empty(t, store.Get().RemediationRules)

	require.NoError(t, os.WriteFile(path, []byte("remediation_rules:\n  - name: r1\n    action: restart_service\n"), 0o644))
	require.NoError(t, store.Reload())
	assert.Len(t, store.Get().RemediationRules, 1)
}

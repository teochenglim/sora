package unit

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teochenglim/sora/internal/config"
	"github.com/teochenglim/sora/internal/remediator"
	"github.com/teochenglim/sora/internal/tools"
	"github.com/teochenglim/sora/internal/types"
)

const tier1RulesYAML = `
remediation_rules:
  - name: "crashloop-auto-restart"
    match:
      alertname_regex: "CrashLoopBackOff"
      restart_count_lt: 3
    action: restart_service
    require_approval: false
    max_per_hour: 3

  - name: "oom-needs-approval"
    match:
      alertname_regex: "OOMKilled"
    action: increase_memory_limit
    require_approval: true
`

func writeTier1Rules(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(tier1RulesYAML), 0o644))
	return path
}

func newTier1(t *testing.T, remediation config.RemediationConfig) *remediator.Tier1 {
	t.Helper()
	rulesPath := writeTier1Rules(t)
	rulesStore, err := config.NewRulesStore(rulesPath)
	require.NoError(t, err)

	registry := tools.NewRegistry(tools.NewMockTool("restart_service", "restarted", true))
	executor := remediator.NewExecutor(registry, 0, false)
	rl := remediator.NewMemoryRateLimiter()
	return remediator.NewTier1(rulesStore, executor, rl, remediation)
}

func TestTier1_MatchesAndExecutesWithinRestartThreshold(t *testing.T) {
	t1 := newTier1(t, config.RemediationConfig{})
	alert := types.ClassifiedAlert{Alert: types.Alert{
		AlertName: "CrashLoopBackOff", Service: "worker-service", Namespace: "default",
		Labels: map[string]string{"restart_count": "1"},
	}}

	outcome, err := t1.Attempt(context.Background(), alert)
	require.NoError(t, err)
	assert.True(t, outcome.Handled)
	assert.False(t, outcome.RequiresApproval)
	assert.True(t, outcome.Action.Success)
	assert.Equal(t, "restart_service", outcome.Action.Action)
}

func TestTier1_DoesNotMatchAboveRestartThreshold(t *testing.T) {
	t1 := newTier1(t, config.RemediationConfig{})
	alert := types.ClassifiedAlert{Alert: types.Alert{
		AlertName: "CrashLoopBackOff", Service: "worker-service", Namespace: "default",
		Labels: map[string]string{"restart_count": "5"},
	}}

	outcome, err := t1.Attempt(context.Background(), alert)
	require.NoError(t, err)
	assert.False(t, outcome.Handled, "restart_count above threshold should not match the rule")
}

func TestTier1_RequiresApprovalRule(t *testing.T) {
	t1 := newTier1(t, config.RemediationConfig{})
	alert := types.ClassifiedAlert{Alert: types.Alert{AlertName: "OOMKilled", Service: "payments-api", Namespace: "default"}}

	outcome, err := t1.Attempt(context.Background(), alert)
	require.NoError(t, err)
	assert.True(t, outcome.Handled)
	assert.True(t, outcome.RequiresApproval)
}

func TestTier1_BlacklistedServiceIsSkipped(t *testing.T) {
	t1 := newTier1(t, config.RemediationConfig{ServiceBlacklist: []string{"worker-service"}})
	alert := types.ClassifiedAlert{Alert: types.Alert{
		AlertName: "CrashLoopBackOff", Service: "worker-service", Namespace: "default",
		Labels: map[string]string{"restart_count": "1"},
	}}

	outcome, err := t1.Attempt(context.Background(), alert)
	require.NoError(t, err)
	assert.False(t, outcome.Handled, "blacklisted service must never be auto-remediated")
}

func TestTier1_RateLimitExceededRequiresApproval(t *testing.T) {
	rulesPath := writeTier1Rules(t)
	rulesStore, err := config.NewRulesStore(rulesPath)
	require.NoError(t, err)
	registry := tools.NewRegistry(tools.NewMockTool("restart_service", "restarted", true))
	executor := remediator.NewExecutor(registry, 0, false)
	rl := remediator.NewMemoryRateLimiter()
	t1 := remediator.NewTier1(rulesStore, executor, rl, config.RemediationConfig{})

	alert := types.ClassifiedAlert{Alert: types.Alert{
		AlertName: "CrashLoopBackOff", Service: "worker-service", Namespace: "default",
		Labels: map[string]string{"restart_count": "1"},
	}}

	for i := 0; i < 3; i++ {
		outcome, err := t1.Attempt(context.Background(), alert)
		require.NoError(t, err)
		require.True(t, outcome.Handled)
		require.False(t, outcome.RequiresApproval)
	}

	outcome, err := t1.Attempt(context.Background(), alert)
	require.NoError(t, err)
	assert.True(t, outcome.Handled)
	assert.True(t, outcome.RequiresApproval, "4th restart within the hour should exceed max_per_hour:3")
}

package unit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teochenglim/sora/internal/config"
	"github.com/teochenglim/sora/internal/incident"
	"github.com/teochenglim/sora/internal/remediator"
	"github.com/teochenglim/sora/internal/tools"
	"github.com/teochenglim/sora/internal/types"
)

const engineRulesYAML = `
remediation_rules:
  - name: "crashloop-auto-restart"
    match:
      alertname_regex: "CrashLoopBackOff"
    action: restart_service
    require_approval: false
    max_per_hour: 5
`

func TestEngine_Tier1SuccessAndVerificationResolves(t *testing.T) {
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(rulesPath, []byte(engineRulesYAML), 0o644))
	rulesStore, err := config.NewRulesStore(rulesPath)
	require.NoError(t, err)

	registry := tools.NewRegistry(
		tools.NewMockTool("restart_service", "restarted", true),
		tools.NewMockTool(tools.ToolQueryPodStatus, "phase=Running", true),
	)
	executor := remediator.NewExecutor(registry, 0, false)
	rl := remediator.NewMemoryRateLimiter()
	tier1 := remediator.NewTier1(rulesStore, executor, rl, config.RemediationConfig{})
	verifier := remediator.NewVerifier(executor, 5*time.Millisecond)
	store := incident.NewMemoryStore()

	engine := remediator.NewEngine(tier1, nil, nil, verifier, store, nil)

	alert := types.ClassifiedAlert{Alert: types.Alert{AlertName: "CrashLoopBackOff", Service: "worker-service", Namespace: "default", Pod: "worker-1"}}
	inc, err := engine.Process(context.Background(), alert, "inc-engine-1")
	require.NoError(t, err)
	assert.Equal(t, types.StatusResolved, inc.Status)
	require.Len(t, inc.Actions, 1)
	assert.Equal(t, "restart_service", inc.Actions[0].Action)
}

func TestEngine_VerificationFailureMarksFailed(t *testing.T) {
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(rulesPath, []byte(engineRulesYAML), 0o644))
	rulesStore, err := config.NewRulesStore(rulesPath)
	require.NoError(t, err)

	registry := tools.NewRegistry(
		tools.NewMockTool("restart_service", "restarted", true),
		tools.NewMockTool(tools.ToolQueryPodStatus, "phase=CrashLoopBackOff", false),
	)
	executor := remediator.NewExecutor(registry, 0, false)
	rl := remediator.NewMemoryRateLimiter()
	tier1 := remediator.NewTier1(rulesStore, executor, rl, config.RemediationConfig{})
	verifier := remediator.NewVerifier(executor, 5*time.Millisecond)
	store := incident.NewMemoryStore()

	engine := remediator.NewEngine(tier1, nil, nil, verifier, store, nil)

	alert := types.ClassifiedAlert{Alert: types.Alert{AlertName: "CrashLoopBackOff", Service: "worker-service", Namespace: "default", Pod: "worker-1"}}
	inc, err := engine.Process(context.Background(), alert, "inc-engine-2")
	require.NoError(t, err)
	assert.Equal(t, types.StatusFailed, inc.Status)
}

func TestEngine_NoMatchingTierFailsIncident(t *testing.T) {
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(rulesPath, []byte("remediation_rules: []"), 0o644))
	rulesStore, err := config.NewRulesStore(rulesPath)
	require.NoError(t, err)

	registry := tools.NewRegistry()
	executor := remediator.NewExecutor(registry, 0, false)
	rl := remediator.NewMemoryRateLimiter()
	tier1 := remediator.NewTier1(rulesStore, executor, rl, config.RemediationConfig{})
	verifier := remediator.NewVerifier(executor, 5*time.Millisecond)
	store := incident.NewMemoryStore()

	// No tier2, no tier3 configured: nothing can handle this alert.
	engine := remediator.NewEngine(tier1, nil, nil, verifier, store, nil)

	alert := types.ClassifiedAlert{Alert: types.Alert{AlertName: "UnknownAlert", Service: "worker-service", Namespace: "default"}}
	_, err = engine.Process(context.Background(), alert, "inc-engine-3")
	assert.Error(t, err)
}

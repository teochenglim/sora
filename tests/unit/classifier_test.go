package unit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teochenglim/sora/internal/cache"
	"github.com/teochenglim/sora/internal/circuit"
	"github.com/teochenglim/sora/internal/classifier"
	"github.com/teochenglim/sora/internal/config"
	"github.com/teochenglim/sora/internal/types"
)

const classifierRulesYAML = `
classifier_rules:
  - name: "crashloop-critical"
    match:
      alertname_regex: "CrashLoopBackOff"
    result:
      level: P0
      business_line: platform
      root_cause_hint: "crash looping"
      actions: ["restart_service"]
`

// mockAIClassifier lets tests control AI success/failure and output.
type mockAIClassifier struct {
	result types.ClassifiedAlert
	err    error
	calls  int
}

func (m *mockAIClassifier) Classify(_ context.Context, alert types.Alert, _ []types.Alert) (types.ClassifiedAlert, error) {
	m.calls++
	if m.err != nil {
		return types.ClassifiedAlert{}, m.err
	}
	out := m.result
	out.Alert = alert
	return out, nil
}

func newRuleClassifierForTest(t *testing.T) *classifier.RuleClassifier {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(classifierRulesYAML), 0o644))
	rulesStore, err := config.NewRulesStore(path)
	require.NoError(t, err)
	return classifier.NewRuleClassifier(rulesStore, map[string]string{"warning": "P1", "critical": "P0"})
}

func TestOrchestrator_UsesAIWhenAvailable(t *testing.T) {
	ai := &mockAIClassifier{result: types.ClassifiedAlert{Level: "P1", Confidence: 0.9, ClassifiedBy: types.ClassifiedByAI}}
	rule := newRuleClassifierForTest(t)
	breaker := circuit.New("test-ai", 10, 0.5, time.Minute)
	orch := classifier.New(ai, rule, breaker, cache.New(time.Minute), nil)

	result, err := orch.Classify(context.Background(), types.Alert{AlertName: "HighLatency", Severity: "warning"})
	require.NoError(t, err)
	assert.Equal(t, types.ClassifiedByAI, result.ClassifiedBy)
	assert.Equal(t, "P1", result.Level)
	assert.Equal(t, 1, ai.calls)
}

func TestOrchestrator_TierOneRuleTakesPriorityOverAI(t *testing.T) {
	// AI would succeed here, but a Tier-1 rule matches CrashLoopBackOff —
	// the rule must win and AI must never even be called (fast/free path).
	ai := &mockAIClassifier{result: types.ClassifiedAlert{Level: "P2", Confidence: 0.9, ClassifiedBy: types.ClassifiedByAI}}
	rule := newRuleClassifierForTest(t)
	breaker := circuit.New("test-ai", 10, 0.5, time.Minute)
	orch := classifier.New(ai, rule, breaker, cache.New(time.Minute), nil)

	result, err := orch.Classify(context.Background(), types.Alert{AlertName: "CrashLoopBackOff", Severity: "critical"})
	require.NoError(t, err)
	assert.Equal(t, types.ClassifiedByRule, result.ClassifiedBy)
	assert.Equal(t, "P0", result.Level)
	assert.Equal(t, 0, ai.calls, "AI must not be called when a Tier-1 rule matches")
}

func TestOrchestrator_FallsBackToRuleOnAIError(t *testing.T) {
	// No Tier-1 rule matches "UnknownAlert", so AI is tried; when it
	// errors, the orchestrator falls back to the severity mapping.
	ai := &mockAIClassifier{err: errors.New("ai unavailable")}
	rule := newRuleClassifierForTest(t)
	breaker := circuit.New("test-ai", 10, 0.5, time.Minute)
	orch := classifier.New(ai, rule, breaker, cache.New(time.Minute), nil)

	result, err := orch.Classify(context.Background(), types.Alert{AlertName: "UnknownAlert", Severity: "critical"})
	require.NoError(t, err)
	assert.Equal(t, types.ClassifiedByFallback, result.ClassifiedBy)
	assert.Equal(t, "P0", result.Level)
	assert.Equal(t, 1, ai.calls)
}

func TestOrchestrator_FallsBackToSeverityWhenNoRuleMatches(t *testing.T) {
	ai := &mockAIClassifier{err: errors.New("ai unavailable")}
	rule := newRuleClassifierForTest(t)
	breaker := circuit.New("test-ai", 10, 0.5, time.Minute)
	orch := classifier.New(ai, rule, breaker, cache.New(time.Minute), nil)

	result, err := orch.Classify(context.Background(), types.Alert{AlertName: "UnknownAlert", Severity: "warning"})
	require.NoError(t, err)
	assert.Equal(t, types.ClassifiedByFallback, result.ClassifiedBy)
	assert.Equal(t, "P1", result.Level)
}

func TestOrchestrator_CircuitBreakerOpensAfterRepeatedFailures(t *testing.T) {
	ai := &mockAIClassifier{err: errors.New("ai down")}
	rule := newRuleClassifierForTest(t)
	breaker := circuit.New("test-ai", 3, 0.5, time.Hour)
	orch := classifier.New(ai, rule, breaker, cache.New(time.Minute), nil)

	for i := 0; i < 3; i++ {
		// "UnknownAlert" matches no Tier-1 rule, so each call reaches AI.
		_, err := orch.Classify(context.Background(), types.Alert{AlertName: "UnknownAlert", Severity: "critical"})
		require.NoError(t, err)
	}
	assert.Equal(t, circuit.Open, breaker.State(), "breaker should open after failure threshold reached")

	// Once open, the orchestrator must not call the AI client at all.
	callsBefore := ai.calls
	_, err := orch.Classify(context.Background(), types.Alert{AlertName: "UnknownAlert", Severity: "critical"})
	require.NoError(t, err)
	assert.Equal(t, callsBefore, ai.calls, "AI should not be called while breaker is open")
}

func TestUnclassifiableError_Message(t *testing.T) {
	err := &classifier.UnclassifiableError{AlertName: "Mystery"}
	assert.Contains(t, err.Error(), "Mystery")
}

func TestUnclassifiableError(t *testing.T) {
	ai := &mockAIClassifier{err: errors.New("ai down")}
	rule := newRuleClassifierForTest(t)
	breaker := circuit.New("test-ai", 10, 0.5, time.Hour)
	orch := classifier.New(ai, rule, breaker, cache.New(time.Minute), nil)

	_, err := orch.Classify(context.Background(), types.Alert{AlertName: "Mystery", Severity: "unmapped"})
	require.Error(t, err)
	var target *classifier.UnclassifiableError
	assert.ErrorAs(t, err, &target)
}

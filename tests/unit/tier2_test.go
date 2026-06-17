package unit

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teochenglim/sora/internal/config"
	"github.com/teochenglim/sora/internal/remediator"
	"github.com/teochenglim/sora/internal/tools"
	"github.com/teochenglim/sora/internal/types"
)

func TestTier2_HighConfidencePlanExecutes(t *testing.T) {
	srv := chatCompletionStub(t, `{"steps":[{"tool":"query_pod_status","params":{"namespace":"default","pod":"worker-1"}}],"confidence":0.9}`, http.StatusOK)
	defer srv.Close()

	planner := remediator.NewHTTPPlanner(config.AIConfig{Model: "test", Timeout: 2 * time.Second}, srv.URL)
	registry := tools.NewRegistry(tools.NewMockTool(tools.ToolQueryPodStatus, "phase=Running", true))
	executor := remediator.NewExecutor(registry, 0, false)
	tier2 := remediator.NewTier2(planner, executor, 3, 2*time.Second)

	alert := types.ClassifiedAlert{Alert: types.Alert{AlertName: "HighCPU", Namespace: "default", Pod: "worker-1"}}
	outcome, err := tier2.Attempt(context.Background(), alert)
	require.NoError(t, err)
	assert.True(t, outcome.Handled)
	assert.False(t, outcome.RequiresApproval)
	assert.True(t, outcome.Action.Success)
}

func TestTier2_LowConfidenceRequiresApproval(t *testing.T) {
	srv := chatCompletionStub(t, `{"steps":[],"confidence":0.4}`, http.StatusOK)
	defer srv.Close()

	planner := remediator.NewHTTPPlanner(config.AIConfig{Model: "test", Timeout: 2 * time.Second}, srv.URL)
	registry := tools.NewRegistry()
	executor := remediator.NewExecutor(registry, 0, false)
	tier2 := remediator.NewTier2(planner, executor, 3, 2*time.Second)

	alert := types.ClassifiedAlert{Alert: types.Alert{AlertName: "Mystery"}}
	outcome, err := tier2.Attempt(context.Background(), alert)
	require.NoError(t, err)
	assert.True(t, outcome.RequiresApproval)
}

func TestTier2_CapsStepsAtMaxToolCalls(t *testing.T) {
	srv := chatCompletionStub(t, `{"steps":[
		{"tool":"query_pod_status","params":{"namespace":"default","pod":"p1"}},
		{"tool":"query_pod_status","params":{"namespace":"default","pod":"p2"}},
		{"tool":"query_pod_status","params":{"namespace":"default","pod":"p3"}}
	],"confidence":0.9}`, http.StatusOK)
	defer srv.Close()

	planner := remediator.NewHTTPPlanner(config.AIConfig{Model: "test", Timeout: 2 * time.Second}, srv.URL)
	mock := tools.NewMockTool(tools.ToolQueryPodStatus, "phase=Running", true)
	registry := tools.NewRegistry(mock)
	executor := remediator.NewExecutor(registry, 0, false)
	tier2 := remediator.NewTier2(planner, executor, 1, 2*time.Second)

	alert := types.ClassifiedAlert{Alert: types.Alert{AlertName: "HighCPU"}}
	_, err := tier2.Attempt(context.Background(), alert)
	require.NoError(t, err)
	assert.Len(t, mock.Invocations(), 1, "tier2 must respect max_tier2_tool_calls")
}

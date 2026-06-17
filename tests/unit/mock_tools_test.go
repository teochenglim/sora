package unit

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teochenglim/sora/internal/tools"
)

func TestMockTool_RecordsInvocationsAndError(t *testing.T) {
	mt := tools.NewMockTool("restart_service", "ok", true)
	_, err := mt.Execute(context.Background(), map[string]string{"service": "worker"})
	require.NoError(t, err)
	assert.Len(t, mt.Invocations(), 1)

	mt.SetError(errors.New("boom"))
	_, err = mt.Execute(context.Background(), nil)
	assert.Error(t, err)
}

func TestNewDemoRegistry_HasAllThreeTools(t *testing.T) {
	r := tools.NewDemoRegistry()
	for _, name := range []string{tools.ToolQueryPodStatus, tools.ToolQueryLogs, tools.ToolRestartService} {
		_, ok := r.Get(name)
		assert.True(t, ok, "demo registry should expose %s", name)
	}
	assert.Len(t, r.Names(), 3)
}

func TestDemoAlertGenerator_CyclesThroughSamples(t *testing.T) {
	g := tools.NewDemoAlertGenerator()
	first := g.Next()
	assert.NotEmpty(t, first.AlertName)
	assert.NotEmpty(t, first.String())
}

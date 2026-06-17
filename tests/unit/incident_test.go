package unit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teochenglim/sora/internal/incident"
	"github.com/teochenglim/sora/internal/types"
)

func TestMemoryStore_SaveAndGet(t *testing.T) {
	s := incident.NewMemoryStore()
	ctx := context.Background()

	inc := types.Incident{ID: "inc-1", Status: types.StatusOpen, Alert: types.ClassifiedAlert{Level: "P0"}}
	require.NoError(t, s.Save(ctx, inc))

	got, ok, err := s.Get(ctx, "inc-1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, types.StatusOpen, got.Status)
}

func TestMemoryStore_AppendAction(t *testing.T) {
	s := incident.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, types.Incident{ID: "inc-1", Status: types.StatusOpen}))

	require.NoError(t, s.AppendAction(ctx, "inc-1", types.ActionRecord{Tier: "tier1", Action: "restart_service", Success: true}))

	got, _, err := s.Get(ctx, "inc-1")
	require.NoError(t, err)
	require.Len(t, got.Actions, 1)
	assert.Equal(t, "restart_service", got.Actions[0].Action)
}

func TestMemoryStore_UpdateStatusSetsResolvedAt(t *testing.T) {
	s := incident.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, types.Incident{ID: "inc-1", Status: types.StatusOpen}))

	require.NoError(t, s.UpdateStatus(ctx, "inc-1", types.StatusResolved))

	got, _, err := s.Get(ctx, "inc-1")
	require.NoError(t, err)
	assert.Equal(t, types.StatusResolved, got.Status)
	assert.NotNil(t, got.ResolvedAt)
}

func TestMemoryStore_AppendActionOnMissingIncidentErrors(t *testing.T) {
	s := incident.NewMemoryStore()
	err := s.AppendAction(context.Background(), "missing", types.ActionRecord{})
	assert.Error(t, err)
}

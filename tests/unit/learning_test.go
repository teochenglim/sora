package unit

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teochenglim/sora/internal/remediator"
)

func TestLearningStore_RecordSuccessAccumulates(t *testing.T) {
	dir := t.TempDir()
	db, err := remediator.NewLearningStore(filepath.Join(dir, "learning.db"))
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		require.NoError(t, db.RecordSuccess(ctx, "HighCPU", "restart_service"))
	}

	patterns, err := db.Patterns(ctx)
	require.NoError(t, err)
	require.Len(t, patterns, 1)
	assert.Equal(t, 3, patterns[0].SuccessCount)
	assert.False(t, patterns[0].Promoted)
}

func TestLearningStore_PromoteEligibleWritesRuleAndMarksPromoted(t *testing.T) {
	dir := t.TempDir()
	db, err := remediator.NewLearningStore(filepath.Join(dir, "learning.db"))
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	for i := 0; i < remediator.PromotionThreshold; i++ {
		require.NoError(t, db.RecordSuccess(ctx, "HighCPU", "restart_service"))
	}

	rulesPath := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(rulesPath, []byte("remediation_rules: []\n"), 0o644))

	n, err := db.PromoteEligible(ctx, rulesPath)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	data, err := os.ReadFile(rulesPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "HighCPU")
	assert.Contains(t, string(data), "restart_service")

	patterns, err := db.Patterns(ctx)
	require.NoError(t, err)
	require.Len(t, patterns, 1)
	assert.True(t, patterns[0].Promoted)

	// Running again should not re-promote the same pattern.
	n2, err := db.PromoteEligible(ctx, rulesPath)
	require.NoError(t, err)
	assert.Equal(t, 0, n2)
}

func TestLearningStore_BelowThresholdNotPromoted(t *testing.T) {
	dir := t.TempDir()
	db, err := remediator.NewLearningStore(filepath.Join(dir, "learning.db"))
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	require.NoError(t, db.RecordSuccess(ctx, "HighCPU", "restart_service"))

	rulesPath := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(rulesPath, []byte("remediation_rules: []\n"), 0o644))

	n, err := db.PromoteEligible(ctx, rulesPath)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

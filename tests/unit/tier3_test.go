package unit

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teochenglim/sora/internal/incident"
	"github.com/teochenglim/sora/internal/notifier"
	"github.com/teochenglim/sora/internal/remediator"
	"github.com/teochenglim/sora/internal/types"
)

type fakeNotifier struct {
	escalations int32
}

func (f *fakeNotifier) Notify(context.Context, types.Incident) error { return nil }
func (f *fakeNotifier) Escalate(context.Context, types.Incident) error {
	atomic.AddInt32(&f.escalations, 1)
	return nil
}
func (f *fakeNotifier) Channel() string { return "fake" }

func TestTier3_ApprovalBeforeTimeoutResolvesQuickly(t *testing.T) {
	store := incident.NewMemoryStore()
	n := &fakeNotifier{}
	tier3 := remediator.NewTier3([]notifier.Notifier{n}, store, 2*time.Second, "")

	require.NoError(t, store.Save(context.Background(), types.Incident{ID: "inc-1", Status: types.StatusOpen}))

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = store.UpdateStatus(context.Background(), "inc-1", remediator.DecisionApproved)
	}()

	start := time.Now()
	outcome, err := tier3.Attempt(context.Background(), types.Incident{ID: "inc-1"})
	require.NoError(t, err)
	assert.True(t, outcome.Handled)
	assert.Equal(t, "approved", outcome.Action.Action)
	assert.Less(t, time.Since(start), 2*time.Second, "should return as soon as approval is recorded, not wait for the full timeout")
	assert.Equal(t, int32(1), atomic.LoadInt32(&n.escalations))
}

func TestTier3_RejectionStopsRemediation(t *testing.T) {
	store := incident.NewMemoryStore()
	n := &fakeNotifier{}
	tier3 := remediator.NewTier3([]notifier.Notifier{n}, store, 2*time.Second, "")
	require.NoError(t, store.Save(context.Background(), types.Incident{ID: "inc-2", Status: types.StatusOpen}))

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = store.UpdateStatus(context.Background(), "inc-2", remediator.DecisionRejected)
	}()

	outcome, err := tier3.Attempt(context.Background(), types.Incident{ID: "inc-2"})
	require.NoError(t, err)
	assert.Equal(t, "rejected", outcome.Action.Action)
	assert.False(t, outcome.Action.Success)
}

func TestTier3_TimeoutWithoutPagerDutyKeyStillCompletes(t *testing.T) {
	store := incident.NewMemoryStore()
	n := &fakeNotifier{}
	tier3 := remediator.NewTier3([]notifier.Notifier{n}, store, 60*time.Millisecond, "")
	require.NoError(t, store.Save(context.Background(), types.Incident{ID: "inc-3", Status: types.StatusOpen}))

	outcome, err := tier3.Attempt(context.Background(), types.Incident{ID: "inc-3"})
	require.NoError(t, err)
	assert.Equal(t, "pagerduty-escalation", outcome.Action.Action)
}

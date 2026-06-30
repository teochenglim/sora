package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teochenglim/sora/internal/config"
	"github.com/teochenglim/sora/internal/notifier"
	"github.com/teochenglim/sora/internal/types"
)

func TestSlackNotifier_Notify(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := notifier.NewSlackNotifier(srv.URL, nil, notifier.WorkHours{})
	inc := types.Incident{ID: "inc-1", Alert: types.ClassifiedAlert{Level: types.LevelP0, Alert: types.Alert{AlertName: "CrashLoopBackOff"}}}

	require.NoError(t, n.Notify(context.Background(), inc))
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
	assert.Equal(t, "slack", n.Channel())
}

func TestSlackNotifier_P2SuppressedOutsideWorkHours(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Window is offset 12h from the current time-of-day so "now" always
	// falls outside it, regardless of when the test happens to run.
	now := time.Now().UTC()
	winStart := now.Add(12 * time.Hour)
	winEnd := winStart.Add(1 * time.Hour)
	wh := notifier.WorkHours{
		Start:    winStart.Format("15:04"),
		End:      winEnd.Format("15:04"),
		Timezone: "UTC",
		Days:     []string{now.Format("Mon")},
	}
	n := notifier.NewSlackNotifier(srv.URL, []config.BusinessOwner{{Name: "platform", SlackID: "U1"}}, wh)
	inc := types.Incident{ID: "inc-2", Alert: types.ClassifiedAlert{Level: types.LevelP2, BusinessLine: "platform", Alert: types.Alert{AlertName: "HighCPU"}}}

	require.NoError(t, n.Notify(context.Background(), inc))
	assert.Equal(t, int32(0), atomic.LoadInt32(&calls), "P2 outside the configured window must not be sent")
}

func TestSlackNotifier_Escalate(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := notifier.NewSlackNotifier(srv.URL, nil, notifier.WorkHours{})
	inc := types.Incident{ID: "inc-3", Alert: types.ClassifiedAlert{Level: types.LevelP1, Alert: types.Alert{AlertName: "OOMKilled"}}}

	require.NoError(t, n.Escalate(context.Background(), inc))
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestSlackNotifier_NonOKStatusIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := notifier.NewSlackNotifier(srv.URL, nil, notifier.WorkHours{})
	inc := types.Incident{ID: "inc-4", Alert: types.ClassifiedAlert{Level: types.LevelP0}}
	assert.Error(t, n.Notify(context.Background(), inc))
}

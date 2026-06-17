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

	wh := notifier.WorkHours{Start: "09:00", End: "10:00", Timezone: "UTC", Days: []string{time.Now().UTC().Format("Mon")}}
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

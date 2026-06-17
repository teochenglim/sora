package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teochenglim/sora/internal/notifier"
	"github.com/teochenglim/sora/internal/types"
)

func TestTelegramNotifier_Notify(t *testing.T) {
	var calls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/bottest-token/sendMessage", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	n := newTestTelegramNotifier(t, srv)
	inc := types.Incident{ID: "inc-1", Alert: types.ClassifiedAlert{Level: types.LevelP0, Alert: types.Alert{AlertName: "CrashLoopBackOff"}}}

	require.NoError(t, n.Notify(context.Background(), inc))
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
	assert.Equal(t, "telegram", n.Channel())
}

func TestTelegramNotifier_Escalate(t *testing.T) {
	var calls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/bottest-token/sendMessage", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	n := newTestTelegramNotifier(t, srv)
	inc := types.Incident{ID: "inc-2", Alert: types.ClassifiedAlert{Level: types.LevelP1, Alert: types.Alert{AlertName: "OOMKilled"}}}

	require.NoError(t, n.Escalate(context.Background(), inc))
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func newTestTelegramNotifier(t *testing.T, srv *httptest.Server) *notifier.TelegramNotifier {
	t.Helper()
	return notifier.NewTelegramNotifierWithBaseURL(srv.URL, "test-token", "chat-1", nil, notifier.WorkHours{})
}

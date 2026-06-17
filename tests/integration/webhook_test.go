package integration

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teochenglim/sora/internal/cache"
	"github.com/teochenglim/sora/internal/circuit"
	"github.com/teochenglim/sora/internal/classifier"
	"github.com/teochenglim/sora/internal/config"
	"github.com/teochenglim/sora/internal/dedup"
	"github.com/teochenglim/sora/internal/incident"
	"github.com/teochenglim/sora/internal/notifier"
	"github.com/teochenglim/sora/internal/tools"
	"github.com/teochenglim/sora/internal/types"
	"github.com/teochenglim/sora/internal/webhook"
)

const rulesYAML = `
classifier_rules:
  - name: "crashloop-critical"
    match:
      alertname_regex: "CrashLoopBackOff"
    result:
      level: P0
      business_line: platform
      root_cause_hint: "crash looping"
      actions: ["restart_service"]
remediation_rules: []
`

// captureNotifier records every Notify/Escalate call for assertions.
type captureNotifier struct {
	mu        sync.Mutex
	notified  []types.Incident
	escalated []types.Incident
}

func (c *captureNotifier) Notify(_ context.Context, inc types.Incident) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notified = append(c.notified, inc)
	return nil
}
func (c *captureNotifier) Escalate(_ context.Context, inc types.Incident) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.escalated = append(c.escalated, inc)
	return nil
}
func (c *captureNotifier) Channel() string { return "test" }

func (c *captureNotifier) NotifiedCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.notified)
}

func newTestHandler(t *testing.T, cn *captureNotifier) *webhook.Handler {
	t.Helper()
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(rulesPath, []byte(rulesYAML), 0o644))

	rulesStore, err := config.NewRulesStore(rulesPath)
	require.NoError(t, err)

	ruleClassifier := classifier.NewRuleClassifier(rulesStore, map[string]string{"critical": "P0", "warning": "P1"})
	breaker := circuit.New("ai", 10, 0.5, time.Minute)
	orch := classifier.New(nil, ruleClassifier, breaker, cache.New(time.Minute), nil)

	cfg := config.Default()
	cfg.Mode = "notify-only"
	cfg.SourceMappings = map[string]map[string]string{
		"generic": {
			"alertname": "alert_name",
			"severity":  "level",
			"instance":  "host",
			"namespace": "environment",
			"pod":       "container",
		},
	}

	return &webhook.Handler{
		Mode:       "notify-only",
		Cfg:        cfg,
		Classifier: orch,
		Deduper:    dedup.NewMemoryDeduper(5 * time.Minute),
		Engine:     nil,
		Notifiers:  []notifier.Notifier{cn},
		Store:      incident.NewMemoryStore(),
		Log:        logrus.New(),
	}
}

func TestWebhookAlert_ClassifiesAndNotifies(t *testing.T) {
	cn := &captureNotifier{}
	h := newTestHandler(t, cn)

	mux := http.NewServeMux()
	h.Routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := []byte(`{
		"alerts": [{
			"labels": {"alertname":"CrashLoopBackOff","severity":"critical","instance":"10.0.0.5","namespace":"default","pod":"worker-1","service":"worker-service"},
			"annotations": {},
			"startsAt": "2026-06-17T10:00:00Z"
		}]
	}`)

	resp, err := http.Post(srv.URL+"/webhook/alert", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	require.Eventually(t, func() bool { return cn.NotifiedCount() == 1 }, time.Second, 10*time.Millisecond)
}

func TestWebhookAlert_DuplicateIsSuppressed(t *testing.T) {
	cn := &captureNotifier{}
	h := newTestHandler(t, cn)

	mux := http.NewServeMux()
	h.Routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := []byte(`{
		"alerts": [{
			"labels": {"alertname":"CrashLoopBackOff","severity":"critical","instance":"10.0.0.5","namespace":"default","pod":"worker-1","service":"worker-service"},
			"annotations": {},
			"startsAt": "2026-06-17T10:00:00Z"
		}]
	}`)

	for i := 0; i < 3; i++ {
		resp, err := http.Post(srv.URL+"/webhook/alert", "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		resp.Body.Close()
	}

	require.Eventually(t, func() bool { return cn.NotifiedCount() == 1 }, time.Second, 10*time.Millisecond)
}

func TestHealthEndpoint(t *testing.T) {
	cn := &captureNotifier{}
	h := newTestHandler(t, cn)
	mux := http.NewServeMux()
	h.Routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestReadyEndpoint(t *testing.T) {
	cn := &captureNotifier{}
	h := newTestHandler(t, cn)
	mux := http.NewServeMux()
	h.Routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ready")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestPrometheusEndpoint(t *testing.T) {
	cn := &captureNotifier{}
	h := newTestHandler(t, cn)
	mux := http.NewServeMux()
	h.Routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := []byte(`{"alerts":[{"labels":{"alertname":"CrashLoopBackOff","severity":"critical","instance":"10.0.0.5","namespace":"default","pod":"worker-1","service":"worker-service"},"annotations":{},"startsAt":"2026-06-17T10:00:00Z"}]}`)
	resp, err := http.Post(srv.URL+"/webhook/prometheus", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	require.Eventually(t, func() bool { return cn.NotifiedCount() == 1 }, time.Second, 10*time.Millisecond)
}

func TestGenericEndpoint(t *testing.T) {
	cn := &captureNotifier{}
	h := newTestHandler(t, cn)
	mux := http.NewServeMux()
	h.Routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := []byte(`{"alert_name":"CrashLoopBackOff","level":"critical","host":"10.0.0.5","environment":"default","container":"worker-1"}`)
	resp, err := http.Post(srv.URL+"/webhook/generic", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	require.Eventually(t, func() bool { return cn.NotifiedCount() == 1 }, time.Second, 10*time.Millisecond)
}

func TestAdminPatternsEndpoint(t *testing.T) {
	cn := &captureNotifier{}
	h := newTestHandler(t, cn)
	mux := http.NewServeMux()
	h.Routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/admin/patterns")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSlackInteractEndpoint_AppliesApproveDecision(t *testing.T) {
	cn := &captureNotifier{}
	h := newTestHandler(t, cn)
	require.NoError(t, h.Store.Save(context.Background(), types.Incident{ID: "inc-slack-1", Status: types.StatusEscalated}))

	mux := http.NewServeMux()
	h.Routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	form := url.Values{}
	form.Set("payload", `{"actions":[{"action_id":"approve","value":"inc-slack-1"}]}`)
	resp, err := http.PostForm(srv.URL+"/slack/interact", form)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	inc, ok, err := h.Store.Get(context.Background(), "inc-slack-1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "approved", inc.Status)
}

func TestIngestDemoAlert(t *testing.T) {
	cn := &captureNotifier{}
	h := newTestHandler(t, cn)

	h.IngestDemoAlert(context.Background(), tools.DemoAlert{
		AlertName: "CrashLoopBackOff", Severity: "critical", Namespace: "default", Pod: "worker-1", Service: "worker-service", Instance: "10.0.0.5",
	})

	require.Eventually(t, func() bool { return cn.NotifiedCount() == 1 }, time.Second, 10*time.Millisecond)
}

func TestUniversalEndpoint_GenericPayload(t *testing.T) {
	cn := &captureNotifier{}
	h := newTestHandler(t, cn)
	mux := http.NewServeMux()
	h.Routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := []byte(`{"alert_name":"CrashLoopBackOff","level":"critical","host":"10.0.0.5","environment":"default","container":"worker-1"}`)
	resp, err := http.Post(srv.URL+"/webhook/alert", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
}

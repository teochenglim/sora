package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teochenglim/sora/internal/webhook"
)

const prometheusPayload = `{
  "alerts": [
    {
      "labels": {"alertname":"CrashLoopBackOff","severity":"critical","instance":"10.0.0.5","namespace":"default","pod":"worker-1","service":"worker-service"},
      "annotations": {"summary":"pod is crash looping"},
      "startsAt": "2026-06-17T10:00:00Z"
    }
  ]
}`

func TestParsePrometheus_MapsLabelsToAlert(t *testing.T) {
	alerts, err := webhook.ParsePrometheus([]byte(prometheusPayload))
	require.NoError(t, err)
	require.Len(t, alerts, 1)

	a := alerts[0]
	assert.Equal(t, "prometheus", a.Source)
	assert.Equal(t, "CrashLoopBackOff", a.AlertName)
	assert.Equal(t, "critical", a.Severity)
	assert.Equal(t, "default", a.Namespace)
	assert.Equal(t, "worker-1", a.Pod)
	assert.Equal(t, "worker-service", a.Service)
	assert.NotEmpty(t, a.Fingerprint)
}

func TestParseGeneric_AppliesFieldMapping(t *testing.T) {
	mapping := map[string]string{
		"alertname": "alert_name",
		"severity":  "level",
		"instance":  "host",
		"namespace": "environment",
		"pod":       "container",
	}
	body := []byte(`{"alert_name":"HighCPU","level":"warning","host":"10.0.0.9","environment":"staging","container":"batch-1"}`)

	a, err := webhook.ParseGeneric(body, mapping)
	require.NoError(t, err)
	assert.Equal(t, "HighCPU", a.AlertName)
	assert.Equal(t, "warning", a.Severity)
	assert.Equal(t, "10.0.0.9", a.Instance)
	assert.Equal(t, "staging", a.Namespace)
	assert.Equal(t, "batch-1", a.Pod)
	assert.NotEmpty(t, a.Fingerprint)
}

func TestIsPrometheusPayload(t *testing.T) {
	assert.True(t, webhook.IsPrometheusPayload(map[string]any{"alerts": []any{}}))
	assert.False(t, webhook.IsPrometheusPayload(map[string]any{"alert_name": "x"}))
}

// Package webhook ingests alerts from Prometheus Alertmanager and generic
// JSON sources, normalizing them into types.Alert.
package webhook

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/teochenglim/sora/internal/types"
)

// alertmanagerPayload mirrors the subset of Alertmanager's webhook schema
// SORA needs.
type alertmanagerPayload struct {
	Alerts []struct {
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		StartsAt    string             `json:"startsAt"`
	} `json:"alerts"`
}

// IsPrometheusPayload reports whether raw looks like an Alertmanager webhook.
func IsPrometheusPayload(raw map[string]any) bool {
	_, ok := raw["alerts"]
	return ok
}

// ParsePrometheus converts an Alertmanager webhook body into one Alert per
// firing/resolved entry.
func ParsePrometheus(body []byte) ([]types.Alert, error) {
	var payload alertmanagerPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parsing prometheus payload: %w", err)
	}

	alerts := make([]types.Alert, 0, len(payload.Alerts))
	for _, a := range payload.Alerts {
		startsAt, _ := time.Parse(time.RFC3339, a.StartsAt)
		alert := types.Alert{
			Source:      "prometheus",
			AlertName:   a.Labels["alertname"],
			Severity:    a.Labels["severity"],
			Instance:    a.Labels["instance"],
			Namespace:   a.Labels["namespace"],
			Pod:         a.Labels["pod"],
			Service:     a.Labels["service"],
			Labels:      a.Labels,
			Annotations: a.Annotations,
			StartsAt:    startsAt,
		}
		if alert.Service == "" {
			alert.Service = a.Labels["job"]
		}
		finalize(&alert)
		alerts = append(alerts, alert)
	}
	return alerts, nil
}

// ParseGeneric converts a flat JSON body into a single Alert using the
// configured field-path mapping (configs/config.yaml source_mappings.generic).
func ParseGeneric(body []byte, mapping map[string]string) (types.Alert, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return types.Alert{}, fmt.Errorf("parsing generic payload: %w", err)
	}

	alert := types.Alert{
		Source:    "generic",
		AlertName: stringField(raw, mapping["alertname"]),
		Severity:  stringField(raw, mapping["severity"]),
		Instance:  stringField(raw, mapping["instance"]),
		Namespace: stringField(raw, mapping["namespace"]),
		Pod:       stringField(raw, mapping["pod"]),
		Service:   stringField(raw, mapping["service"]),
		StartsAt:  time.Now(),
	}
	finalize(&alert)
	return alert, nil
}

// stringField resolves a dot-separated path (e.g. "labels.alertname")
// against a generic decoded JSON map.
func stringField(raw map[string]any, path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(path, ".")
	var cur any = raw
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[p]
	}
	s, _ := cur.(string)
	return s
}

func finalize(a *types.Alert) {
	a.Fingerprint = fingerprint(*a)
	if a.ID == "" {
		a.ID = a.Fingerprint
	}
	if a.StartsAt.IsZero() {
		a.StartsAt = time.Now()
	}
}

func fingerprint(a types.Alert) string {
	h := sha256.Sum256([]byte(a.AlertName + a.Instance + a.Namespace + a.Pod))
	return hex.EncodeToString(h[:])
}

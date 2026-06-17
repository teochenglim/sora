package webhook

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	"github.com/teochenglim/sora/internal/classifier"
	"github.com/teochenglim/sora/internal/config"
	"github.com/teochenglim/sora/internal/dedup"
	"github.com/teochenglim/sora/internal/incident"
	"github.com/teochenglim/sora/internal/metrics"
	"github.com/teochenglim/sora/internal/notifier"
	"github.com/teochenglim/sora/internal/remediator"
	"github.com/teochenglim/sora/internal/tools"
	"github.com/teochenglim/sora/internal/types"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// Handler wires the webhook HTTP surface to the classification,
// deduplication, remediation and notification pipelines.
type Handler struct {
	Mode                         string
	Cfg                          *config.Config
	Classifier                   classifier.Classifier
	Deduper                      dedup.Deduper
	Engine                       *remediator.Engine // nil when mode does not remediate
	Notifiers                    []notifier.Notifier
	Store                        incident.Store
	Learning                     *remediator.LearningStore // nil unless remediation enabled
	Log                          *logrus.Logger
	BasicAuthUser, BasicAuthPass string
	StartedAt                    time.Time
}

// Routes registers all SORA HTTP endpoints on mux, including the embedded
// status dashboard served at "/" (see internal/webhook/static.go), the
// same way the Prometheus binary ships its own web UI.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("/webhook/alert", h.handleUniversal)
	mux.HandleFunc("/webhook/prometheus", h.handlePrometheus)
	mux.HandleFunc("/webhook/generic", h.handleGeneric)
	mux.HandleFunc("/slack/interact", h.handleSlackInteract)
	mux.HandleFunc("/health", h.handleHealth)
	mux.HandleFunc("/ready", h.handleReady)
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/admin/patterns", h.handlePatterns)
	mux.HandleFunc("/api/stats", h.handleStats)
	mux.HandleFunc("/api/config", h.handleConfigAPI)
	mux.Handle("/", http.FileServer(http.FS(staticFS())))
}

func (h *Handler) handleUniversal(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "reading body", http.StatusBadRequest)
		return
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if IsPrometheusPayload(raw) {
		h.ingestPrometheus(w, r, body)
		return
	}
	h.ingestGeneric(w, r, body, "generic")
}

func (h *Handler) handlePrometheus(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "reading body", http.StatusBadRequest)
		return
	}
	h.ingestPrometheus(w, r, body)
}

func (h *Handler) handleGeneric(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "reading body", http.StatusBadRequest)
		return
	}
	h.ingestGeneric(w, r, body, "generic")
}

func (h *Handler) ingestPrometheus(w http.ResponseWriter, r *http.Request, body []byte) {
	alerts, err := ParsePrometheus(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	metrics.AlertsReceived.WithLabelValues("prometheus", h.Mode).Add(float64(len(alerts)))
	for _, a := range alerts {
		h.processAsync(a)
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) ingestGeneric(w http.ResponseWriter, r *http.Request, body []byte, source string) {
	mapping := h.Cfg.SourceMappings[source]
	alert, err := ParseGeneric(body, mapping)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	alert.Source = source
	metrics.AlertsReceived.WithLabelValues(source, h.Mode).Inc()
	h.processAsync(alert)
	w.WriteHeader(http.StatusAccepted)
}

// processAsync hands an alert to process in the background and returns
// immediately, so the webhook HTTP response isn't held open for however
// long classification (a slow LLM call) or remediation (Tier-2's 90s
// budget, Tier-3's multi-minute human-approval wait) takes. Alertmanager
// and most webhook senders apply a short receive timeout (commonly 10s) —
// the response must not depend on the pipeline finishing.
//
// It deliberately uses a context detached from the request, since the
// request's context is canceled the moment the handler returns.
func (h *Handler) processAsync(alert types.Alert) {
	go h.process(context.Background(), alert)
}

func (h *Handler) process(ctx context.Context, alert types.Alert) {
	isDup, count, err := h.Deduper.CheckAndStore(ctx, alert.Fingerprint)
	if err != nil {
		h.Log.WithError(err).Error("dedup check failed")
		return
	}
	if isDup {
		metrics.DedupHits.Inc()
		h.Log.WithFields(logrus.Fields{"alertname": alert.AlertName, "occurrences": count}).Info("duplicate alert suppressed")
		return
	}

	release, ok, err := h.Deduper.AcquireLock(ctx, alert.Fingerprint, 5*time.Minute)
	if err != nil {
		h.Log.WithError(err).Error("lock acquisition failed")
		return
	}
	if !ok {
		h.Log.WithField("alertname", alert.AlertName).Info("alert already being processed by another replica")
		return
	}
	defer release(ctx)

	classified, err := h.Classifier.Classify(ctx, alert)
	if err != nil {
		h.Log.WithError(err).Error("classification failed")
		return
	}
	metrics.AlertsClassified.WithLabelValues(classified.Level, classified.ClassifiedBy).Inc()

	inc := types.Incident{ID: alert.ID, Alert: classified, Status: types.StatusOpen, CreatedAt: time.Now()}
	for _, n := range h.Notifiers {
		if err := n.Notify(ctx, inc); err != nil {
			h.Log.WithError(err).WithField("channel", n.Channel()).Error("notification failed")
			continue
		}
		metrics.NotificationsSent.WithLabelValues(n.Channel(), classified.Level).Inc()
	}

	if h.Engine == nil {
		return // classifier / notify-only modes stop here
	}
	if _, err := h.Engine.Process(ctx, classified, alert.ID); err != nil {
		h.Log.WithError(err).WithField("incident_id", alert.ID).Error("remediation failed")
	}
}

// IngestDemoAlert feeds a synthetic demo-mode alert through the same
// dedup -> classify -> notify -> remediate pipeline as a real webhook,
// asynchronously so a slow remediation (e.g. Tier-3's approval wait)
// doesn't stall the next tick of the demo alert generator.
func (h *Handler) IngestDemoAlert(_ context.Context, a tools.DemoAlert) {
	alert := types.Alert{
		Source:    "demo",
		AlertName: a.AlertName,
		Severity:  a.Severity,
		Instance:  a.Instance,
		Namespace: a.Namespace,
		Pod:       a.Pod,
		Service:   a.Service,
		StartsAt:  time.Now(),
	}
	finalize(&alert)
	metrics.AlertsReceived.WithLabelValues("demo", h.Mode).Inc()
	h.processAsync(alert)
}

func (h *Handler) handleSlackInteract(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	var payload struct {
		Actions []struct {
			ActionID string `json:"action_id"`
			Value    string `json:"value"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(r.FormValue("payload")), &payload); err != nil || len(payload.Actions) == 0 {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	action := payload.Actions[0]
	h.applyDecision(r.Context(), action.Value, action.ActionID)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) applyDecision(ctx context.Context, incidentID, actionID string) {
	var status string
	switch actionID {
	case "approve":
		status = remediator.DecisionApproved
	case "reject":
		status = remediator.DecisionRejected
	case "snooze":
		status = remediator.DecisionSnoozed
	default:
		return
	}
	if err := h.Store.UpdateStatus(ctx, incidentID, status); err != nil {
		h.Log.WithError(err).WithField("incident_id", incidentID).Error("failed to apply decision")
	}
}

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "mode": h.Mode, "version": Version})
}

func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, _, err := h.Deduper.CheckAndStore(ctx, "sora:readiness-probe"); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready", "reason": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) handlePatterns(w http.ResponseWriter, r *http.Request) {
	if h.BasicAuthUser != "" {
		user, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(h.BasicAuthUser)) != 1 ||
			subtle.ConstantTimeCompare([]byte(pass), []byte(h.BasicAuthPass)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="sora"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	if h.Learning == nil {
		writeJSON(w, http.StatusOK, []remediator.Pattern{})
		return
	}
	patterns, err := h.Learning.Patterns(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, patterns)
}

// statsResponse is the payload behind the embedded dashboard's "Status"
// and "Metrics" panels.
type statsResponse struct {
	Mode          string         `json:"mode"`
	Version       string         `json:"version"`
	StartedAt     time.Time      `json:"started_at"`
	UptimeSeconds float64        `json:"uptime_seconds"`
	Metrics       []metricSample `json:"metrics"`
}

type metricSample struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
	Value  float64           `json:"value"`
}

func (h *Handler) handleStats(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, statsResponse{
		Mode:          h.Mode,
		Version:       Version,
		StartedAt:     h.StartedAt,
		UptimeSeconds: time.Since(h.StartedAt).Seconds(),
		Metrics:       gatherMetrics(),
	})
}

// gatherMetrics flattens every sora_* metric registered with the default
// Prometheus registry into a flat list of {name, labels, value} samples,
// so the dashboard can render them as plain numbers without re-parsing
// the Prometheus text exposition format.
func gatherMetrics() []metricSample {
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return nil
	}
	var samples []metricSample
	for _, family := range families {
		name := family.GetName()
		if !strings.HasPrefix(name, "sora_") {
			continue
		}
		for _, m := range family.GetMetric() {
			labels := make(map[string]string, len(m.GetLabel()))
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			var value float64
			switch {
			case m.GetCounter() != nil:
				value = m.GetCounter().GetValue()
			case m.GetGauge() != nil:
				value = m.GetGauge().GetValue()
			case m.GetHistogram() != nil:
				value = float64(m.GetHistogram().GetSampleCount())
			default:
				continue
			}
			samples = append(samples, metricSample{Name: name, Labels: labels, Value: value})
		}
	}
	return samples
}

// handleConfigAPI returns a secret-redacted view of the running config for
// the dashboard's "Configuration" panel. Anything that could be a
// credential (API keys, webhook URLs, tokens, passwords) is replaced with
// a boolean "configured" flag instead of being echoed back.
func (h *Handler) handleConfigAPI(w http.ResponseWriter, _ *http.Request) {
	cfg := h.Cfg
	writeJSON(w, http.StatusOK, map[string]any{
		"mode": h.Mode,
		"server": map[string]any{
			"port": cfg.Server.Port,
		},
		"ai": map[string]any{
			"provider":     cfg.AI.Provider,
			"model":        cfg.AI.Model,
			"temperature":  cfg.AI.Temperature,
			"timeout":      cfg.AI.Timeout.String(),
			"max_retries":  cfg.AI.MaxRetries,
			"api_key_set":  cfg.AI.APIKey != "",
			"base_url_set": cfg.AI.BaseURL != "",
		},
		"dedup": map[string]any{
			"window_seconds": cfg.Dedup.WindowSeconds,
			"redis_set":      cfg.Dedup.RedisAddr != "",
		},
		"notifications": map[string]any{
			"slack_configured":     cfg.Notifications.Slack.WebhookURL != "",
			"telegram_configured":  cfg.Notifications.Telegram.BotToken != "",
			"pagerduty_configured": cfg.Notifications.PagerDuty.IntegrationKey != "",
		},
		"work_hours":        cfg.WorkHours,
		"business_owners":   redactBusinessOwners(cfg.BusinessOwners),
		"business_mappings": cfg.BusinessMappings,
		"remediation": map[string]any{
			"dry_run":              cfg.Remediation.DryRun,
			"tool_timeout":         cfg.Remediation.ToolTimeout.String(),
			"max_tier2_tool_calls": cfg.Remediation.MaxTier2ToolCalls,
			"approval_timeout":     cfg.Remediation.ApprovalTimeout.String(),
			"verification_wait":    cfg.Remediation.VerificationWait.String(),
			"service_whitelist":    cfg.Remediation.ServiceWhitelist,
			"service_blacklist":    cfg.Remediation.ServiceBlacklist,
		},
		"fallback_rules": cfg.FallbackRules,
	})
}

func redactBusinessOwners(owners []config.BusinessOwner) []map[string]any {
	out := make([]map[string]any, 0, len(owners))
	for _, o := range owners {
		out = append(out, map[string]any{
			"name":                o.Name,
			"slack_configured":    o.SlackID != "",
			"telegram_configured": o.TelegramID != "",
		})
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

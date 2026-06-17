// Package metrics centralizes all Prometheus metrics emitted by SORA.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	AlertsReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sora_alerts_received_total",
		Help: "Total alerts received by source and run mode.",
	}, []string{"source", "mode"})

	AlertsClassified = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sora_alerts_classified_total",
		Help: "Total alerts classified by level and classifier.",
	}, []string{"level", "classified_by"})

	ClassificationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "sora_classification_duration_seconds",
		Help: "Time taken to classify an alert.",
	}, []string{"provider"})

	DedupHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "sora_dedup_hits_total",
		Help: "Total alerts deduplicated.",
	})

	RemediationAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sora_remediation_attempts_total",
		Help: "Total remediation attempts by tier.",
	}, []string{"tier"})

	RemediationSuccess = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sora_remediation_success_total",
		Help: "Total successful remediations by tier and action.",
	}, []string{"tier", "action"})

	RemediationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "sora_remediation_duration_seconds",
		Help: "Time taken to complete a remediation attempt.",
	}, []string{"tier"})

	Escalations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sora_escalations_total",
		Help: "Total escalations by reason.",
	}, []string{"reason"})

	CircuitBreakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "sora_circuit_breaker_state",
		Help: "Circuit breaker state per component: 0=closed,1=open,2=half-open.",
	}, []string{"component"})

	NotificationsSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sora_notifications_sent_total",
		Help: "Total notifications sent by channel and level.",
	}, []string{"channel", "level"})

	RemediationVerified = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sora_remediation_verified_total",
		Help: "Total post-action verifications by outcome.",
	}, []string{"outcome"})
)

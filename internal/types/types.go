// Package types holds the data model shared by every SORA pipeline stage.
package types

import "time"

// Alert is the unified internal representation regardless of source.
type Alert struct {
	ID          string            `json:"id"`
	Source      string            `json:"source"`
	AlertName   string            `json:"alertname"`
	Severity    string            `json:"severity"`
	Instance    string            `json:"instance"`
	Namespace   string            `json:"namespace"`
	Pod         string            `json:"pod"`
	Service     string            `json:"service"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	StartsAt    time.Time         `json:"starts_at"`
	Fingerprint string            `json:"fingerprint"`
}

// ClassifiedAlert extends Alert with AI/rule classification output.
type ClassifiedAlert struct {
	Alert
	Level         string   `json:"level"` // P0, P1, P2
	BusinessLine  string   `json:"business_line"`
	RootCauseHint string   `json:"root_cause_hint"`
	Actions       []string `json:"recommended_actions"`
	Confidence    float64  `json:"confidence"`
	ClassifiedBy  string   `json:"classified_by"` // ai, rule, fallback
}

// ActionRecord tracks a single remediation action taken against an incident.
type ActionRecord struct {
	Tier       string    `json:"tier"`
	Action     string    `json:"action"`
	Params     string    `json:"params"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

// Incident tracks the full lifecycle of a classified alert.
type Incident struct {
	ID         string         `json:"id"`
	Alert      ClassifiedAlert `json:"alert"`
	Status     string         `json:"status"` // open, remediating, resolved, escalated, failed
	Actions    []ActionRecord `json:"actions"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	ResolvedAt *time.Time     `json:"resolved_at,omitempty"`
}

const (
	LevelP0 = "P0"
	LevelP1 = "P1"
	LevelP2 = "P2"

	StatusOpen        = "open"
	StatusRemediating = "remediating"
	StatusResolved    = "resolved"
	StatusEscalated   = "escalated"
	StatusFailed      = "failed"

	ClassifiedByAI       = "ai"
	ClassifiedByRule     = "rule"
	ClassifiedByFallback = "fallback"
)

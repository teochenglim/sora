package remediator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/teochenglim/sora/internal/incident"
	"github.com/teochenglim/sora/internal/notifier"
	"github.com/teochenglim/sora/internal/types"
)

// Tier3 escalates an incident to a human via chat notifiers and waits for
// an approve/reject decision (recorded by the webhook /slack/interact and
// Telegram callback handlers), auto-escalating to PagerDuty on timeout.
type Tier3 struct {
	notifiers       []notifier.Notifier
	store           incident.Store
	approvalTimeout time.Duration
	pollInterval    time.Duration
	pagerDutyKey    string
	httpClient      *http.Client
}

// NewTier3 creates a Tier3 engine.
func NewTier3(notifiers []notifier.Notifier, store incident.Store, approvalTimeout time.Duration, pagerDutyKey string) *Tier3 {
	return &Tier3{
		notifiers:       notifiers,
		store:           store,
		approvalTimeout: approvalTimeout,
		pollInterval:    500 * time.Millisecond,
		pagerDutyKey:    pagerDutyKey,
		httpClient:      &http.Client{Timeout: 10 * time.Second},
	}
}

// Decision values written to the incident's status by interactive callbacks
// while a Tier-3 escalation is pending.
const (
	DecisionApproved = "approved"
	DecisionRejected = "rejected"
	DecisionSnoozed  = "snoozed"
)

// Attempt sends the escalation and blocks (up to approvalTimeout) waiting
// for a human decision, polling the incident store. On timeout it pages
// PagerDuty as a final backstop.
func (t *Tier3) Attempt(ctx context.Context, inc types.Incident) (Outcome, error) {
	for _, n := range t.notifiers {
		if err := n.Escalate(ctx, inc); err != nil {
			return Outcome{}, fmt.Errorf("escalating via %s: %w", n.Channel(), err)
		}
	}
	if err := t.store.UpdateStatus(ctx, inc.ID, types.StatusEscalated); err != nil {
		return Outcome{}, fmt.Errorf("marking incident %s escalated: %w", inc.ID, err)
	}

	deadline := time.Now().Add(t.approvalTimeout)
	ticker := time.NewTicker(t.pollInterval)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return Outcome{}, ctx.Err()
		case <-ticker.C:
			cur, ok, err := t.store.Get(ctx, inc.ID)
			if err != nil {
				return Outcome{}, fmt.Errorf("polling incident %s: %w", inc.ID, err)
			}
			if !ok {
				continue
			}
			switch cur.Status {
			case DecisionApproved:
				return Outcome{Handled: true, Action: types.ActionRecord{Tier: "tier3", Action: "approved", Success: true}}, nil
			case DecisionRejected:
				return Outcome{Handled: true, Action: types.ActionRecord{Tier: "tier3", Action: "rejected", Success: false}}, nil
			case DecisionSnoozed:
				return Outcome{Handled: true, Action: types.ActionRecord{Tier: "tier3", Action: "snoozed", Success: false}}, nil
			}
		}
	}

	if err := t.pageOnCall(ctx, inc); err != nil {
		return Outcome{}, fmt.Errorf("paging on-call for %s: %w", inc.ID, err)
	}
	return Outcome{Handled: true, Action: types.ActionRecord{Tier: "tier3", Action: "pagerduty-escalation", Success: true}}, nil
}

func (t *Tier3) pageOnCall(ctx context.Context, inc types.Incident) error {
	if t.pagerDutyKey == "" {
		return nil
	}
	payload, err := json.Marshal(map[string]any{
		"routing_key":  t.pagerDutyKey,
		"event_action": "trigger",
		"payload": map[string]string{
			"summary":  fmt.Sprintf("SORA: no human response for incident %s (%s)", inc.ID, inc.Alert.AlertName),
			"source":   "sora",
			"severity": "critical",
		},
	})
	if err != nil {
		return fmt.Errorf("marshalling pagerduty event: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://events.pagerduty.com/v2/enqueue", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building pagerduty request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling pagerduty: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("pagerduty returned status %d", resp.StatusCode)
	}
	return nil
}

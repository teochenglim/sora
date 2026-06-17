package remediator

import (
	"context"
	"fmt"
	"time"

	"github.com/teochenglim/sora/internal/incident"
	"github.com/teochenglim/sora/internal/metrics"
	"github.com/teochenglim/sora/internal/types"
)

// Engine runs the full Tier1 -> Tier2 -> Tier3 decision chain for a
// classified alert, then verifies and records the outcome.
type Engine struct {
	tier1    *Tier1
	tier2    *Tier2
	tier3    *Tier3
	verifier *Verifier
	store    incident.Store
	learning *LearningStore
}

// NewEngine creates an Engine. tier3 and learning may be nil to disable
// human escalation / pattern learning respectively (e.g. notify-only mode).
func NewEngine(tier1 *Tier1, tier2 *Tier2, tier3 *Tier3, verifier *Verifier, store incident.Store, learning *LearningStore) *Engine {
	return &Engine{tier1: tier1, tier2: tier2, tier3: tier3, verifier: verifier, store: store, learning: learning}
}

// Process drives one classified alert through the tiers and returns the
// resulting Incident.
func (e *Engine) Process(ctx context.Context, alert types.ClassifiedAlert, incidentID string) (types.Incident, error) {
	inc := types.Incident{
		ID:        incidentID,
		Alert:     alert,
		Status:    types.StatusOpen,
		CreatedAt: time.Now(),
	}
	if err := e.store.Save(ctx, inc); err != nil {
		return inc, fmt.Errorf("saving new incident %s: %w", incidentID, err)
	}

	outcome, tier, err := e.runTiers(ctx, alert, incidentID)
	if err != nil {
		_ = e.store.UpdateStatus(ctx, incidentID, types.StatusFailed)
		return inc, err
	}
	if !outcome.Handled {
		_ = e.store.UpdateStatus(ctx, incidentID, types.StatusFailed)
		return inc, fmt.Errorf("no tier could handle incident %s", incidentID)
	}

	metrics.RemediationAttempts.WithLabelValues(tier).Inc()
	if err := e.store.AppendAction(ctx, incidentID, outcome.Action); err != nil {
		return inc, fmt.Errorf("recording action for %s: %w", incidentID, err)
	}

	if outcome.RequiresApproval {
		metrics.Escalations.WithLabelValues("requires_approval").Inc()
		return e.finalize(ctx, incidentID, types.StatusEscalated)
	}
	if !outcome.Action.Success {
		return e.finalize(ctx, incidentID, types.StatusFailed)
	}

	metrics.RemediationSuccess.WithLabelValues(tier, outcome.Action.Action).Inc()

	resolved, verr := e.verifier.Verify(ctx, alert, outcome.Action)
	if verr != nil {
		return e.finalize(ctx, incidentID, types.StatusFailed)
	}
	if !resolved {
		metrics.Escalations.WithLabelValues("verification_failed").Inc()
		return e.finalize(ctx, incidentID, types.StatusFailed)
	}

	if e.learning != nil {
		_ = e.learning.RecordSuccess(ctx, alert.AlertName, outcome.Action.Action)
	}
	return e.finalize(ctx, incidentID, types.StatusResolved)
}

func (e *Engine) runTiers(ctx context.Context, alert types.ClassifiedAlert, incidentID string) (Outcome, string, error) {
	outcome, err := e.tier1.Attempt(ctx, alert)
	if err != nil {
		return Outcome{}, "tier1", err
	}
	if outcome.Handled && !outcome.RequiresApproval {
		return outcome, "tier1", nil
	}
	if outcome.Handled && outcome.RequiresApproval {
		return e.escalate(ctx, alert, incidentID)
	}

	if e.tier2 != nil {
		outcome, err = e.tier2.Attempt(ctx, alert)
		if err != nil {
			return Outcome{}, "tier2", err
		}
		if outcome.Handled && !outcome.RequiresApproval {
			return outcome, "tier2", nil
		}
	}

	return e.escalate(ctx, alert, incidentID)
}

func (e *Engine) escalate(ctx context.Context, alert types.ClassifiedAlert, incidentID string) (Outcome, string, error) {
	if e.tier3 == nil {
		return Outcome{}, "tier3", fmt.Errorf("no tier3 escalation configured for %s", alert.AlertName)
	}
	inc := types.Incident{ID: incidentID, Alert: alert}
	outcome, err := e.tier3.Attempt(ctx, inc)
	return outcome, "tier3", err
}

func (e *Engine) finalize(ctx context.Context, id, status string) (types.Incident, error) {
	if err := e.store.UpdateStatus(ctx, id, status); err != nil {
		return types.Incident{}, fmt.Errorf("finalizing incident %s as %s: %w", id, status, err)
	}
	inc, _, err := e.store.Get(ctx, id)
	return inc, err
}

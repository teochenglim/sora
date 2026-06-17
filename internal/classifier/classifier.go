// Package classifier turns a raw Alert into a ClassifiedAlert. It tries the
// Tier-1 rule engine first (fast, free, handles the ~80% of alerts with a
// known pattern), falls through to AI (circuit-breaker guarded) for
// anything the rules don't match, and falls back to a severity-only
// mapping as a last resort when AI is unavailable or erroring.
package classifier

import (
	"context"
	"regexp"

	"github.com/teochenglim/sora/internal/cache"
	"github.com/teochenglim/sora/internal/circuit"
	"github.com/teochenglim/sora/internal/config"
	"github.com/teochenglim/sora/internal/types"
)

// Classifier is the public interface used by the rest of SORA.
type Classifier interface {
	Classify(ctx context.Context, alert types.Alert) (types.ClassifiedAlert, error)
}

// Orchestrator implements Classifier with the rule -> AI -> severity
// fallback chain (see package doc for the order and why).
type Orchestrator struct {
	ai       AIClassifier
	rule     *RuleClassifier
	breaker  *circuit.Breaker
	cache    *cache.ContextCache
	mappings []config.BusinessMapping
}

// New creates an Orchestrator. ai may be nil to force rule-only operation
// (used by --mode=demo and notify-only without AI credentials).
func New(ai AIClassifier, rule *RuleClassifier, breaker *circuit.Breaker, ctxCache *cache.ContextCache, mappings []config.BusinessMapping) *Orchestrator {
	return &Orchestrator{ai: ai, rule: rule, breaker: breaker, cache: ctxCache, mappings: mappings}
}

func (o *Orchestrator) Classify(ctx context.Context, alert types.Alert) (types.ClassifiedAlert, error) {
	if result, ok := o.rule.MatchRule(alert); ok {
		o.cache.Store(alert)
		return o.enrichBusinessLine(result), nil
	}

	if o.ai != nil && o.breaker.Allow() {
		similar := o.cache.Similar(alert.AlertName, alert.Namespace)
		result, err := o.ai.Classify(ctx, alert, similar)
		if err == nil {
			o.breaker.Record(true)
			o.cache.Store(alert)
			return o.enrichBusinessLine(result), nil
		}
		o.breaker.Record(false)
	}

	result, ok := o.rule.FallbackBySeverity(alert)
	if !ok {
		return types.ClassifiedAlert{}, &UnclassifiableError{AlertName: alert.AlertName}
	}
	o.cache.Store(alert)
	return o.enrichBusinessLine(result), nil
}

// enrichBusinessLine fills in BusinessLine from business_mappings when the
// classifier didn't already set one.
func (o *Orchestrator) enrichBusinessLine(c types.ClassifiedAlert) types.ClassifiedAlert {
	if c.BusinessLine != "" {
		return c
	}
	for _, m := range o.mappings {
		re, err := regexp.Compile(m.Pattern)
		if err != nil {
			continue
		}
		if re.MatchString(c.AlertName) {
			c.BusinessLine = m.BusinessLine
			return c
		}
	}
	return c
}

// UnclassifiableError is returned when neither AI nor any rule/fallback
// could classify the alert.
type UnclassifiableError struct {
	AlertName string
}

func (e *UnclassifiableError) Error() string {
	return "unable to classify alert: " + e.AlertName
}

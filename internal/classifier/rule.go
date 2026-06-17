package classifier

import (
	"fmt"
	"regexp"

	"github.com/teochenglim/sora/internal/config"
	"github.com/teochenglim/sora/internal/types"
)

// RuleClassifier matches alerts against configs/rules.yaml classifier_rules,
// and is used both as Tier-1 classification and as the AI fallback path.
type RuleClassifier struct {
	rules         *config.RulesStore
	fallbackRules map[string]string
}

// NewRuleClassifier creates a RuleClassifier backed by the given rules store.
func NewRuleClassifier(rules *config.RulesStore, fallbackRules map[string]string) *RuleClassifier {
	return &RuleClassifier{rules: rules, fallbackRules: fallbackRules}
}

// MatchRule is the Tier-1 fast path: it checks alert against
// configs/rules.yaml classifier_rules only (no severity fallback). This is
// tried before AI so the ~80% of alerts with a known pattern are
// classified in well under a second, for free, without an LLM call.
func (r *RuleClassifier) MatchRule(alert types.Alert) (types.ClassifiedAlert, bool) {
	for _, rule := range r.rules.Get().ClassifierRules {
		if !ruleMatches(rule, alert) {
			continue
		}
		return types.ClassifiedAlert{
			Alert:         alert,
			Level:         rule.Result.Level,
			BusinessLine:  rule.Result.BusinessLine,
			RootCauseHint: rule.Result.RootCauseHint,
			Actions:       rule.Result.Actions,
			Confidence:    1.0,
			ClassifiedBy:  types.ClassifiedByRule,
		}, true
	}
	return types.ClassifiedAlert{}, false
}

// FallbackBySeverity maps alert.Severity to a level via fallback_rules in
// config.yaml. It's the last resort, used only when no Tier-1 rule matched
// and AI is unavailable, circuit-open, or erroring.
func (r *RuleClassifier) FallbackBySeverity(alert types.Alert) (types.ClassifiedAlert, bool) {
	level, ok := r.fallbackRules[alert.Severity]
	if !ok {
		return types.ClassifiedAlert{}, false
	}
	return types.ClassifiedAlert{
		Alert:         alert,
		Level:         level,
		BusinessLine:  "platform",
		RootCauseHint: fmt.Sprintf("no rule matched; mapped from severity=%s", alert.Severity),
		Confidence:    0.5,
		ClassifiedBy:  types.ClassifiedByFallback,
	}, true
}

func ruleMatches(rule config.ClassifierRule, alert types.Alert) bool {
	if rule.Match.AlertNameRegex != "" {
		re, err := regexp.Compile(rule.Match.AlertNameRegex)
		if err != nil || !re.MatchString(alert.AlertName) {
			return false
		}
	}
	if rule.Match.Severity != "" && rule.Match.Severity != alert.Severity {
		return false
	}
	if rule.Match.Namespace != "" && rule.Match.Namespace != alert.Namespace {
		return false
	}
	return true
}

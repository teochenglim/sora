package remediator

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/teochenglim/sora/internal/config"
	"github.com/teochenglim/sora/internal/types"
)

// Tier1 is the rule-based remediation engine: regex-matched, no external
// calls beyond the actual action, executes in well under a second.
type Tier1 struct {
	rules       *config.RulesStore
	executor    *Executor
	rateLimiter RateLimiter
	remediation config.RemediationConfig
}

// NewTier1 creates a Tier1 engine.
func NewTier1(rules *config.RulesStore, executor *Executor, rl RateLimiter, remediation config.RemediationConfig) *Tier1 {
	return &Tier1{rules: rules, executor: executor, rateLimiter: rl, remediation: remediation}
}

// Outcome describes what a tier did with an incident.
type Outcome struct {
	Handled         bool
	RequiresApproval bool
	Action          types.ActionRecord
}

// Attempt tries to match alert against remediation_rules and, if matched
// and the service is whitelisted and within rate limit, executes the action.
func (t *Tier1) Attempt(ctx context.Context, alert types.ClassifiedAlert) (Outcome, error) {
	if !t.remediation.IsWhitelisted(alert.Service) {
		return Outcome{}, nil
	}

	for _, rule := range t.rules.Get().RemediationRules {
		if !tier1RuleMatches(rule, alert) {
			continue
		}
		if rule.RequireApproval {
			return Outcome{Handled: true, RequiresApproval: true, Action: types.ActionRecord{
				Tier: "tier1", Action: rule.Action, Params: fmt.Sprintf("rule=%s", rule.Name),
			}}, nil
		}

		limit := rule.MaxPerHour
		if limit <= 0 {
			limit = 5
		}
		allowed, err := t.rateLimiter.Allow(ctx, alert.Service, limit, time.Hour)
		if err != nil {
			return Outcome{}, fmt.Errorf("checking rate limit for %s: %w", alert.Service, err)
		}
		if !allowed {
			return Outcome{Handled: true, RequiresApproval: true, Action: types.ActionRecord{
				Tier: "tier1", Action: rule.Action, Params: "rate-limit-exceeded",
			}}, nil
		}

		started := time.Now()
		params := map[string]string{"namespace": alert.Namespace, "service": alert.Service, "pod": alert.Pod}
		result, execErr := t.executor.Run(ctx, rule.Action, params)
		record := types.ActionRecord{
			Tier: "tier1", Action: rule.Action, Params: fmt.Sprintf("rule=%s", rule.Name),
			Success: execErr == nil && result.Success, StartedAt: started, FinishedAt: time.Now(),
		}
		if execErr != nil {
			record.Error = execErr.Error()
			return Outcome{Handled: true, Action: record}, execErr
		}
		return Outcome{Handled: true, Action: record}, nil
	}
	return Outcome{}, nil
}

func tier1RuleMatches(rule config.RemediationRule, alert types.ClassifiedAlert) bool {
	if rule.Match.AlertNameRegex != "" {
		re, err := regexp.Compile(rule.Match.AlertNameRegex)
		if err != nil || !re.MatchString(alert.AlertName) {
			return false
		}
	}
	if rule.Match.RestartCountLt != nil {
		v, ok := alert.Labels["restart_count"]
		if !ok {
			return false
		}
		count, err := strconv.Atoi(v)
		if err != nil || count >= *rule.Match.RestartCountLt {
			return false
		}
	}
	return true
}

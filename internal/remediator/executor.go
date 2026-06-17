// Package remediator implements SORA's tiered decision engine: a fast
// rule-based Tier 1, an LLM plan-and-execute Tier 2, and human escalation
// Tier 3, plus post-action verification and pattern learning.
package remediator

import (
	"context"
	"fmt"
	"time"

	"github.com/teochenglim/sora/internal/tools"
)

// Executor runs a named tool action idempotently, retrying transient
// failures with exponential backoff.
type Executor struct {
	registry   *tools.Registry
	maxRetries int
	dryRun     bool
}

// NewExecutor creates an Executor.
func NewExecutor(registry *tools.Registry, maxRetries int, dryRun bool) *Executor {
	return &Executor{registry: registry, maxRetries: maxRetries, dryRun: dryRun}
}

// Run executes the named tool with the given params, retrying on error.
// In dry-run mode it logs the intended action and returns success without
// calling the tool.
func (e *Executor) Run(ctx context.Context, action string, params map[string]string) (tools.Result, error) {
	if e.dryRun {
		return tools.Result{Output: fmt.Sprintf("dry-run: would execute %s with %v", action, params), Success: true}, nil
	}

	tool, ok := e.registry.Get(action)
	if !ok {
		return tools.Result{}, fmt.Errorf("unknown tool/action %q", action)
	}

	var lastErr error
	for attempt := 0; attempt <= e.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			case <-ctx.Done():
				return tools.Result{}, ctx.Err()
			}
		}
		result, err := tool.Execute(ctx, params)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	return tools.Result{}, fmt.Errorf("action %s failed after %d attempts: %w", action, e.maxRetries+1, lastErr)
}

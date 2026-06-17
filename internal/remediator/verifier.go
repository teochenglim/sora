package remediator

import (
	"context"
	"time"

	"github.com/teochenglim/sora/internal/metrics"
	"github.com/teochenglim/sora/internal/tools"
	"github.com/teochenglim/sora/internal/types"
)

// Verifier re-checks pod/service status after a remediation action and
// triggers a rollback if the underlying condition is still unresolved.
type Verifier struct {
	executor *Executor
	wait     time.Duration
}

// NewVerifier creates a Verifier.
func NewVerifier(executor *Executor, wait time.Duration) *Verifier {
	return &Verifier{executor: executor, wait: wait}
}

// Verify waits the configured grace period, then re-queries pod status.
// If the pod is not Running, it attempts a rollback (re-running the
// inverse/retry action) and reports resolved=false so the caller escalates.
func (v *Verifier) Verify(ctx context.Context, alert types.ClassifiedAlert, lastAction types.ActionRecord) (resolved bool, err error) {
	select {
	case <-time.After(v.wait):
	case <-ctx.Done():
		return false, ctx.Err()
	}

	result, err := v.executor.Run(ctx, tools.ToolQueryPodStatus, map[string]string{
		"namespace": alert.Namespace, "pod": alert.Pod,
	})
	if err != nil {
		metrics.RemediationVerified.WithLabelValues("error").Inc()
		return false, err
	}

	if result.Success {
		metrics.RemediationVerified.WithLabelValues("resolved").Inc()
		return true, nil
	}

	metrics.RemediationVerified.WithLabelValues("unresolved").Inc()
	_ = v.rollback(ctx, alert, lastAction)
	return false, nil
}

// rollback reverses a reversible action. Only restart_service is currently
// treated as reversible (re-issuing it is itself idempotent and safe);
// other actions are left as-is and rely on Tier-3 escalation instead.
func (v *Verifier) rollback(ctx context.Context, alert types.ClassifiedAlert, lastAction types.ActionRecord) error {
	if lastAction.Action != tools.ToolRestartService {
		return nil
	}
	_, err := v.executor.Run(ctx, tools.ToolRestartService, map[string]string{
		"namespace": alert.Namespace, "service": alert.Service,
	})
	return err
}

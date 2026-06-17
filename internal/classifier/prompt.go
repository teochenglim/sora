package classifier

import (
	"fmt"
	"strings"

	"github.com/teochenglim/sora/internal/types"
)

const systemPrompt = `You are SORA, an SRE alert triage assistant. Classify the incoming alert into a severity level and recommend actions.

Level definitions:
- P0: production down, data loss risk, SLA breach imminent.
- P1: degraded service, latency spike >3x baseline, partial outage.
- P2: warning, capacity concern, non-critical anomaly.

Respond with ONLY a JSON object matching this schema, no prose:
{"level":"P0|P1|P2","business_line":"string","root_cause_hint":"string","recommended_actions":["string"],"confidence":0.0}`

// BuildUserPrompt renders the alert (and any similar recent alerts) into
// the user message sent to the model.
func BuildUserPrompt(a types.Alert, similar []types.Alert) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Alert: %s\nSeverity: %s\nNamespace: %s\nPod: %s\nService: %s\nInstance: %s\n",
		a.AlertName, a.Severity, a.Namespace, a.Pod, a.Service, a.Instance)
	if len(a.Labels) > 0 {
		fmt.Fprintf(&b, "Labels: %v\n", a.Labels)
	}
	if len(a.Annotations) > 0 {
		fmt.Fprintf(&b, "Annotations: %v\n", a.Annotations)
	}
	if len(similar) > 0 {
		b.WriteString("\nRecently seen similar alerts (most recent first):\n")
		for _, s := range similar {
			fmt.Fprintf(&b, "- %s at %s (pod=%s)\n", s.AlertName, s.StartsAt.Format("15:04:05"), s.Pod)
		}
	}
	return b.String()
}

# SORA — Service Operations Remediation Agent

Go 1.26+ binary unifying AI alert classification and tiered K8s auto-remediation.

## Dictionary

| Term | Meaning |
|---|---|
| `--mode` | `classifier`\|`remediation`\|`full`(default)\|`notify-only`\|`demo` |
| Tier 1 | YAML rule engine, <1s, no LLM, 80% of incidents |
| Tier 2 | LLM plan-and-execute, ≤3 tool calls, confidence gate (<0.8 → Tier 3) |
| Tier 3 | Human escalation via Slack/Telegram, 2min timeout → PagerDuty |
| Fingerprint | `sha256(alertname+instance+namespace+pod)`, dedup window 5min |
| P0/P1/P2 | Outage/data-loss / degraded·latency·partial / warning·capacity |
| Circuit breaker | 50% failure over last 10 AI calls → disable AI, half-open probe 60s |
| Learning | Successful Tier2/3 resolutions stored in SQLite, promoted to Tier1 at ≥5 successes |

## Layout
`cmd/sora` entry · `internal/{classifier,remediator,tools,dedup,notifier,webhook,incident,cache,circuit,config}` · `pkg/logger` · `configs/*.yaml` · `deployments/{k8s,helm,docker}` · `tests/{unit,integration}` · `design/DESIGN.md` (full original spec).

## Commands
`make` (no args) prints all targets. `make build|test|lint|run-demo|run-local|coverage`.

## Conventions
- Every interface has a mock (see `internal/tools/mock.go`, `internal/notifier` mocks in tests).
- Errors wrapped with `fmt.Errorf("...: %w", err)`. No global mutable state — constructor DI.
- All external calls (AI/K8s/Redis/Slack/Telegram) take `context.Context` with timeout.
- Config: `configs/config.yaml` (Viper, `${ENV_VAR}` substitution, SIGHUP hot-reload for rules.yaml).

## Details live in skills, not here
For prompt schemas, rule YAML structure, metric names, API endpoints, Helm/K8s specifics, and the full architecture diagram: invoke the `sora-architecture` skill. Don't duplicate that content here — this file stays a quick-reference dictionary only.

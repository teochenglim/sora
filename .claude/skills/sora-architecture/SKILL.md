---
name: sora-architecture
description: Full SORA architecture reference — prompt schemas, rules.yaml structure, config.yaml sections, metric names, API endpoints, K8s/Helm/Docker specifics. Use when implementing/modifying classifier, remediator, webhook, notifier, deployment manifests, or when the quick CLAUDE.md dictionary isn't enough detail.
---

# SORA Architecture Reference

Full original spec: `design/DESIGN.md`. This skill condenses the parts needed during implementation so CLAUDE.md doesn't have to carry them every turn.

## Data model
```go
type Alert struct {
    ID, Source, AlertName, Severity, Instance, Namespace, Pod, Service string
    Labels, Annotations map[string]string
    StartsAt time.Time
    Fingerprint string // sha256(alertname+instance+namespace+pod)
}
type ClassifiedAlert struct {
    Alert
    Level string // P0|P1|P2
    BusinessLine, RootCauseHint string
    Actions []string
    Confidence float64 // <0.8 -> escalate
    ClassifiedBy string // ai|rule|fallback
}
type Incident struct {
    ID string
    Alert ClassifiedAlert
    Status string // open|remediating|resolved|escalated|failed
    Actions []ActionRecord
    CreatedAt, UpdatedAt time.Time
    ResolvedAt *time.Time
}
```

## AI classifier output schema
```json
{"level":"P0|P1|P2","business_line":"string","root_cause_hint":"string","recommended_actions":["string"],"confidence":0.0}
```
P0 = down/data-loss/SLA breach imminent. P1 = degraded, latency>3x baseline, partial outage. P2 = warning/capacity/non-critical.
AI client: OpenAI-compatible interface (works with Anthropic, OpenAI, DeepSeek, Ollama/LiteLLM via `LLM_BASE_URL`/`LLM_MODEL`/`LLM_PROVIDER`/`LLM_TEMPERATURE`, each also settable as `${VAR:-default}` directly in config.yaml). 8s timeout, 2 retries exponential backoff — bump via `LLM_TIMEOUT`/`SERVER_TIMEOUT` for slow local/reasoning models (`server.write_timeout` must stay above `ai.timeout`, since Go's `http.Server.WriteTimeout` spans the whole handler call). Raw LLM content is passed through `internal/llmjson.Extract` before `json.Unmarshal`, since some models (notably local reasoning models like Ollama's) wrap JSON answers in a ` ```json ` fence despite being told not to.

**Classification order matters**: `Orchestrator.Classify` tries the Tier-1 rule engine first (`RuleClassifier.MatchRule` — fast, free, ~80% of alerts), then AI only on a miss, then `RuleClassifier.FallbackBySeverity` as a last resort if AI is unavailable/erroring. AI must never run when a Tier-1 rule already matched.

## rules.yaml structure
```yaml
classifier_rules:
  - name: "high-cpu-warning"
    match: {alertname_regex: "HighCPU.*", severity: "warning"}
    result: {level: P2, business_line: platform, root_cause_hint: "...", actions: ["..."]}
remediation_rules:
  - name: "crashloop-auto-restart"
    match: {alertname_regex: "CrashLoopBackOff", restart_count_lt: 3}
    action: restart_pod
    require_approval: false
    max_per_hour: 3
  - name: "oom-needs-approval"
    match: {alertname_regex: "OOMKilled"}
    action: increase_memory_limit
    require_approval: true
```

## config.yaml sections
`server` (port/timeouts) · `mode` · `ai` (provider/api_key/model/temperature/timeout/max_retries) · `dedup` (window_seconds/redis_addr/redis_password) · `notifications` (slack/telegram/pagerduty) · `work_hours` (start/end/timezone/days) · `business_owners` · `business_mappings` (regex→business_line) · `remediation` (dry_run/kubeconfig/namespace/tool_timeout/max_tier2_tool_calls/approval_timeout/verification_wait/service_whitelist/service_blacklist) · `fallback_rules` · `source_mappings` (per-source field paths).

## Tiered remediation
- **Tier1** (`internal/remediator/tier1.go`): regex match on rules.yaml, <1s, no external calls, service whitelist enforced, rate limit 5 restarts/service/hour via Redis counter.
- **Tier2** (`tier2.go`): one LLM call → plan with ≤3 tool calls from `{query_pod_status, query_logs, restart_service}`. confidence<0.8 → Tier3. Dry-run logs only. 30s/tool, 90s total budget.
- **Tier3** (`tier3.go`): Slack Block Kit + Telegram inline keyboard, Approve/Reject/Snooze, 2min window → auto-escalate PagerDuty. Approval state in Redis.
- **Verifier**: re-check status post-action; unresolved after 60s → rollback reversible action + escalate; emits `sora_remediation_verified_total`.
- **Learning**: on success, store `(fingerprint_pattern, actions)` in SQLite (`modernc.org/sqlite`, pure Go — not `mattn/go-sqlite3` — so `CGO_ENABLED=0` cross-compilation and the static Docker build both work); hourly job promotes patterns with ≥5 successes to Tier1 YAML; `/admin/patterns` (basic-auth) to view.

## Notifications
Slack: Block Kit, P0→@channel, P1→@here, P2→DM owner only during work hours. Telegram: MarkdownV2, inline keyboard, group + per-owner chat IDs. Notifier is an interface — adding a new IM channel means implementing it, no other changes (`shall allow expand to other IM in future`).

## Safety rails
`service_whitelist`/`service_blacklist` in config, Redis rate limits, distributed lock per incident ID, dry-run mode, K8s circuit breaker halts all actions on API error threshold.

## Metrics (Prometheus)
`sora_alerts_received_total{source,mode}` · `sora_alerts_classified_total{level,classified_by}` · `sora_classification_duration_seconds{provider}` · `sora_dedup_hits_total` · `sora_remediation_attempts_total{tier}` · `sora_remediation_success_total{tier,action}` · `sora_remediation_duration_seconds{tier}` · `sora_escalations_total{reason}` · `sora_circuit_breaker_state{component}` (0/1/2) · `sora_notifications_sent_total{channel,level}`.

## API endpoints
```
POST /webhook/alert        # universal
POST /webhook/prometheus
POST /webhook/generic
POST /slack/interact       # interactive button callbacks
GET  /health                # {"status":"ok","mode":"...","version":"..."}
GET  /ready                 # checks Redis + AI reachability
GET  /metrics
GET  /admin/patterns         # basic-auth
GET  /                        # embedded status dashboard (internal/webhook/static/, go:embed)
GET  /api/stats               # mode/version/uptime + every sora_* metric as {name,labels,value}
GET  /api/config              # running config, secrets redacted to *_set/*_configured booleans
```

All three `/webhook/*` handlers and `IngestDemoAlert` hand off to `Handler.processAsync` (a detached goroutine, `context.Background()` not the request context) and return `202` immediately — dedup/classify/notify/remediate never block the HTTP response, since remediation can include a multi-minute Tier-3 approval wait.

## Deployment
Dockerfile: multi-stage `golang:1.26-alpine` build, true static binary (`CGO_ENABLED=0` — safe because the learning store uses the pure-Go `modernc.org/sqlite`) → `alpine:3.19` runtime, non-root, `ENTRYPOINT ["/sora"]`, default `CMD ["--mode=full","--config=/etc/sora/config.yaml"]`.
K8s: 2+ replicas, requests 100m/128Mi, limits 500m/512Mi, liveness `/health`@15s, readiness `/ready`@10s (initial delay 5s), rolling update maxUnavailable:0, HPA min2/max10 on CPU>70%, pod+container `securityContext` (`runAsNonRoot`, `allowPrivilegeEscalation: false`, `capabilities: drop: [ALL]`).
Helm: `values.yaml` exposes image/replicas/mode/resources/config/secrets; templates use `_helpers.tpl` for name/labels; same `securityContext` as the plain manifest.

## CI/CD
- `.github/workflows/ci.yml`: vet/build/test, then a linux/amd64+arm64 build matrix and a multi-arch Docker push to `ghcr.io/teochenglim/sora:latest` on `main`.
- `.github/workflows/security.yml`: Semgrep (`p/golang`,`p/secrets`,`p/owasp-top-ten`,`p/sql-injection`,`p/dockerfile`,`p/kubernetes`) + Trivy (deps + built image), SARIF uploaded to the Security tab, on push/PR/weekly cron. Local equivalent: `make security`.
- `.github/workflows/release.yml`: triggers on `v[0-9]+.[0-9]+.[0-9]+` tags, gated by tests, builds linux/darwin × amd64/arm64 checksummed tarballs, publishes a GitHub Release explicitly pinned to that tag (`tag_name`/`name`/`target_commitish` all set from `github.ref_name`/`github.sha`, never inferred), and pushes the versioned Docker image. Cut one with `make release VERSION=x.y.z` — it refuses to run unless the working tree is clean, local `HEAD` matches the pushed remote branch, and `make test` passes, so the tag is always bound to tested code that's actually on GitHub. `make release-dry-run` builds the same artifacts locally without tagging/pushing anything.

## Demo mode
Must run with zero external deps: in-memory dedup/cache, mock K8s tools, mock alert generator firing every 30s, no real AI/Slack/Telegram credentials required.

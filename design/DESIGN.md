The ask

- have an excellent and concise "claude.md" summarise below for absolutely necessary prompt allow to explain the what and how but as a dictionary. 
- the further long details will create ./.claude/skills/... skill to explain further details. This is to avoid per turn loading unessacry details using claude skill best pratice is not enough, i need to save token also. keep it short and smart with the "Claude Skills" and claude.md
- implement the fully functional code
- write the proper test and the test shall support unit function that this software is working as designed
- Build the full agent harness around it such as "claude code hook", "git pre-commit", "github action". Do at least 1 each of 3.
- have proper .gitignore
- have matrix build for amd and arm
- will open source using apache 2.0
- Makefile as first class citizen for the entire development cycle, and "make" alone will give the print out
- allow this repo to be clone and above attempt is to allow other developer to get up to speed within the "single folder"

The architecture diagram 

╔══════════════════════════════════════════════════════════════════════╗
║                         INGESTION LAYER                              ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                      ║
║  [Prometheus Alertmanager]  [Generic Webhooks]                       ║
║           │                        │                                 ║
║           └──────────┬─────────────┘                                 ║
║                      ▼                                               ║
║             [Webhook Handler]                                        ║
║         /webhook/alert  /webhook/{source}                            ║
║                      │                                               ║
║                      ▼                                               ║
║             [Deduplication]                                          ║
║           Redis (Valkey) · 5min window                               ║
║                                                                      ║
╠══════════════════════════════════════════════════════════════════════╣
║                     CLASSIFICATION ENGINE                            ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                      ║
║  [Tier 1: Rule Engine] --miss--> [Tier 2: AI Classifier] --<0.8-->   ║
║   YAML rules · <1s                Claude/OpenAI                      ║
║   80% of incidents                Confidence scoring        [Tier 3] ║
║   P0 / P1 / P2                    Business line tagging    Human Esc ║
║        │                               │                    Slack/TG ║
║        └───────────────┬───────────────┘                             ║
║                        ▼                                             ║
╠══════════════════════════════════════════════════════════════════════╣
║                    ACTION & REMEDIATION                              ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                      ║
║  [Action Executor] → [K8s Tools] → [Verification]                    ║
║   Idempotent            query_pod_status   Re-check post-action      ║
║   Retry + backoff       query_logs         Rollback if unresolved    ║
║   Rate limit            restart_service                              ║
║                                                                      ║
╠══════════════════════════════════════════════════════════════════════╣
║               PERSISTENCE · LEARNING · OBSERVABILITY                 ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                      ║
║  [Redis]          [SQLite]         [Context Cache]                   ║
║   Incidents        Learned          15min TTL                        ║
║   Dedup            patterns         Similarity query                 ║
║   Dist locks       → Tier 1 rules                                    ║
║                                                                      ║
║  [Metrics]        [Logging]         [Tracing]                        ║
║   Prometheus       logrus/slog       OpenTelemetry                   ║
║   /metrics         JSON output       OTLP export                     ║
║                                                                      ║
╠══════════════════════════════════════════════════════════════════════╣
║                          RUN MODES                                   ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                      ║
║  --mode=classifier    --mode=remediation    --mode=full (default)    ║
║  --mode=notify-only   --mode=demo                                    ║
║                                                                      ║
╚══════════════════════════════════════════════════════════════════════╝


The SORA

You are an expert Go developer. Build SORA (Service Operations Remediation Agent) — a production-grade, AI-powered SRE platform in Go 1.21+.

SORA unifies two capabilities into one binary:
1. **Alert Classifier** — AI-powered alert triage, severity assignment, and smart notifications
2. **Auto-Remediator** — autonomous incident response with tiered decision-making and K8s repair actions

The binary must support selectable run modes via a `--mode` flag:
- `--mode=classifier`     → Run only the alert classification + notification pipeline
- `--mode=remediation`    → Run only the auto-remediation pipeline
- `--mode=full`           → Run both pipelines (default)
- `--mode=notify-only`    → Classification + notification, no remediation actions
- `--mode=demo`           → Simulate alerts with mock tools, no real K8s or notifications

---

## Project Name & Branding
Name: **SORA** — Service Operations Remediation Agent
Binary: `sora`
All log lines prefix with `[SORA]`

---

## Project Structure
sora/
├── cmd/sora/
│   └── main.go                    # Entry point, mode selection, graceful shutdown
├── internal/
│   ├── classifier/
│   │   ├── ai.go                  # Anthropic/OpenAI client, prompt execution
│   │   ├── rule.go                # Rule-based fallback classifier
│   │   ├── prompt.go              # Prompt templates, output schema
│   │   └── classifier.go          # Interface + orchestrator (AI → rule fallback)
│   ├── remediator/
│   │   ├── engine.go              # Tiered decision engine (T1 → T2 → T3)
│   │   ├── tier1.go               # Rule engine (YAML rules, <1s, no LLM)
│   │   ├── tier2.go               # LLM plan-and-execute (max 3 tool calls)
│   │   ├── tier3.go               # Human escalation (Slack/Telegram approve/reject)
│   │   ├── executor.go            # Idempotent action executor with retry
│   │   ├── verifier.go            # Post-action verification + rollback
│   │   └── learning.go            # Pattern storage + Tier 1 promotion
│   ├── tools/
│   │   ├── interface.go           # Tool interface
│   │   ├── k8s.go                 # query_pod_status, query_logs, restart_service
│   │   └── mock.go                # Demo-mode mock implementations
│   ├── dedup/
│   │   └── dedup.go               # Redis + in-memory dedup, 5min sliding window, distributed lock
│   ├── notifier/
│   │   ├── interface.go           # Notifier interface
│   │   ├── slack.go               # Slack webhooks, block kit, @mentions, approve/reject buttons
│   │   └── telegram.go            # Telegram Bot API, Markdown, @mentions
│   ├── webhook/
│   │   ├── handler.go             # HTTP handlers for all endpoints
│   │   └── parser.go              # Multi-source parser (Prometheus + generic JSON)
│   ├── incident/
│   │   └── store.go               # Redis-backed incident state (ID, service, actions, timestamps)
│   ├── cache/
│   │   └── context.go             # 15-min TTL context cache, alertname/namespace similarity query
│   ├── circuit/
│   │   └── breaker.go             # Circuit breaker for AI (50% failure → auto-disable) and K8s API
│   └── config/
│       ├── config.go              # All config structs
│       └── loader.go              # Viper loader, env substitution, hot-reload
├── pkg/
│   └── logger/
│       └── logger.go              # logrus structured logger, JSON output
├── configs/
│   ├── config.yaml                # Full example config with all sections
│   ├── prompts.yaml               # AI prompt templates
│   └── rules.yaml                 # Tier 1 rule engine rules + fallback classifier rules
├── deployments/
│   ├── k8s/
│   │   ├── deployment.yaml        # With liveness/readiness probes, resource limits
│   │   ├── service.yaml
│   │   ├── configmap.yaml
│   │   ├── secrets.yaml
│   │   └── hpa.yaml               # HorizontalPodAutoscaler
│   ├── helm/
│   │   ├── Chart.yaml
│   │   ├── values.yaml
│   │   └── templates/             # All K8s resources as Helm templates
│   └── docker/
│       ├── Dockerfile             # Multi-stage, alpine, <50MB
│       └── docker-compose.yaml    # Includes Redis, Prometheus, mock alertmanager
├── tests/
│   ├── unit/
│   │   ├── dedup_test.go
│   │   ├── classifier_test.go
│   │   ├── tier1_test.go
│   │   └── parser_test.go
│   └── integration/
│       └── webhook_test.go
├── .env.example
├── Makefile
├── go.mod
└── README.md

---

## Shared Data Model

```go
// Alert is the unified internal representation regardless of source
type Alert struct {
    ID          string
    Source      string            // "prometheus", "generic", etc.
    AlertName   string
    Severity    string            // raw input severity
    Instance    string
    Namespace   string
    Pod         string
    Service     string
    Labels      map[string]string
    Annotations map[string]string
    StartsAt    time.Time
    Fingerprint string            // sha256(alertname+instance+namespace+pod)
}

// ClassifiedAlert extends Alert with AI/rule output
type ClassifiedAlert struct {
    Alert
    Level          string   // "P0", "P1", "P2"
    BusinessLine   string
    RootCauseHint  string
    Actions        []string // recommended actions
    Confidence     float64  // 0.0–1.0; <0.8 triggers human escalation
    ClassifiedBy   string   // "ai", "rule", "fallback"
}

// Incident tracks the full lifecycle
type Incident struct {
    ID             string
    Alert          ClassifiedAlert
    Status         string    // "open", "remediating", "resolved", "escalated", "failed"
    Actions        []ActionRecord
    CreatedAt      time.Time
    UpdatedAt      time.Time
    ResolvedAt     *time.Time
}
```

---

## Feature Requirements

### 1. Webhook Ingestion (`internal/webhook/`)
- `POST /webhook/alert` — universal receiver
- `POST /webhook/prometheus` — Prometheus Alertmanager format
- `POST /webhook/generic` — generic JSON with configurable field mapping
- `GET /health` — returns `{"status":"ok","mode":"full","version":"..."}` 
- `GET /ready` — readiness probe (checks Redis connection, AI reachability)
- `GET /metrics` — Prometheus metrics endpoint
- Per-source field mapping from config (configurable `source_mappings`)
- Context-aware handlers with configurable request timeouts

### 2. Deduplication (`internal/dedup/`)
- Fingerprint: `sha256(alertname + instance + namespace + pod)`
- 5-minute sliding window; increment occurrence counter on duplicate
- Redis primary store; in-memory fallback when Redis unavailable
- Thread-safe; distributed lock prevents duplicate processing across replicas
- Returns: `isDuplicate bool, occurrenceCount int`

### 3. Alert Classifier (`internal/classifier/`)

**AI Path (ai.go):**
- Support both Anthropic (claude-3-5-sonnet) and OpenAI (gpt-4o) via `LLM_PROVIDER` env
- AI interface shall be openai compatible so that all other major llm provider can help such as deepseek and ollama/litellm
- env such as LLM_BASE_URL, LLM_MODEL, LLM_TEMPERATURE ...
- 8-second timeout, 2 retries with exponential backoff
- Structured JSON output schema:
```json
  {
    "level": "P0|P1|P2",
    "business_line": "string",
    "root_cause_hint": "string",
    "recommended_actions": ["string"],
    "confidence": 0.0
  }
```
- P-level definitions in prompt:
  - P0: production down, data loss risk, SLA breach imminent
  - P1: degraded service, latency spike >3x baseline, partial outage
  - P2: warning, capacity concern, non-critical anomaly

**Rule-based fallback (rule.go):**
- Loaded from `configs/rules.yaml`
- Match by: alertname pattern (regex), severity label, namespace
- Assign P-level + business line + canned actions
- Used when AI fails OR as Tier 1 classifier

**Circuit Breaker (circuit/breaker.go):**
- Track AI call success/failure rate
- 50% failure rate over last 10 calls → auto-disable AI, use rule fallback only
- Half-open probe every 60 seconds

**Context Cache (cache/context.go):**
- 15-min TTL per alert fingerprint
- Store last 5 similar alerts for context injection into AI prompt
- Query by alertname prefix and namespace

### 4. Tiered Remediator (`internal/remediator/`)

**Tier 1 — Rule Engine (tier1.go):**
- Loaded from `configs/rules.yaml` (same file, separate section)
- Example rules:
```yaml
  - match:
      alertname: "CrashLoopBackOff"
      restart_count_lt: 3
    action: restart_pod
    require_approval: false
  - match:
      alertname: "OOMKilled"
    action: increase_memory_limit
    require_approval: true
```
- Must execute in <1s; no external calls
- Service whitelist enforcement
- Rate limit: max 5 restarts per service per hour (Redis counter, use Valkey)

**Tier 2 — LLM Plan-and-Execute (tier2.go):**
- Single prompt → structured plan with up to 3 tool calls
- Available tools: `query_pod_status`, `query_logs`, `restart_service`
- Confidence field in response; if < 0.8 → skip to Tier 3
- Dry-run mode: log actions without executing
- 30s timeout per tool call; total 90s budget

**Tier 3 — Human Escalation (tier3.go):**
- Send to Slack and/or Telegram with full incident context
- Slack: Block Kit message with Approve / Reject / Snooze buttons
- Telegram: Inline keyboard with same options
- 2-minute response window; no response → auto-escalate to PagerDuty
- Store approval state in Redis (Valkey)

**Verification (verifier.go):**
- After any action: re-query pod/service status
- If alert condition not resolved after 60s → trigger rollback
- Rollback: reverse the action if reversible, then escalate
- Emit `sora_remediation_verified_total` metric

**Learning (learning.go):**
- On successful resolution: store `(alert_fingerprint_pattern, actions_taken)` in SQLite
- Periodic job (hourly): promote high-confidence patterns (≥5 successful resolutions) to Tier 1 YAML
- Expose `/admin/patterns` endpoint to view learned patterns

### 5. Notifications (`internal/notifier/`)

**Slack (slack.go):**
- Webhook-based for alerts
- Block Kit formatting: header, alert details section, P-level badge, owner mention
- P0 → @channel mention
- P1 → @here mention  
- P2 → no mention, DM owner only
- Work hours routing: P2 only sent during work hours (configurable timezone, start/end)
- Approval buttons use Slack interactive webhooks (separate endpoint)

**Telegram (telegram.go):**
- Bot API (not webhook — use polling or configure webhook URL)
- Markdown V2 formatting
- Inline keyboard for approve/reject on escalations
- Group chat ID + individual owner chat IDs from config

## shall allow expand to other IM in the future

### 6. Observability

**Metrics (Prometheus):**

sora_alerts_received_total{source, mode}
sora_alerts_classified_total{level, classified_by}
sora_classification_duration_seconds{provider}
sora_dedup_hits_total
sora_remediation_attempts_total{tier}
sora_remediation_success_total{tier, action}
sora_remediation_duration_seconds{tier}
sora_escalations_total{reason}
sora_circuit_breaker_state{component}  # 0=closed,1=open,2=half-open
sora_notifications_sent_total{channel, level}

**Logging:** logrus, JSON output, fields: `timestamp`, `alert_id`, `fingerprint`, `tier`, `action`, `duration_ms`, `error`

**Tracing:** OpenTelemetry with OTLP exporter (configurable endpoint); trace per alert through full pipeline

### 7. Safety Rails
- `service_whitelist` in config: only auto-remediate listed services
- `service_blacklist`: never auto-remediate these (critical services)
- Rate limits via Redis: configurable per-service per-hour restart cap
- Distributed lock per incident ID prevents duplicate remediation across replicas
- Dry-run mode: all actions logged, none executed
- K8s circuit breaker: if K8s API errors exceed threshold, halt all actions

### 8. Configuration (`configs/config.yaml`)

Full config.yaml must support these sections:
```yaml
server:
  port: 8080
  read_timeout: 30s
  write_timeout: 30s
  shutdown_timeout: 15s

mode: full  # classifier | remediation | full | notify-only | demo

ai:
  provider: anthropic  # or openai
  api_key: ${AI_API_KEY}
  model: claude-3-5-sonnet-20241022
  temperature: 0.1
  timeout: 8s
  max_retries: 2

dedup:
  window_seconds: 300
  redis_addr: ${REDIS_ADDR}
  redis_password: ${REDIS_PASSWORD}

notifications:
  slack:
    webhook_url: ${SLACK_WEBHOOK_URL}
    interactive_signing_secret: ${SLACK_SIGNING_SECRET}
  telegram:
    bot_token: ${TELEGRAM_BOT_TOKEN}
    default_chat_id: ${TELEGRAM_CHAT_ID}
  pagerduty:
    integration_key: ${PAGERDUTY_KEY}

work_hours:
  start: "09:00"
  end: "18:00"
  timezone: "America/New_York"
  days: [Mon, Tue, Wed, Thu, Fri]

business_owners:
  - name: payments
    slack_id: "U12345678"
    telegram_id: "123456789"
  - name: platform
    slack_id: "U87654321"
    telegram_id: "987654321"

business_mappings:
  - pattern: "payment.*"
    business_line: payments
  - pattern: "checkout.*|cart.*"
    business_line: commerce
  - pattern: ".*"
    business_line: platform

remediation:
  dry_run: false
  kubeconfig: ${KUBECONFIG_PATH}
  namespace: ${NAMESPACE}
  tool_timeout: 30s
  max_tier2_tool_calls: 3
  approval_timeout: 120s
  verification_wait: 60s
  service_whitelist:
    - worker-service
    - batch-processor
  service_blacklist:
    - payments-api
    - auth-service

fallback_rules:
  critical: P0
  warning: P1
  info: P2

source_mappings:
  prometheus:
    alertname: labels.alertname
    severity: labels.severity
    instance: labels.instance
    namespace: labels.namespace
    pod: labels.pod
  generic:
    alertname: alert_name
    severity: level
    instance: host
    namespace: environment
    pod: container
```

### 9. rules.yaml structure
```yaml
classifier_rules:
  - name: "high-cpu-warning"
    match:
      alertname_regex: "HighCPU.*"
      severity: "warning"
    result:
      level: P2
      business_line: platform
      root_cause_hint: "CPU saturation, check top processes"
      actions: ["check_cpu_usage", "identify_hot_process"]

remediation_rules:
  - name: "crashloop-auto-restart"
    match:
      alertname_regex: "CrashLoopBackOff"
      restart_count_lt: 3
    action: restart_pod
    require_approval: false
    max_per_hour: 3

  - name: "oom-needs-approval"
    match:
      alertname_regex: "OOMKilled"
    action: increase_memory_limit
    require_approval: true
```

---

## API Endpoints Summary
POST /webhook/alert           # Universal receiver
POST /webhook/prometheus      # Prometheus Alertmanager format
POST /webhook/generic         # Generic JSON with field mapping
POST /slack/interact          # Slack interactive button callbacks
GET  /health                  # {"status":"ok","mode":"...","version":"..."}
GET  /ready                   # Readiness: checks Redis (Valkey) + AI
GET  /metrics                 # Prometheus metrics
GET  /admin/patterns          # View learned remediation patterns (basic auth protected)

---

## Dependencies (go.mod)
github.com/spf13/viper
github.com/sirupsen/logrus
github.com/prometheus/client_golang
github.com/redis/go-redis/v9
github.com/anthropics/anthropic-sdk-go   # if provider=anthropic
github.com/sashabaranov/go-openai        # if provider=openai
github.com/mattn/go-sqlite3              # for learning store
k8s.io/client-go                         # K8s tool implementations
go.opentelemetry.io/otel                 # tracing
Minimize other dependencies. Standard library for HTTP server.

---

## Makefile Targets
```makefile
build          # go build -o bin/sora ./cmd/sora
test           # go test ./... -race -cover
lint           # golangci-lint run
docker-build   # docker build -t sora:latest .
docker-push    # docker push
k8s-deploy     # kubectl apply -f deployments/k8s/
helm-deploy    # helm upgrade --install sora deployments/helm/
run-demo       # ./bin/sora --mode=demo
run-local      # ./bin/sora --config=configs/config.yaml
coverage       # go test ./... -coverprofile=coverage.out && go tool cover -html=coverage.out
```

---

## Dockerfile
Multi-stage build:
- Stage 1: `golang:1.21-alpine` — build binary with CGO_ENABLED=0
- Stage 2: `alpine:3.19` — copy binary + configs only
- Final image < 50MB
- Non-root user
- ENTRYPOINT `["/sora"]`
- Default CMD `["--mode=full", "--config=/etc/sora/config.yaml"]`

---

## Kubernetes Manifests

**deployment.yaml:**
- 2 replicas minimum
- Resource requests: 100m CPU, 128Mi memory
- Resource limits: 500m CPU, 512Mi memory
- Liveness probe: GET /health every 15s
- Readiness probe: GET /ready every 10s, initial delay 5s
- Rolling update strategy, maxUnavailable: 0

**hpa.yaml:**
- Min 2, max 10 replicas
- Scale on CPU > 70%

**configmap.yaml:** mount configs/rules.yaml and configs/prompts.yaml

**secrets.yaml:** template with all ${VAR} placeholders

---

## Helm Chart
- `values.yaml` exposes: image, replicas, mode, resources, config overrides, secrets refs
- All K8s manifests as templates with `.Values` substitution
- `_helpers.tpl` with name/label helpers

---

## Testing Requirements
- Unit tests for: dedup (fingerprinting, window expiry, concurrency), tier1 (rule matching), parser (prometheus + generic), classifier (AI mock + rule fallback)
- Mock AI client implementing the same interface, returns configurable responses
- Integration test: POST to /webhook/alert → verify classification + notification mock called
- Coverage target: >70%
- Use `testing` stdlib + `testify/assert`

---

## Implementation Notes
- All external calls (AI, K8s, Redis (Valkey), Slack, Telegram) must have context-propagated timeouts
- Graceful shutdown: drain in-flight requests, flush metrics, close Redis (Valkey) connections
- Signal handling: SIGTERM + SIGINT
- Config hot-reload via SIGHUP (re-read rules.yaml and config.yaml without restart)
- All operations must be idempotent — safe to retry
- No global mutable state; dependency injection via constructor functions
- Every interface must have a mock implementation for testing
- All errors must be wrapped with context using `fmt.Errorf("...: %w", err)`
- The demo mode must work completely without Redis (Valkey), K8s, or real AI credentials — use in-memory stubs and a mock alert generator that fires sample alerts every 30 seconds

Generate every file fully implemented. No stubs, no TODOs, no placeholder comments. Each file must compile and be correct Go code. Start with go.mod, then cmd/sora/main.go, then work through internal packages in dependency order (config → logger → dedup → cache → circuit → classifier → tools → remediator → notifier → webhook), then configs, then deployments, then tests.

Name rationale: SORA is 4 letters, easy to say, memorable, and the acronym is clean. Alternatives if you want options: NERVE (Network Event Response & Verification Engine) or ARIA (Autonomous Remediation & Intelligence Agent). I'd go with SORA — it has a nice sound and reads well in [SORA] alert received log lines.
The key architectural decisions baked into the prompt:

The --mode flag lets you run just the classifier (your system 2) or just the remediator (system 1) or both, from one binary
Shared Alert and ClassifiedAlert types mean the classifier feeds naturally into the remediator
Circuit breakers protect both the AI path and the K8s API path independently
Demo mode works with zero external dependencies — great for presentations
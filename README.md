# SORA — Service Operations Remediation Agent

SORA is a single Go binary that unifies two SRE capabilities:

- **Alert Classifier** — turns raw Prometheus/webhook alerts into a severity (`P0`/`P1`/`P2`), business-line tag, root-cause hint, and recommended actions, using an AI classifier (any OpenAI-compatible endpoint — OpenAI, DeepSeek, Ollama, or Anthropic via a LiteLLM-style gateway) with a YAML rule-engine fallback.
- **Auto-Remediator** — a tiered decision engine that fixes what it can (Tier 1 rules), asks an LLM to investigate and act when rules don't match (Tier 2), and escalates to a human via Slack/Telegram when confidence is low (Tier 3) — with post-action verification, rollback, and pattern learning that promotes repeated LLM fixes into Tier 1 rules over time.

```
Alert → Dedup (5min window) → Classify (AI → rule fallback) → Notify
                                                              → Remediate (Tier1 → Tier2 → Tier3) → Verify → Learn
```

See [`design/DESIGN.md`](design/DESIGN.md) for the full original architecture spec, [`CLAUDE.md`](CLAUDE.md) for a quick-reference dictionary, and the `sora-architecture` Claude Code skill for implementation-level details (prompt schemas, metric names, rule YAML structure).

## Why one binary

A `--mode` flag lets you run the classifier, the remediator, or both from the same binary and config:

| Mode | What runs |
|---|---|
| `full` (default) | Classification + notifications + remediation |
| `classifier` | Classification + notifications only |
| `notify-only` | Same as classifier mode — no remediation actions are ever taken |
| `remediation` | Remediation pipeline only (rule-based classification, no AI) |
| `demo` | Everything, with mock Kubernetes tools and a synthetic alert generator — **no Redis, Kubernetes, or AI credentials required** |

## Quick start

```bash
git clone https://github.com/teochenglim/sora.git
cd sora
make            # prints every available target
make run-demo   # builds and runs with zero external dependencies
open http://localhost:8080   # live status dashboard (mode, uptime, metrics, config)
```

Within ~30 seconds the demo alert generator fires a sample `CrashLoopBackOff` alert that flows end-to-end through dedup → classification → Tier 1 remediation → verification. Watch it with:

```bash
curl localhost:8080/metrics | grep sora_
```

### Status dashboard

SORA embeds its own web UI in the binary (`internal/webhook/static/`, via `go:embed` — no separate static/ directory needed at runtime, the same way the Prometheus binary ships its UI) and serves it at `/` on the same port as the API:

- **Status** — mode, version, uptime
- **Metrics** — every `sora_*` Prometheus counter/gauge as a live number, polled every 5s
- **Configuration** — the running config with all secrets redacted to `*_set`/`*_configured` booleans, polled every 30s

It's backed by two small JSON APIs you can also hit directly: `GET /api/stats` and `GET /api/config`. The raw Prometheus exposition format is still available at `/metrics` for scraping.

### Running against real infrastructure

```bash
cp .env.example .env   # fill in AI_API_KEY, REDIS_ADDR, SLACK_WEBHOOK_URL, etc.
export $(cat .env | xargs)
make run-local          # uses configs/config.yaml
```

Or via Docker Compose (spins up Redis + Prometheus alongside SORA):

```bash
make docker-up     # build + run the full stack
make docker-logs   # tail logs from any service
make docker-down   # stop and remove it
```

### Testing against a real LLM (e.g. Ollama)

`configs/config.ollama.yaml` is a ready-to-use config for testing the AI classification path against a local OpenAI-compatible server like Ollama, with real Redis for dedup/state and no Kubernetes/Slack/Telegram required (`mode: notify-only`):

```bash
cp .env.example .env   # set LLM_BASE_URL=http://localhost:11434/v1, LLM_MODEL=<your model>, AI_API_KEY=anything
make test-e2e          # builds sora, starts redis, fires a simulated alert, asserts it was classified by the AI (not the rule fallback)
```

Local reasoning models can take well over the default 8s/30s timeouts — `LLM_TIMEOUT` and `SERVER_TIMEOUT` in `.env` control `ai.timeout` and the HTTP server's `write_timeout` respectively (which must stay above `ai.timeout`, since Go's `http.Server.WriteTimeout` covers the whole handler call, not just the response write). See `scripts/test-e2e.sh` for what it checks.

## Everyday development

```bash
make build      # go build -> bin/sora
make test       # go test ./... -race -cover
make coverage   # generates coverage.html
make lint       # golangci-lint (falls back to go vet)
make fmt        # gofmt
```

Run `make` with no arguments at any time to see the full target list.

## Project layout

```
cmd/sora/            entry point, mode selection, graceful shutdown
internal/
  classifier/         AI (OpenAI-compatible) + rule-engine classifier, circuit-breaker guarded
  remediator/          tier1 (rules) / tier2 (LLM plan+execute) / tier3 (human escalation),
                       executor, verifier, SQLite-backed pattern learning
  tools/               Kubernetes actions (query_pod_status, query_logs, restart_service) + demo mocks
  dedup/                Redis-backed (Valkey) sliding-window dedup + distributed lock, in-memory fallback
  cache/                15-minute similar-alert context cache
  circuit/              sliding-window circuit breaker
  notifier/             Slack + Telegram (interface-based — adding a new IM is one new file)
  webhook/              HTTP handlers + multi-source alert parsing
  incident/             incident lifecycle store (Redis-backed, in-memory fallback)
  config/               config.yaml + rules.yaml loaders, SIGHUP hot-reload
  metrics/              all Prometheus metrics
pkg/logger/            structured JSON logger
configs/               config.yaml, rules.yaml, prompts.yaml (annotated examples)
deployments/
  docker/              Dockerfile (multi-stage, <50MB), docker-compose.yaml
  k8s/                 plain manifests (Deployment, Service, HPA, RBAC, ConfigMap, Secret template)
  helm/                Helm chart wrapping the same manifests
tests/
  unit/                dedup, tier1/2/3, classifier, parser, cache, circuit, learning, config, etc.
  integration/          full webhook -> classify -> notify -> remediate flow over httptest
design/DESIGN.md       original full architecture spec
```

## Agent harness

This repo is wired up with three layers of automated checks, so problems are caught as early as possible:

1. **Claude Code hook** (`.claude/settings.json` + `scripts/claude-post-edit-check.sh`) — gofmt + `go vet` runs automatically after Claude edits any `.go` file during development.
2. **Git pre-commit hook** (`scripts/pre-commit.sh`) — gofmt, `go vet`, and the full race-enabled test suite run before every commit. Install with `make hooks-install`.
3. **GitHub Actions** (`.github/workflows/ci.yml`) — vets, builds, and tests on every push/PR; cross-compiles for `linux/amd64` and `linux/arm64`; builds and pushes a multi-arch Docker image to `ghcr.io/teochenglim/sora` on pushes to `main`.

## Configuration

`configs/config.yaml` is the annotated reference for every setting (server timeouts, AI provider/model, dedup window, Slack/Telegram/PagerDuty, work-hours routing, business-line mappings, remediation safety rails, source field mappings). Secrets are referenced as `${ENV_VAR}` and substituted at load time — see `.env.example`.

`configs/rules.yaml` holds both the Tier-1 classifier rules and the Tier-1 remediation rules. It hot-reloads on `SIGHUP` without a restart, and is also where the learning loop appends newly-promoted patterns (after ≥5 successful Tier-2 resolutions of the same alert/action pair).

## Safety rails

- `service_whitelist` / `service_blacklist` — only listed services are ever auto-remediated; blacklist always wins.
- Per-service rate limiting (default 5 restarts/hour) via Redis, with an in-memory fallback.
- A distributed lock per alert fingerprint prevents two replicas from double-remediating the same incident.
- `dry_run: true` logs every intended action without executing it.
- An independent circuit breaker protects the AI path (50% failure rate over the last 10 calls trips it; auto-recovers via a half-open probe every 60s).

## License

Apache License 2.0 — see [LICENSE](LICENSE).

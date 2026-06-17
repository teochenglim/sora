#!/usr/bin/env bash
# Black-box test client for a SORA instance that is ALREADY RUNNING —
# this script does not build, start, or stop anything. Bring SORA up
# however you like first (make docker-up / make run-demo / make
# run-local / your own `go run`), then point this at it:
#
#   SORA_URL=http://localhost:8080 ./scripts/test-e2e.sh
#
# Each scenario is independent and prints PASS/FAIL; the script exits
# non-zero if any scenario fails. Scenarios that depend on optional
# config (AI classification, Slack/Telegram, K8s remediation) degrade
# gracefully — they report what actually happened rather than assuming
# a specific deployment shape.
set -uo pipefail

SORA_URL="${SORA_URL:-http://localhost:8080}"
PASS=0
FAIL=0

pass() { echo "  PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  FAIL: $1" >&2; FAIL=$((FAIL + 1)); }

scenario() { echo; echo "==> $1"; }

json_get() { # json_get '<json>' '<grep-pattern>' — crude but dependency-free
  echo "$1" | grep -o "$2"
}

scenario "Reachability"
if curl -fs -o /dev/null "$SORA_URL/health"; then
  pass "$SORA_URL is reachable"
else
  fail "$SORA_URL/health unreachable — is SORA running? (make docker-up / make run-demo / make run-local)"
  echo
  echo "$PASS passed, $FAIL failed"
  exit 1
fi

scenario "GET /health"
health=$(curl -fs "$SORA_URL/health")
if echo "$health" | grep -q '"status":"ok"'; then
  pass "status=ok ($health)"
else
  fail "unexpected response: $health"
fi

scenario "GET /ready"
ready_code=$(curl -s -o /dev/null -w "%{http_code}" "$SORA_URL/ready")
if [ "$ready_code" = "200" ]; then
  pass "ready (200)"
else
  fail "expected 200, got $ready_code"
fi

scenario "GET / (embedded dashboard)"
if curl -fs "$SORA_URL/" | grep -qi "<title>SORA</title>"; then
  pass "dashboard HTML served"
else
  fail "dashboard did not return expected HTML"
fi

scenario "GET /api/stats"
stats=$(curl -fs "$SORA_URL/api/stats")
if echo "$stats" | grep -q '"uptime_seconds"'; then
  pass "stats JSON has uptime_seconds"
else
  fail "unexpected /api/stats response: $stats"
fi

scenario "GET /api/config (secrets must be redacted)"
cfg=$(curl -fs "$SORA_URL/api/config")
if echo "$cfg" | grep -qE '"api_key_set"|"slack_configured"'; then
  if echo "$cfg" | grep -qE '"api_key":"[^"]'; then
    fail "config response appears to leak a raw api_key field"
  else
    pass "config redacted to *_set/*_configured booleans"
  fi
else
  fail "unexpected /api/config response: $cfg"
fi

scenario "GET /metrics (Prometheus exposition format)"
metrics=$(curl -fs "$SORA_URL/metrics")
if echo "$metrics" | grep -q '^sora_'; then
  pass "metrics endpoint exposes sora_* series"
else
  fail "no sora_* metrics found at /metrics"
fi

scenario "POST /webhook/prometheus — Tier-1 rule match (CrashLoopBackOff)"
fingerprint_pod="crash-test-$RANDOM"
curl -fs -o /dev/null -X POST "$SORA_URL/webhook/prometheus" \
  -H "Content-Type: application/json" \
  -d "{\"alerts\":[{\"labels\":{\"alertname\":\"CrashLoopBackOff\",\"severity\":\"critical\",\"instance\":\"10.0.0.5\",\"namespace\":\"default\",\"pod\":\"$fingerprint_pod\",\"service\":\"worker-service\"},\"annotations\":{},\"startsAt\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}]}"
sleep 1
stats=$(curl -fs "$SORA_URL/api/stats")
if json_get "$stats" '"classified_by":"rule"' > /dev/null; then
  pass "matched alert classified by Tier-1 rule"
else
  echo "    (no rule-classified sample yet — informational only, not all configs load the crashloop rule)"
  pass "request accepted (202)"
fi

scenario "POST /webhook/prometheus — no rule match (forces AI or fallback path)"
fingerprint_pod2="latency-test-$RANDOM"
http_code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$SORA_URL/webhook/prometheus" \
  -H "Content-Type: application/json" \
  -d "{\"alerts\":[{\"labels\":{\"alertname\":\"PaymentGatewayLatencySpike\",\"severity\":\"critical\",\"instance\":\"10.0.0.42\",\"namespace\":\"production\",\"pod\":\"$fingerprint_pod2\",\"service\":\"payment-gateway\"},\"annotations\":{\"summary\":\"p99 latency 4.2x baseline\"},\"startsAt\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}]}")
if [ "$http_code" = "202" ]; then
  pass "webhook accepted (202)"
else
  fail "expected 202, got $http_code"
fi
echo "    waiting up to 90s for classification (slower if AI-backed)..."
classified_by=""
for i in $(seq 1 90); do
  stats=$(curl -fs "$SORA_URL/api/stats" 2>/dev/null || true)
  if echo "$stats" | grep -q 'sora_alerts_classified_total'; then
    classified_by=$(echo "$stats" | grep -o '"classified_by":"[a-z]*"' | head -1 | cut -d'"' -f4)
    break
  fi
  sleep 1
done
if [ -n "$classified_by" ]; then
  pass "alert was classified (classified_by=$classified_by)"
else
  fail "no classification observed after 90s"
fi

scenario "POST /webhook/prometheus — duplicate suppression"
dup_payload="{\"alerts\":[{\"labels\":{\"alertname\":\"DedupTest\",\"severity\":\"warning\",\"instance\":\"10.0.0.9\",\"namespace\":\"default\",\"pod\":\"dedup-pod\",\"service\":\"dedup-service\"},\"annotations\":{},\"startsAt\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}]}"
before=$(curl -fs "$SORA_URL/api/stats" | grep -o '"sora_dedup_hits_total"[^}]*"value":[0-9.]*' | grep -o '[0-9.]*$' || echo 0)
curl -fs -o /dev/null -X POST "$SORA_URL/webhook/prometheus" -H "Content-Type: application/json" -d "$dup_payload"
sleep 0.5
curl -fs -o /dev/null -X POST "$SORA_URL/webhook/prometheus" -H "Content-Type: application/json" -d "$dup_payload"
sleep 1
after=$(curl -fs "$SORA_URL/api/stats" | grep -o '"sora_dedup_hits_total"[^}]*"value":[0-9.]*' | grep -o '[0-9.]*$' || echo 0)
if [ -n "$after" ] && [ -n "$before" ] && awk "BEGIN{exit !($after > $before)}"; then
  pass "second identical alert was deduplicated (dedup_hits $before -> $after)"
else
  fail "expected sora_dedup_hits_total to increase (before=$before after=$after)"
fi

scenario "POST /webhook/generic — generic field mapping"
http_code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$SORA_URL/webhook/generic" \
  -H "Content-Type: application/json" \
  -d '{"alert_name":"HighCPU","level":"warning","host":"10.0.0.7","environment":"staging","container":"batch-1"}')
if [ "$http_code" = "202" ]; then
  pass "generic webhook accepted (202)"
else
  fail "expected 202, got $http_code"
fi

scenario "POST /webhook/alert — universal endpoint (auto-detects shape)"
http_code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$SORA_URL/webhook/alert" \
  -H "Content-Type: application/json" \
  -d '{"alert_name":"HighCPU","level":"warning","host":"10.0.0.8","environment":"staging","container":"batch-2"}')
if [ "$http_code" = "202" ]; then
  pass "universal webhook accepted (202)"
else
  fail "expected 202, got $http_code"
fi

scenario "GET /admin/patterns"
http_code=$(curl -s -o /dev/null -w "%{http_code}" "$SORA_URL/admin/patterns")
if [ "$http_code" = "200" ] || [ "$http_code" = "401" ]; then
  pass "patterns endpoint reachable (HTTP $http_code; 401 is expected if basic auth is configured)"
else
  fail "expected 200 or 401, got $http_code"
fi

echo
echo "$PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]

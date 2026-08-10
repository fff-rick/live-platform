#!/usr/bin/env bash
set -euo pipefail

API=${API_BASE:-http://localhost:8080}
WORKER=${WORKER_BASE:-http://localhost:9090}
CF=${CENTRIFUGO_BASE:-http://localhost:8000}
PROM=${PROMETHEUS_BASE:-http://localhost:9091}
GRAFANA=${GRAFANA_BASE:-http://localhost:3000}
TEMPO=${TEMPO_BASE:-http://localhost:3200}

say(){ printf '\n[M6] %s\n' "$*"; }
retry(){
  local name=$1 url=$2
  for _ in $(seq 1 40); do
    if curl -fsS "$url" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  echo "$name unavailable: $url" >&2
  return 1
}

say "waiting for observability stack"
retry api "$API/health"
retry api-ready "$API/ready"
retry worker "$WORKER/health"
retry centrifugo "$CF/health"
retry prometheus "$PROM/-/ready"
retry grafana "$GRAFANA/api/health"
retry tempo "$TEMPO/ready"

say "checking metrics endpoints"
curl -fsS "$API/metrics" | grep -q 'live_http_requests_total'
curl -fsS "$WORKER/metrics" | grep -q 'live_outbox_pending'
curl -fsS "$CF/metrics" | grep -q 'centrifugo_node_num_clients'

say "generating API traffic"
for _ in $(seq 1 5); do curl -fsS "$API/health" >/dev/null; done
sleep 6

say "checking Prometheus scrape targets"
for job in live-api live-worker centrifugo; do
  body=$(curl -fsSG "$PROM/api/v1/query" --data-urlencode "query=up{job=\"$job\"}")
  echo "$body" | grep -q '"status":"success"'
  echo "$body" | grep -q '"1"' || { echo "Prometheus target $job is not UP" >&2; exit 1; }
done

say "checking Grafana provisioning"
curl -fsS "$GRAFANA/api/datasources/uid/prometheus" | grep -q 'Prometheus'
curl -fsS "$GRAFANA/api/datasources/uid/tempo" | grep -q 'Tempo'

say "M6 observability smoke test PASS"
echo "Grafana:    $GRAFANA"
echo "Prometheus: $PROM"
echo "Tempo:      $TEMPO"

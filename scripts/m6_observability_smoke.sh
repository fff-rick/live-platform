#!/usr/bin/env bash
set -euo pipefail

API=${API_BASE:-http://localhost:8080}
WORKER=${WORKER_BASE:-http://localhost:${WORKER_HOST_PORT:-19090}}
CF=${CENTRIFUGO_BASE:-http://localhost:8000}
PROM=${PROMETHEUS_BASE:-http://localhost:${PROMETHEUS_HOST_PORT:-19091}}
GRAFANA=${GRAFANA_BASE:-http://localhost:3000}
TEMPO=${TEMPO_BASE:-http://localhost:3200}
LOKI=${LOKI_BASE:-http://localhost:${LOKI_HOST_PORT:-13100}}
ALERTMANAGER=${ALERTMANAGER_BASE:-http://localhost:${ALERTMANAGER_HOST_PORT:-19093}}

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
retry loki "$LOKI/ready"
retry alertmanager "$ALERTMANAGER/-/ready"

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

say "checking log collection and alert routing"
logs=$(curl -fsSG "$LOKI/loki/api/v1/query_range" --data-urlencode 'query={service_name="live-api"}' --data-urlencode 'limit=5')
echo "$logs" | grep -q 'live-api'
curl -fsS "$PROM/api/v1/alertmanagers" | grep -q 'alertmanager:9093'

say "checking Grafana provisioning"
curl -fsS "$GRAFANA/api/datasources/uid/prometheus" | grep -q 'Prometheus'
curl -fsS "$GRAFANA/api/datasources/uid/tempo" | grep -q 'Tempo'
curl -fsS "$GRAFANA/api/datasources/uid/loki" | grep -q 'Loki'

say "M6 observability smoke test PASS"
echo "Grafana:    $GRAFANA"
echo "Prometheus: $PROM"
echo "Tempo:      $TEMPO"
echo "Loki:       $LOKI"
echo "Alertmanager: $ALERTMANAGER"

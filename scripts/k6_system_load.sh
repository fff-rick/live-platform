#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
WORKER_URL="${WORKER_URL:-http://localhost:${WORKER_HOST_PORT:-19090}}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://localhost:${PROMETHEUS_HOST_PORT:-19091}}"
GRAFANA_URL="${GRAFANA_URL:-http://localhost:3000}"
CENTRIFUGO_URL="${CENTRIFUGO_URL:-http://localhost:8000}"
REPORT_ROOT="${REPORT_ROOT:-reports/k6-system}"
START_TIMEOUT="${START_TIMEOUT:-300}"
SETTLE_TIMEOUT="${SETTLE_TIMEOUT:-30}"
RUN_ID="$(date +%Y%m%d%H%M%S)_${RANDOM}"
REPORT_DIR="${REPORT_ROOT}/${RUN_ID}"

log() {
  printf '[k6-system] %s\n' "$*"
}

fail() {
  printf '[k6-system] ERROR: %s\n' "$*" >&2
  exit 1
}

for command in docker curl jq k6 awk; do
  command -v "$command" >/dev/null 2>&1 || fail "缺少命令: $command"
done
docker info >/dev/null 2>&1 || fail "Docker daemon 不可用"

stack_ready() {
  curl -fsS --max-time 3 "$BASE_URL/ready" >/dev/null 2>&1 &&
    curl -fsS --max-time 3 "$WORKER_URL/health" >/dev/null 2>&1 &&
    curl -fsS --max-time 3 "$PROMETHEUS_URL/-/ready" >/dev/null 2>&1 &&
    curl -fsS --max-time 3 "$GRAFANA_URL/api/health" >/dev/null 2>&1 &&
    curl -fsS --max-time 3 "$CENTRIFUGO_URL/health" >/dev/null 2>&1
}

if ! stack_ready; then
  log "项目未就绪，执行 docker compose up -d --build"
  docker compose up -d --build
fi

deadline=$((SECONDS + START_TIMEOUT))
until stack_ready; do
  (( SECONDS < deadline )) || fail "项目在 ${START_TIMEOUT}s 内未就绪"
  sleep 2
done

prom_value() {
  local query="$1"
  curl -fsSG --max-time 10 "$PROMETHEUS_URL/api/v1/query" \
    --data-urlencode "query=$query" |
    jq -er '.data.result[0].value[1] // "0"'
}

deadline=$((SECONDS + START_TIMEOUT))
until targets="$(prom_value 'sum(up{job=~"live-.*|centrifugo|mysql|redis|kafka"} == 1)' 2>/dev/null)" &&
  awk -v targets="$targets" 'BEGIN { exit !(targets >= 9) }'; do
  (( SECONDS < deadline )) || fail "Prometheus 未能抓取全部 9 个目标"
  sleep 2
done

mkdir -p "$REPORT_DIR"

# 保存一组低基数关键指标，便于把 k6 结果与服务端观测数据关联起来。
prom_snapshot() {
  local output="$1"
  curl -fsSG --max-time 10 "$PROMETHEUS_URL/api/v1/query" \
    --data-urlencode 'query={__name__=~"live_http_requests_total|live_danmaku_total|live_likes_total|live_gift_orders_total|live_kafka_produce_total|live_kafka_consume_total|live_outbox_pending|kafka_consumergroup_lag"}' |
    jq '.data.result' >"$output"
}

prom_snapshot "$REPORT_DIR/prometheus-before.json"
log "开始 k6 联合压测，报告目录: $REPORT_DIR"

set +e
k6 run --no-color \
  --summary-export "$REPORT_DIR/summary.json" \
  --out "json=$REPORT_DIR/samples.json" \
  scripts/k6/system_load.js 2>&1 | tee "$REPORT_DIR/console.log"
K6_EXIT=${PIPESTATUS[0]}
set -e

sleep 7

# Kafka/Outbox 是异步链路，HTTP 成功后仍需等待 backlog 收敛。
deadline=$((SECONDS + SETTLE_TIMEOUT))
while :; do
  pending="$(prom_value 'max(live_outbox_pending) or vector(0)')"
  lag="$(prom_value 'max(kafka_consumergroup_lag) or vector(0)')"
  if awk -v pending="$pending" -v lag="$lag" 'BEGIN { exit !(pending <= 0 && lag <= 10) }'; then
    break
  fi
  (( SECONDS < deadline )) || break
  sleep 2
done
prom_snapshot "$REPORT_DIR/prometheus-after.json"

printf 'metric\tbefore\tafter\tdelta\tresult\n' >"$REPORT_DIR/server-metrics.tsv"
SERVER_FAILED=0
for metric in live_http_requests_total live_danmaku_total live_likes_total live_gift_orders_total live_kafka_produce_total live_kafka_consume_total; do
  before="$(jq -r --arg metric "$metric" '[.[] | select(.metric.__name__ == $metric) | .value[1] | tonumber] | add // 0' "$REPORT_DIR/prometheus-before.json")"
  after="$(jq -r --arg metric "$metric" '[.[] | select(.metric.__name__ == $metric) | .value[1] | tonumber] | add // 0' "$REPORT_DIR/prometheus-after.json")"
  delta="$(awk -v before="$before" -v after="$after" 'BEGIN { printf "%.0f", after-before }')"
  result=PASS
  if ! awk -v delta="$delta" 'BEGIN { exit !(delta > 0) }'; then
    result=FAIL
    SERVER_FAILED=1
  fi
  printf '%s\t%s\t%s\t%s\t%s\n' "$metric" "$before" "$after" "$delta" "$result" >>"$REPORT_DIR/server-metrics.tsv"
done

if ! awk -v pending="$pending" -v lag="$lag" 'BEGIN { exit !(pending <= 0 && lag <= 10) }'; then
  log "异步链路未收敛: outbox_pending=${pending}, kafka_lag=${lag}"
  SERVER_FAILED=1
fi

if (( K6_EXIT != 0 )); then
  fail "k6 thresholds 未通过，退出码 ${K6_EXIT}；请检查 $REPORT_DIR/console.log"
fi
if (( SERVER_FAILED != 0 )); then
  fail "服务端指标门禁未通过；请检查 $REPORT_DIR/server-metrics.tsv"
fi

log "k6 thresholds 全部通过"
log "服务端指标与异步 backlog 验证通过"
log "汇总报告: $REPORT_DIR/summary.json"

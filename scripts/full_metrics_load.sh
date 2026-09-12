#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
WORKER_URL="${WORKER_URL:-http://localhost:${WORKER_HOST_PORT:-19090}}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://localhost:${PROMETHEUS_HOST_PORT:-19091}}"
GRAFANA_URL="${GRAFANA_URL:-http://localhost:3000}"
CENTRIFUGO_URL="${CENTRIFUGO_URL:-http://localhost:8000}"

DURATION="${DURATION:-20s}"
USERS="${USERS:-20}"
READ_RATE="${READ_RATE:-40}"
DANMAKU_RATE="${DANMAKU_RATE:-8}"
LIKE_RATE="${LIKE_RATE:-30}"
GIFT_RATE="${GIFT_RATE:-2}"
CONCURRENCY="${CONCURRENCY:-32}"
WS_CLIENTS="${WS_CLIENTS:-100}"
WS_PUBLISH_RATE="${WS_PUBLISH_RATE:-10}"
START_TIMEOUT="${START_TIMEOUT:-300}"
SETTLE_TIMEOUT="${SETTLE_TIMEOUT:-30}"
REPORT_ROOT="${REPORT_ROOT:-reports/metrics-load}"

RUN_ID="$(date +%Y%m%d%H%M%S)_${RANDOM}"
REPORT_DIR="${REPORT_ROOT}/${RUN_ID}"
TMP_DIR="$(mktemp -d)"
TOKENS_FILE="${TMP_DIR}/viewer-tokens.txt"
METRICS_FILE="${REPORT_DIR}/metrics.tsv"
STARTED_BY_SCRIPT=false

cleanup() {
  rm -rf -- "$TMP_DIR"
}
trap cleanup EXIT

log() {
  printf '[metrics-load] %s\n' "$*"
}

fail() {
  printf '[metrics-load] ERROR: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "缺少命令: $1"
}

http_ready() {
  curl -fsS --max-time 3 "$1" >/dev/null 2>&1
}

# 只有所有外部入口都可用时才直接进入压测；否则由 Compose 补齐或重建服务。
stack_ready() {
  http_ready "$BASE_URL/ready" &&
    http_ready "$WORKER_URL/health" &&
    http_ready "$PROMETHEUS_URL/-/ready" &&
    http_ready "$GRAFANA_URL/api/health" &&
    http_ready "$CENTRIFUGO_URL/health"
}

wait_for_stack() {
  local deadline=$((SECONDS + START_TIMEOUT))
  until stack_ready; do
    (( SECONDS < deadline )) || {
      docker compose ps >&2 || true
      fail "项目在 ${START_TIMEOUT}s 内未就绪"
    }
    sleep 2
  done
}

prom_query() {
  local query="$1"
  curl -fsSG --max-time 10 "$PROMETHEUS_URL/api/v1/query" \
    --data-urlencode "query=$query" |
    jq -er '.data.result[0].value[1] // "0"'
}

wait_for_prometheus_targets() {
  local expected=9
  local deadline=$((SECONDS + START_TIMEOUT))
  local value
  until value="$(prom_query 'sum(up{job=~"live-.*|centrifugo|mysql|redis|kafka"} == 1)' 2>/dev/null)" &&
    awk -v value="$value" -v expected="$expected" 'BEGIN { exit !(value >= expected) }'; do
    (( SECONDS < deadline )) || fail "Prometheus 未能在 ${START_TIMEOUT}s 内抓取全部 ${expected} 个目标"
    sleep 2
  done
}

api_json() {
  local method="$1" url="$2" token="${3:-}" body="${4:-}"
  local args=(-fsS --max-time 15 -X "$method" "$url")
  if [[ -n "$token" ]]; then
    args+=(-H "Authorization: Bearer $token")
  fi
  if [[ -n "$body" ]]; then
    args+=(-H 'Content-Type: application/json' -d "$body")
  fi
  curl "${args[@]}"
}

register_user() {
  local username="$1" nickname="$2"
  api_json POST "$BASE_URL/api/v1/auth/register" "" \
    "$(jq -nc --arg username "$username" --arg nickname "$nickname" '{username:$username,nickname:$nickname,password:"password123"}')"
}

metric_value() {
  prom_query "sum($1) or vector(0)"
}

metric_increased() {
  local name="$1" query="$2" before="$3" after delta result
  after="$(metric_value "$query")"
  delta="$(awk -v before="$before" -v after="$after" 'BEGIN { printf "%.6f", after-before }')"
  result="PASS"
  if ! awk -v delta="$delta" 'BEGIN { exit !(delta > 0) }'; then
    result="FAIL"
  fi
  printf '%s\t%s\t%s\t%s\t%s\n' "$name" "$before" "$after" "$delta" "$result" >>"$METRICS_FILE"
  [[ "$result" == "PASS" ]]
}

metric_at_least() {
  local name="$1" query="$2" minimum="$3" value result
  value="$(prom_query "$query")"
  result="PASS"
  if ! awk -v value="$value" -v minimum="$minimum" 'BEGIN { exit !(value >= minimum) }'; then
    result="FAIL"
  fi
  printf '%s\t-\t%s\t-\t%s\n' "$name" "$value" "$result" >>"$METRICS_FILE"
  [[ "$result" == "PASS" ]]
}

metric_at_most() {
  local name="$1" query="$2" maximum="$3" value result
  value="$(prom_query "$query")"
  result="PASS"
  if ! awk -v value="$value" -v maximum="$maximum" 'BEGIN { exit !(value <= maximum) }'; then
    result="FAIL"
  fi
  printf '%s\t-\t%s\t-\t%s\n' "$name" "$value" "$result" >>"$METRICS_FILE"
  [[ "$result" == "PASS" ]]
}

for cmd in docker curl jq go awk; do
  require_command "$cmd"
done
docker info >/dev/null 2>&1 || fail "Docker daemon 不可用"
mkdir -p "$REPORT_DIR"

if stack_ready; then
  log "检测到项目已启动，直接进行指标压测"
else
  log "项目未完整启动，执行 docker compose up -d --build"
  docker compose up -d --build
  STARTED_BY_SCRIPT=true
fi
wait_for_stack
wait_for_prometheus_targets

log "编译 HTTP 与 WebSocket 压测工具"
go build -o "$TMP_DIR/httpload" ./tools/httpload
(cd tools/loadtest && go build -o "$TMP_DIR/wsload" .)

log "创建主播、直播间和 ${USERS} 个压测观众"
ANCHOR_JSON="$(register_user "mla_${RUN_ID}" "Metrics Anchor")"
ANCHOR_TOKEN="$(jq -er '.access_token' <<<"$ANCHOR_JSON")"
ROOM_JSON="$(api_json POST "$BASE_URL/api/v1/rooms" "$ANCHOR_TOKEN" '{"title":"Metrics load room"}')"
ROOM_ID="$(jq -er '.room_id' <<<"$ROOM_JSON")"
api_json POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" "$ANCHOR_TOKEN" >/dev/null

FIRST_VIEWER_TOKEN=""
FIRST_VIEWER_ID=""
for ((i = 1; i <= USERS; i++)); do
  viewer_json="$(register_user "mlv_${RUN_ID}_${i}" "Metrics Viewer ${i}")"
  viewer_token="$(jq -er '.access_token' <<<"$viewer_json")"
  viewer_id="$(jq -er '.user.user_id // .user.id' <<<"$viewer_json")"
  printf '%s\n' "$viewer_token" >>"$TOKENS_FILE"
  api_json POST "$BASE_URL/api/v1/rooms/$ROOM_ID/join" "$viewer_token" >/dev/null
  api_json POST "$BASE_URL/api/v1/rooms/$ROOM_ID/heartbeat" "$viewer_token" >/dev/null
  if [[ -z "$FIRST_VIEWER_TOKEN" ]]; then
    FIRST_VIEWER_TOKEN="$viewer_token"
    FIRST_VIEWER_ID="$viewer_id"
  fi
done

GIFT_ID="$(api_json GET "$BASE_URL/api/v1/gifts" | jq -er '.items[0].gift_id')"
api_json POST "$BASE_URL/api/v1/wallet/dev-credit" "$FIRST_VIEWER_TOKEN" '{"amount":100000000}' >/dev/null

# 治理接口采用“写入、读取、撤销”闭环，既制造指标，也避免目标用户影响后续合法流量。
api_json POST "$BASE_URL/api/v1/rooms/$ROOM_ID/mutes" "$ANCHOR_TOKEN" \
  "$(jq -nc --argjson user_id "$FIRST_VIEWER_ID" '{user_id:$user_id,duration_seconds:30,reason:"metrics load validation"}')" >/dev/null
api_json GET "$BASE_URL/api/v1/rooms/$ROOM_ID/mutes" "$ANCHOR_TOKEN" >/dev/null
api_json DELETE "$BASE_URL/api/v1/rooms/$ROOM_ID/mutes/$FIRST_VIEWER_ID" "$ANCHOR_TOKEN" >/dev/null
api_json POST "$BASE_URL/api/v1/rooms/$ROOM_ID/bans" "$ANCHOR_TOKEN" \
  "$(jq -nc --argjson user_id "$FIRST_VIEWER_ID" '{user_id:$user_id,reason:"metrics load validation"}')" >/dev/null
api_json GET "$BASE_URL/api/v1/rooms/$ROOM_ID/bans" "$ANCHOR_TOKEN" >/dev/null
api_json DELETE "$BASE_URL/api/v1/rooms/$ROOM_ID/bans/$FIRST_VIEWER_ID" "$ANCHOR_TOKEN" >/dev/null

declare -A BASELINE
BASELINE[http]="$(metric_value 'live_http_requests_total')"
BASELINE[danmaku]="$(metric_value 'live_danmaku_total')"
BASELINE[likes]="$(metric_value 'live_likes_total')"
BASELINE[gifts]="$(metric_value 'live_gift_orders_total')"
BASELINE[realtime]="$(metric_value 'live_realtime_publish_total')"
BASELINE[stats]="$(metric_value 'live_stats_broadcast_total')"
BASELINE[kafka_produce]="$(metric_value 'live_kafka_produce_total')"
BASELINE[kafka_consume]="$(metric_value 'live_kafka_consume_total')"
BASELINE[outbox]="$(metric_value 'live_outbox_publish_total')"

log "并发执行 HTTP、弹幕、点赞、礼物及 WebSocket 压测，持续 ${DURATION}"
"$TMP_DIR/httpload" --scenario metrics-http-read \
  --url "$BASE_URL/api/v1/rooms?status=LIVING&limit=24" --rate "$READ_RATE" \
  --concurrency "$CONCURRENCY" --duration "$DURATION" \
  --report "$REPORT_DIR/http-read.json" >"$REPORT_DIR/http-read.log" &
PIDS=("$!")

"$TMP_DIR/httpload" --scenario metrics-danmaku --method POST \
  --url "$BASE_URL/api/v1/rooms/$ROOM_ID/danmaku" --body '{"content":"metrics load danmaku"}' \
  --bearer-file "$TOKENS_FILE" --rate "$DANMAKU_RATE" --concurrency "$CONCURRENCY" --duration "$DURATION" \
  --report "$REPORT_DIR/danmaku.json" >"$REPORT_DIR/danmaku.log" &
PIDS+=("$!")

"$TMP_DIR/httpload" --scenario metrics-like --method POST \
  --url "$BASE_URL/api/v1/rooms/$ROOM_ID/like" --body '{"count":1}' \
  --bearer-file "$TOKENS_FILE" --rate "$LIKE_RATE" --concurrency "$CONCURRENCY" --duration "$DURATION" \
  --report "$REPORT_DIR/like.json" >"$REPORT_DIR/like.log" &
PIDS+=("$!")

"$TMP_DIR/httpload" --scenario metrics-gift --method POST \
  --url "$BASE_URL/api/v1/rooms/$ROOM_ID/gifts" --body "$(jq -nc --argjson gift_id "$GIFT_ID" '{gift_id:$gift_id,count:1}')" \
  --bearer "$FIRST_VIEWER_TOKEN" --idempotency --idempotency-prefix "metrics-${RUN_ID}" \
  --rate "$GIFT_RATE" --concurrency "$CONCURRENCY" --duration "$DURATION" \
  --report "$REPORT_DIR/gift.json" >"$REPORT_DIR/gift.log" &
PIDS+=("$!")

"$TMP_DIR/wsload" --scenario metrics-websocket --clients "$WS_CLIENTS" --rooms 1 \
  --room-base "$ROOM_ID" --connect-rate 100 --connect-concurrency "$CONCURRENCY" \
  --publish-rate "$WS_PUBLISH_RATE" --publish-concurrency "$CONCURRENCY" --duration "$DURATION" \
  --report "$REPORT_DIR/websocket.json" >"$REPORT_DIR/websocket.log" &
PIDS+=("$!")

LOAD_FAILED=0
for pid in "${PIDS[@]}"; do
  wait "$pid" || LOAD_FAILED=1
done
(( LOAD_FAILED == 0 )) || fail "至少一个压测进程异常退出，请检查 $REPORT_DIR/*.log"

# 生成器本身不因 HTTP 4xx/5xx 返回非零，因此必须把报告错误数纳入验收门禁。
REPORT_FAILED=0
for report in http-read danmaku like gift; do
  failed="$(jq -er '.failed' "$REPORT_DIR/${report}.json")"
  if (( failed > 0 )); then
    log "${report} 存在 ${failed} 个失败请求"
    REPORT_FAILED=1
  fi
done
ws_connected="$(jq -er '.initial_connected' "$REPORT_DIR/websocket.json")"
ws_publish_errors="$(jq -er '.publish_errors' "$REPORT_DIR/websocket.json")"
if (( ws_connected < WS_CLIENTS || ws_publish_errors > 0 )); then
  log "WebSocket 验收失败: connected=${ws_connected}/${WS_CLIENTS}, publish_errors=${ws_publish_errors}"
  REPORT_FAILED=1
fi

log "等待异步消费与下一轮 Prometheus 抓取"
sleep 7
deadline=$((SECONDS + SETTLE_TIMEOUT))
while :; do
  pending="$(prom_query 'max(live_outbox_pending) or vector(0)')"
  lag="$(prom_query 'max(kafka_consumergroup_lag) or vector(0)')"
  if awk -v pending="$pending" -v lag="$lag" 'BEGIN { exit !(pending <= 0 && lag <= 10) }'; then
    break
  fi
  (( SECONDS < deadline )) || break
  sleep 2
done

printf 'metric\tbefore\tafter\tdelta\tresult\n' >"$METRICS_FILE"
METRIC_FAILED=0
metric_increased http_requests live_http_requests_total "${BASELINE[http]}" || METRIC_FAILED=1
metric_increased danmaku live_danmaku_total "${BASELINE[danmaku]}" || METRIC_FAILED=1
metric_increased likes live_likes_total "${BASELINE[likes]}" || METRIC_FAILED=1
metric_increased gift_orders live_gift_orders_total "${BASELINE[gifts]}" || METRIC_FAILED=1
metric_increased realtime_publish live_realtime_publish_total "${BASELINE[realtime]}" || METRIC_FAILED=1
metric_increased stats_broadcast live_stats_broadcast_total "${BASELINE[stats]}" || METRIC_FAILED=1
metric_increased kafka_produce live_kafka_produce_total "${BASELINE[kafka_produce]}" || METRIC_FAILED=1
metric_increased kafka_consume live_kafka_consume_total "${BASELINE[kafka_consume]}" || METRIC_FAILED=1
metric_increased outbox_publish live_outbox_publish_total "${BASELINE[outbox]}" || METRIC_FAILED=1
metric_at_least prometheus_targets_up 'sum(up{job=~"live-.*|centrifugo|mysql|redis|kafka"} == 1)' 9 || METRIC_FAILED=1
metric_at_least centrifugo_client_metric 'count(centrifugo_node_num_clients)' 1 || METRIC_FAILED=1
metric_at_least mysql_up 'max(mysql_up) or vector(0)' 1 || METRIC_FAILED=1
metric_at_least redis_up 'max(redis_up) or vector(0)' 1 || METRIC_FAILED=1
metric_at_least kafka_brokers 'max(kafka_brokers) or vector(0)' 1 || METRIC_FAILED=1
metric_at_least db_pool_metrics 'count(live_db_pool_max_open_connections)' 1 || METRIC_FAILED=1
metric_at_least http_latency_histogram 'count(live_http_request_duration_seconds_bucket)' 1 || METRIC_FAILED=1
metric_at_least process_metrics 'count(process_cpu_seconds_total{job=~"live-.*|centrifugo"})' 1 || METRIC_FAILED=1
metric_at_least mysql_dashboard_metrics 'count(mysql_global_status_threads_connected) + count(mysql_global_status_questions)' 2 || METRIC_FAILED=1
metric_at_least redis_dashboard_metrics 'count(redis_memory_used_bytes) + count(redis_commands_processed_total)' 2 || METRIC_FAILED=1
metric_at_least kafka_lag_metric 'count(kafka_consumergroup_lag)' 1 || METRIC_FAILED=1
metric_at_most outbox_pending 'max(live_outbox_pending) or vector(0)' 0 || METRIC_FAILED=1
metric_at_most kafka_consumer_lag 'max(kafka_consumergroup_lag) or vector(0)' 10 || METRIC_FAILED=1

jq -n \
  --arg run_id "$RUN_ID" --arg room_id "$ROOM_ID" --arg duration "$DURATION" \
  --arg started_by_script "$STARTED_BY_SCRIPT" --arg report_dir "$REPORT_DIR" \
  --argjson users "$USERS" --argjson ws_clients "$WS_CLIENTS" \
  --argjson request_reports_ok "$((1 - REPORT_FAILED))" --argjson metrics_ok "$((1 - METRIC_FAILED))" \
  '{run_id:$run_id,room_id:($room_id|tonumber),duration:$duration,users:$users,websocket_clients:$ws_clients,started_by_script:($started_by_script=="true"),request_reports_ok:($request_reports_ok==1),metrics_ok:($metrics_ok==1),report_dir:$report_dir}' \
  >"$REPORT_DIR/summary.json"

log "指标验收结果"
column -t -s $'\t' "$METRICS_FILE" 2>/dev/null || sed 's/\t/  /g' "$METRICS_FILE"
log "报告目录: $REPORT_DIR"
log "Grafana: $GRAFANA_URL/d/live-platform-overview"

(( REPORT_FAILED == 0 && METRIC_FAILED == 0 )) || fail "压测或指标验收未通过"
log "全部链路与指标验收通过"

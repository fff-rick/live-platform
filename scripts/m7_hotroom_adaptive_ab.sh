#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
CLIENTS="${CLIENTS:-5000}"
RATE="${RATE:-20}"
DURATION="${DURATION:-60s}"
LISTENER_DURATION="${LISTENER_DURATION:-65s}"
CONNECT_RATE="${CONNECT_RATE:-3000}"
CONNECT_CONCURRENCY="${CONNECT_CONCURRENCY:-256}"
STAMP="$(date +%s)"
TMP="$(mktemp -d)"
mkdir -p reports/m7/hotroom-adaptive-ab

json_field() { python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"; }
restore_api() {
  rm -rf "$TMP"
  docker compose up -d --force-recreate live-api >/dev/null || true
}
trap restore_api EXIT

# One business user drives the HTTP endpoint. Its normal per-user limiter is
# raised only for this benchmark; production defaults remain unchanged.
DANMAKU_USER_RATE_LIMIT=100000 DANMAKU_USER_RATE_WINDOW=1s docker compose up -d --force-recreate live-api >/dev/null
until curl -fsS "$BASE_URL/ready" >/dev/null 2>&1; do sleep 1; done
REG=$(curl -fsS -X POST "$BASE_URL/api/v1/auth/register" -H 'Content-Type: application/json' \
  -d "{\"username\":\"m7ab_${STAMP}_$RANDOM\",\"nickname\":\"M7AB\",\"password\":\"password123\"}")
TOKEN=$(printf '%s' "$REG" | json_field access_token)
ROOM=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"title":"M7 adaptive A/B"}')
ROOM_ID=$(printf '%s' "$ROOM" | json_field room_id)
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" -H "Authorization: Bearer $TOKEN" >/dev/null

seed_viewers() {
  local expires
  expires=$(( $(date +%s%3N) + 600000 ))
  docker compose exec -T redis redis-cli EVAL "for i=1,tonumber(ARGV[1]) do redis.call('ZADD',KEYS[1],ARGV[2],tostring(900000000+i)) end return redis.call('ZCARD',KEYS[1])" 1 "live:room:${ROOM_ID}:viewers" "$CLIENTS" "$expires" >/dev/null
}
clear_pressure() {
  docker compose exec -T redis redis-cli DEL "live:room:${ROOM_ID}:danmaku:rolling" >/dev/null
  seed_viewers
}

run_case() {
  local name="$1" adaptive="$2" hot_fanout="$3" protect_fanout="$4"
  echo "case=$name adaptive=$adaptive clients=$CLIENTS incoming=$RATE/s"
  DANMAKU_USER_RATE_LIMIT=100000 DANMAKU_USER_RATE_WINDOW=1s \
  DANMAKU_ADAPTIVE_ENABLED="$adaptive" \
  DANMAKU_HOT_FANOUT_RATE="$hot_fanout" DANMAKU_PROTECT_FANOUT_RATE="$protect_fanout" \
  DANMAKU_TARGET_FANOUT_RATE=25000 DANMAKU_MIN_SAMPLE_RATE=0.05 \
  docker compose up -d --force-recreate live-api >/dev/null
  until curl -fsS "$BASE_URL/ready" >/dev/null 2>&1; do sleep 1; done
  clear_pressure

  (cd tools/loadtest && go run . --scenario "$name-listener" --clients "$CLIENTS" --rooms 1 --room-base "$ROOM_ID" \
    --connect-rate "$CONNECT_RATE" --connect-concurrency "$CONNECT_CONCURRENCY" --publish-rate 0 \
    --duration "$LISTENER_DURATION" --report "../../reports/m7/hotroom-adaptive-ab/${name}-ws.json") >"$TMP/${name}-ws.log" 2>&1 &
  local listener_pid=$!
  sleep 3
  go run ./tools/httpload --scenario "$name-danmaku-http" \
    --url "$BASE_URL/api/v1/rooms/$ROOM_ID/danmaku" --method POST --body '{"content":"m7 adaptive benchmark"}' \
    --bearer "$TOKEN" --rate "$RATE" --concurrency 128 --duration "$DURATION" \
    --report "reports/m7/hotroom-adaptive-ab/${name}-http.json"
  wait "$listener_pid"
  curl -fsS "$BASE_URL/metrics" > "reports/m7/hotroom-adaptive-ab/${name}-metrics.txt"
  awk '/^live_kafka_produce_total\{/ && /topic="live.danmaku.v1"/ {print}' \
    "reports/m7/hotroom-adaptive-ab/${name}-metrics.txt" > "reports/m7/hotroom-adaptive-ab/${name}-kafka-summary.txt" || true
  awk '/^live_kafka_produce_errors_total\{/ && /topic="live.danmaku.v1"/ {print}' \
    "reports/m7/hotroom-adaptive-ab/${name}-metrics.txt" >> "reports/m7/hotroom-adaptive-ab/${name}-kafka-summary.txt" || true
}

# Baseline emulates the pre-optimization policy: no fan-out threshold and no
# adaptive controller, so this 5k x 20/s case is left unthrottled.
run_case baseline false 1000000000 2000000000
sleep 3
# Optimized case uses Round-2 measured capacity thresholds.
run_case adaptive true 30000 40000

python3 scripts/m7_optimization_compare.py reports/m7/hotroom-adaptive-ab || true
echo "done room_id=$ROOM_ID reports/m7/hotroom-adaptive-ab/"

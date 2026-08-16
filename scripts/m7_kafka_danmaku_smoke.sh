#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
MESSAGES="${MESSAGES:-100}"
RATE="${RATE:-20}"
DURATION="${DURATION:-5s}"
STAMP="$(date +%s)"
TMP="$(mktemp -d)"
METRICS="$TMP/metrics.txt"
mkdir -p reports/m7/kafka-danmaku

json_field() { python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"; }
restore_api() {
  rm -rf "$TMP"
  docker compose up -d --force-recreate live-api >/dev/null || true
}
trap restore_api EXIT

# This smoke test exercises the exact realtime path that previously bound
# franz-go TryProduce to the HTTP request context.
DANMAKU_USER_RATE_LIMIT=100000 DANMAKU_USER_RATE_WINDOW=1s \
  docker compose up -d --force-recreate live-api >/dev/null
until curl -fsS "$BASE_URL/ready" >/dev/null 2>&1; do sleep 1; done

docker compose exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server kafka:19092 --list | grep -qx 'live.danmaku.v1'

REG=$(curl -fsS -X POST "$BASE_URL/api/v1/auth/register" -H 'Content-Type: application/json' \
  -d "{\"username\":\"m7kafka_${STAMP}_$RANDOM\",\"nickname\":\"M7Kafka\",\"password\":\"password123\"}")
TOKEN=$(printf '%s' "$REG" | json_field access_token)
ROOM=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"title":"M7 Kafka danmaku smoke"}')
ROOM_ID=$(printf '%s' "$ROOM" | json_field room_id)
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" -H "Authorization: Bearer $TOKEN" >/dev/null

# Send exactly MESSAGES requests. Duration is derived from count/rate so the
# report remains deterministic even if defaults are changed.
LOAD_DURATION=$(python3 - "$MESSAGES" "$RATE" <<'PY'
import sys
n=float(sys.argv[1]); r=float(sys.argv[2]); print(f"{max(n/r, 1):.3f}s")
PY
)
go run ./tools/httpload --scenario kafka-danmaku-after-context-fix \
  --url "$BASE_URL/api/v1/rooms/$ROOM_ID/danmaku" --method POST \
  --body '{"content":"m7 kafka context smoke"}' --bearer "$TOKEN" \
  --rate "$RATE" --concurrency 64 --duration "$LOAD_DURATION" \
  --report reports/m7/kafka-danmaku/http.json

# httpload is time-based and may be +/- one request at the boundary; derive the
# expected count from its report rather than assuming MESSAGES exactly.
EXPECTED=$(python3 - <<'PY'
import json
print(json.load(open('reports/m7/kafka-danmaku/http.json'))['completed'])
PY
)

for _ in $(seq 1 30); do
  curl -fsS "$BASE_URL/metrics" > "$METRICS"
  produced=$(awk '/^live_kafka_produce_total\{/ && /topic="live.danmaku.v1"/ && /result="success"/ {print $NF}' "$METRICS" | tail -1)
  failed=$(awk '/^live_kafka_produce_total\{/ && /topic="live.danmaku.v1"/ && /result="failed"/ {print $NF}' "$METRICS" | tail -1)
  produced=${produced:-0}; failed=${failed:-0}
  persisted=$(docker compose exec -T mysql mysql -ulive -plive live -N -s -e "SELECT COUNT(*) FROM danmaku_records WHERE room_id=$ROOM_ID;" 2>/dev/null | tr -d '\r')
  persisted=${persisted:-0}
  if python3 - "$EXPECTED" "$produced" "$failed" "$persisted" <<'PY2'
import sys
expected=int(sys.argv[1]); done=float(sys.argv[2])+float(sys.argv[3]); persisted=int(sys.argv[4])
raise SystemExit(0 if done >= expected and persisted >= expected else 1)
PY2
  then
    break
  fi
  sleep 1
done

curl -fsS "$BASE_URL/metrics" > reports/m7/kafka-danmaku/metrics.txt
produced=$(awk '/^live_kafka_produce_total\{/ && /topic="live.danmaku.v1"/ && /result="success"/ {print $NF}' reports/m7/kafka-danmaku/metrics.txt | tail -1)
failed=$(awk '/^live_kafka_produce_total\{/ && /topic="live.danmaku.v1"/ && /result="failed"/ {print $NF}' reports/m7/kafka-danmaku/metrics.txt | tail -1)
produced=${produced:-0}; failed=${failed:-0}
persisted=$(docker compose exec -T mysql mysql -ulive -plive live -N -s -e "SELECT COUNT(*) FROM danmaku_records WHERE room_id=$ROOM_ID;" 2>/dev/null | tr -d '\r')

printf 'expected=%s produced_success=%s produced_failed=%s persisted=%s\n' "$EXPECTED" "$produced" "$failed" "$persisted" | tee reports/m7/kafka-danmaku/summary.txt

grep '^live_kafka_produce_errors_total' reports/m7/kafka-danmaku/metrics.txt > reports/m7/kafka-danmaku/error-reasons.txt || true

python3 - "$EXPECTED" "$produced" "$failed" "$persisted" <<'PY'
import sys
expected=int(sys.argv[1]); success=float(sys.argv[2]); failed=float(sys.argv[3]); persisted=int(sys.argv[4])
if failed != 0:
    raise SystemExit(f"Kafka async produce still has failures: {failed}")
if success < expected:
    raise SystemExit(f"Kafka success {success} < expected {expected}")
if persisted < expected:
    raise SystemExit(f"danmaku persisted {persisted} < expected {expected}")
print("M7 Kafka danmaku smoke: OK")
PY

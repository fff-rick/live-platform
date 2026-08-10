#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
USERNAME="m5_$(date +%s)_$RANDOM"
PASSWORD="password123"

sql(){ docker compose exec -T mysql mysql -ulive -plive live -Nse "$1" 2>/dev/null; }
wait_sql(){
  local query="$1" expected="$2" label="$3"
  for _ in $(seq 1 40); do
    local got; got="$(sql "$query" || true)"
    if [[ "$got" == "$expected" ]]; then return 0; fi
    sleep 0.25
  done
  echo "timeout waiting $label; got=$(sql "$query" || true) expected=$expected" >&2
  exit 1
}

echo "[1/8] M5 health"
curl -fsS "$BASE_URL/health" | python3 -c 'import json,sys; assert json.load(sys.stdin)["milestone"]=="M5"'

echo "[2/8] register, credit, create room"
REGISTER=$(curl -fsS -X POST "$BASE_URL/api/v1/auth/register" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$USERNAME\",\"nickname\":\"M5\",\"password\":\"$PASSWORD\"}")
TOKEN=$(printf '%s' "$REGISTER" | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
curl -fsS -X POST "$BASE_URL/api/v1/wallet/dev-credit" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"amount":1000}' >/dev/null
ROOM=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"title":"M5 smoke"}')
ROOM_ID=$(printf '%s' "$ROOM" | python3 -c 'import json,sys; print(json.load(sys.stdin)["room_id"])')
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" -H "Authorization: Bearer $TOKEN" >/dev/null

echo "[3/8] gift transaction queues outbox"
KEY="m5-gift-$(date +%s)-$RANDOM"
GIFT=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/gifts" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -H "Idempotency-Key: $KEY" \
  -d '{"gift_id":1,"count":1}')
ORDER_NO=$(printf '%s' "$GIFT" | python3 -c 'import json,sys; v=json.load(sys.stdin); assert v["event_queued"] is True; assert v["idempotent_replay"] is False; print(v["order"]["order_no"])')
wait_sql "SELECT COUNT(*) FROM outbox_events WHERE aggregate_id='$ORDER_NO'" "1" "outbox insert"

echo "[4/8] outbox reaches Kafka and gift consumer completes"
wait_sql "SELECT status FROM outbox_events WHERE aggregate_id='$ORDER_NO'" "1" "outbox published"
EVENT_ID=$(sql "SELECT event_id FROM outbox_events WHERE aggregate_id='$ORDER_NO'")
wait_sql "SELECT status FROM processed_events WHERE consumer_group='live-gift-realtime-v1' AND event_id='$EVENT_ID'" "1" "gift consumer"

echo "[5/8] replay does not create a second outbox event"
REPLAY=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/gifts" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -H "Idempotency-Key: $KEY" \
  -d '{"gift_id":1,"count":1}')
printf '%s' "$REPLAY" | python3 -c 'import json,sys; v=json.load(sys.stdin); assert v["idempotent_replay"] is True; assert v["event_queued"] is False'
[[ "$(sql "SELECT COUNT(*) FROM outbox_events WHERE aggregate_id='$ORDER_NO'")" == "1" ]]

echo "[6/8] danmaku realtime request succeeds"
DANMAKU=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/danmaku" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"content":"m5 kafka persistence"}')
MESSAGE_ID=$(printf '%s' "$DANMAKU" | python3 -c 'import json,sys; print(json.load(sys.stdin)["message_id"])')

echo "[7/8] danmaku consumer persists asynchronously"
wait_sql "SELECT COUNT(*) FROM danmaku_records WHERE message_id='$MESSAGE_ID'" "1" "danmaku persistence"

echo "[8/8] wallet remains authoritative"
BALANCE=$(curl -fsS "$BASE_URL/api/v1/wallet" -H "Authorization: Bearer $TOKEN")
printf '%s' "$BALANCE" | python3 -c 'import json,sys; assert json.load(sys.stdin)["balance"]==900'

echo "OK M5 order=$ORDER_NO event=$EVENT_ID message=$MESSAGE_ID outbox=published gift_consumer=done danmaku=persisted"

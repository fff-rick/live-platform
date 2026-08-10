#!/usr/bin/env bash
set -euo pipefail
BASE_URL="${BASE_URL:-http://localhost:8080}"
USERNAME="m5fail_$(date +%s)_$RANDOM"
PASSWORD="password123"

sql(){ docker compose exec -T mysql mysql -ulive -plive live -Nse "$1" 2>/dev/null; }
trap 'docker compose start kafka >/dev/null 2>&1 || true' EXIT

REGISTER=$(curl -fsS -X POST "$BASE_URL/api/v1/auth/register" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$USERNAME\",\"nickname\":\"M5Fail\",\"password\":\"$PASSWORD\"}")
TOKEN=$(printf '%s' "$REGISTER" | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
curl -fsS -X POST "$BASE_URL/api/v1/wallet/dev-credit" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"amount":1000}' >/dev/null
ROOM=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"title":"M5 recovery"}')
ROOM_ID=$(printf '%s' "$ROOM" | python3 -c 'import json,sys; print(json.load(sys.stdin)["room_id"])')
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" -H "Authorization: Bearer $TOKEN" >/dev/null

echo "[1/4] stop Kafka"
docker compose stop kafka >/dev/null

echo "[2/4] gift must still commit while Kafka is unavailable"
KEY="m5-down-$RANDOM-$(date +%s)"
GIFT=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/gifts" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -H "Idempotency-Key: $KEY" -d '{"gift_id":1,"count":1}')
ORDER_NO=$(printf '%s' "$GIFT" | python3 -c 'import json,sys; v=json.load(sys.stdin); assert v["order"]["status"]=="SUCCESS"; assert v["event_queued"] is True; print(v["order"]["order_no"])')
BALANCE=$(curl -fsS "$BASE_URL/api/v1/wallet" -H "Authorization: Bearer $TOKEN" | python3 -c 'import json,sys; print(json.load(sys.stdin)["balance"])')
[[ "$BALANCE" == "900" ]]
sleep 6
STATUS=$(sql "SELECT status FROM outbox_events WHERE aggregate_id='$ORDER_NO'")
if [[ "$STATUS" == "1" ]]; then echo "expected unpublished outbox while Kafka down" >&2; exit 1; fi

echo "[3/4] restart Kafka"
docker compose start kafka >/dev/null
for _ in $(seq 1 60); do
  STATUS=$(sql "SELECT status FROM outbox_events WHERE aggregate_id='$ORDER_NO'" 2>/dev/null || true)
  [[ "$STATUS" == "1" ]] && break
  sleep 1
done
# Query again with a clean statement; the loop above intentionally tolerates broker recovery latency.
STATUS=$(sql "SELECT status FROM outbox_events WHERE aggregate_id='$ORDER_NO'")
[[ "$STATUS" == "1" ]] || { echo "outbox did not recover, status=$STATUS" >&2; exit 1; }

echo "[4/4] verify consumer completed"
EVENT_ID=$(sql "SELECT event_id FROM outbox_events WHERE aggregate_id='$ORDER_NO'")
for _ in $(seq 1 30); do
  DONE=$(sql "SELECT status FROM processed_events WHERE consumer_group='live-gift-realtime-v1' AND event_id='$EVENT_ID'" || true)
  [[ "$DONE" == "1" ]] && break
  sleep 0.5
done
[[ "${DONE:-}" == "1" ]] || { echo "gift consumer did not complete" >&2; exit 1; }
echo "OK Kafka outage did not roll back order; outbox recovered order=$ORDER_NO event=$EVENT_ID"

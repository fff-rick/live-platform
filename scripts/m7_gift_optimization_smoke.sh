#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
STAMP="$(date +%s)"
USERNAME="m7giftopt_${STAMP}_$RANDOM"
PASSWORD="password123"

json_field() { python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"; }
restore_api() {
  echo "restoring default gift limits"
  docker compose up -d --force-recreate live-api >/dev/null
}
trap restore_api EXIT

GIFT_USER_RATE_LIMIT=2 GIFT_USER_RATE_WINDOW=5s GIFT_MAX_COUNT_PER_REQUEST=100 \
  docker compose up -d --force-recreate live-api >/dev/null
until curl -fsS "$BASE_URL/ready" >/dev/null 2>&1; do sleep 1; done

REG=$(curl -fsS -X POST "$BASE_URL/api/v1/auth/register" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$USERNAME\",\"nickname\":\"GiftOpt\",\"password\":\"$PASSWORD\"}")
TOKEN=$(printf '%s' "$REG" | json_field access_token)
ROOM=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"title":"M7 gift optimization"}')
ROOM_ID=$(printf '%s' "$ROOM" | json_field room_id)
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" -H "Authorization: Bearer $TOKEN" >/dev/null
curl -fsS -X POST "$BASE_URL/api/v1/wallet/dev-credit" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"amount":1000000}' >/dev/null

KEY1="m7-opt-combo-$STAMP"
KEY2="m7-opt-single-$STAMP"
KEY3="m7-opt-limit-$STAMP"

echo "[1/4] combo count=100 should be one transaction"
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/gifts" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -H "Idempotency-Key: $KEY1" \
  -d '{"gift_id":1,"count":100}' >/dev/null

echo "[2/4] second unique gift is allowed"
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/gifts" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -H "Idempotency-Key: $KEY2" \
  -d '{"gift_id":1,"count":1}' >/dev/null

echo "[3/4] third unique request inside the window must be 429"
STATUS=$(curl -sS -o /tmp/m7-gift-rate-body.$$ -w '%{http_code}' -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/gifts" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -H "Idempotency-Key: $KEY3" \
  -d '{"gift_id":1,"count":1}')
[[ "$STATUS" == "429" ]] || { cat /tmp/m7-gift-rate-body.$$ >&2; rm -f /tmp/m7-gift-rate-body.$$; echo "expected 429 got $STATUS" >&2; exit 1; }
rm -f /tmp/m7-gift-rate-body.$$

echo "[4/4] replay of the committed combo bypasses limiter and returns 200"
REPLAY=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/gifts" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -H "Idempotency-Key: $KEY1" \
  -d '{"gift_id":1,"count":100}')
python3 - <<'PY' "$REPLAY"
import json,sys
v=json.loads(sys.argv[1])
assert v["idempotent_replay"] is True, v
assert v["order"]["gift_count"] == 100, v
PY

TX=$(curl -fsS "$BASE_URL/api/v1/wallet/transactions" -H "Authorization: Bearer $TOKEN")
python3 - <<'PY' "$TX"
import json,sys
v=json.loads(sys.argv[1])
items=v.get("items", v if isinstance(v,list) else [])
gifts=[x for x in items if x.get("biz_type")=="GIFT"]
assert len(gifts) == 2, f"expected two gift wallet transactions, got {len(gifts)}"
PY

echo "OK gift combo + rate limit + idempotent replay room_id=$ROOM_ID"

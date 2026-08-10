#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
USERNAME="${USERNAME:-m4_$(date +%s)}"
PASSWORD="${PASSWORD:-password123}"
NICKNAME="${NICKNAME:-M4用户}"

json_field() {
  python3 -c 'import json,sys; v=json.load(sys.stdin); print(v'"$1"')'
}

echo "[1/10] register $USERNAME"
REGISTER=$(curl -fsS -X POST "$BASE_URL/api/v1/auth/register" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"$USERNAME\",\"nickname\":\"$NICKNAME\",\"password\":\"$PASSWORD\"}")
TOKEN=$(printf '%s' "$REGISTER" | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')

echo "[2/10] dev credit 1000"
CREDIT=$(curl -fsS -X POST "$BASE_URL/api/v1/wallet/dev-credit" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"amount":1000}')
printf '%s' "$CREDIT" | python3 -c 'import json,sys; assert json.load(sys.stdin)["balance"] == 1000'

echo "[3/10] create and start room"
ROOM=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"title":"M4 smoke room"}')
ROOM_ID=$(printf '%s' "$ROOM" | python3 -c 'import json,sys; print(json.load(sys.stdin)["room_id"])')
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" -H "Authorization: Bearer $TOKEN" >/dev/null

echo "[4/10] list seeded gifts"
GIFTS=$(curl -fsS "$BASE_URL/api/v1/gifts")
printf '%s' "$GIFTS" | python3 -c 'import json,sys; v=json.load(sys.stdin); assert len(v["items"]) >= 3; assert v["items"][0]["price"] == 100'

echo "[5/10] send Rose once"
KEY="m4-smoke-$(date +%s)-$RANDOM"
FIRST=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/gifts" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -H "Idempotency-Key: $KEY" \
  -d '{"gift_id":1,"count":1}')
ORDER_NO=$(printf '%s' "$FIRST" | python3 -c 'import json,sys; v=json.load(sys.stdin); assert v["idempotent_replay"] is False; assert v["order"]["total_amount"] == 100; print(v["order"]["order_no"])')

echo "[6/10] replay same request; order must be identical"
REPLAY=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/gifts" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -H "Idempotency-Key: $KEY" \
  -d '{"gift_id":1,"count":1}')
printf '%s' "$REPLAY" | python3 -c 'import json,sys; v=json.load(sys.stdin); assert v["idempotent_replay"] is True; assert v["order"]["order_no"] == sys.argv[1]' "$ORDER_NO"

echo "[7/10] same key with different payload must conflict"
STATUS=$(curl -sS -o /tmp/m4_conflict.json -w '%{http_code}' -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/gifts" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -H "Idempotency-Key: $KEY" \
  -d '{"gift_id":1,"count":2}')
if [[ "$STATUS" != "409" ]]; then
  echo "expected 409, got $STATUS: $(cat /tmp/m4_conflict.json)" >&2
  exit 1
fi

echo "[8/10] balance must be 900, not 800"
BALANCE=$(curl -fsS "$BASE_URL/api/v1/wallet" -H "Authorization: Bearer $TOKEN")
printf '%s' "$BALANCE" | python3 -c 'import json,sys; assert json.load(sys.stdin)["balance"] == 900'

echo "[9/10] order query"
ORDER=$(curl -fsS "$BASE_URL/api/v1/gift-orders/$ORDER_NO" -H "Authorization: Bearer $TOKEN")
printf '%s' "$ORDER" | python3 -c 'import json,sys; v=json.load(sys.stdin); assert v["status"] == "SUCCESS"; assert v["total_amount"] == 100'

echo "[10/10] audit wallet ledger"
TXS=$(curl -fsS "$BASE_URL/api/v1/wallet/transactions" -H "Authorization: Bearer $TOKEN")
printf '%s' "$TXS" | python3 -c 'import json,sys; v=json.load(sys.stdin)["items"]; assert len(v) >= 2; gift=[x for x in v if x["biz_type"]=="GIFT"]; assert len(gift)==1; assert gift[0]["amount"]==-100; assert gift[0]["balance_before"]==1000; assert gift[0]["balance_after"]==900'

echo "OK room_id=$ROOM_ID order_no=$ORDER_NO balance=900 idempotency=passed ledger=passed"

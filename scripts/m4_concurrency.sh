#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
USERNAME="m4c_$(date +%s)_$RANDOM"
PASSWORD="password123"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

REGISTER=$(curl -fsS -X POST "$BASE_URL/api/v1/auth/register" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$USERNAME\",\"nickname\":\"并发测试\",\"password\":\"$PASSWORD\"}")
TOKEN=$(printf '%s' "$REGISTER" | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
curl -fsS -X POST "$BASE_URL/api/v1/wallet/dev-credit" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"amount":1000}' >/dev/null
ROOM=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"title":"M4 concurrency"}')
ROOM_ID=$(printf '%s' "$ROOM" | python3 -c 'import json,sys; print(json.load(sys.stdin)["room_id"])')
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" -H "Authorization: Bearer $TOKEN" >/dev/null

echo "fire 20 concurrent Rose orders against balance=1000"
for i in $(seq 1 20); do
  (
    code=$(curl -sS -o "$TMP/body.$i" -w '%{http_code}' -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/gifts" \
      -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -H "Idempotency-Key: m4-concurrent-$USERNAME-$i" \
      -d '{"gift_id":1,"count":1}')
    printf '%s' "$code" > "$TMP/code.$i"
  ) &
done
wait

SUCCESS=$(grep -l '^200$' "$TMP"/code.* | wc -l | tr -d ' ')
CONFLICT=$(grep -l '^409$' "$TMP"/code.* | wc -l | tr -d ' ')
if [[ "$SUCCESS" != "10" || "$CONFLICT" != "10" ]]; then
  echo "expected 10 successes and 10 conflicts, got successes=$SUCCESS conflicts=$CONFLICT" >&2
  for f in "$TMP"/code.*; do echo "$(basename "$f"): $(cat "$f")"; done
  exit 1
fi
BALANCE=$(curl -fsS "$BASE_URL/api/v1/wallet" -H "Authorization: Bearer $TOKEN")
LEFT=$(printf '%s' "$BALANCE" | python3 -c 'import json,sys; print(json.load(sys.stdin)["balance"])')
if [[ "$LEFT" != "0" ]]; then
  echo "expected final balance=0, got $LEFT" >&2
  exit 1
fi

echo "OK successes=10 rejected=$CONFLICT final_balance=0"

#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
USERNAME="${USERNAME:-m3_$(date +%s)}"
PASSWORD="${PASSWORD:-password123}"
NICKNAME="${NICKNAME:-M3主播}"

json_field() {
  python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"
}

echo "[1/8] register $USERNAME"
REGISTER=$(curl -fsS -X POST "$BASE_URL/api/v1/auth/register" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"$USERNAME\",\"nickname\":\"$NICKNAME\",\"password\":\"$PASSWORD\"}")
TOKEN=$(printf '%s' "$REGISTER" | json_field access_token)

echo "[2/8] create room"
ROOM=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"title":"M3 smoke room"}')
ROOM_ID=$(printf '%s' "$ROOM" | json_field room_id)

echo "[3/8] start room $ROOM_ID"
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" \
  -H "Authorization: Bearer $TOKEN" >/dev/null

echo "[4/8] join twice; same user must still count once"
JOIN1=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/join" -H "Authorization: Bearer $TOKEN")
JOIN2=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/join" -H "Authorization: Bearer $TOKEN")
VIEWERS=$(printf '%s' "$JOIN2" | python3 -c 'import json,sys; print(json.load(sys.stdin)["stats"]["viewer_count"])')
if [[ "$VIEWERS" != "1" ]]; then
  echo "expected viewer_count=1, got $VIEWERS" >&2
  exit 1
fi

echo "[5/8] batch 10 likes"
LIKE=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/like" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"count":10}')
TOTAL=$(printf '%s' "$LIKE" | json_field total)
if [[ "$TOTAL" != "10" ]]; then
  echo "expected like total=10, got $TOTAL" >&2
  exit 1
fi

echo "[6/8] heartbeat"
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/heartbeat" \
  -H "Authorization: Bearer $TOKEN" >/dev/null

echo "[7/8] read room stats"
STATS=$(curl -fsS "$BASE_URL/api/v1/rooms/$ROOM_ID/stats")
printf '%s' "$STATS" | python3 -c 'import json,sys; v=json.load(sys.stdin); assert v["viewer_count"] == 1; assert v["like_count"] == 10'

echo "[8/8] send danmaku to verify M2 regression"
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/danmaku" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"content":"M3 keeps M2 working"}' >/dev/null

echo "OK room_id=$ROOM_ID viewer_count=1 like_count=10"

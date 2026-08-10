#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
USERNAME="${USERNAME:-anchor_$(date +%s)}"
PASSWORD="${PASSWORD:-password123}"
NICKNAME="${NICKNAME:-M2主播}"

json_field() {
  python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"
}

echo "[1/6] register $USERNAME"
REGISTER=$(curl -fsS -X POST "$BASE_URL/api/v1/auth/register" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"$USERNAME\",\"nickname\":\"$NICKNAME\",\"password\":\"$PASSWORD\"}")
TOKEN=$(printf '%s' "$REGISTER" | json_field access_token)

echo "[2/6] create room"
ROOM=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"title":"M2 smoke room"}')
ROOM_ID=$(printf '%s' "$ROOM" | json_field room_id)

echo "[3/6] start room $ROOM_ID"
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" \
  -H "Authorization: Bearer $TOKEN" >/dev/null

echo "[4/6] join room and obtain subscription tokens"
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/join" \
  -H "Authorization: Bearer $TOKEN" >/dev/null

echo "[5/6] issue Centrifugo connection token"
curl -fsS -X POST "$BASE_URL/api/v1/realtime/token" \
  -H "Authorization: Bearer $TOKEN" >/dev/null

echo "[6/6] send danmaku"
DANMAKU=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/danmaku" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"content":"M2 smoke test"}')

echo "OK room_id=$ROOM_ID"
echo "$DANMAKU"

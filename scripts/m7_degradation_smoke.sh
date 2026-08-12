#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
USERNAME="${USERNAME:-m7degrade_$(date +%s)}"
PASSWORD="${PASSWORD:-password123}"

json_field() {
  python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"
}

restore_api() {
  echo "restoring normal M7 thresholds"
  docker compose up -d --force-recreate live-api >/dev/null
}
trap restore_api EXIT

echo "[1/7] restart live-api with tiny thresholds so one user can exercise NORMAL/HOT/PROTECT"
DANMAKU_HOT_VIEWERS=999999999 \
DANMAKU_PROTECT_VIEWERS=999999999 \
DANMAKU_HOT_RATE=2 \
DANMAKU_PROTECT_RATE=4 \
DANMAKU_RATE_WINDOW=30s \
DANMAKU_HOT_SAMPLE_RATE=1 \
DANMAKU_PROTECT_SAMPLE_RATE=1 \
docker compose up -d --force-recreate live-api >/dev/null

until curl -fsS "$BASE_URL/ready" >/dev/null 2>&1; do sleep 1; done

echo "[2/7] register"
REGISTER=$(curl -fsS -X POST "$BASE_URL/api/v1/auth/register" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"$USERNAME\",\"nickname\":\"M7Degrade\",\"password\":\"$PASSWORD\"}")
TOKEN=$(printf '%s' "$REGISTER" | json_field access_token)

echo "[3/7] create/start/join room"
ROOM=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"M7 degradation smoke"}')
ROOM_ID=$(printf '%s' "$ROOM" | json_field room_id)
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" -H "Authorization: Bearer $TOKEN" >/dev/null
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/join" -H "Authorization: Bearer $TOKEN" >/dev/null

echo "[4/7] send first request => NORMAL"
R1=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/danmaku" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"content":"m7-normal"}')
M1=$(printf '%s' "$R1" | json_field traffic_mode)
[[ "$M1" == "NORMAL" ]] || { echo "expected NORMAL got $M1" >&2; exit 1; }

echo "[5/7] second request reaches HOT threshold"
R2=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/danmaku" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"content":"m7-hot"}')
M2=$(printf '%s' "$R2" | json_field traffic_mode)
[[ "$M2" == "HOT" ]] || { echo "expected HOT got $M2" >&2; exit 1; }

echo "[6/7] third remains HOT; fourth reaches PROTECT"
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/danmaku" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"content":"m7-hot-2"}' >/dev/null
R4=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/danmaku" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"content":"m7-protect"}')
M4=$(printf '%s' "$R4" | json_field traffic_mode)
[[ "$M4" == "PROTECT" ]] || { echo "expected PROTECT got $M4" >&2; exit 1; }

echo "[7/7] metrics expose HOT/PROTECT decisions"
METRICS=$(curl -fsS "$BASE_URL/metrics")
printf '%s\n' "$METRICS" | grep -q 'live_danmaku_degradation_total{action="broadcast",mode="HOT"}'
printf '%s\n' "$METRICS" | grep -q 'live_danmaku_degradation_total{action="broadcast",mode="PROTECT"}'

echo "OK room_id=$ROOM_ID modes=$M1,$M2,$M4"

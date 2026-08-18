#!/usr/bin/env sh
set -eu
BASE_URL="${BASE_URL:-http://localhost:8080}"
STAMP="$(date +%s)$$"
USER="ui${STAMP}"
PASS="password123"
NICK="UI主播${STAMP}"

echo "[ui] root SPA"
curl -fsS "$BASE_URL/" | grep -q 'LiveFlow'

echo "[ui] SPA room fallback"
curl -fsS "$BASE_URL/room/999999" | grep -q 'LiveFlow'

echo "[ui] register showcase user"
AUTH="$(curl -fsS -X POST "$BASE_URL/api/v1/auth/register" -H 'Content-Type: application/json' -d "{\"username\":\"$USER\",\"nickname\":\"$NICK\",\"password\":\"$PASS\"}")"
TOKEN="$(printf '%s' "$AUTH" | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')"

echo "[ui] create + start room"
ROOM="$(curl -fsS -X POST "$BASE_URL/api/v1/rooms" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"title":"LiveFlow Showcase Smoke"}')"
ROOM_ID="$(printf '%s' "$ROOM" | python3 -c 'import json,sys; print(json.load(sys.stdin)["room_id"])')"
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" -H "Authorization: Bearer $TOKEN" >/dev/null

echo "[ui] lobby contains real room + anchor nickname"
LIST="$(curl -fsS "$BASE_URL/api/v1/rooms?status=LIVING&limit=100")"
printf '%s' "$LIST" | python3 -c 'import json,sys; d=json.load(sys.stdin); rid=int(sys.argv[1]); nick=sys.argv[2]; assert any(int(x["room_id"])==rid and x.get("anchor_nickname")==nick for x in d["items"])' "$ROOM_ID" "$NICK"

echo "[ui] room route is SPA"
curl -fsS "$BASE_URL/room/$ROOM_ID" | grep -q 'LiveFlow'

echo "[ui] OK room_id=$ROOM_ID"

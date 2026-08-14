#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
USERS="${USERS:-100}"
RATES="${RATES:-200 500 1000 2000}"
CONCURRENCY="${CONCURRENCY:-512}"
DURATION="${DURATION:-60s}"
CREDIT="${CREDIT:-100000000}"
GIFT_ID="${GIFT_ID:-1}"
STAMP="$(date +%s)"
TMP="$(mktemp -d)"
TOKENS="$TMP/tokens.txt"
mkdir -p reports/m7/gift-platform-ladder

restore_api() {
  rm -rf "$TMP"
  docker compose up -d --force-recreate live-api >/dev/null || true
}
trap restore_api EXIT

# Platform-capacity benchmark intentionally raises the per-user guard so the
# measured bottleneck is MySQL/API/Outbox rather than the abuse-protection layer.
GIFT_USER_RATE_LIMIT=100000 GIFT_USER_RATE_WINDOW=1s docker compose up -d --force-recreate live-api >/dev/null
until curl -fsS "$BASE_URL/ready" >/dev/null 2>&1; do sleep 1; done

json_token() { python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])'; }
json_room() { python3 -c 'import json,sys; print(json.load(sys.stdin)["room_id"])'; }
register_user() {
  curl -fsS -X POST "$BASE_URL/api/v1/auth/register" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$1\",\"nickname\":\"$2\",\"password\":\"password123\"}" | json_token
}
collect_lock() {
  docker compose exec -T mysql mysql -ulive -plive live -N -e "SHOW GLOBAL STATUS WHERE Variable_name IN ('Innodb_row_lock_current_waits','Innodb_row_lock_time','Innodb_row_lock_time_avg','Innodb_row_lock_time_max','Innodb_row_lock_waits','Threads_running','Threads_connected');" > "$1" 2>&1 || true
}

OWNER=$(register_user "m7platform_owner_${STAMP}_$RANDOM" "M7 Platform Owner")
ROOM_ID=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms" -H "Authorization: Bearer $OWNER" -H 'Content-Type: application/json' -d '{"title":"M7 gift platform ladder"}' | json_room)
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" -H "Authorization: Bearer $OWNER" >/dev/null

for i in $(seq 1 "$USERS"); do
  token=$(register_user "m7platform_${STAMP}_${i}_$RANDOM" "M7 Platform $i")
  printf '%s\n' "$token" >> "$TOKENS"
  curl -fsS -X POST "$BASE_URL/api/v1/wallet/dev-credit" -H "Authorization: Bearer $token" -H 'Content-Type: application/json' -d "{\"amount\":$CREDIT}" >/dev/null
done

for rate in $RATES; do
  echo "multi-wallet platform ladder: users=$USERS target=$rate/s"
  before="reports/m7/gift-platform-ladder/rate-${rate}-lock-before.txt"
  after="reports/m7/gift-platform-ladder/rate-${rate}-lock-after.txt"
  collect_lock "$before"
  go run ./tools/httpload \
    --scenario "gift-platform-${USERS}-users-rate-${rate}" \
    --url "$BASE_URL/api/v1/rooms/$ROOM_ID/gifts" --method POST \
    --body "{\"gift_id\":$GIFT_ID,\"count\":1}" --bearer-file "$TOKENS" \
    --idempotency --idempotency-prefix "m7-platform-${rate}-${STAMP}" \
    --rate "$rate" --concurrency "$CONCURRENCY" --duration "$DURATION" \
    --report "reports/m7/gift-platform-ladder/rate-${rate}.json"
  collect_lock "$after"
  sleep 3
done

echo "done room_id=$ROOM_ID reports/m7/gift-platform-ladder/"

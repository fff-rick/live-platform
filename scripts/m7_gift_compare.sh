#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
USERS="${USERS:-100}"
RATE="${RATE:-50}"
CONCURRENCY="${CONCURRENCY:-128}"
DURATION="${DURATION:-60s}"
CREDIT="${CREDIT:-10000000}"
GIFT_ID="${GIFT_ID:-1}"
STAMP="$(date +%s)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p reports/m7
TOKENS="$TMP/tokens.txt"

json_token() {
  python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])'
}
json_room() {
  python3 -c 'import json,sys; print(json.load(sys.stdin)["room_id"])'
}


collect_mysql_lock_status() {
  local out="$1"
  docker compose exec -T mysql mysql -ulive -plive live -N -e "SHOW GLOBAL STATUS WHERE Variable_name IN ('Innodb_row_lock_current_waits','Innodb_row_lock_time','Innodb_row_lock_time_avg','Innodb_row_lock_time_max','Innodb_row_lock_waits','Threads_running','Threads_connected');" > "$out" 2>&1 || true
}

register_user() {
  local username="$1" nickname="$2"
  curl -fsS -X POST "$BASE_URL/api/v1/auth/register" \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"$username\",\"nickname\":\"$nickname\",\"password\":\"password123\"}" | json_token
}

echo "[1/5] create benchmark owner and room"
OWNER_TOKEN="$(register_user "m7gift_owner_${STAMP}_$RANDOM" "M7 Gift Owner")"
ROOM_ID="$(curl -fsS -X POST "$BASE_URL/api/v1/rooms" \
  -H "Authorization: Bearer $OWNER_TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"M7 gift compare"}' | json_room)"
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" -H "Authorization: Bearer $OWNER_TOKEN" >/dev/null

echo "[2/5] prepare $USERS funded gift senders"
for i in $(seq 1 "$USERS"); do
  token="$(register_user "m7gift_${STAMP}_${i}_$RANDOM" "M7 Gift $i")"
  printf '%s\n' "$token" >> "$TOKENS"
  curl -fsS -X POST "$BASE_URL/api/v1/wallet/dev-credit" \
    -H "Authorization: Bearer $token" -H 'Content-Type: application/json' \
    -d "{\"amount\":$CREDIT}" >/dev/null
done
SINGLE_TOKEN="$(head -n 1 "$TOKENS")"

echo "[3/5] single-wallet hotspot: rate=$RATE concurrency=$CONCURRENCY duration=$DURATION"
collect_mysql_lock_status reports/m7/gift-single-wallet-lock-before.txt
go run ./tools/httpload \
  --scenario "gift-single-wallet" \
  --url "$BASE_URL/api/v1/rooms/$ROOM_ID/gifts" --method POST \
  --body "{\"gift_id\":$GIFT_ID,\"count\":1}" \
  --bearer "$SINGLE_TOKEN" --idempotency --idempotency-prefix "m7-single-$STAMP" \
  --rate "$RATE" --concurrency "$CONCURRENCY" --duration "$DURATION" \
  --report reports/m7/gift-single-wallet.json
collect_mysql_lock_status reports/m7/gift-single-wallet-lock-after.txt
OUT="reports/m7/snapshot-gift-single-$STAMP" ./scripts/m7_snapshot.sh >/dev/null || true

sleep 3

echo "[4/5] multi-wallet: users=$USERS rate=$RATE concurrency=$CONCURRENCY duration=$DURATION"
collect_mysql_lock_status reports/m7/gift-multi-wallet-lock-before.txt
go run ./tools/httpload \
  --scenario "gift-multi-wallet-${USERS}-users" \
  --url "$BASE_URL/api/v1/rooms/$ROOM_ID/gifts" --method POST \
  --body "{\"gift_id\":$GIFT_ID,\"count\":1}" \
  --bearer-file "$TOKENS" --idempotency --idempotency-prefix "m7-multi-$STAMP" \
  --rate "$RATE" --concurrency "$CONCURRENCY" --duration "$DURATION" \
  --report reports/m7/gift-multi-wallet.json
collect_mysql_lock_status reports/m7/gift-multi-wallet-lock-after.txt
OUT="reports/m7/snapshot-gift-multi-$STAMP" ./scripts/m7_snapshot.sh >/dev/null || true

echo "[5/5] done"
echo "room_id=$ROOM_ID"
echo "reports/m7/gift-single-wallet.json"
echo "reports/m7/gift-multi-wallet.json"
echo "Compare P95/P99, lock-before/after deltas, and Tempo spans (especially gift.db.wallet_update)."

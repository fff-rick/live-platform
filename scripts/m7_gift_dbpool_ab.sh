#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
USERS="${USERS:-100}"
POOLS="${POOLS:-20 40 80}"
RATES="${RATES:-500 1000}"
CONCURRENCY="${CONCURRENCY:-512}"
DURATION="${DURATION:-60s}"
# wallet.DevCredit caps a single development credit at 100,000,000.
# This is still far above the default 100 users × 1,000 gifts/s × 60s run.
CREDIT="${CREDIT:-100000000}"
GIFT_ID="${GIFT_ID:-1}"
OTEL_SAMPLE_RATIO_BENCHMARK="${OTEL_SAMPLE_RATIO_BENCHMARK:-1}"
STAMP="$(date +%s)"
TMP="$(mktemp -d)"
TOKENS="$TMP/tokens.txt"
mkdir -p reports/m7/gift-dbpool-ab

json_field() { python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"; }
register_user() {
  curl -fsS -X POST "$BASE_URL/api/v1/auth/register" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$1\",\"nickname\":\"$2\",\"password\":\"password123\"}" | json_field access_token
}
collect_lock() {
  docker compose exec -T mysql mysql -ulive -plive live -N -e "SHOW GLOBAL STATUS WHERE Variable_name IN ('Innodb_row_lock_current_waits','Innodb_row_lock_time','Innodb_row_lock_time_avg','Innodb_row_lock_time_max','Innodb_row_lock_waits','Threads_running','Threads_connected');" > "$1" 2>&1 || true
}
sample_db_pool() {
  local out="$1" stop="$2"
  printf 'unix_ms,max_open,open,in_use,idle,wait_total,wait_duration_seconds\n' > "$out"
  while [[ ! -f "$stop" ]]; do
    metrics=$(curl -fsS "$BASE_URL/metrics" 2>/dev/null || true)
    value() {
      local name="$1"
      printf '%s\n' "$metrics" | awk -v n="$name" '$1 ~ ("^" n "\\{") {print $NF; exit}'
    }
    max_open=$(value live_db_pool_max_open_connections); max_open=${max_open:-0}
    open=$(value live_db_pool_open_connections); open=${open:-0}
    in_use=$(value live_db_pool_in_use_connections); in_use=${in_use:-0}
    idle=$(value live_db_pool_idle_connections); idle=${idle:-0}
    waits=$(value live_db_pool_wait_total); waits=${waits:-0}
    wait_sec=$(value live_db_pool_wait_duration_seconds_total); wait_sec=${wait_sec:-0}
    printf '%s,%s,%s,%s,%s,%s,%s\n' "$(date +%s%3N)" "$max_open" "$open" "$in_use" "$idle" "$waits" "$wait_sec" >> "$out"
    sleep 1
  done
}
restore_api() {
  rm -rf "$TMP"
  docker compose up -d --force-recreate live-api >/dev/null || true
}
trap restore_api EXIT

# Bootstrap users with the default pool. Tokens remain valid across API restarts.
GIFT_USER_RATE_LIMIT=100000 GIFT_USER_RATE_WINDOW=1s docker compose up -d --force-recreate live-api >/dev/null
until curl -fsS "$BASE_URL/ready" >/dev/null 2>&1; do sleep 1; done
OWNER=$(register_user "m7pool_owner_${STAMP}_$RANDOM" "M7 Pool Owner")
ROOM=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms" -H "Authorization: Bearer $OWNER" -H 'Content-Type: application/json' -d '{"title":"M7 DB pool A/B"}')
ROOM_ID=$(printf '%s' "$ROOM" | json_field room_id)
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" -H "Authorization: Bearer $OWNER" >/dev/null
for i in $(seq 1 "$USERS"); do
  token=$(register_user "m7pool_${STAMP}_${i}_$RANDOM" "M7 Pool $i")
  printf '%s\n' "$token" >> "$TOKENS"
  curl -fsS -X POST "$BASE_URL/api/v1/wallet/dev-credit" -H "Authorization: Bearer $token" -H 'Content-Type: application/json' -d "{\"amount\":$CREDIT}" >/dev/null
done

for pool in $POOLS; do
  idle=$((pool / 2))
  if (( idle < 1 )); then idle=1; fi
  for rate in $RATES; do
    name="pool-${pool}-rate-${rate}"
    dir="reports/m7/gift-dbpool-ab/$name"
    mkdir -p "$dir"
    echo "DB pool A/B: max_open=$pool max_idle=$idle target=$rate/s users=$USERS"

    # Restart API for every case so sql.DB WaitCount/WaitDuration counters start
    # from zero and the resulting metrics file is directly comparable.
    MYSQL_MAX_OPEN_CONNS="$pool" MYSQL_MAX_IDLE_CONNS="$idle" \
      GIFT_USER_RATE_LIMIT=100000 GIFT_USER_RATE_WINDOW=1s \
      OTEL_SAMPLE_RATIO="$OTEL_SAMPLE_RATIO_BENCHMARK" \
      docker compose up -d --force-recreate live-api >/dev/null
    until curl -fsS "$BASE_URL/ready" >/dev/null 2>&1; do sleep 1; done

    collect_lock "$dir/mysql-lock-before.txt"
    stopfile="$dir/.stop-sampler"
    rm -f "$stopfile"
    sample_db_pool "$dir/db-pool-samples.csv" "$stopfile" &
    sampler_pid=$!
    go run ./tools/httpload \
      --scenario "gift-dbpool-${pool}-rate-${rate}" \
      --url "$BASE_URL/api/v1/rooms/$ROOM_ID/gifts" --method POST \
      --body "{\"gift_id\":$GIFT_ID,\"count\":1}" --bearer-file "$TOKENS" \
      --idempotency --idempotency-prefix "m7-dbpool-${pool}-${rate}-${STAMP}" \
      --rate "$rate" --concurrency "$CONCURRENCY" --duration "$DURATION" \
      --report "$dir/http.json"
    touch "$stopfile"
    wait "$sampler_pid" || true
    rm -f "$stopfile"
    curl -fsS "$BASE_URL/metrics" > "$dir/api-metrics.txt"
    collect_lock "$dir/mysql-lock-after.txt"

    # Keep a tiny, greppable summary next to the raw evidence.
    grep -E '^live_db_pool_(max_open_connections|open_connections|in_use_connections|idle_connections|wait_total|wait_duration_seconds_total)' "$dir/api-metrics.txt" > "$dir/db-pool-summary.txt" || true
    sleep 3
  done
done

python3 scripts/m7_dbpool_compare.py reports/m7/gift-dbpool-ab || true
echo "done room_id=$ROOM_ID reports/m7/gift-dbpool-ab/"

#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
WALLETS="${WALLETS:-1000}"
RATES="${RATES:-500 1000 1500}"
POOL="${POOL:-40}"
IDLE="${IDLE:-$((POOL / 2))}"
CONCURRENCY="${CONCURRENCY:-512}"
DURATION="${DURATION:-60s}"
CREDIT="${CREDIT:-100000000}"
GIFT_ID="${GIFT_ID:-1}"
BOOTSTRAP_CONCURRENCY="${BOOTSTRAP_CONCURRENCY:-32}"
OTEL_SAMPLE_RATIO_BENCHMARK="${OTEL_SAMPLE_RATIO_BENCHMARK:-1}"
STAMP="$(date +%s)"
TMP="$(mktemp -d)"
TOKENS="$TMP/tokens.txt"
REPORT_ROOT="reports/m7/gift-1000-wallet-capacity"
mkdir -p "$REPORT_ROOT"
cat > "$REPORT_ROOT/experiment.txt" <<META
wallets=$WALLETS
pool=$POOL
idle=$IDLE
rates=$RATES
concurrency=$CONCURRENCY
duration=$DURATION
bootstrap_concurrency=$BOOTSTRAP_CONCURRENCY
otel_sample_ratio=$OTEL_SAMPLE_RATIO_BENCHMARK
META

json_field() { python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"; }
register_user() {
  curl -fsS -X POST "$BASE_URL/api/v1/auth/register" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$1\",\"nickname\":\"$2\",\"password\":\"password123\"}" | json_field access_token
}
collect_lock() {
  docker compose exec -T mysql mysql -ulive -plive live -N -e \
    "SHOW GLOBAL STATUS WHERE Variable_name IN ('Innodb_row_lock_current_waits','Innodb_row_lock_time','Innodb_row_lock_time_avg','Innodb_row_lock_time_max','Innodb_row_lock_waits','Threads_running','Threads_connected');" \
    > "$1" 2>&1 || true
}
sample_db_pool() {
  local out="$1" stop="$2"
  printf 'unix_ms,max_open,open,in_use,idle,wait_total,wait_duration_seconds\n' > "$out"
  while [[ ! -f "$stop" ]]; do
    local metrics
    metrics=$(curl -fsS "$BASE_URL/metrics" 2>/dev/null || true)
    value() {
      local name="$1"
      printf '%s\n' "$metrics" | awk -v n="$name" '$1 ~ ("^" n "\\{") {print $NF; exit}'
    }
    local max_open open in_use idle waits wait_sec
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
wait_ready() {
  until curl -fsS "$BASE_URL/ready" >/dev/null 2>&1; do sleep 1; done
}
restore_api() {
  rm -rf "$TMP"
  docker compose up -d --force-recreate live-api >/dev/null || true
}
trap restore_api EXIT

# Keep the database-pool setting fixed so the experiment isolates wallet
# cardinality instead of changing two independent variables at once.
MYSQL_MAX_OPEN_CONNS="$POOL" MYSQL_MAX_IDLE_CONNS="$IDLE" \
  GIFT_USER_RATE_LIMIT=100000 GIFT_USER_RATE_WINDOW=1s \
  OTEL_SAMPLE_RATIO="$OTEL_SAMPLE_RATIO_BENCHMARK" \
  docker compose up -d --force-recreate live-api >/dev/null
wait_ready

OWNER=$(register_user "m7scale_owner_${STAMP}_$RANDOM" "M7 Scale Owner")
ROOM=$(curl -fsS -X POST "$BASE_URL/api/v1/rooms" -H "Authorization: Bearer $OWNER" -H 'Content-Type: application/json' -d '{"title":"M7 1000-wallet capacity"}')
ROOM_ID=$(printf '%s' "$ROOM" | json_field room_id)
curl -fsS -X POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" -H "Authorization: Bearer $OWNER" >/dev/null

echo "bootstrapping wallets=$WALLETS concurrency=$BOOTSTRAP_CONCURRENCY"
export BASE_URL CREDIT STAMP TMP
bootstrap_one() {
  local i="$1" username response token
  username="m7scale_${STAMP}_${i}_$RANDOM"
  response=$(curl -fsS -X POST "$BASE_URL/api/v1/auth/register" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$username\",\"nickname\":\"M7 Scale $i\",\"password\":\"password123\"}")
  token=$(printf '%s' "$response" | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
  curl -fsS -X POST "$BASE_URL/api/v1/wallet/dev-credit" -H "Authorization: Bearer $token" -H 'Content-Type: application/json' \
    -d "{\"amount\":$CREDIT}" >/dev/null
  printf '%s\n' "$token" > "$TMP/token-$i.txt"
}
export -f bootstrap_one
seq 1 "$WALLETS" | xargs -P "$BOOTSTRAP_CONCURRENCY" -n 1 bash -euo pipefail -c 'bootstrap_one "$1"' _
find "$TMP" -maxdepth 1 -name 'token-*.txt' -print0 | sort -zV | xargs -0 cat > "$TOKENS"
actual_wallets=$(wc -l < "$TOKENS" | tr -d ' ')
if [[ "$actual_wallets" != "$WALLETS" ]]; then
  echo "wallet bootstrap incomplete expected=$WALLETS actual=$actual_wallets" >&2
  exit 1
fi

for rate in $RATES; do
  name="wallets-${WALLETS}-pool-${POOL}-rate-${rate}"
  dir="$REPORT_ROOT/$name"
  mkdir -p "$dir"
  echo "wallet-scale capacity: wallets=$WALLETS pool=$POOL target=$rate/s concurrency=$CONCURRENCY"

  # Restart each case so sql.DB WaitCount/WaitDuration are case-local.
  MYSQL_MAX_OPEN_CONNS="$POOL" MYSQL_MAX_IDLE_CONNS="$IDLE" \
    GIFT_USER_RATE_LIMIT=100000 GIFT_USER_RATE_WINDOW=1s \
    OTEL_SAMPLE_RATIO="$OTEL_SAMPLE_RATIO_BENCHMARK" \
    docker compose up -d --force-recreate live-api >/dev/null
  wait_ready

  collect_lock "$dir/mysql-lock-before.txt"
  stopfile="$dir/.stop-sampler"
  rm -f "$stopfile"
  sample_db_pool "$dir/db-pool-samples.csv" "$stopfile" &
  sampler_pid=$!

  go run ./tools/httpload \
    --scenario "gift-platform-${WALLETS}-wallets-pool-${POOL}-rate-${rate}" \
    --url "$BASE_URL/api/v1/rooms/$ROOM_ID/gifts" --method POST \
    --body "{\"gift_id\":$GIFT_ID,\"count\":1}" --bearer-file "$TOKENS" \
    --idempotency --idempotency-prefix "m7-wallet-scale-${rate}-${STAMP}" \
    --rate "$rate" --concurrency "$CONCURRENCY" --duration "$DURATION" \
    --report "$dir/http.json"

  touch "$stopfile"
  wait "$sampler_pid" || true
  rm -f "$stopfile"
  curl -fsS "$BASE_URL/metrics" > "$dir/api-metrics.txt"
  collect_lock "$dir/mysql-lock-after.txt"
  grep -E '^live_db_pool_(max_open_connections|open_connections|in_use_connections|idle_connections|wait_total|wait_duration_seconds_total)' \
    "$dir/api-metrics.txt" > "$dir/db-pool-summary.txt" || true
  sleep 3
done

python3 scripts/m7_wallet_scale_compare.py "$REPORT_ROOT" | tee "$REPORT_ROOT/summary.md"
echo "done room_id=$ROOM_ID reports=$REPORT_ROOT"

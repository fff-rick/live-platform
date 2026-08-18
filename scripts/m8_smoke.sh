#!/usr/bin/env bash
set -euo pipefail

API_URL="${API_URL:-http://localhost:8080}"
WORKER_URL="${WORKER_URL:-http://localhost:9090}"
CENTRIFUGO_URL="${CENTRIFUGO_URL:-http://localhost:8000}"
EXPECTED_MIGRATIONS="${EXPECTED_MIGRATIONS:-4}"

require_json_field() {
  local url="$1" key="$2" expected="$3"
  local body
  body="$(curl -fsS "$url")"
  python3 - "$key" "$expected" "$body" <<'PY'
import json, sys
key, expected, raw = sys.argv[1:]
data = json.loads(raw)
value = data
for part in key.split('.'):
    value = value[part]
if str(value) != expected:
    raise SystemExit(f"{key}: expected {expected!r}, got {value!r}; body={raw}")
PY
}

echo "[M8] checking API health/readiness"
require_json_field "$API_URL/health" milestone M8
require_json_field "$API_URL/ready" status ready

echo "[M8] checking worker health"
require_json_field "$WORKER_URL/health" milestone M8

echo "[M8] checking Centrifugo health"
curl -fsS "$CENTRIFUGO_URL/health" >/dev/null

migration_count() {
  docker compose exec -T mysql mysql -N -ulive -plive live \
    -e 'SELECT COUNT(*) FROM schema_migrations;' 2>/dev/null | tr -d '[:space:]'
}

before="$(migration_count)"
if [[ "$before" != "$EXPECTED_MIGRATIONS" ]]; then
  echo "expected $EXPECTED_MIGRATIONS applied migrations, got $before" >&2
  exit 1
fi

echo "[M8] re-running migration gate to verify idempotency"
docker compose run --rm live-migrate >/dev/null

after="$(migration_count)"
if [[ "$after" != "$before" ]]; then
  echo "migration count changed after idempotent rerun: before=$before after=$after" >&2
  exit 1
fi

echo "[M8] migration versions"
docker compose exec -T mysql mysql -N -ulive -plive live \
  -e 'SELECT version FROM schema_migrations ORDER BY version;' 2>/dev/null

echo "M8 smoke: PASS (api, worker, centrifugo, migration gate)"

echo "[m8] showcase UI"
BASE_URL="${BASE_URL:-http://localhost:8080}" ./scripts/ui_smoke.sh

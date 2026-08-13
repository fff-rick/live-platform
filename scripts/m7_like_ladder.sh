#!/usr/bin/env bash
set -euo pipefail
: "${ROOM_ID:?set ROOM_ID}"
: "${TOKEN:?set TOKEN}"
mkdir -p reports/m7/like-ladder

BASE_URL="${BASE_URL:-http://localhost:8080}"
DURATION="${DURATION:-60s}"
CONCURRENCY="${CONCURRENCY:-256}"
RATES="${RATES:-200 500 1000}"
COUNT="${COUNT:-100}"

for rate in $RATES; do
  logical=$((rate * COUNT))
  echo "like ladder: target_api_rate=$rate req/s logical_likes=$logical/s"
  go run ./tools/httpload \
    --scenario "like-${logical}-per-sec" \
    --url "$BASE_URL/api/v1/rooms/${ROOM_ID}/like" --method POST \
    --body "{\"count\":$COUNT}" --bearer "$TOKEN" \
    --rate "$rate" --concurrency "$CONCURRENCY" --duration "$DURATION" \
    --report "reports/m7/like-ladder/likes-${logical}.json"
done

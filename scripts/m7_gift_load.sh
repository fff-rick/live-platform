#!/usr/bin/env bash
set -euo pipefail
: "${ROOM_ID:?set ROOM_ID}"
: "${TOKEN:?set TOKEN}"
: "${GIFT_ID:?set GIFT_ID}"
mkdir -p reports/m7
go run ./tools/httpload --url "http://localhost:8080/api/v1/rooms/${ROOM_ID}/gifts" --method POST --body "{\"gift_id\":${GIFT_ID},\"count\":1}" --bearer "$TOKEN" --idempotency --rate "${RATE:-100}" --concurrency "${CONCURRENCY:-64}" --duration "${DURATION:-60s}" --report reports/m7/gift-load.json

#!/usr/bin/env bash
set -euo pipefail
: "${ROOM_ID:?set ROOM_ID}"
: "${TOKEN:?set TOKEN}"
mkdir -p reports/m7
go run ./tools/httpload --url "http://localhost:8080/api/v1/rooms/${ROOM_ID}/like" --method POST --body '{"count":100}' --bearer "$TOKEN" --rate "${RATE:-1000}" --concurrency "${CONCURRENCY:-128}" --duration "${DURATION:-60s}" --report reports/m7/like-storm.json

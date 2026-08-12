#!/usr/bin/env bash
set -euo pipefail
mkdir -p reports/m7
CLIENTS="${CLIENTS:-10000}"
RATE="${PUBLISH_RATE:-100}"
DURATION="${DURATION:-60s}"
(cd tools/loadtest && go run . --clients "$CLIENTS" --rooms 1 --connect-rate 3000 --publish-rate "$RATE" --duration "$DURATION" --report ../../reports/m7/hot-room.json)
(cd tools/loadtest && go run . --clients "$CLIENTS" --rooms 100 --connect-rate 3000 --publish-rate "$RATE" --duration "$DURATION" --report ../../reports/m7/distributed-rooms.json)

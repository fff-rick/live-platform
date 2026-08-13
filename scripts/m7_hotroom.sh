#!/usr/bin/env bash
set -euo pipefail
mkdir -p reports/m7
CLIENTS="${CLIENTS:-10000}"
RATE="${PUBLISH_RATE:-100}"
DURATION="${DURATION:-60s}"
CONNECT_RATE="${CONNECT_RATE:-3000}"
CONNECT_CONCURRENCY="${CONNECT_CONCURRENCY:-256}"
PUBLISH_CONCURRENCY="${PUBLISH_CONCURRENCY:-64}"
MESSAGE_BYTES="${MESSAGE_BYTES:-256}"
(cd tools/loadtest && go run . --scenario hot-room --clients "$CLIENTS" --rooms 1 --connect-rate "$CONNECT_RATE" --connect-concurrency "$CONNECT_CONCURRENCY" --publish-rate "$RATE" --publish-concurrency "$PUBLISH_CONCURRENCY" --message-bytes "$MESSAGE_BYTES" --duration "$DURATION" --report ../../reports/m7/hot-room.json)
(cd tools/loadtest && go run . --scenario distributed-100-rooms --clients "$CLIENTS" --rooms 100 --connect-rate "$CONNECT_RATE" --connect-concurrency "$CONNECT_CONCURRENCY" --publish-rate "$RATE" --publish-concurrency "$PUBLISH_CONCURRENCY" --message-bytes "$MESSAGE_BYTES" --duration "$DURATION" --report ../../reports/m7/distributed-rooms.json)

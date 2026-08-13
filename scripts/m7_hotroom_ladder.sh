#!/usr/bin/env bash
set -euo pipefail
mkdir -p reports/m7/hotroom-ladder

DURATION="${DURATION:-60s}"
CONNECT_RATE="${CONNECT_RATE:-3000}"
CONNECT_CONCURRENCY="${CONNECT_CONCURRENCY:-256}"
PUBLISH_CONCURRENCY="${PUBLISH_CONCURRENCY:-64}"
MESSAGE_BYTES="${MESSAGE_BYTES:-256}"
RATE_CLIENTS="${RATE_CLIENTS:-1000}"
PUBLISH_RATES="${PUBLISH_RATES:-10 20 30 40 50}"
SUBSCRIBER_COUNTS="${SUBSCRIBER_COUNTS:-1000 2000 5000}"
FIXED_RATE="${FIXED_RATE:-20}"

for rate in $PUBLISH_RATES; do
  echo "hot-room publish ladder: clients=$RATE_CLIENTS rate=$rate/s"
  (cd tools/loadtest && go run . \
    --scenario "hot-room-rate-${rate}" \
    --clients "$RATE_CLIENTS" --rooms 1 \
    --connect-rate "$CONNECT_RATE" --connect-concurrency "$CONNECT_CONCURRENCY" \
    --publish-rate "$rate" --publish-concurrency "$PUBLISH_CONCURRENCY" \
    --message-bytes "$MESSAGE_BYTES" --duration "$DURATION" \
    --report "../../reports/m7/hotroom-ladder/rate-${rate}.json")
done

for clients in $SUBSCRIBER_COUNTS; do
  echo "hot-room subscriber ladder: clients=$clients rate=$FIXED_RATE/s"
  (cd tools/loadtest && go run . \
    --scenario "hot-room-subscribers-${clients}" \
    --clients "$clients" --rooms 1 \
    --connect-rate "$CONNECT_RATE" --connect-concurrency "$CONNECT_CONCURRENCY" \
    --publish-rate "$FIXED_RATE" --publish-concurrency "$PUBLISH_CONCURRENCY" \
    --message-bytes "$MESSAGE_BYTES" --duration "$DURATION" \
    --report "../../reports/m7/hotroom-ladder/subscribers-${clients}.json")
done

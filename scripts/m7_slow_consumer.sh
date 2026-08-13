#!/usr/bin/env bash
set -euo pipefail
mkdir -p reports/m7
(cd tools/loadtest && go run . \
  --scenario slow-consumer \
  --clients "${CLIENTS:-5000}" --rooms 1 \
  --connect-rate "${CONNECT_RATE:-2000}" --connect-concurrency "${CONNECT_CONCURRENCY:-256}" \
  --publish-rate "${PUBLISH_RATE:-200}" --publish-concurrency "${PUBLISH_CONCURRENCY:-64}" \
  --message-bytes "${MESSAGE_BYTES:-256}" \
  --slow-ratio "${SLOW_RATIO:-0.1}" --slow-delay "${SLOW_DELAY:-1s}" \
  --duration "${DURATION:-60s}" --report ../../reports/m7/slow-consumer.json)

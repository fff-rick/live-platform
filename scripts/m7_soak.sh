#!/usr/bin/env bash
set -euo pipefail
mkdir -p reports/m7
(cd tools/loadtest && go run . \
  --scenario soak \
  --clients "${CLIENTS:-10000}" --rooms "${ROOMS:-100}" \
  --connect-rate "${CONNECT_RATE:-2000}" --connect-concurrency "${CONNECT_CONCURRENCY:-256}" \
  --publish-rate "${PUBLISH_RATE:-20}" --publish-concurrency "${PUBLISH_CONCURRENCY:-64}" \
  --message-bytes "${MESSAGE_BYTES:-256}" \
  --duration "${DURATION:-6h}" --report ../../reports/m7/soak.json)

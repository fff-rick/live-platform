#!/usr/bin/env bash
set -euo pipefail
mkdir -p reports/m7
(cd tools/loadtest && go run . --clients "${CLIENTS:-5000}" --rooms 1 --connect-rate 2000 --publish-rate "${PUBLISH_RATE:-200}" --slow-ratio "${SLOW_RATIO:-0.1}" --slow-delay "${SLOW_DELAY:-1s}" --duration "${DURATION:-60s}" --report ../../reports/m7/slow-consumer.json)

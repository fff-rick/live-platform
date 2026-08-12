#!/usr/bin/env bash
set -euo pipefail
mkdir -p reports/m7
(cd tools/loadtest && go run . --clients "${CLIENTS:-10000}" --rooms "${ROOMS:-100}" --connect-rate 2000 --publish-rate "${PUBLISH_RATE:-20}" --duration "${DURATION:-6h}" --report ../../reports/m7/soak.json)

#!/usr/bin/env bash
set -euo pipefail
mkdir -p reports/m7
CLIENTS_LIST="${CLIENTS_LIST:-10000 50000 100000}"
CONNECT_RATE="${CONNECT_RATE:-5000}"
DURATION="${DURATION:-60s}"
for n in $CLIENTS_LIST; do
  echo "== connection test: $n clients =="
  (cd tools/loadtest && go run . --clients "$n" --rooms 100 --connect-rate "$CONNECT_RATE" --duration "$DURATION" --report "../../reports/m7/connections-${n}.json")
done

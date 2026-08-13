#!/usr/bin/env bash
set -euo pipefail
mkdir -p reports/m7
CLIENTS_LIST="${CLIENTS_LIST:-10000 50000}"
CONNECT_RATE="${CONNECT_RATE:-5000}"
CONNECT_CONCURRENCY="${CONNECT_CONCURRENCY:-512}"
DURATION="${DURATION:-60s}"
for n in $CLIENTS_LIST; do
  echo "== connection test: $n clients target_rate=$CONNECT_RATE/s concurrency=$CONNECT_CONCURRENCY =="
  (cd tools/loadtest && go run . --scenario "connections-${n}" --clients "$n" --rooms 100 --connect-rate "$CONNECT_RATE" --connect-concurrency "$CONNECT_CONCURRENCY" --duration "$DURATION" --report "../../reports/m7/connections-${n}.json")
done

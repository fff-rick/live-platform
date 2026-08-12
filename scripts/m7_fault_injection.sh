#!/usr/bin/env bash
set -euo pipefail
mkdir -p reports/m7
REPORT="reports/m7/failure-recovery.csv"
if [[ ! -f "$REPORT" ]]; then
  echo "service,stopped_at_ms,recovered_at_ms,recovery_ms" > "$REPORT"
fi

now_ms() { date +%s%3N; }

wait_ready() {
  local svc="$1"
  local deadline=$(( $(date +%s) + ${RECOVERY_TIMEOUT_SECONDS:-120} ))
  while (( $(date +%s) < deadline )); do
    case "$svc" in
      centrifugo)
        curl -fsS http://localhost:8000/health >/dev/null 2>&1 && return 0
        ;;
      live-api)
        curl -fsS http://localhost:8080/ready >/dev/null 2>&1 && return 0
        ;;
      live-worker)
        curl -fsS http://localhost:9090/health >/dev/null 2>&1 && return 0
        ;;
      redis)
        docker compose exec -T redis redis-cli ping 2>/dev/null | grep -q PONG && return 0
        ;;
      kafka)
        docker compose exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --list >/dev/null 2>&1 && return 0
        ;;
    esac
    sleep 1
  done
  return 1
}

SERVICES=(centrifugo live-api live-worker redis kafka)
for svc in "${SERVICES[@]}"; do
  echo "== fault injection: $svc =="
  before=$(now_ms)
  docker compose stop "$svc"
  sleep "${DOWN_SECONDS:-5}"
  docker compose start "$svc"
  if ! wait_ready "$svc"; then
    echo "service $svc did not recover within timeout" >&2
    exit 1
  fi
  after=$(now_ms)
  echo "$svc,$before,$after,$((after-before))" >> "$REPORT"
  echo "recovered $svc in $((after-before)) ms (includes configured downtime)"
done

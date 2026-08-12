#!/usr/bin/env bash
set -euo pipefail
STAMP="$(date +%Y%m%d-%H%M%S)"
OUT="${OUT:-reports/m7/snapshot-${STAMP}}"
mkdir -p "$OUT"

echo "collecting M7 infrastructure snapshot into $OUT"
docker stats --no-stream > "$OUT/docker-stats.txt"
docker compose ps > "$OUT/compose-ps.txt"
curl -fsS http://localhost:8080/metrics > "$OUT/live-api.prom" || true
curl -fsS http://localhost:9090/metrics > "$OUT/live-worker.prom" || true
curl -fsS http://localhost:8000/metrics > "$OUT/centrifugo.prom" || true
docker compose exec -T redis redis-cli INFO > "$OUT/redis-info.txt" || true
docker compose exec -T mysql mysql -ulive -plive live -e 'SHOW GLOBAL STATUS; SHOW ENGINE INNODB STATUS\G' > "$OUT/mysql-status.txt" 2>&1 || true
docker compose exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --describe > "$OUT/kafka-topics.txt" 2>&1 || true
printf '%s\n' "$OUT"

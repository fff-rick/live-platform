#!/usr/bin/env bash
set -euo pipefail
STAMP="$(date +%Y%m%d-%H%M%S)"
OUT="${OUT:-reports/m7/snapshot-${STAMP}}"
mkdir -p "$OUT"

echo "collecting M7 infrastructure snapshot into $OUT"
{
  echo "timestamp=$(date -Iseconds)"
  echo "hostname=$(hostname)"
  echo "uname=$(uname -a)"
  echo "ulimit_n=$(ulimit -n)"
  echo "go_version=$(go version 2>/dev/null || true)"
  echo "docker_version=$(docker --version 2>/dev/null || true)"
  echo "docker_compose_version=$(docker compose version 2>/dev/null || true)"
} > "$OUT/environment.txt"
lscpu > "$OUT/lscpu.txt" 2>&1 || true
free -h > "$OUT/memory.txt" 2>&1 || true
cat /proc/sys/net/ipv4/ip_local_port_range > "$OUT/ip-local-port-range.txt" 2>&1 || true
sysctl net.core.somaxconn net.ipv4.tcp_max_syn_backlog > "$OUT/network-sysctl.txt" 2>&1 || true

docker stats --no-stream > "$OUT/docker-stats.txt"
docker compose ps > "$OUT/compose-ps.txt"
curl -fsS http://localhost:8080/metrics > "$OUT/live-api.prom" || true
curl -fsS http://localhost:9090/metrics > "$OUT/live-worker.prom" || true
curl -fsS http://localhost:8000/metrics > "$OUT/centrifugo.prom" || true
docker compose exec -T redis redis-cli INFO > "$OUT/redis-info.txt" || true
docker compose exec -T mysql mysql -ulive -plive live -e 'SHOW GLOBAL STATUS; SHOW ENGINE INNODB STATUS\G' > "$OUT/mysql-status.txt" 2>&1 || true
docker compose exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --describe > "$OUT/kafka-topics.txt" 2>&1 || true
printf '%s\n' "$OUT"

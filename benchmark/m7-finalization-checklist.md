# M7 Finalization Checklist

Run these in order. Do not enter M8 until the first three gates are understood.

## Gate 1 — Kafka danmaku async path

```bash
make m7-kafka-danmaku-smoke
```

Pass criteria:

- HTTP danmaku requests succeed;
- `live_kafka_produce_total{topic="live.danmaku.v1",result="failed"}` remains 0;
- `live_kafka_produce_errors_total` has no non-zero failure reason;
- `danmaku_records` persists at least the number of completed requests.

If this fails, inspect `reports/m7/kafka-danmaku/error-reasons.txt` and live-api logs before running a full hot-room benchmark.

## Gate 2 — Gift database pool A/B

```bash
make m7-gift-dbpool-ab
```

Default matrix:

```text
MaxOpenConns: 20, 40, 80
Target Gift TPS: 500, 1000
Users: 100 wallets
```

For each case inspect:

- achieved TPS and P50/P95/P99;
- `db-pool-samples.csv` peak `in_use`;
- final `live_db_pool_wait_total`;
- final `live_db_pool_wait_duration_seconds_total`;
- InnoDB row-lock delta.

Decision rule:

- If 20 connections show peak InUse≈20 and high WaitCount/WaitDuration, while 40/80 materially improve throughput/tail latency without pushing MySQL into a worse state, the Go DB pool was a limiting layer.
- If larger pools do not improve throughput, stop increasing pool size and profile transaction SQL/commit/storage instead.

## Gate 3 — Full-pipeline hot-room A/B

After Gate 1 passes:

```bash
make m7-hotroom-adaptive-ab
make m7-optimization-report
```

Accept the A/B only if the Kafka produce side-path is healthy in both cases. Keep the baseline/adaptive HTTP, WS and metrics files together.

## Remaining non-blocking benchmark work

- Like ladder: 20K → 50K → 100K logical likes/s.
- Separate load-generator host, then repeat the connection campaign before claiming per-node 10K/50K/100K capacity.

## M7 freeze condition

M7 can be frozen when:

1. Kafka full-path smoke passes;
2. DB pool A/B identifies or rules out pool saturation;
3. final hot-room A/B is captured with Kafka healthy;
4. `benchmark/capacity.md` is updated only with measured results.

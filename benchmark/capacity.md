# M7 容量决策：为 M8 冻结

This file separates **measured facts in the current test environment** from capacity that remains unproven. These numbers are not universal Centrifugo/MySQL limits.

## Realtime / Hot Room

- Centrifugo safe connections/node: **not established**. A separate load-generator host is still required before making a per-node connection-capacity claim.
- Corrected Round 2 baseline: 1,000 subscribers × 30 publish/s stayed around P99 ~45 ms; 1,000 × 40/s rose to ~124 ms.
- Benchmark-derived adaptive target: **25K fan-out deliveries/s**, HOT around 30K/s, PROTECT around 40K/s. All remain configuration, not hard platform constants.
- In the overloaded 5K-listener × 20 msg/s scenario, adaptive sampling reduced the effective realtime fan-out to roughly the target range, reduced WS P99 from ~27.7 s to ~584 ms, and eliminated steady-state reconnects in that run.
- The optimized run still showed initial connection errors, so it is evidence for fan-out protection, not a claim that 5K simultaneous connection establishment is fully solved.

## Kafka danmaku async path

Correctness smoke: **PASS**.

```text
100 HTTP completed
100 Kafka produce success
0 Kafka produce failure
100 MySQL persisted
```

This is a correctness result only. The development topology is one broker with topic replication factor 1, so it is **not** evidence of Kafka HA, replica durability, recovery time, or capacity.

## Gift transaction path

### Per-wallet serialization

A pathological same-wallet workload saturates near ~90 strong-consistency transactions/s. This is a deliberate serialization boundary of one balance row, not total platform capacity. M7 mitigates abusive clicking with per-user request limiting and Gift combo aggregation rather than weakening wallet correctness.

### Database-pool A/B

The measured 100-wallet A/B showed:

| MaxOpen | Target TPS | Actual TPS | P95 | P99 | Avg Go DB connection wait | InnoDB row-lock waits Δ |
|---:|---:|---:|---:|---:|---:|---:|
| 20 | 500 | 458 | 1.970 s | 2.580 s | 226 ms | 2,595 |
| 40 | 500 | 493 | **287 ms** | **456 ms** | **25 ms** | **276** |
| 80 | 500 | 492 | 586 ms | 899 ms | 56 ms | 1,386 |
| 20 | 1000 | 471 | 2.179 s | 2.901 s | 261 ms | 2,756 |
| 40 | 1000 | 518 | 1.976 s | 2.627 s | 225 ms | 5,674 |
| 80 | 1000 | 616 | 1.646 s | 2.124 s | 171 ms | 12,142 |

Decision for the standalone benchmark environment:

- default `MYSQL_MAX_OPEN_CONNS=40`
- default `MYSQL_MAX_IDLE_CONNS=20`
- 80 connections remain an experimental max-throughput setting, not the default

Kubernetes does **not** copy 40 to every Pod. Connection budgets are per-Pod and must satisfy total database connection capacity after HPA scaling.

### 1,000-wallet cardinality isolation — final result

Fixed pool=40, 1,000 funded wallets, 512 HTTP concurrency:

| Target TPS | Actual TPS | P50 | P95 | P99 | Failed | Peak InUse | DB WaitCount | DB Wait sec | Row-lock waits Δ | Row-lock time Δ ms |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 500 | 479.4 | 164.5 ms | 1.136 s | 1.884 s | 0 | 40 | 76,490 | 6,566.586 | 7 | 81 |
| 1000 | 689.7 | 669.2 ms | 1.425 s | 1.903 s | 0 | 40 | 165,358 | 27,785.416 | 23 | 530 |
| 1500 | 717.8 | 644.5 ms | 1.395 s | 1.845 s | 0 | 40 | 172,165 | 28,084.072 | 24 | 550 |

Interpretation:

- Increasing active-wallet cardinality from 100 to 1,000 nearly removed wallet-row contention and raised achieved throughput beyond the prior ~500 TPS plateau. The old plateau was therefore partly **workload-hotspot driven**, not an absolute platform limit.
- Throughput then flattens around **~700–720 TPS observed saturation throughput**: increasing target load from 1,000 to 1,500 TPS raises achieved throughput only from 689.7 to 717.8 TPS.
- All three runs hit `Peak InUse=40` while row-lock deltas stayed tiny, so the dominant pressure after hotspot removal is Go `sql.DB` connection waiting plus transaction/MySQL processing rather than wallet-row locking.
- Saturation throughput is **not** the production-safe capacity. At saturation P99 is ~1.8–1.9 s.
- The last measured low-latency operating point is approximately **200 Gift TPS with P99 ~67 ms** from the earlier 100-wallet test. The exact SLO boundary between 200 and 500 TPS was intentionally not chased further after M7 freeze.

Raw final isolation summary: `benchmark/raw/final/gift-1000-wallet-summary.md`.

## Frozen M7 policy entering M8

- Standalone MySQL pool benchmark default: 40 max open / 20 max idle.
- Kubernetes DB pools: budget per Pod; API base 20/10, event worker base 10/5.
- Danmaku target fan-out: 25K/s.
- HOT: 30K/s.
- PROTECT: 40K/s.
- Adaptive sample floor: 5%.
- Gift per-user request guard: 10 requests/s.
- Gift combo max: 100 per transaction.
- Verified low-latency Gift point: ~200 TPS, P99 ~67 ms.
- Observed Gift saturation in the final isolation environment: ~700–720 TPS, with ~1.8–1.9 s P99.

## M7 status

**FROZEN.** M8 must not silently turn the saturation number into a production SLO claim or continue tuning merely to increase a headline TPS value.

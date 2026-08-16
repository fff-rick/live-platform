# M7 Capacity Decision

This file separates **measured facts in the current test environment** from capacity that is still unproven. None of the numbers below should be presented as universal Centrifugo/MySQL limits.

## Realtime / Hot Room

- Centrifugo safe connections/node: **not established yet**. A separate load-generator host is still required before making a per-node connection-capacity claim.
- Corrected Round 2 baseline: 1,000 subscribers × 30 publish/s stayed around P99 ~45 ms; 1,000 × 40/s rose to ~124 ms.
- Current working fan-out target: **25K deliveries/s**, HOT around 30K/s, PROTECT around 40K/s; all are configurable benchmark-derived thresholds.
- Adaptive protection materially reduced the overloaded 5K-listener case, but the final production-style capacity claim must preserve the exact topology and side-path health used for the measurement.

## Kafka danmaku async path

Correctness smoke: **PASS**.

Measured closure:

```text
100 HTTP completed
100 Kafka produce success
0 Kafka produce failure
100 MySQL persisted
```

This is a correctness result only. The development topology is one broker with topic replication factor 1, so it is **not** evidence of Kafka HA, replica durability, or capacity.

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

Decision for the current environment:

- default `MYSQL_MAX_OPEN_CONNS=40`
- default `MYSQL_MAX_IDLE_CONNS=20`
- 80 connections remain an experimental max-throughput setting, not the default

All effective cases reached their configured pool ceiling. Increasing the pool reduces Go-side connection waiting, but excessive concurrency pushes pressure into InnoDB row-lock contention. The 1,000 TPS target has **not** been achieved.

### Absolute Gift platform capacity

**Not established yet.** The existing high-load matrix used only 100 active wallets, which can itself create repeated balance-row hotspots. The final isolation experiment keeps pool=40 fixed and increases active-wallet cardinality to 1,000:

```bash
make m7-gift-1000-wallet-capacity
```

Targets: 500, 1000 and 1500 TPS. This experiment is the final M7 blocking capacity gate.

## Current default policy

- MySQL pool: 40 max open / 20 max idle
- Danmaku target fan-out: 25K/s
- HOT: 30K/s
- PROTECT: 40K/s
- Adaptive sample floor: 5%
- Gift per-user request guard: 10 requests/s
- Gift combo max: 100 per transaction

## M7 freeze condition

After the 1,000-wallet isolation result:

1. record the measured ceiling and limiting resource without extrapolation;
2. do not keep tuning M7 simply to obtain a larger headline number;
3. move unresolved scale boundaries into M8/production-capacity notes;
4. proceed to M8.

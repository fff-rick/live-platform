# M7 优化轮次发现

This document records measured results from the first Optimization Round. Raw evidence is preserved under `benchmark/raw/optimization-round/`.

## Hot-room adaptive fan-out A/B

Test shape: 5,000 clients, one room, approximately 20 incoming danmaku requests/s.

| Metric | Baseline | Adaptive |
|---|---:|---:|
| HTTP achieved rate | 19.92/s | 20.00/s |
| HTTP P99 | 2.86 s | 37.2 ms |
| Listener fan-out observed | 36.3K/s | 20.7K/s |
| WS P50 | 9.83 s | 80.6 ms |
| WS P95 | 23.37 s | 347 ms |
| WS P99 | 27.73 s | 585 ms |
| Reconnect events | 5,683 | 0 |
| Subscription errors | 1,550 | 0 |

The adaptive run accepted all 1,200 HTTP requests. Traffic-policy metrics recorded 285 realtime broadcasts and 915 sampled events, approximately a 23.75% observed broadcast ratio. This is close to the 25% controller target for a theoretical `5,000 × 20/s = 100K deliveries/s` input.

The optimized listener still recorded 252 immediate connection errors and only 4,748 initial subscriptions, so initial connection ramp remains a separate capacity issue. The improvement above should be interpreted as **steady-state fan-out protection**, not final connection-capacity validation.

## Kafka caveat discovered during A/B

The same runs showed that danmaku Kafka production was mostly failing (baseline: 8 success / 1,187 failed; adaptive: 2 success / 1,198 failed). Therefore the A/B proves realtime fan-out protection but is not yet a final full-pipeline benchmark.

Code review found a lifecycle bug capable of producing this failure pattern: async franz-go `TryProduce` records used the HTTP request context, which is canceled when the handler returns. M7 finalization detaches asynchronous produce cancellation with `context.WithoutCancel` while preserving trace values, and adds `live_kafka_produce_errors_total{topic,reason}`. `make m7-kafka-danmaku-smoke` must pass before the final A/B is accepted.

## Gift platform capacity ladder

100 wallets were used to remove pathological single-wallet serialization from the platform test.

| Target TPS | Achieved TPS | P50 | P95 | P99 |
|---:|---:|---:|---:|---:|
| 200 | 199.8 | 18.9 ms | 42.8 ms | 66.9 ms |
| 500 | 471.3 | 727 ms | 1.80 s | 2.41 s |
| 1,000 | 491.9 | 954 ms | 2.04 s | 2.65 s |
| 2,000 | 478.4 | 980 ms | 2.12 s | 2.75 s |

Throughput plateaus around 480–500 TPS while latency grows, so this is a platform-level saturation knee in the current topology.

InnoDB row-lock deltas were approximately:

| Target TPS | New row-lock waits | New cumulative lock wait |
|---:|---:|---:|
| 200 | 16 | 354 ms |
| 500 | 2,446 | 41.2 s |
| 1,000 | 2,852 | 44.7 s |
| 2,000 | 2,864 | 47.5 s |

Row locking contributes under load, but the average lock wait is far below the observed ~0.7–1.0 second P50 at saturation. This does **not** support blaming wallet-row locks alone. The next experiment measures `database/sql` pool pressure directly.

## Final M7 decision gate

Kafka correctness, DB-pool A/B, and the 1,000-wallet cardinality isolation are complete. M7 is frozen; `benchmark/capacity.md` is now the source of truth for capacity claims carried into M8.

## Finalization update — Kafka correctness and DB-pool A/B

Kafka correctness smoke now passes end-to-end: 100 completed HTTP danmaku requests produced 100 Kafka records, 0 producer failures, and 100 MySQL persisted rows. Treat this strictly as a correctness result; the local single-broker / replication-factor-1 topology does not validate Kafka HA or replica durability.

The corrected Gift DB-pool A/B selected **40 max-open / 20 max-idle** as the current default. Pool 20 suffers excessive Go-side connection waiting; pool 80 can raise extreme-load throughput but pushes materially more contention into MySQL and worsens the 500 TPS latency case. The 1,000 TPS target is still unmet.

The remaining ambiguity is wallet cardinality: the A/B used only 100 active wallets. M7 therefore has one final blocking experiment, `make m7-gift-1000-wallet-capacity`, which keeps pool=40 fixed and tests 1,000 wallets at 500/1,000/1,500 target TPS. After that result, freeze M7 rather than continuing to tune for a larger headline number.

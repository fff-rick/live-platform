# M7 第二轮发现

Round 2 corrected the load-generator issues found in Round 1 and produced two actionable capacity findings. Raw evidence is retained under `benchmark/raw/round2/`.

## Hot-room fan-out

With 1,000 subscribers in one channel, increasing publication rate from 10/s to 50/s produced the following measured P99 latency:

| Publish rate | Approx fan-out/s | P99 |
|---:|---:|---:|
| 10/s | 9,991 | 33.9 ms |
| 20/s | 19,983 | 36.4 ms |
| 30/s | 29,983 | 44.7 ms |
| 40/s | 39,974 | 124.3 ms |
| 50/s | 49,683 | 222.6 ms |

The first clear tail-latency knee is between roughly 30K and 40K deliveries/s in the current test environment. Therefore M7 Optimization uses a configurable target of 25K deliveries/s, HOT threshold of 30K/s and PROTECT threshold of 40K/s. These are benchmark-derived defaults for this environment, not universal Centrifugo limits.

Subscriber-count tests reinforce the result. At 20 publications/s, 1,000 subscribers stayed healthy (P99 ~34 ms), 2,000 subscribers reached P99 ~230 ms, and 5,000 subscribers entered an unstable state with reconnects/client errors and multi-second latency.

## Gift wallet-row contention

At 50 TPS, single-wallet and 100-wallet tests were similar, so wallet-row serialization was not yet the dominant bottleneck.

At target 100 TPS, the scenarios diverged sharply:

| Scenario | Achieved TPS | P99 |
|---|---:|---:|
| Single wallet | 89.3 | 6.62 s |
| 100 wallets | ~100.0 | 87 ms |

During the 100 TPS single-wallet run, InnoDB row-lock waits increased from 506 to 5,919 and cumulative row-lock wait time increased by about 1.15M ms. The corresponding multi-wallet run added only 279 waits and about 10.3K ms of cumulative wait time.

At target 200 TPS, the single wallet remained saturated at ~91 TPS with P99 ~6.60 s, while 100 wallets reached ~199.6 TPS with P99 ~92 ms. This confirms the individual wallet row as a deliberate serialization boundary, not a platform-wide 90 TPS limit.

## 优化决策

1. Keep wallet mutation strongly consistent; do not introduce an unsafe async balance ledger merely to accelerate one pathological account.
2. Add per-user gift request limiting and client-side combo aggregation. A combo is one strong-consistency transaction with `count > 1`.
3. Benchmark platform Gift capacity using many wallets with the abuse limiter raised only for the benchmark.
4. Replace fixed HOT/PROTECT percentages with a configurable adaptive controller based on estimated fan-out (`viewer_count × incoming_rate`).
5. Keep viewer count and incoming rate as independent safety signals because equal average fan-out can still have different burst characteristics.

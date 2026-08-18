# M7 礼物 TPS 压测

Correctness is a hard gate: a higher TPS result is invalid if wallet/order/outbox invariants fail.

## A. Single-wallet hotspot

| Target req/s | Actual req/s | Tokens | Success | Error | API P95 | API P99 | InnoDB row-lock delta | Interpretation |
|---:|---:|---:|---:|---:|---:|---:|---:|---|
| 50 | ~50.0 | 1 | 3,000 | 0 | 25.3 ms | 39.7 ms | not captured | healthy |
| 100 | 89.3 | 1 | 5,358 | 0 | 5.19 s | 6.62 s | +5,413 waits / +1,154,719 ms | saturated |
| 200 | 91.2 | 1 | 5,471 | 1 transport | 5.30 s | 6.60 s | +5,528 waits / +1,161,356 ms | throughput plateau |

The 100 -> 200 target increase barely changes achieved throughput while tail latency remains multi-second. One wallet row is therefore a real serialization boundary under pathological same-account load.

M7 deliberately does **not** weaken wallet consistency for this case. It uses per-user rate limiting plus Gift combo aggregation so repeated clicks can become one bounded strong-consistency transaction.

## B. 100-wallet platform workload

The DB-pool A/B found that `MaxOpenConns=40` is the best current default balance:

| Pool | Target TPS | Actual TPS | P95 | P99 | Avg pool wait | Row-lock waits Δ |
|---:|---:|---:|---:|---:|---:|---:|
| 20 | 500 | 458 | 1.970 s | 2.580 s | 226 ms | 2,595 |
| 40 | 500 | 493 | **287 ms** | **456 ms** | **25 ms** | **276** |
| 80 | 500 | 492 | 586 ms | 899 ms | 56 ms | 1,386 |
| 20 | 1000 | 471 | 2.179 s | 2.901 s | 261 ms | 2,756 |
| 40 | 1000 | 518 | 1.976 s | 2.627 s | 225 ms | 5,674 |
| 80 | 1000 | 616 | 1.646 s | 2.124 s | 171 ms | 12,142 |

Interpretation:

- pool 20 is too restrictive at high load because Go-side connection waiting dominates;
- pool 40 sharply improves the 500 TPS case and is the current recommended default;
- pool 80 can raise extreme-load throughput, but it also pushes substantially more contention into MySQL and is not a free optimization;
- the 1,000 TPS target is not yet achieved.

## C. Final wallet-cardinality isolation

The 100-wallet workload does not prove an absolute platform ceiling because repeated transactions revisit a small wallet-row set. The final M7 experiment changes wallet cardinality only:

```bash
make m7-gift-1000-wallet-capacity
```

Fixed:

```text
MaxOpenConns = 40
MaxIdleConns = 20
Concurrency = 512
```

Variable:

```text
1000 active wallets
500 / 1000 / 1500 target TPS
```

The generated `reports/m7/gift-1000-wallet-capacity/summary.md` includes HTTP latency, peak DB-pool usage, Go pool waiting and InnoDB lock deltas.

If higher wallet cardinality reduces lock pressure and raises achieved TPS, the earlier ~500 TPS plateau was workload-hotspot driven. If throughput remains similar with low lock pressure, profile SQL round trips, COMMIT/storage, MySQL capacity and API CPU instead.

## Invariants

- duplicate charge = 0
- negative balance = 0
- successful gift without successful order = 0
- committed gift without outbox row = 0

## D. Final 1,000-wallet result

The final isolation is complete and M7 is frozen:

| Target TPS | Actual TPS | P95 | P99 | Peak InUse | Row-lock waits Δ |
|---:|---:|---:|---:|---:|---:|
| 500 | 479.4 | 1.136 s | 1.884 s | 40 | 7 |
| 1000 | 689.7 | 1.425 s | 1.903 s | 40 | 23 |
| 1500 | 717.8 | 1.395 s | 1.845 s | 40 | 24 |

Wallet-row contention nearly disappears compared with the 100-wallet workload, while throughput rises beyond the previous ~500 TPS plateau and then flattens around ~700–720 TPS. This proves the old plateau was partly hotspot-driven. The new dominant pressure is DB-pool / transaction processing, not the wallet row. The saturation point is not a low-latency production capacity claim; the last measured low-latency point remains ~200 TPS with P99 ~67 ms.

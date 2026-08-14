# M7 Gift TPS Benchmark

Correctness is a hard gate: a higher TPS result is invalid if wallet/order/outbox invariants fail. Raw Round 2 evidence is retained under `benchmark/raw/round2/gift-*`.

## A. Single-wallet hotspot

| Target req/s | Actual req/s | Tokens | Success | Error | API P95 | API P99 | InnoDB row-lock delta | Interpretation |
|---:|---:|---:|---:|---:|---:|---:|---:|---|
| 50 | ~50.0 | 1 | 3,000 | 0 | 25.3 ms | 39.7 ms | not captured | healthy |
| 100 | 89.3 | 1 | 5,358 | 0 | 5.19 s | 6.62 s | +5,413 waits / +1,154,719 ms | saturated |
| 200 | 91.2 | 1 | 5,471 | 1 transport | 5.30 s | 6.60 s | +5,528 waits / +1,161,356 ms | throughput plateau |

The 100 → 200 target increase barely changes achieved throughput (~89 → ~91 TPS), while tail latency stays multi-second. This confirms one wallet row as a deliberate serialization boundary under pathological same-account load.

## B. Multi-wallet platform throughput

| Target req/s | Actual req/s | Tokens | Success | Error | API P95 | API P99 | InnoDB row-lock delta | Interpretation |
|---:|---:|---:|---:|---:|---:|---:|---:|---|
| 50 | ~50.0 | 100 | 3,000 | 0 | 22.7 ms | 42.9 ms | not captured | healthy |
| 100 | ~100.0 | 100 | 6,000 | 0 | 56.5 ms | 87.1 ms | +279 waits / +10,314 ms | healthy |
| 200 | 199.6 | 100 | 11,978 | 0 | 60.5 ms | 92.2 ms | +126 waits / +4,066 ms | healthy under current <100 ms target |
| 500 | TBD | 100 | TBD | TBD | TBD | TBD | TBD | next Optimization Round test |
| 1000 | TBD | 100 | TBD | TBD | TBD | TBD | TBD | next Optimization Round test |
| 2000 | TBD | 100 | TBD | TBD | TBD | TBD | TBD | next Optimization Round test |

Run `make m7-gift-platform-ladder` to continue the platform-capacity ladder. The script raises the per-user abuse limiter only for this benchmark and restores defaults afterward.

## Optimization decision

Do **not** weaken wallet consistency to accelerate a single pathological account. M7 Optimization adds:

- per-user Gift request limiting;
- browser-side combo aggregation (`count > 1` in one strong-consistency transaction);
- bounded combo size;
- idempotent replay handling even when the user is currently rate-limited.

## Invariants

- duplicate charge = 0
- negative balance = 0
- successful gift without successful order = 0
- committed gift without outbox row = 0

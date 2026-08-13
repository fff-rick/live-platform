# M7 Gift TPS Benchmark

Correctness is a hard gate: a higher TPS result is invalid if wallet/order/outbox invariants fail.

## A. Single-wallet hotspot

| Target req/s | Actual req/s | Tokens | Success | Error | API P95 | API P99 | wallet_update span P99 | InnoDB row-lock delta | Outbox pending |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| TBD | TBD | 1 | TBD | TBD | TBD | TBD | TBD | TBD | TBD |

## B. Multi-wallet platform throughput

| Target req/s | Actual req/s | Tokens | Success | Error | API P95 | API P99 | wallet_update span P99 | InnoDB row-lock delta | Outbox pending |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| TBD | TBD | 100 | TBD | TBD | TBD | TBD | TBD | TBD | TBD |

Run both with the same target rate/concurrency using `make m7-gift-compare`.

If single-wallet tail latency is much worse and the difference concentrates in `gift.db.wallet_update` / InnoDB row-lock wait, wallet-row serialization is confirmed. Do not claim this before the evidence agrees.

## Invariants

- duplicate charge = 0
- negative balance = 0
- successful gift without successful order = 0
- committed gift without outbox row = 0

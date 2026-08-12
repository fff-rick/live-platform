# M7 Gift TPS Benchmark

Correctness is a hard gate: a higher TPS result is invalid if wallet/order/outbox invariants fail.

| Request TPS | Success | Insufficient | Error | API P99 | MySQL CPU | Lock wait | Outbox pending | Kafka lag |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| TBD | TBD | TBD | TBD | TBD | TBD | TBD | TBD | TBD |

## Invariants

- duplicate charge = 0
- negative balance = 0
- successful gift without successful order = 0
- committed gift without outbox row = 0

# M7 Gift Wallet-Cardinality Isolation

## Why this experiment exists

The DB-pool A/B established two measured facts in the current environment:

- `MaxOpenConns=40` is the best default balance observed so far for latency, throughput and MySQL contention;
- raising the pool to 80 can increase extreme-throughput headroom, but also pushes materially more concurrency into InnoDB and is not a sensible default.

The remaining ambiguity is whether the ~500 TPS plateau seen with 100 active wallets is an **absolute Gift platform ceiling** or a **workload hotspot created by repeatedly updating a small set of wallet rows**.

## Controlled experiment

Keep these fixed:

- `MYSQL_MAX_OPEN_CONNS=40`
- `MYSQL_MAX_IDLE_CONNS=20`
- 512 HTTP concurrency
- the same Gift transaction, Outbox and idempotency path
- per-user abuse limiter raised only for the benchmark

Change only active-wallet cardinality to **1,000 wallets** and run:

```text
500 TPS
1000 TPS
1500 TPS
```

Run:

```bash
make m7-gift-1000-wallet-capacity
```

## Evidence captured per case

- target and achieved TPS;
- P50/P95/P99 and failures;
- peak Go `sql.DB` InUse / MaxOpen;
- DB WaitCount and WaitDuration;
- InnoDB row-lock wait/time deltas.

The script produces one compact comparison file:

```text
reports/m7/gift-1000-wallet-capacity/summary.md
```

## Decision rule

If 1,000 wallets materially reduce row-lock waits and allow throughput to move beyond the previous ~500 TPS plateau, the old plateau was workload-hotspot driven and must **not** be documented as absolute platform capacity.

If throughput remains near the old plateau while row-lock pressure is low, the next limiting layer is likely elsewhere in the transaction path (SQL round trips, COMMIT/storage, MySQL capacity, API CPU, or another shared resource). Stop tuning wallet cardinality and profile that layer instead.

## M7 freeze rule

Do not add more Gift architecture changes after this experiment. Freeze the measured result into `benchmark/capacity.md`, document the limiting layer honestly, and move to M8.

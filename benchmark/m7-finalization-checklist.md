# M7 Finalization Checklist

The Kafka correctness and DB-pool A/B gates have been completed. The final blocking experiment is wallet-cardinality isolation.

## Completed — Kafka danmaku correctness

Measured smoke result: 100 HTTP danmaku requests -> 100 Kafka produces -> 0 produce failures -> 100 MySQL persisted records. This proves the repaired asynchronous path closes correctly. It is a correctness smoke, **not** a Kafka capacity or HA claim; the local single-broker/RF=1 topology does not demonstrate replica-level durability.

## Completed — Gift DB pool A/B

Current environment decision:

- default `MYSQL_MAX_OPEN_CONNS=40`
- default `MYSQL_MAX_IDLE_CONNS=20`
- pool 80 remains benchmark-only when chasing maximum throughput

Pool saturation was measurable through `sql.DB.Stats()`; increasing the pool reduced Go-side waiting but eventually pushed more contention into MySQL. Do not increase MaxOpenConns blindly.

## Final blocking gate — 1,000-wallet platform isolation

```bash
make m7-gift-1000-wallet-capacity
```

Default matrix:

```text
Wallets: 1000
MaxOpenConns: 40
Target Gift TPS: 500, 1000, 1500
Concurrency: 512
Duration: 60s/case
```

Inspect the generated:

```text
reports/m7/gift-1000-wallet-capacity/summary.md
```

Decision rule:

- materially lower row-lock pressure + materially higher TPS => prior ~500 TPS plateau was a 100-wallet hotspot artifact;
- similar TPS with low row-lock pressure => platform bottleneck is elsewhere in the transaction/DB path.

## M7 freeze condition

After this one experiment:

1. write the measured result into `benchmark/capacity.md`;
2. do not invent unmeasured 1K/1.5K TPS claims;
3. stop M7 optimization even if the result is lower than desired;
4. enter M8 with the bottleneck documented as a known capacity boundary.

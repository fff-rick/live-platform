# M7 收尾检查清单

All M7 finalization gates are complete. Kafka correctness, DB-pool A/B, and wallet-cardinality isolation have been measured and frozen.

## 已完成：Kafka 弹幕正确性

Measured smoke result: 100 HTTP danmaku requests -> 100 Kafka produces -> 0 produce failures -> 100 MySQL persisted records. This proves the repaired asynchronous path closes correctly. It is a correctness smoke, **not** a Kafka capacity or HA claim; the local single-broker/RF=1 topology does not demonstrate replica-level durability.

## 已完成：礼物数据库连接池 A/B

Current environment decision:

- default `MYSQL_MAX_OPEN_CONNS=40`
- default `MYSQL_MAX_IDLE_CONNS=20`
- pool 80 remains benchmark-only when chasing maximum throughput

Pool saturation was measurable through `sql.DB.Stats()`; increasing the pool reduced Go-side waiting but eventually pushed more contention into MySQL. Do not increase MaxOpenConns blindly.

## 已完成：1,000 钱包平台隔离

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

Measured result:

- 500 target -> 479.4 actual TPS, P99 1.884 s, row-lock delta 7;
- 1000 target -> 689.7 actual TPS, P99 1.903 s, row-lock delta 23;
- 1500 target -> 717.8 actual TPS, P99 1.845 s, row-lock delta 24;
- Peak InUse reached 40 in every case.

Higher wallet cardinality removed the earlier row-lock hotspot and raised saturation throughput to roughly 700–720 TPS, after which DB pool / transaction processing became the dominant pressure.

## M7 freeze status

**COMPLETE.** `benchmark/capacity.md` is the source of truth for measured capacity entering M8.

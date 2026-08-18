# M7 第二轮检查清单

Round 2 目的：验证第一轮发现的两个问题——Hot Channel fan-out 与 Gift 尾延迟——并确认压测器不再成为主要瓶颈。

## 0. 环境快照

```bash
make m7-snapshot
```

保存 `reports/m7/snapshot-*/`，尤其是：

- `environment.txt`
- `lscpu.txt`
- `memory.txt`
- `ip-local-port-range.txt`
- `docker-stats.txt`

## 1. Gift: single wallet vs multi wallet

```bash
RATE=50 CONCURRENCY=128 USERS=100 DURATION=60s make m7-gift-compare
```

提交：

```text
reports/m7/gift-single-wallet.json
reports/m7/gift-multi-wallet.json
reports/m7/gift-*-lock-before.txt
reports/m7/gift-*-lock-after.txt
reports/m7/snapshot-gift-*/mysql-status.txt
```

同时在 Grafana / Tempo 检查：

```text
gift.transaction
gift.db.order_insert
gift.db.wallet_update
gift.db.outbox_insert
gift.db.commit
```

如果 single-wallet 的 P99 和 `wallet_update` 明显高于 multi-wallet，并且 InnoDB row-lock wait delta 同方向增长，才确认 wallet row serialization。

## 2. Hot Room ladders

```bash
make m7-hotroom-ladder
```

生成：

```text
reports/m7/hotroom-ladder/rate-10.json
...
rate-50.json
subscribers-1000.json
subscribers-2000.json
subscribers-5000.json
```

汇总：

```bash
python3 scripts/m7_report_table.py reports/m7/hotroom-ladder/*.json
```

重点看：

- `publish_rate_actual_per_sec` 是否跟上 target；
- `fanout_delivery_actual_per_sec`；
- P95 / P99；
- Centrifugo CPU / egress；
- disconnect / reconnect。

## 3. Like ladder

准备一个已开播 Room 与 Token：

```bash
ROOM_ID=... TOKEN=... make m7-like-ladder
```

对应：

```text
20K logical likes/s
50K logical likes/s
100K logical likes/s
```

重点同时记录 stats broadcast rate，验证聚合放大比。

## 4. Slow consumer re-test

```bash
CLIENTS=1000 PUBLISH_RATE=50 SLOW_RATIO=0.1 SLOW_DELAY=1s make m7-slow-consumer
```

必须使用新版字段：

```text
connected_current
reconnect_events
fast_disconnect_events
slow_disconnect_events
fast_latency
slow_latency
```

`connected_current` 不应再超过配置的 client 数。

## 5. Connection sweep

```bash
CLIENTS_LIST="10000 50000" CONNECT_RATE=5000 CONNECT_CONCURRENCY=512 make m7-connection-sweep
```

先检查 Load Generator：

- CPU 是否耗尽；
- `ulimit -n`；
- ephemeral ports；
- `connect_rate_actual_per_sec` 是否接近 target。

只有压测机仍有明显余量，才进入 100K。

## 第二轮已完成项

完成后应能回答：

1. Hot Room 的 P99 拐点发生在哪个 fan-out delivery rate？
2. 单钱包 Gift P99 是否来自 `wallet_update` 行锁等待？
3. 多钱包情况下平台 Gift TPS 和 P99 是多少？
4. Like 在 100K logical likes/s 下是否仍保持低延迟？
5. Slow Consumer 是否会影响正常消费者？
6. 连接建立速率的瓶颈在 Load Generator 还是 Centrifugo？

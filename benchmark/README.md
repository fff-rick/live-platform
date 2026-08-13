# M7 Benchmark Reports

这里仅保存**真实压测结果**。仓库不预填性能数字。

## Round 1

原始报告：

```text
benchmark/raw/round1/
```

分析：

```text
benchmark/round1-findings.md
```

Round 1 已经发现：

- 单热点 Channel fan-out 是首个明确实时链路压力点；
- 旧 Load Generator 的连接和 publish 驱动存在串行瓶颈，必须重测实际速率；
- Slow Consumer 旧版 `connected_current` 会被 reconnect 重复累计；
- Gift 首轮其实是“单钱包热点”而不是平台 Gift TPS。

因此旧数据只保留为 baseline，不直接填写最终 Capacity。

## Round 2 推荐顺序

1. `make m7-gift-compare`
2. `make m7-hotroom-ladder`
3. `ROOM_ID=... TOKEN=... make m7-like-ladder`
4. `make m7-slow-consumer`
5. `make m7-connection-sweep`
6. `DURATION=6h make m7-soak`
7. `make m7-snapshot`
8. `make m7-fault`

每次报告至少记录机器规格、Git Commit、节点数量、连接数、target/actual rate、消息大小、P50/P95/P99、CPU、内存、网络、Redis、Kafka 和 MySQL 指标。

新版 WebSocket / HTTP Load Generator 会自动写入 Load Generator hostname、CPU 数、Go 版本以及 target/actual rate；`m7_snapshot.sh` 额外保存 CPU、内存、ulimit、ephemeral port range 和 Docker 环境。

## 容量结论模板

- 最大观测连接数：`TBD`
- 安全连接容量（CPU/内存/网络均保留约 30% 余量）：`TBD`
- 单热点房间安全广播速率：`TBD`
- 100 房间均匀分布安全广播速率：`TBD`
- Like 聚合前/后广播放大比：`TBD`
- Gift 安全 TPS：`TBD`
- Kafka backlog 恢复速率：`TBD`
- 故障恢复时间：`TBD`

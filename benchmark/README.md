# M7 Benchmark Reports

这里仅保存**真实压测结果**。仓库不预填性能数字。

建议依次执行：

1. `make m7-degradation-smoke`
2. `make m7-connection-sweep`
3. `make m7-hotroom`
4. `make m7-slow-consumer`
5. `ROOM_ID=... TOKEN=... make m7-like-storm`
6. `ROOM_ID=... TOKEN=... GIFT_ID=... make m7-gift-load`
7. `DURATION=6h make m7-soak`
8. `make m7-snapshot`（在关键压测阶段留存原始基础设施数据）
9. `make m7-fault`

每次报告至少记录机器规格、Git Commit、节点数量、连接数、消息速率、消息大小、P50/P95/P99、CPU、内存、网络、Redis、Kafka 和 MySQL 指标。

## 容量结论模板

- 最大观测连接数：`TBD`
- 安全连接容量（CPU/内存/网络均保留约 30% 余量）：`TBD`
- 单热点房间安全广播速率：`TBD`
- 100 房间均匀分布安全广播速率：`TBD`
- Like 聚合前/后广播放大比：`TBD`
- Gift 安全 TPS：`TBD`
- Kafka backlog 恢复速率：`TBD`
- 故障恢复时间：`TBD`

# Kafka 事件目录 v1

本目录记录当前代码真正生产和消费的事件。Outbox Publisher 只负责传输，不是业务生产者。

| Topic | Event type | 业务所有者 / 生产者 | Consumer / group | Partition key | 当前保留期 | DLQ |
| --- | --- | --- | --- | --- | --- | --- |
| `live.danmaku.v1` | `danmaku.sent` | Interaction；`internal/danmaku.KafkaProducer` | Danmaku archive / `live-danmaku-persist-v1` | `room_id` 十进制字符串 | 7 天（Compose `retention.ms=604800000`） | `live.danmaku.v1.dlq`，30 天 |
| `live.gift.v1` | `gift.sent` | Commerce；`internal/gift.Repository` 事务 Outbox | Realtime gift / `live-gift-realtime-v1` | `room_id` 十进制字符串 | 7 天（Compose `retention.ms=604800000`） | `live.gift.v1.dlq`，30 天 |

## 所有权和兼容规则

- `danmaku.sent` 的业务生产点只能在 Interaction；`gift.sent` 只能在 Commerce 的送礼本地事务中生成。
- 消费者必须以 `event_id` 幂等；同一房间的顺序仅由 `room_id` partition key 保证。
- 源 Topic 只有在演练环境明确创建时才能声称上表保留期；Kubernetes 环境若仍使用 Broker 默认值，发布前必须由平台配置对齐。
- DLQ 值使用 `internal/mq.DeadLetter`，保留源 topic、partition、offset、key、value、consumer group、错误和失败时间。

## DLQ 导出与重放

```bash
# 先导出需要人工确认的 DLQ 记录（示例）
docker compose exec -T kafka /opt/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server kafka:19092 --topic live.gift.v1.dlq --from-beginning \
  > /tmp/live-gift-dlq.jsonl

# 修复消费者或数据后，在本地显式记录操作人并重放
go run ./cmd/dlq-replay -brokers localhost:19092 \
  -input /tmp/live-gift-dlq.jsonl -actor "$USER"
```

重放命令不修改原 `event_id`、key 或 payload，因此必须依赖消费者幂等。命令每成功重放一条，向标准输出写入包含 actor、原 offset 和时间的 JSON 审计记录。

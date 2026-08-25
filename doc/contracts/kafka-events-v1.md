# Kafka 事件 v1 契约

## 通用 envelope

```json
{
  "event_id": "01J...",
  "event_type": "gift.sent",
  "room_id": 10001,
  "created_at": "2026-08-24T12:00:00.000Z",
  "trace": { "traceparent": "00-..." },
  "payload": {}
}
```

必填字段为 `event_id`、`event_type`、`room_id`、`created_at`、`payload`。`trace` 可选，只用于上下文传播。消息 key 是 Kafka 分区键，未编码在 envelope 中。

## 已发布事件

| Topic（默认） | `event_type` | 生产者 | 消费者 | key | 语义 |
| --- | --- | --- | --- | --- |
| `live.danmaku.v1` | `danmaku.sent` | API / 后续互动接入服务 | 弹幕归档 | `room_id` | 弹幕接受后最佳努力归档；实时广播不依赖此消费。 |
| `live.gift.v1` | `gift.sent` | 钱包-礼物事务 Outbox | 礼物投递 | `room_id` | 已提交礼物订单的异步实时投递。 |

`gift.sent` payload：

```json
{
  "order_no": "GO...",
  "user_id": 10086,
  "anchor_id": 10001,
  "gift_id": 1,
  "gift_name": "Rose",
  "count": 1,
  "unit_price": 100,
  "total_amount": 100
}
```

`danmaku.sent` payload 与 HTTP 接受后的弹幕事件一致，至少包含 `message_id`、`room_id`、`user_id`、`nickname`、`content`、`created_at`。

## 消费与故障规则

- Kafka 仅提供至少一次处理预期；消费者以 `(consumer_group, event_id)` 去重。
- 事件 payload 无法解析或违反契约时，消费者将其视为永久失败并记录告警；不得无限重试毒消息。
- 消费者的临时处理失败将重试，成功处理后才提交 offset。
- `gift.sent` 由事务 Outbox 发布。Kafka 不可用时，已提交的礼物订单仍正确存在，等待 Publisher 重试。
- 当前 `danmaku.sent` 为直接异步生产，Kafka 不可用时允许归档缺失但不得影响实时弹幕成功。迁移到需要可靠归档的业务要求时，必须先改为事务 Outbox。

## 演进方式

本版本不修改既有 `event_type` 字符串。未来的破坏性变更使用新的 event type、Topic 或两者，并至少经历：双发/双消费、数据比对、切流、保留窗口、下线旧版本。Schema Registry 只在跨仓库或多语言消费者出现且人工契约审查不再可承受时评估引入。

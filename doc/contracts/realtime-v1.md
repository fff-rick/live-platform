# Centrifugo 实时消息 v1 契约

## Channel

| Channel | 用途 | 订阅限制 |
| --- | --- | --- |
| `room:{room_id}:stream` | 弹幕、礼物及房间流事件 | 已进房用户的订阅 token |
| `room:{room_id}:stats` | 在线人数、点赞等最新状态 | 已进房用户的订阅 token |
| `user:{user_id}` / `personal:*` | 用户私有通知 | 仅对应用户 |

`room` namespace 当前启用短期 history 和 `force_recovery`。history 是重连优化，不是账务或消息归档的事实来源；恢复失败时客户端通过 HTTP 查询房间状态与历史消息。

## Event envelope

```json
{
  "event_id": "01J...",
  "type": "danmaku",
  "room_id": 10001,
  "priority": "P3",
  "timestamp": 1786300000123,
  "data": {}
}
```

| 字段 | 规则 |
| --- | --- |
| `event_id` | 非空，客户端短窗口去重键；不是数据库主键。 |
| `type` | 当前包括 `danmaku`、`gift`、房间统计类事件。新增类型不得改变既有类型数据结构。 |
| `room_id` | 正整数。 |
| `priority` | 可选；当前礼物为 `P1`、弹幕为 `P3`，仅供传输/展示策略，不能影响账务。 |
| `timestamp` | UTC Unix 毫秒。 |
| `data` | 随 `type` 变化的 JSON 对象。 |

## 传输语义

- 事件可以重复、乱序或因热点保护被采样；客户端必须能够处理。
- 弹幕 HTTP 返回 `broadcasted=false` 时，服务端不会向 `stream` 广播；这属于既有热点房间降级契约。
- 礼物实时事件仅代表已成功提交的订单事件；客户端余额和订单最终以 HTTP 查询为准。
- 不在实时事件中传递 JWT、密码、余额扣款凭据等敏感信息。

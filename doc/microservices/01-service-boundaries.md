# 服务边界与目标架构

## 1. 目标调用图

```text
浏览器
  ├─ HTTP ───────────────────────────────┐
  └─ WebSocket ── Centrifugo             │
                                         ▼
                                  live-gateway
          ┌──────────────┬───────────────┼──────────────┬──────────────┐
          ▼              ▼               ▼              ▼              ▼
   live-identity    live-room      live-presence  live-interaction live-commerce
          │              │               │              │              │
          │              └──── Outbox / Kafka events ───┼──────────────┘
          │                                              │
          └────────── Outbox / Kafka events ─────────────┤
                                                         ▼
                                                    live-query

独立 Worker：
  identity/room/commerce outbox publisher
  room/access projection consumers
  danmaku archive consumer
  gift delivery consumer
  stats/read-model projector
  like snapshot worker
```

同步请求只用于必须立即给出结果的命令或查询；跨域状态传播默认使用版本化事件和本地投影。

## 2. 路由所有权

客户端继续使用现有 `/api/v1` 契约，Gateway 只改变后端路由，不要求前端同步改版。

| 外部路由 | 目标所有者 | 说明 |
| --- | --- | --- |
| `POST /auth/register`、`POST /auth/login`、`GET /me` | `live-identity` | 用户与认证唯一入口 |
| `POST /realtime/token` | `live-identity` | 生成用户级 Centrifugo 连接令牌 |
| `POST /rooms`、`POST /rooms/{id}/start|stop` | `live-room` | 房间生命周期命令 |
| `POST/GET/DELETE /rooms/{id}/mutes|bans` | `live-room` | 房主治理命令和控制面查询 |
| `GET /rooms`、`GET /rooms/{id}` | `live-query` | 从房间与用户投影读取，不联查源库 |
| `POST /rooms/{id}/join|leave|heartbeat` | `live-presence` | 在线状态与订阅授权 |
| `GET /rooms/{id}/viewers|top-viewers` | `live-presence` | Presence Redis + 用户展示投影 |
| `POST /rooms/{id}/danmaku|like` | `live-interaction` | 高频互动写入 |
| `GET /rooms/{id}/stats` | `live-query` | Presence/Interaction 聚合读模型 |
| `GET /rooms/{id}/messages` | `live-query` | 弹幕与礼物统一消息流读模型 |
| `GET /gifts`、`GET /wallet*`、`POST /wallet/dev-credit` | `live-commerce` | 礼物目录与钱包 |
| `POST /rooms/{id}/gifts`、`GET /gift-orders/{no}` | `live-commerce` | 强一致交易命令与订单查询 |

## 3. `live-gateway`

### 职责

- 提供内嵌 Web UI 和唯一公网 HTTP 入口。
- 校验 JWT 基本合法性，生成/透传 request ID、`traceparent` 和客户端 IP 信息。
- 按固定路由表转发请求；只允许少量无业务状态的响应聚合。
- 执行请求体大小、连接数和公网级限流。
- 统一映射超时、上游不可用和熔断错误，不重写业务错误码。

### 允许依赖

- Identity、Room、Presence、Interaction、Commerce、Query 的内部 HTTP Service。
- OpenTelemetry、Prometheus 和结构化日志。

Gateway 验证用户 JWT 后，不得用可伪造的明文 `X-User-ID` 作为下游信任根。过渡期向下游传递原始 JWT；引入内部身份上下文时，必须签名并包含 audience、过期时间和 request ID，下游同时校验服务身份。

### 禁止事项

- 不接收 `MYSQL_DSN`、`REDIS_*`、`KAFKA_*` 或 Centrifugo API Key。
- 不导入 `auth.Repository`、`room.Repository`、`gift.Repository`、`wallet.Repository`。
- 不直接查询消息历史、在线列表、统计或任何业务表。
- 不在上游超时时自动重放非幂等 POST。

### 故障行为

- 单个上游失败只影响它拥有的路由；Gateway readiness 不因一个业务服务失败而摘除自身。
- 上游连接失败返回稳定的 `503 + UPSTREAM_UNAVAILABLE`；超时返回 `504 + UPSTREAM_TIMEOUT`。
- Gateway 只对显式幂等 GET 和带幂等键的受控请求使用有限重试。

## 4. `live-identity`

### 职责

- 用户注册、密码哈希、登录、用户状态和展示资料。
- 签发应用 JWT 与 Centrifugo 用户连接 JWT。
- 发布用户注册、展示资料和状态变更事件。

### 数据所有权

| 类型 | 所有内容 |
| --- | --- |
| MySQL schema | `identity` |
| 表 | `users`、`identity_outbox_events` |
| Kafka 生产 | `live.identity.v1` |
| Kafka 事件 | `identity.user.registered.v1`、`identity.user.profile-updated.v1`、`identity.user.status-changed.v1` |

JWT 初期可继续使用共享 HMAC secret；完成边界收口后改为 Identity 持有私钥、其他服务只持有公钥/JWKS，避免所有服务拥有签发能力。

### 内部接口

- `GET /internal/v1/users/{user_id}`：低频兜底查询。
- `POST /internal/v1/users:batch-get`：Room 控制面批量校验目标用户，禁止 N+1。
- `GET /internal/v1/users/{user_id}/status`：只返回授权所需最小字段。

### 禁止事项

- 不读写房间、钱包、礼物、互动或在线状态。
- 用户事件不得携带密码哈希、JWT、钱包余额或私密资料。

## 5. `live-room`

### 职责

- 创建房间、开播、停播和房主授权。
- 禁言、解禁、封禁和解封。
- 在本地事务内写入房间/治理状态与 Outbox。
- 为低频控制面提供权威访问校验接口。

### 数据所有权

| 类型 | 所有内容 |
| --- | --- |
| MySQL schema | `room` |
| 表 | `live_rooms`、`room_mutes`、`room_bans`、`room_outbox_events` |
| 本地投影 | `room_user_projection`，只保存 `user_id`、展示名和状态 |
| Kafka 生产 | `live.room.v1`、`live.governance.v1` |
| 房间事件 | `room.created.v1`、`room.status-changed.v1` |
| 治理事件 | `room.user-muted.v1`、`room.user-unmuted.v1`、`room.user-banned.v1`、`room.user-unbanned.v1` |

当前 `room.Repository` 对 `users` 的 JOIN 必须移除；房主昵称来自本地用户投影或 Query 层聚合。

### 内部接口

- `GET /internal/v1/rooms/{room_id}/access/{user_id}`：权威兜底，返回房间状态、房主、禁言、封禁和数据版本。
- `POST /internal/v1/rooms:batch-get`：Query 回填或修复使用。

### 一致性规则

- 房间状态和治理事件必须与源表更新在同一事务写入 Outbox。
- 同一 `room_id` 的事件以 `room_id` 作为 Kafka key，保留分区内顺序。
- 治理事件包含递增 `version`；消费者忽略旧版本，防止乱序撤销最新封禁状态。

## 6. `live-presence`

### 职责

- 进房、离房、心跳与在线 TTL。
- 生成房间订阅令牌和个人频道信息。
- 在线观众列表、入场顺序与礼物 Top 3。
- 消费 Gift 事件更新榜单，消费 Room/Governance/User 事件维护本地投影。

### Redis 所有权

统一迁移到 Presence namespace，避免多个服务写同一 Key：

| Key 前缀 | 用途 |
| --- | --- |
| `live:presence:room:{room_id}:viewers` | 在线观众及心跳时间 |
| `live:presence:room:{room_id}:join-seq` | 同分时的入场顺序 |
| `live:presence:room:{room_id}:gift-score` | 在线观众礼物累计值 |
| `live:presence:room:{room_id}:last-published-count` | 上次广播在线数 |
| `live:presence:active-rooms:{shard}` | 活跃房间索引 |
| `live:presence:projection:room:{room_id}` | 房间状态投影 |
| `live:presence:projection:access:{room_id}:{user_id}` | 治理投影 |
| `live:presence:projection:user:{user_id}` | 展示资料投影 |

`live_sessions` 若只用于在线判断应删除；若需要审计，则迁入 `presence.live_sessions`，由异步 Session Worker 写入，不进入心跳同步路径。

### Kafka

- 消费：`live.identity.v1`、`live.room.v1`、`live.governance.v1`、现有 `live.gift.v1`。
- 生产：`live.presence.v1`，只发布聚合后的 `presence.viewer-count-changed.v1`，不为每次心跳制造 Kafka 消息。

### 失败策略

- 房间/治理投影未命中时，最多调用一次 Room 内部 API；800ms 内失败则默认拒绝进房和续约，不能默认放行。
- 投影超过 `ACCESS_PROJECTION_MAX_STALENESS` 后，新增进房默认拒绝；已有连接允许到当前 TTL 到期，不无限续期。
- 重复 join/heartbeat 必须幂等，不得重复增加在线人数或重置初次入场顺序。
- 礼物 Top 3 是最终一致读模型，`gift-score` 只能由 Presence 的 Gift Consumer 写入；切流前必须定义并验证事件到榜单的 P99 延迟预算。

## 7. `live-interaction`

### 职责

- 弹幕输入验证、敏感词、用户限流和热点房间自适应采样。
- 点赞聚合、用户点赞限流和点赞快照事件。
- 发布 Centrifugo 弹幕和统计事件。
- 消费 Room/Governance/User 事件形成自己的访问投影。

### Redis 所有权

| Key 前缀 | 用途 |
| --- | --- |
| `live:interaction:room:{room_id}:danmaku:rolling` | 弹幕速率滑窗 |
| `live:interaction:limit:danmaku:user:{user_id}` | 用户弹幕限流 |
| `live:interaction:room:{room_id}:like:total|delta` | 点赞总量与增量 |
| `live:interaction:room:{room_id}:like:rate:{user_id}` | 用户点赞限流 |
| `live:interaction:active-rooms:{shard}` | 点赞活跃房间 |
| `live:interaction:projection:*` | 本服务房间、治理、用户展示投影 |

### Kafka 与持久化语义

- 继续兼容 `live.danmaku.v1 / danmaku.sent`，当前语义为“实时优先、归档尽力而为”。
- HTTP 200 只保证弹幕通过业务校验并完成约定的实时发布，不保证 Kafka 归档成功。
- 如果产品未来要求“成功必可回放”，再为弹幕增加本地持久化 Outbox；不能悄悄改变现有成功语义。
- 点赞不逐次写 Kafka；按房间聚合后发布 `interaction.like-snapshot-updated.v1`。

### 禁止事项

- 不访问 Identity/Room/Commerce MySQL。
- 不写 Presence 在线 Key。
- 不同步调用 Identity 获取每条弹幕昵称；昵称来自本地用户展示投影。

## 8. `live-commerce`

### 职责

- 礼物目录、钱包余额、充值、钱包流水、礼物订单和幂等。
- 在一个 MySQL 本地事务内完成订单、扣款、流水和 Outbox。
- 消费 Room/Governance/User 事件维护最小授权投影。

### 数据所有权

| 类型 | 所有内容 |
| --- | --- |
| MySQL schema | `commerce` |
| 表 | `gifts`、`wallets`、`wallet_transactions`、`gift_orders`、`commerce_outbox_events` |
| 投影表或缓存 | `commerce_room_projection`、`commerce_user_projection`、`commerce_access_projection` |
| Kafka 生产 | 保持 `live.gift.v1 / gift.sent` 兼容；后续新事件使用 `commerce.gift-order.completed.v1` |

### 不变量

- `wallet.balance >= 0`。
- 一个 `Idempotency-Key` 最多对应一个参数完全一致的订单。
- 成功订单必有一条钱包流水和一条 Outbox 事件。
- 房间或治理投影过期时，首次请求可调用 Room 权威 API；调用失败默认拒绝新交易。
- 已成功订单的幂等重放不再依赖当前房间状态，必须返回原结果。

### 禁止事项

- 不创建 `room.Repository`，不查询 `live_rooms`、`room_bans` 或 `users`。
- Gift Consumer 不得回写钱包或订单；它只负责幂等实时投递。

## 9. `live-query`

### 职责

- 提供高频只读接口：房间列表、详情、历史消息和聚合统计。
- 消费 Identity、Room、Presence、Interaction 和 Commerce 事件形成反规范化读模型。
- 允许最终一致，但必须暴露投影版本和滞后指标。

### 数据所有权

| 类型 | 所有内容 |
| --- | --- |
| MySQL schema | `query` |
| 表 | `room_cards`、`room_message_feed`、`user_display_projection`、`query_processed_events` |
| Redis | `live:query:room:{room_id}:stats`、房间列表缓存 |

`room_message_feed.message_id` 必须唯一：弹幕使用现有 `message_id`，礼物使用 `gift:{order_no}`。重复、乱序事件不得生成重复消息。

### 读一致性

- 房间控制命令成功后，详情投影目标延迟不超过 2 秒。
- 禁言/封禁是授权数据，不由 Query 提供权威判断。
- 查询响应可携带 `projection_updated_at`，便于前端和排障识别陈旧读模型。

## 10. Worker 部署边界

| Deployment | 数据权限 | 输入 | 输出 |
| --- | --- | --- | --- |
| `identity-outbox-publisher` | Identity Outbox 读写 | Identity Outbox | `live.identity.v1` |
| `room-outbox-publisher` | Room Outbox 读写 | Room Outbox | Room/Governance Topic |
| `commerce-outbox-publisher` | Commerce Outbox 读写 | Commerce Outbox | `live.gift.v1` |
| `access-projector-*` | 对应服务投影 namespace | Identity/Room/Governance | 本地 Redis/投影表 |
| `danmaku-archive-worker` | Archive schema | `live.danmaku.v1` | `danmaku_records` |
| `gift-delivery-worker` | 自己的去重表 + Centrifugo | `live.gift.v1` | WebSocket 礼物事件 |
| `query-projector` | Query schema/Redis | 全部读模型事件 | 房间、消息、统计投影 |
| `like-snapshot-worker` | Interaction snapshot 表 | Interaction Redis | `room_like_snapshots` |

每个 Deployment 使用独立 consumer group、指标、资源配额和最小权限账号。通用二进制可以复用，但不能给一个进程配置所有 schema 的凭据。

## 11. 事件统一格式

在兼容现有字段的前提下，新增事件统一使用：

```json
{
  "event_id": "01J...",
  "event_type": "room.status-changed.v1",
  "aggregate_type": "room",
  "aggregate_id": "1024",
  "aggregate_version": 7,
  "partition_key": "1024",
  "occurred_at": "2026-10-14T10:00:00Z",
  "traceparent": "00-...",
  "data": {}
}
```

规则：

- `event_id` 用于消费幂等；`aggregate_version` 用于拒绝乱序旧事件。
- 新增字段必须可选；破坏性变更创建新 `event_type` 或新 Topic。
- 权限事件必须通过事务 Outbox，不能先改表再异步拼事件。
- 永久坏消息写入 `<source-topic>.dlq.v1`，保留原 Topic、partition、offset、payload、错误和首次失败时间。

## 12. 数据库与网络边界

第一阶段仍可使用一个 MySQL 实例，但必须创建独立 schema 和账号：

| 账号 | 允许权限 |
| --- | --- |
| `live_identity_app` | `identity.*` |
| `live_room_app` | `room.*` |
| `live_commerce_app` | `commerce.*` |
| `live_archive_app` | `archive.*`，仅归档和消费幂等 Worker 使用 |
| `live_query_app` | `query.*`，仅投影写入 Worker 使用 |
| `live_query_audit` | 只读 `query.*`，由 `live-query` 使用 |

运行时禁止使用 `GRANT ALL ON live.*`。迁移账号单独保管，只在 Job 中使用。

网络默认拒绝，只放行：

- Gateway → 各 HTTP 服务；
- Presence/Interaction/Commerce → Room 权威兜底接口；
- Room → Identity 批量用户校验；
- 业务服务 → 自己的数据源、Kafka、OTLP；
- 需要实时发布的服务/Worker → Centrifugo API；
- Prometheus → `/metrics`。

内部接口必须验证服务身份；短期可采用轮换 HMAC service token，长期再按需要评估 mTLS，不以 Service Mesh 作为本轮前置条件。所有从公网进入的身份 Header 先由 Gateway 删除，再生成受信的内部身份上下文。

# Live Platform 微服务细化拆分方案

**状态：提案，等待按阶段实施**  
**基线：2026-10-14 当前代码**  
**适用范围：直播互动平面，不包含视频采集、转码与 CDN**

## 结论

当前项目已经完成 `live-commerce`、`live-interaction`、`live-identity-room` 和按角色运行的 `live-worker` 进程拆分，但仍存在四个会阻止独立扩缩容和故障隔离的问题：

1. `live-api` 仍初始化 MySQL、Redis、Kafka 以及全部本地业务 Service，并直接处理历史、观众和统计读取。
2. `live-commerce` 直接读取 `live_rooms`、`room_bans`，仍跨越账户房间域的数据边界。
3. `live-interaction` 虽已不直连 MySQL，但弹幕和点赞依赖同步调用 `live-identity-room`，高频链路会被控制面故障拖累。
4. 所有服务共享一个 MySQL schema、账号和 Redis namespace，数据所有权主要靠代码约定，数据库权限无法阻止越界。

因此，本方案不继续按 `internal/` 包机械拆服务，而按以下三个条件确定边界：

- 数据有唯一写入者；
- 负载与扩缩容模型不同；
- 一个服务故障时，其他业务可以给出明确且可验证的行为。

目标是形成七个业务边界：

| 目标服务 | 核心职责 | 主要扩缩容信号 |
| --- | --- | --- |
| `live-gateway` | Web UI、外部 API 路由、JWT 校验、请求 ID、Trace、边缘限流 | HTTP RPS、P95/P99、错误率 |
| `live-identity` | 注册、登录、用户状态、公开展示资料、连接 JWT | 登录 RPS、密码计算 CPU、错误率 |
| `live-room` | 房间生命周期、房主权限、禁言和封禁 | 控制面 RPS、DB 延迟 |
| `live-presence` | 进房、离房、心跳、在线列表、礼物榜、订阅令牌 | 心跳 RPS、Redis CPU、在线人数 |
| `live-interaction` | 弹幕、点赞、内容过滤、限流和热点保护 | 弹幕/点赞 RPS、扇出速率 |
| `live-commerce` | 礼物目录、钱包、流水、订单、幂等和事务 Outbox | 交易 TPS、DB 锁等待、Outbox backlog |
| `live-query` | 房间列表、房间详情、消息历史和聚合统计读模型 | 查询 RPS、投影滞后、缓存命中率 |

`Centrifugo` 继续作为独立实时平台；Kafka Worker 按数据所有者独立部署，但不会把每个循环都包装成一个新的业务服务。

## 文档导航

1. [服务边界与目标架构](01-service-boundaries.md)：每个服务拥有的路由、表、Redis Key、Topic、依赖和禁止事项。
2. [分阶段迁移计划](02-migration-roadmap.md)：从当前代码迁移到目标架构的顺序、切流、回滚和完成门槛。
3. [验证与边界测试清单](03-validation-and-boundary-tests.md)：契约、一致性、越权、故障、性能与部署验收用例。

## 当前代码与目标边界的映射

| 当前实现 | 目标去向 | 当前完成度 |
| --- | --- | --- |
| `cmd/api` | `live-gateway` | ⚠️ 已有代理，但仍承载本地业务和直连数据源 |
| `cmd/identityroom` | `live-identity` + `live-room` | ⚠️ 功能真实，但身份与房间仍绑定在同一进程和 Repository 组合中 |
| `cmd/interaction` | `live-presence` + `live-interaction` | ⚠️ 高频写已拆出，但在线与弹幕点赞负载仍混在同一进程 |
| `cmd/commerce` | `live-commerce` | ⚠️ 交易边界完整，房间/治理校验仍跨域读库 |
| `cmd/worker` | 服务所有者的独立 Worker Deployment | ⚠️ 角色开关和 K8s 清单存在，Compose 仍合并运行 |
| `httpapi.roomMessages` 等读接口 | `live-query` | ❌ 仍由 Gateway 联表查询共享 MySQL / Redis |

判断依据来自当前真实调用链：

- Gateway 路由切换：[`../../internal/httpapi/server.go`](../../internal/httpapi/server.go)
- Gateway 全量依赖装配：[`../../cmd/api/main.go`](../../cmd/api/main.go)
- Commerce 跨域房间读取：[`../../cmd/commerce/main.go`](../../cmd/commerce/main.go)
- Interaction 内部身份房间 HTTP 调用：[`../../internal/identityclient/client.go`](../../internal/identityclient/client.go)
- Worker 角色装配：[`../../cmd/worker/main.go`](../../cmd/worker/main.go)

## 拆分原则

### 必须遵守

- 每张业务表、每个 Redis Key 前缀、每个 Kafka Topic 都有一个明确写入者。
- 同一强一致事务内的数据不得为了服务数量而拆开；钱包、礼物订单、钱包流水和 Commerce Outbox 保持在一个服务内。
- 高频链路优先读本地投影，控制面同步 API 只能作为受限兜底，并必须有超时、熔断和失败策略。
- Gateway 不访问业务表，不持有 Kafka Producer，不写业务 Redis。
- 内部 HTTP API 不是“内网即可信”；必须使用服务身份、NetworkPolicy 和最小权限凭据。
- 所有切流采用单写者，禁止以双写共享数据库作为迁移手段。
- 先在同一个 MySQL 实例中拆 schema 与账号，只有容量、隔离或合规指标证明需要时才物理独库。

### 明确不拆

- 不拆 `wallet-service` 与 `gift-order-service`，否则一次送礼会变成跨服务扣款事务。
- 暂不把弹幕、点赞、热点策略拆成三个服务；它们共享房间互动限流和同一扇出容量模型。
- 暂不引入 Service Mesh、分布式事务、全量 gRPC、Redis Cluster 或 Schema Registry。
- 不为每个 Kafka consumer 单独创建代码仓库；保持 monorepo 和共享基础库，但独立构建、部署、授权和扩缩容。

## 最终成功标准

只有同时满足以下条件，才可以称为完成本轮微服务化：

1. `live-gateway` 在没有 `MYSQL_DSN`、`REDIS_ADDR`、`KAFKA_BROKERS` 的环境中仍可启动并通过 readiness。
2. Commerce、Interaction、Presence 无法使用数据库权限读取或写入 Identity/Room 表。
3. `live-identity` 或 `live-room` 短时不可用时，已进入直播间的弹幕、点赞和心跳按约定继续或明确失败，不发生隐式放行。
4. 房间状态、禁言和封禁变更通过 Outbox/Kafka 到达各服务本地投影，并可量化传播延迟。
5. 永久坏消息进入 DLQ，可审计、修复和幂等重放；瞬时错误有上限重试和退避。
6. 每个服务可独立发布、回滚和扩缩容，不需要重启全部应用进程。
7. 所有边界、不变量和故障行为均通过[验证与边界测试清单](03-validation-and-boundary-tests.md)。

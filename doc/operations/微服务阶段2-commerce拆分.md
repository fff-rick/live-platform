# 微服务阶段 2：钱包与礼物服务拆分

`live-commerce` 是第一个独立业务进程，负责礼物目录、余额、钱包流水、开发充值、礼物订单、幂等与事务 Outbox。客户端仍访问原有 `/api/v1` 路径；设置 `COMMERCE_BASE_URL=http://live-commerce:8081` 后，`live-api` 仅代理以下路由：

- `GET /api/v1/gifts`
- `GET /api/v1/wallet`、`GET /api/v1/wallet/transactions`
- `POST /api/v1/wallet/dev-credit`
- `POST /api/v1/rooms/{room_id}/gifts`
- `GET /api/v1/gift-orders/{order_no}`

清空 `COMMERCE_BASE_URL` 即恢复 API 本地实现，作为灰度回滚开关。不可在同一请求上双写或双调用两个实现；礼物的 `Idempotency-Key` 只能由一个路由处理。

## 一致性边界

`live-commerce` 继续在同一个 MySQL 本地事务内提交余额扣减、订单、钱包流水和 `outbox_events`。这保证阶段 2 不会引入分布式事务或重复扣款风险。

当前是**逻辑所有权迁移**：commerce 拥有钱包/订单/礼物/Outbox 的写入，但为校验开播状态和封禁，暂时通过共享 MySQL 的 `room.Service` 读取房间。此依赖必须在阶段 4 以前替换为版本化房间 API 或房间事件读模型；不得新增 commerce 对用户、房间或治理表的写入。

礼物榜的 Redis 写入也仅是阶段 3 前的兼容投影，账务事实仍以 commerce 数据库为准。

## 验证与切换

1. 启动 `live-commerce`，其 `/health`、`/ready` 和 `/metrics` 必须正常。
2. 在预发将 `COMMERCE_BASE_URL` 指向 commerce，执行注册后充值、送礼、幂等重放、余额与订单查询。
3. 校验：负余额为 0；相同 idempotency key 不重复扣款；每个成功订单都有流水及 Outbox；Gift consumer 正常投递。
4. 对比迁移前后的 HTTP 状态码、错误码、响应 JSON 与阶段 0 指标。
5. 出现异常时清空 API 的 `COMMERCE_BASE_URL` 并仅重启 API；保留 commerce/Outbox 数据以便排查，禁止回滚数据库事务。

当前 demo GitOps overlay 仍只管理 SealedSecret，不可作为本服务的发布入口。新的受审查环境 overlay 应先指定外部 MySQL、Redis、Kafka、Secret 及回滚策略。

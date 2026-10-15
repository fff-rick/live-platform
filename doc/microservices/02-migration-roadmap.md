# 分阶段迁移计划

## 总体策略

迁移采用“先阻止继续越界，再替换读取，再切写入，最后删除旧路径”的顺序。每一阶段只改变一个主要边界，并保留明确回滚点。

任何阶段都不得：

- 同时向新旧服务双写同一业务状态；
- 在没有数据校验的情况下删除旧表、旧 Topic 或旧 consumer group；
- 通过给所有服务共享数据库超级账号来绕过边界问题；
- 将一次性全链路重写作为切流条件。

## 阶段 P0：冻结基线和建立自动护栏

**实施状态：已完成（2026-10-15）**

### 目标

确保后续拆分不会无意破坏现有 HTTP、WebSocket、Kafka 和数据库不变量。

### 实施项

1. 从当前路由生成 `public-api-v1` 清单，记录方法、路径、认证、成功状态码和错误码。
2. 为 `live-api → live-commerce/live-interaction/live-identity-room` 增加真实 HTTP 契约测试，不只测试 Go interface。
3. 为 `danmaku.sent`、`gift.sent` 增加 golden payload 和兼容性测试。
4. 新增架构边界测试：扫描 Gateway import、SQL、Redis Key、各服务 DSN 和跨域表名。
5. 给所有事件增加 producer、consumer、owner、partition key 和保留期目录。
6. 为永久错误建立 DLQ 数据结构和本地重放命令，替换当前“记录日志后提交 offset”的行为。

### 当前落地结果

- [x] `doc/contracts/public-api-v1.json` 冻结 30 条对外路由，并与 Gateway 注册表自动比对。
- [x] `tests/contracts` 通过真实 HTTP 连接验证三个上游的认证、Header、Body、request ID 和业务错误透传。
- [x] `danmaku.sent` 和 `gift.sent` golden payload 由当前真实 Consumer 解析，同时验证可选字段向后兼容。
- [x] `tests/boundaries` 冻结 Gateway import、服务 import、SQL 表、Redis Key、事件类型和内部路由基线。当前 Gateway 本地回退与 Commerce 直读 Room 是显式例外，只能在后续阶段删减。
- [x] `doc/contracts/event-catalog-v1.md` 记录生产者、消费者、所有者、partition key、保留期和 DLQ。
- [x] 永久坏消息必须先写入 `<source-topic>.dlq` 才提交 offset；`cmd/dlq-replay` 可保持原 key/payload 并输出操作人审计记录。
- [x] 在可用的 Docker 环境完成 UI smoke、M8 smoke 和真实 Kafka DLQ 导出/重放演练。

### 影响代码

- `doc/contracts/`
- `internal/mq/`
- `cmd/dlq-replay/`
- 新增 `tests/contracts/`、`tests/boundaries/`
- Makefile 新增 `test-contracts`、`test-boundaries`

### 完成门槛

- [验证文档](03-validation-and-boundary-tests.md)中的 `ARCH-*`、`HTTP-*`、`EVENT-*` 基线目录进入 CI；P0 允许列出已知越界，但必须通过显式允许列表阻止新增债务。
- 现有 `go test ./...`、UI 测试和 M8 smoke 全部通过。
- 旧客户端无需修改。

### 回滚

本阶段不改变业务成功路径和路由流量。如需回滚 DLQ，先停止 Consumer，回滚 Worker 镜像后再恢复消费；回滚会恢复“永久错误只记日志”的旧风险，不应作为常态配置。

## 阶段 P1：先把 `live-api` 变成真正 Gateway

**实施状态：已完成（2026-10-15）**

### 目标

解除 Gateway 对 MySQL、Redis、Kafka 和业务 Repository 的依赖，先建立清晰的公网边界。

### 实施项

1. 建立 `live-query` 进程，第一版仅迁移以下现有读取实现：
   - `GET /rooms/{id}/messages`；
   - `GET /rooms/{id}/stats`；
   - `GET /rooms/{id}/viewers`、`top-viewers` 在 Presence 拆分前可临时进入 Query。
2. Query 在 P1 使用独立连接池读取共享源表；P2 已切换为 `query` schema 的只读账号。
3. `cmd/api` 只装配四个固定 HTTP upstream，不再创建本地 Auth/Room/Gift/Wallet/Danmaku Service。
4. Gateway readiness 只检查自身 HTTP 线程和路由配置，不 Ping 业务数据库。
5. 删除本地业务回退；任一必要 upstream 配置非法时 Gateway 启动失败。

### 切流顺序

```text
部署 live-query
→ 四条本地查询路由一次性切换到 Query
→ 从 live-api 删除本地 Handler、Repository 和 fallback
→ 移除 live-api 数据源配置与 Secret
```

### 完成门槛

- `live-api` 在未提供 MySQL、Redis、Kafka、Centrifugo API Key 时正常启动并 readiness=200。
- Query 故障只影响读接口；登录、送礼、弹幕路由仍可由 Gateway 转发。
- `live-api` 二进制中不再包含需要业务 DSN 才能初始化的本地装配路径；独立生产镜像在 P7 收口。

### 回滚

- 本项目不保留运行时双路径；需要回滚时整体回滚到上一不可变镜像与 Compose/Kubernetes 清单。
- 不在运行时重新授予 Gateway 数据库权限。

## 阶段 P2：数据库账号与数据所有权收口

**实施状态：已完成（2026-10-15）**

### 目标

让“服务不能越界”由数据库权限保证，而不只是代码注释。

### 实施项

1. 在同一 MySQL 实例创建 `identity`、`room`、`commerce`、`archive`、`query` schema。
2. 为每个 schema 创建应用账号、迁移账号和只读审计账号。
3. 使用 expand/contract 迁移表：先复制/回填、校验行数与 checksum，再切换 Repository DSN。
4. `live-commerce` 暂时通过 Room 内部 API 校验房间和封禁，删除 `room.NewRepository(mysql)`。
5. 将全局 `outbox_events`、`processed_events` 拆成所有者自己的表，避免一个 Worker 获得全库权限。
6. 迁移过程中所有旧表保持只读观察期，确认无写入后再删除。
7. P4 拆进程前，合并的 `live-identity-room` 临时同时接收 `IDENTITY_MYSQL_DSN` 和 `ROOM_MYSQL_DSN`，Auth/Room Repository 分别使用各自连接池；不得为方便过渡保留共享超级账号。

### 已落地结果

- `008_p2_schema_ownership.sql` 建立五个 schema，回填旧表，并保留 `live.*` 作为只读观察副本。
- `live-migrate` 为每个 schema 管理 app、migration、audit 三类账号；运行进程不接收迁移凭据。
- IdentityRoom 使用 Identity/Room 两个连接池；Room Repository 已删除对 `users` 的 JOIN。
- Commerce 通过带 800ms 超时的内部 Room API 校验房间和封禁，不再持有 Room 数据库权限。
- Worker 按角色打开 Commerce、Archive 或 Query Writer 连接，不再使用共享 DSN。
- Query 只持有 `query` audit 账号；当前消息和用户展示读模型使用 `SQL SECURITY DEFINER` 视图过渡，P3 改为 Kafka 维护的实体投影表。
- 已验证 Commerce 账号读取 `room.live_rooms`、Query 账号直接读取 `identity.users` 均被 MySQL 拒绝。

### 表迁移顺序

| 顺序 | 目标 schema | 表 |
| --- | --- | --- |
| 1 | `commerce` | `gifts`、`wallets`、`wallet_transactions`、`gift_orders`、Commerce Outbox |
| 2 | `archive` | `danmaku_records`、消费幂等表 |
| 3 | `identity` | `users`、Identity Outbox |
| 4 | `room` | `live_rooms`、`room_mutes`、`room_bans`、Room Outbox |
| 5 | `query` | 新读模型表 |

### 完成门槛

- 使用 Commerce 凭据执行 `SELECT * FROM room.live_rooms` 必须被 MySQL 拒绝。
- 使用 Gateway Pod 凭据无法连接任何业务数据库。
- 礼物并发与幂等测试在新 Commerce schema 上保持全部不变量。

### 回滚

- 新 schema 已成为单写者，旧 `live.*` 表只保留观察；回滚时必须停止写流量并把新 schema 增量回填到旧表后切回。
- 不采用应用层双写，因此回滚必须有明确维护窗口和增量复制记录。

## 阶段 P3：房间、治理和身份事件投影

### 目标

消除 Interaction/Commerce/Presence 高频链路对 Identity/Room 同步查询的依赖。

### 实施项

1. 在 Identity 与 Room 的业务事务中写入各自 Outbox。
2. 发布 `live.identity.v1`、`live.room.v1`、`live.governance.v1`。
3. 为 Interaction、Presence、Commerce 分别建立自己的投影 namespace，不共享投影写入权。
4. 投影记录 `aggregate_version`、`projected_at`、源 offset 和最大允许陈旧时间。
5. 缓存未命中时允许一次有 deadline 的 Room/Identity API 兜底；调用失败对权限操作默认拒绝。
6. 增加投影重建命令：从权威 API 分页快照 + Kafka 保留窗口增量追平。

### 切流顺序

```text
先发事件但不读投影
→ 影子构建投影并比对权威查询
→ 只读投影 + 同步 API 校验差异
→ 投影优先、API miss fallback
→ 移除正常路径同步调用
```

### 完成门槛

- Room/Identity 停止 5 分钟时，已有房间互动继续遵循最后有效投影；超过最大陈旧时间后新增受保护操作 fail closed。
- 禁言和封禁传播延迟 P99 达到约定值，本方案默认不超过 2 秒。
- 重复、乱序事件不会回退房间状态或解除最新治理结果。

### 回滚

- 将读取切回权威内部 API；事件继续发布，避免投影断档。
- 禁止回滚到跨库读取。

## 阶段 P4：拆分 `live-identity-room`

### 目标

把安全敏感的账户认证和房间控制面分成独立故障域。

### 实施项

1. 新增 `cmd/identity`，迁移 Auth Service、用户 Repository 和 JWT 签发。
2. 新增 `cmd/room`，迁移 Room Service、房间生命周期和治理 Handler。
3. Room 消费 Identity 用户展示/状态事件，移除房间查询对 `users` JOIN。
4. Room 创建和治理目标用户校验使用本地投影；缺失时调用 Identity batch API。
5. Gateway 分路由切换 `IDENTITY_BASE_URL` 与 `ROOM_BASE_URL`。
6. 观察一个发布窗口后删除 `cmd/identityroom` 生产 Deployment；保留旧镜像作为回滚制品。

### 完成门槛

- Identity 重启不影响已有 JWT 的本地验证、房间查询和互动投影读取。
- Room 故障不影响登录、钱包查询和已有礼物订单查询。
- Identity 账号无权访问 Room schema，Room 账号无权读取密码哈希。

### 回滚

- 切流窗口内路由回旧 `live-identity-room`。
- 新旧服务不得同时写 `users` 或 Room 表；回滚前必须停止新服务写流量。

## 阶段 P5：从 Interaction 拆出 Presence

### 目标

让高频心跳/在线状态和弹幕/点赞分别扩缩容，避免 Redis 热点与连接流量互相抢占资源。

### 实施项

1. 新增 `cmd/presence`，迁移 join、leave、heartbeat、viewers、top-viewers。
2. 将 Viewer Redis Key 从 `live:room:*` 迁移到 `live:presence:*`。
3. Interaction 只保留 danmaku、like 和热点保护，读取 Presence 聚合后的 viewer count 投影，不读取 Presence 原始 ZSET。
4. Presence 消费 Gift 事件更新在线礼物榜；Commerce 不写 Presence Redis。
5. Presence 发布聚合 viewer-count 事件，Query/统计投影消费。
6. 订阅 JWT 由 Presence 签发或通过受限 token signer，避免所有业务服务共享 Centrifugo 主密钥。

### Redis Key 迁移

- 采用“旧 Key 只读 + 新 Key 单写”的短窗口迁移。
- 切换前将仍在 TTL 内的旧在线成员复制到新 Key，保留原到期时间和入场顺序。
- 不双写心跳；否则故障时无法判断哪个 Key 是权威来源。

### 完成门槛

- Presence 扩容/缩容不会改变在线人数、首次入场顺序和 TTL。
- Interaction Pod 全部重启时，在线状态和订阅续期不受影响。
- Presence Redis 故障不会影响钱包余额与礼物订单事务。

### 回滚

- 在旧 Key 保留窗口内切回旧 Interaction join/heartbeat Handler。
- 回滚前停止 Presence 写流量，并把新 Key 活跃成员一次性合并回旧 Key。

## 阶段 P6：读模型、Worker 和 DLQ 收口

### 目标

完成历史、统计、榜单投影和异步消费者的所有权隔离。

### 实施项

1. Query 改为完全消费事件构建读模型，不再联查 Identity、Room、Commerce 或 Archive 源表。
2. Danmaku Archive、Gift Delivery、Query Projector、Like Snapshot 使用独立 Deployment 与账号。
3. 每个 consumer group 配置 DLQ、有限重试、指数退避和人工重放命令。
4. 重放必须先经过同一 schema 校验和幂等键，不允许跳过业务验证直接写投影。
5. 将 Worker readiness 从“进程活着”升级为依赖可用、consumer 正常加入 group、投影未超过最大滞后。

### 完成门槛

- Query 数据库清空后可以从权威快照和 Kafka 事件重建。
- Poison message 不再阻塞同一分区后续消息，并可在修复后幂等重放。
- 一个 Worker 账号泄露时，无法访问其他服务业务表。

## 阶段 P7：独立发布、弹性和 HA

### 目标

把逻辑边界落实为可独立交付和可演练的运行边界。

### 实施项

1. 每个业务服务生成独立镜像标签、SBOM 和扫描结果；不再用一个包含全部二进制的镜像作为生产制品。
2. 配置 ServiceAccount、Secret、NetworkPolicy、PodDisruptionBudget 和 topology spread。
3. Gateway、Presence、Interaction、Query 根据资源和业务指标配置 HPA；Kafka Worker 根据稳定的 lag 指标决定是否引入 KEDA。
4. Kafka 使用多 Broker、RF=3、`min.insync.replicas=2`、ACL 和 TLS/SASL。
5. Redis 按 Presence、Interaction、Query、Centrifugo 至少完成账号和前缀隔离；指标证明需要后再物理分集群。
6. MySQL 使用托管 HA 或主从方案，演练备份、PITR 和主切换。
7. 启用真正包含 application base 的 GitOps overlay；当前只管理 SealedSecret 的 demo overlay 不作为发布完成证据。

### 完成门槛

- 任一无状态 Pod 下线不造成用户可见中断超出 SLO。
- Kafka Broker、Redis 主节点、MySQL 主节点故障均有实测 RTO/RPO。
- 每个服务可以独立发布和回滚，不要求全平台同步重启。

## 推荐实施顺序与依赖

```text
P0 护栏
 ↓
P1 薄 Gateway
 ↓
P2 数据权限
 ↓
P3 事件投影
 ├─→ P4 Identity / Room 拆分
 └─→ P5 Presence 拆分
          ↓
P6 Query / Worker / DLQ 收口
          ↓
P7 独立交付与 HA
```

P4 和 P5 在 P3 完成后可以由不同小组并行，但同一环境中的生产切流应错开，避免同时改变身份、房间和在线状态三个关键边界。

## 每阶段通用发布检查

1. 契约、单元、集成、边界测试全部通过。
2. 新旧路径影子比对无未解释差异。
3. 数据迁移行数、checksum、不变量校验通过。
4. Dashboard、告警和 Runbook 已随服务上线。
5. 回滚镜像、配置、数据恢复步骤已演练。
6. 切流后观察至少一个业务高峰或等价压测窗口。
7. 删除旧路径必须作为独立变更，不能和首次切流同批完成。

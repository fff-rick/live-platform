# 拆分后的验证与边界测试事项

## 1. 测试目标

测试不仅证明接口“能返回 200”，还必须证明：

- 服务不能越权访问其他服务的数据；
- 重复、乱序、超时和局部故障不会破坏业务不变量；
- Gateway、同步 API、Kafka、Redis 和数据库故障的用户可见行为符合约定；
- 每个服务可以独立扩缩容、发布和回滚。

建议新增以下 CI 门禁，名称为规划目标，当前仓库尚未全部实现：

```bash
make test-unit          # 包内业务逻辑
make test-contracts     # HTTP / Kafka / WebSocket 契约
make test-boundaries    # import、SQL、权限和网络边界
make p2-db-boundaries   # Compose MySQL 真实账号越权拒绝
make test-integration   # 真实 MySQL / Redis / Kafka / Centrifugo
make test-fault         # 依赖超时、重启和故障注入
make test-load          # 容量与回归门禁
```

P0 已落地 `make test-contracts` 和 `make test-boundaries`，并已接入 CI。P1 已删除 Gateway 本地业务依赖；P2 已删除 Room 对 `users` 的 JOIN 和 Commerce 对 Room Repository 的依赖，并用真实 MySQL 验证跨 schema 权限拒绝。P3—P7 继续删减 Redis、事件投影和部署层例外。

P0 已自动化的 HTTP 基线包括 `HTTP-001`、`HTTP-003`、`HTTP-004`、`HTTP-008`；事件基线包括向后兼容 golden、永久错误 DLQ 元数据、原 key/payload 重放和操作人审计。需要真实依赖、故障注入或后续服务才能验证的编号仍是计划项。

## 2. 架构边界静态测试

| ID | 检查 | 通过条件 |
| --- | --- | --- |
| `ARCH-001` | Gateway import 边界 | `cmd/api` 不导入任何业务 Repository 或 `database/sql` |
| `ARCH-002` | Gateway 配置边界 | Gateway 配置结构中不存在 MySQL、Redis、Kafka、Centrifugo API Key |
| `ARCH-003` | Commerce SQL 边界 | Commerce 源码不出现 `live_rooms`、`room_bans`、`room_mutes`、`users` |
| `ARCH-004` | Interaction SQL 边界 | Interaction/Presence 不导入 `mysqlstore` 或任何业务 Repository |
| `ARCH-005` | Room SQL 边界 | Room Repository 不 JOIN Identity `users` 源表 |
| `ARCH-006` | Redis 写入边界 | 每个 Key 前缀只有一个生产代码包拥有写操作 |
| `ARCH-007` | Topic 所有权 | 每个事件类型只有一个业务生产者；Outbox Publisher 不算业务生产者 |
| `ARCH-008` | 内部路由暴露 | `/internal/*` 不出现在公网 Ingress/Gateway 路由表 |
| `ARCH-009` | 镜像边界 | 生产镜像只包含目标服务二进制和必要迁移资源 |
| `ARCH-010` | 旧路径清理 | 切流观察期结束后，CI 阻止重新引入已删除的本地 fallback Handler |

这些检查应使用可读的允许列表，不要用脆弱的全仓字符串黑名单替代代码审查。

## 3. 数据库和凭据边界测试

| ID | 操作 | 预期结果 |
| --- | --- | --- |
| `DB-001` | 使用 `identity_app` 查询 `commerce.wallets` | MySQL 权限拒绝 |
| `DB-002` | 使用 `commerce_app` 查询 `room.live_rooms` | MySQL 权限拒绝 |
| `DB-003` | 使用 `room_app` 查询 `identity.users.password_hash` | MySQL 权限拒绝 |
| `DB-004` | 使用 `query_app` UPDATE 任一源表 | MySQL 权限拒绝 |
| `DB-005` | 使用 Worker 账号访问非本 Worker 表 | MySQL 权限拒绝 |
| `DB-006` | Gateway Pod 查看环境变量和挂载 Secret | 不存在业务 DSN 和数据库密码 |
| `DB-007` | 迁移 Job 完成后检查凭据 | 迁移账号未挂载到应用 Pod |
| `DB-008` | 检查跨 schema FK | 不存在阻止独立迁移/恢复的跨服务外键 |

数据迁移验证至少包括：

- 源/目标行数；
- 主键范围和重复键；
- 钱包余额、订单金额、流水前后余额不变量；
- Outbox 未发布数量和最大年龄；
- 抽样或全量 checksum；
- 切流后旧表零新增写入。

## 4. HTTP 契约测试

### Gateway 通用契约

| ID | 场景 | 预期结果 |
| --- | --- | --- |
| `HTTP-001` | 缺少 Bearer Token 访问受保护接口 | 原有 `401` 和稳定错误码 |
| `HTTP-002` | 非法 JSON、未知字段、超大 Body | 原有 `400/413`，不上游或数据库 |
| `HTTP-003` | Gateway 生成 request ID | 上游日志、响应头和 Trace 使用同一 ID |
| `HTTP-004` | 上游返回业务 `409/422/429` | Gateway 原样保留状态和错误码 |
| `HTTP-005` | 上游连接失败 | `503 UPSTREAM_UNAVAILABLE` |
| `HTTP-006` | 上游超过 deadline | `504 UPSTREAM_TIMEOUT`，取消下游 context |
| `HTTP-007` | 非幂等 POST 超时 | Gateway 不自动重试 |
| `HTTP-008` | 公网请求 `/internal/v1/*` | `404` 或 `403`，不得转发 |

### Identity

| ID | 场景 | 预期结果 |
| --- | --- | --- |
| `ID-001` | 并发注册相同用户名 | 仅一个成功，其余 `409 USERNAME_TAKEN` |
| `ID-002` | 错误密码/禁用用户 | 分别返回稳定 `401/403` |
| `ID-003` | JWT 过期、签名错误、算法替换 | 全部拒绝 |
| `ID-004` | 用户注册事务成功 | 用户和对应 Outbox 事件同时存在 |
| `ID-005` | Outbox 插入失败 | 注册事务整体回滚 |
| `ID-006` | 内部 batch-get 超过限制 | 拒绝，防止无界查询和内存占用 |

### Room 与治理

| ID | 场景 | 预期结果 |
| --- | --- | --- |
| `ROOM-001` | 非房主 start/stop/mute/ban | `403`，无表更新、无事件 |
| `ROOM-002` | 非法状态迁移 | `409`，状态版本不增加 |
| `ROOM-003` | 重复 mute/ban | 幂等更新并产生单调递增版本 |
| `ROOM-004` | unmute/unban 后旧事件晚到 | 消费者根据 version 忽略旧事件 |
| `ROOM-005` | 状态更新成功但 Outbox 失败 | 同一事务回滚 |
| `ROOM-006` | 目标用户不存在或被禁用 | 根据产品规则拒绝且不生成治理记录 |

### Presence

| ID | 场景 | 预期结果 |
| --- | --- | --- |
| `PRES-001` | 重复 join | 在线人数只增加一次，入场顺序不变 |
| `PRES-002` | 并发 heartbeat | TTL 延长但不制造重复成员 |
| `PRES-003` | 显式 leave | 立即移除，重复 leave 幂等 |
| `PRES-004` | 客户端断网 | TTL 到期后自动移除并更新人数 |
| `PRES-005` | ban 事件到达 | 移除在线状态、阻止续约、触发取消订阅 |
| `PRES-006` | 房间关闭事件到达 | 拒绝新 join，现有成员按策略清理 |
| `PRES-007` | Gift 事件重复 | 榜单金额只累计一次 |
| `PRES-008` | 同礼物值并列 | 按首次进房顺序稳定排序 |
| `PRES-009` | 投影缺失且 Room 超时 | fail closed，不签发订阅令牌 |
| `PRES-010` | Presence 多副本扩缩容 | 在线人数与 TTL 不受 Pod 身份影响 |
| `PRES-011` | Gift 事件到榜单 | P99 延迟在切流前约定的预算内，超限时告警 |

### Interaction

| ID | 场景 | 预期结果 |
| --- | --- | --- |
| `INT-001` | 空弹幕、超长、敏感词 | 拒绝且不发布 WebSocket/Kafka |
| `INT-002` | 被禁言或封禁用户 | 按投影拒绝，不调用源数据库 |
| `INT-003` | 用户限流 | 稳定 `429`，不同用户互不影响 |
| `INT-004` | 热点房间采样 | 达阈值后按策略降低广播，冷房间不受影响 |
| `INT-005` | Kafka 不可用 | 实时发布成功后按既定“尽力归档”语义返回，并记录降级指标 |
| `INT-006` | Centrifugo 不可用 | 弹幕返回明确失败，不错误声称已广播 |
| `INT-007` | 重复/批量点赞 | 总量和限流按点击数计算，不按请求数绕过 |
| `INT-008` | Like Snapshot 重放 | MySQL 快照单调不下降 |
| `INT-009` | 用户资料投影缺失 | 有界兜底或拒绝，不对每条弹幕产生无界 N+1 |

### Commerce

| ID | 场景 | 预期结果 |
| --- | --- | --- |
| `COM-001` | 相同幂等键、相同参数并发 100 次 | 一个订单、一次扣款、一条流水、一条 Outbox |
| `COM-002` | 相同幂等键、不同参数 | `409 IDEMPOTENCY_CONFLICT` |
| `COM-003` | 余额不足 | 无订单成功状态、无扣款、无 Outbox |
| `COM-004` | 钱包余额临界并发 | 余额永不为负，成功金额之和不超过初始余额 |
| `COM-005` | Outbox 插入失败 | 订单、扣款和流水全部回滚 |
| `COM-006` | Kafka 不可用 | 已提交交易返回成功，Outbox backlog 增长并最终补发 |
| `COM-007` | 房间关闭/用户被封禁 | 新交易拒绝；已成功幂等重放仍返回原订单 |
| `COM-008` | Room 权威接口超时且投影过期 | 新交易 fail closed |
| `COM-009` | Gift Consumer 重复消费 | 只发布一次业务消息或客户端可按稳定 `message_id` 去重 |
| `COM-010` | Gift Consumer 发布后提交 offset 前崩溃 | 重启重放不再次扣款，消息保持幂等语义 |

### Query

| ID | 场景 | 预期结果 |
| --- | --- | --- |
| `QUERY-001` | 房间事件重复/乱序 | Room Card 保持最高 version |
| `QUERY-002` | 弹幕事件重复 | Message Feed 只有一个 `message_id` |
| `QUERY-003` | 礼物事件重复 | Message Feed 只有一个 `gift:{order_no}` |
| `QUERY-004` | 用户改名事件 | 后续展示更新；历史是否回填按契约固定 |
| `QUERY-005` | 清空 Query DB 后重建 | 快照 + 事件追平后结果与源数据一致 |
| `QUERY-006` | 投影滞后超阈值 | readiness/告警反映 degraded，不静默返回无限陈旧数据 |

## 5. Kafka、Outbox 与 DLQ 测试

| ID | 场景 | 通过条件 |
| --- | --- | --- |
| `EVENT-001` | Producer 增加可选字段 | 旧 Consumer 正常处理 |
| `EVENT-002` | 缺少必填字段/非法 JSON | 进入 DLQ，记录源 offset 和错误 |
| `EVENT-003` | 同 `event_id` 重复 10 次 | 业务副作用一次，offset 可提交 |
| `EVENT-004` | 同 aggregate 版本乱序 | 低版本被忽略并计数 |
| `EVENT-005` | Consumer 业务写成功、提交 offset 前崩溃 | 重启后幂等，不重复副作用 |
| `EVENT-006` | Consumer 瞬时 DB 错误 | 有限重试和退避，恢复后成功 |
| `EVENT-007` | 错误持续超过重试预算 | 进入 DLQ，不永久阻塞分区 |
| `EVENT-008` | 修复后重放 DLQ | 使用原 event_id 幂等成功并记录审计人/时间 |
| `EVENT-009` | Outbox 多副本 Claim | `SKIP LOCKED` 下同一记录只被一个 Worker 持有 |
| `EVENT-010` | Worker Claim 后崩溃 | lease 到期后其他 Worker 接管 |
| `EVENT-011` | Kafka 成功但 MarkPublished 失败 | 允许重复投递，下游幂等吸收 |
| `EVENT-012` | Trace 传播 | Gateway → Service → Outbox → Producer → Consumer → Centrifugo 可关联 |

## 6. 服务身份与网络边界测试

| ID | 场景 | 预期结果 |
| --- | --- | --- |
| `SEC-001` | 无 service token 调用内部 API | `401/403` |
| `SEC-002` | 用 Presence 身份调用 Identity 管理接口 | 拒绝 |
| `SEC-003` | 从公网访问内部 ClusterIP/Ingress 路径 | 不可达 |
| `SEC-004` | Pod 访问未声明的数据源 | NetworkPolicy 拒绝 |
| `SEC-005` | JWT HMAC 算法混淆或 `alg=none` | 拒绝 |
| `SEC-006` | 日志/Trace/DLQ 扫描 | 不包含密码、JWT、密钥或完整敏感 payload |
| `SEC-007` | service token 轮换 | 新旧密钥短窗口兼容，旧密钥按时失效 |
| `SEC-008` | Centrifugo API Key 泄露面 | 只有明确的发布服务和 Worker 持有 |
| `SEC-009` | 客户端伪造 `X-User-ID`/内部身份 Header | Gateway 删除伪造值，下游只接受经验签上下文 |

## 7. 故障隔离矩阵

| 故障 | 必须保持可用 | 允许失败/降级 | 验证重点 |
| --- | --- | --- | --- |
| Identity 停止 | 已有 JWT 验证、房间互动、钱包查询 | 注册、登录、资料修改 | 服务不级联重启；投影未过期 |
| Room 停止 | 登录、钱包、已有房间短时互动 | 创建/开停播/治理；投影过期后的授权写 | 权限操作 fail closed |
| Presence 停止 | 登录、房间查询、钱包交易、已有 WebSocket 短时接收 | join/heartbeat/viewers | 在线 TTL 最终清理 |
| Interaction 停止 | 登录、房间控制、Presence、钱包礼物 | 弹幕、点赞 | Commerce 不依赖 Interaction |
| Commerce 停止 | 登录、房间、在线、弹幕、点赞 | 钱包、送礼 | 互动链路不受 DB 锁影响 |
| Query 停止 | 所有命令写入 | 房间列表、消息、统计读取 | Gateway 只返回对应路由 503 |
| Kafka 停止 | 登录、房间控制事务、送礼事务、实时弹幕 | 投影延迟、礼物异步展示、弹幕归档 | Outbox 积压、恢复补发 |
| Centrifugo 停止 | HTTP 查询、房间控制、钱包交易 | 实时弹幕/礼物/统计展示 | 不把实时失败扩大到交易回滚 |
| Presence Redis 停止 | 其他服务全部功能 | 在线相关接口 | Redis 故障不影响 Commerce/Identity |
| MySQL 主切换 | Redis 实时链路按策略继续 | 控制面/交易短暂失败 | 连接恢复、无重复事务 |

每个故障测试都要记录：注入时间、检测时间、用户可见行为、告警时间、恢复时间、积压追平时间和是否违反数据不变量。

## 8. 性能与容量回归

拆分不能只验证平均延迟。每次阶段切流至少比较：

| 链路 | 指标 |
| --- | --- |
| Gateway → Service | RPS、P50/P95/P99、连接池、上游超时、5xx |
| Presence | 心跳 RPS、Redis CPU、单热点房间 ZSET 大小、TTL 清理延迟 |
| Interaction | 弹幕/点赞成功率、热点采样率、预计 fanout、Centrifugo 发布延迟 |
| Commerce | 单钱包/多钱包 TPS、DB 锁等待、P99、余额不变量、Outbox oldest age |
| Kafka Worker | consumer lag、处理时延、重试、DLQ、rebalance 时间 |
| Query | 缓存命中率、投影滞后、重建速度、列表/历史 P99 |

默认发布门禁继续沿用现有系统压测基线：HTTP 失败率低于 1%、业务成功率高于 99%、P95 小于 500ms、P99 小于 1s，且无 dropped iterations。某服务若有更严格的历史基线，使用更严格值。

必须额外验证：

- Interaction 2→N 扩容时热点房间吞吐近似提升且无重复统计；
- Presence 扩容时在线人数不随 Pod 数翻倍；
- Commerce 扩容时总 MySQL 连接数不超过预算；
- Consumer 副本数超过 Topic 分区数时不会被误判为吞吐提升；
- Gateway 连接池、超时和重试不会形成依赖故障放大。

## 9. 发布与回滚测试

| ID | 场景 | 通过条件 |
| --- | --- | --- |
| `DEPLOY-001` | 单服务滚动更新 | 其他服务无重启，目标路由满足 SLO |
| `DEPLOY-002` | 新旧事件消费者并存 | 兼容字段正常，破坏性版本隔离 |
| `DEPLOY-003` | Gateway 单路由回滚 | 只切目标路由，无双写 |
| `DEPLOY-004` | 数据库 expand/contract | 旧版本可读新增字段，新版本不依赖尚未删除字段 |
| `DEPLOY-005` | Worker 扩容/缩容 | consumer group 正常 rebalance，无长时间停顿 |
| `DEPLOY-006` | 投影版本回滚 | 不用旧代码覆盖更高 aggregate version |
| `DEPLOY-007` | Secret 轮换 | 无全平台同时重启，旧 Secret 可按计划撤销 |
| `DEPLOY-008` | GitOps 同步 | 期望资源包含业务 base，不只包含 Secret |

## 10. 迁移阶段与测试门禁

| 阶段 | 必跑测试 | 阶段特有验收 |
| --- | --- | --- |
| P0 | `ARCH-*`、`HTTP-*`、`EVENT-001~008` | 契约基线、DLQ 和边界扫描进入 CI |
| P1 | `ARCH-001~003`、`HTTP-*`、Query 故障隔离 | Gateway 无业务数据源也可独立就绪 |
| P2 | `DB-*`、`COM-*`、`ARCH-004` | 各账号的越界 SQL 均被 MySQL 拒绝 |
| P3 | `ROOM-004~005`、`EVENT-003~012` | 投影乱序、重放、回源和积压恢复通过 |
| P4 | `ID-*`、`ROOM-*`、`SEC-001~009` | Identity 与 Room 可分别发布、回滚和故障隔离 |
| P5 | `PRES-*`、`INT-*`、Redis Key 所有权扫描 | Presence 与 Interaction 无共享可写 Key |
| P6 | `QUERY-*`、`EVENT-*`、Worker 故障注入 | 读模型可重建，毒消息可隔离与幂等重放 |
| P7 | `SEC-*`、`DEPLOY-*`、全量容量与 HA 演练 | 单服务可独立扩缩容、发布和回滚 |

阶段切流只允许使用当前代码库已实现的测试 ID 作为证据；文档中列出但尚未落地的用例不能视为“已通过”。

## 11. 每阶段 Go/No-Go 清单

### Go

- [ ] 目标路由与数据所有者明确。
- [ ] 单元、契约、边界、集成测试通过。
- [ ] 影子流量差异全部解释。
- [ ] 数据校验和不变量通过。
- [ ] Dashboard、告警和 Runbook 已上线。
- [ ] 故障注入结果满足约定行为。
- [ ] 回滚操作在预发布环境演练成功。
- [ ] 数据库连接、Kafka 分区和 Redis 容量预算完成。

### No-Go

- [ ] 存在两个可写同一业务表/Key 的服务。
- [ ] 新服务需要共享超级数据库账号才能运行。
- [ ] 永久坏消息仍会无限阻塞分区或被静默丢弃。
- [ ] 权限投影不可用时默认放行。
- [ ] Gateway 仍持有业务数据库或 Kafka 凭据。
- [ ] 不能解释新旧路径结果差异。
- [ ] 没有经过演练的数据或路由回滚方案。

## 12. 建议的测试目录

```text
tests/
  contracts/
    http/                 外部与内部 HTTP golden cases
    kafka/                事件 schema 与兼容性
    realtime/             WebSocket envelope
  boundaries/
    imports_test.go       包/二进制依赖规则
    sql_ownership_test.go SQL 表所有权扫描
    redis_keys_test.go    Key 前缀唯一写入者
    deployment_test.py    Secret、Service、NetworkPolicy 边界
  integration/
    identity_room/
    presence_interaction/
    commerce_outbox/
    query_projection/
  fault/
    dependency_timeout/
    kafka_outage/
    redis_outage/
    pod_restart/
```

测试代码不应依赖生产外网或人工点击。涉及 Docker/Kubernetes 的慢测试可分层执行，但发布候选必须至少运行一次完整集成与故障门禁。

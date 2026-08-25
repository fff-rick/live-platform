# 微服务阶段 1：Worker 部署拆分

## 范围

本阶段将原 `live-worker` 按既有 `WORKER_ROLES` 拆为五个 Kubernetes Deployment：

| Deployment | `WORKER_ROLES` | 初始副本 | 依赖 | 扩容立场 |
| --- | --- | ---: | --- | --- |
| `live-worker-stats` | `stats` | 1 | Redis、Centrifugo | 暂不扩容；缺少房间归属租约。 |
| `live-worker-like-snapshot` | `like-snapshot` | 1 | Redis、MySQL | 暂不自动扩容。 |
| `live-worker-outbox` | `outbox` | 1 | MySQL、Kafka | 可在验证 Outbox 锁与 backlog 后扩容。 |
| `live-worker-gift-delivery` | `gift-consumer` | 1 | MySQL、Kafka、Centrifugo | 按 consumer lag 扩容，最多不超过可分配分区数。 |
| `live-worker-danmaku-archive` | `danmaku-consumer` | 1 | MySQL、Kafka | 按 consumer lag 扩容，最多不超过可分配分区数。 |

阶段 1 不拆业务代码、不改变 Topic、消费组、数据库表、外部 HTTP API 或实时消息格式。它只是将现有进程能力变成独立的资源与故障域。

## 资源和数据库预算

所有访问 MySQL 的新 Worker 均显式设置 `MYSQL_MAX_OPEN_CONNS=10`、`MYSQL_MAX_IDLE_CONNS=5`；API 设置为 20/10。默认初始配置的最大应用连接数为：

```text
API 20 + like snapshot 10 + outbox 10 + gift delivery 10 + danmaku archive 10 = 60
```

这不是 MySQL 的完整连接预算。迁移、监控、管理连接及系统预留仍必须加入计算。扩容任一 Worker 前先更新并审批预算，禁止将单进程压测中的 40 连接复制到每个 Pod。

## 部署策略

- `stats` 与 `like-snapshot` 使用单副本 `Recreate`。`stats` 在未实现房间归属前绝不能通过 HPA 水平扩展。
- Outbox 和 Kafka consumer Worker 使用 `RollingUpdate` 且 `maxSurge=0`，避免发布时无谓增加数据库连接；它们的正确性分别依赖 Outbox 锁和 Kafka consumer group / 幂等消费。
- 每个 Deployment 都暴露 `/ready` 与 `/metrics`；`live-worker-metrics` Service 用 label selector 聚合所有 Worker 指标端点，供未来 ServiceMonitor 使用。

Kubernetes Deployment 支持 `Recreate` 与 `RollingUpdate` 两种策略；`Recreate` 会在新 Pod 创建前终止旧 revision，但不能替代真正的分布式租约。参考 [Kubernetes Deployment](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/)。

## 切换步骤

当前 `deploy/k8s/overlays/demo` 刻意不引用 base，因此**不得**直接取消注释后部署。应先在一个经过审查、使用外部高可用依赖的新环境 overlay 中执行：

1. 记录阶段 0 基线：Outbox 数量/最老年龄、Kafka lag、数据库池等待、实时发布失败。
2. 将旧 `live-worker` 的 `WORKER_ROLES` 先缩减为不含 `stats`；确认旧 stats 循环停止。
3. 部署 `live-worker-stats`，确认只有一个 stats Worker 运行并持续广播。
4. 部署剩余四个 Deployment。Kafka consumer 与旧 Worker 使用相同 consumer group，短暂重叠时由 consumer group 分配分区；Outbox 则由领取锁协调。
5. 对比事件生产与消费、历史归档、礼物实时投递、点赞快照以及所有阶段 0 指标。
6. 从旧 `live-worker` 移除剩余 role，保留观察窗口后再由其原控制面删除该 Deployment。

切换期间不要使用 `--prune` 删除来源不明或不属于同一 GitOps Application 的旧资源。

## 回滚

若任一新 Worker 的 ready、lag、Outbox 年龄或业务正确性不满足预期：

1. 将旧 `live-worker` 恢复为对应 role；对于 `stats`，先终止新 stats Pod 再恢复旧 role，避免双聚合；
2. 将新 Deployment 缩容至 0，而不是删除数据库、Topic、消费组或 Outbox 数据；
3. 记录 offset、Outbox event ID、错误日志和阶段 0 指标后分析；
4. 修复并在预发重放验证后再进入切换。

## 验证

```bash
python3 scripts/phase1_k8s_static_validate.py
make phase1-k8s-validate
```

该校验确认 base 中不存在遗留 `live-worker`、每个新 Deployment 只运行一个 role、数据库连接预算和 metrics selector 正确。它不等价于生产发布验证。

# 当前 GitOps 运维手册

## 发布门禁

1. PR 和 main 推送运行格式、Lint、vet、单元测试与 Docker Buildx 预构建。
2. main 合并后，工作流从 `deploy/k8s/overlays/demo/kustomization.yaml` 读取唯一 `newTag`，发布对应 GHCR 镜像。
3. 镜像推送成功后，GitHub Actions 调用不带 `--prune` 的 `argocd app sync live-platform-demo`。
4. 当前 demo overlay 的期望状态只有 `SealedSecret/live-platform-runtime`；它不会发布 M8 应用工作负载或迁移 Job。
5. 若要处理旧资源，先运行 `make gitops-demo-preflight`，在 Argo CD 中核对 resource ownership 和 diff；确认无数据保留需求后，才允许人工执行带 `--prune` 的同步或删除。

不要使用旧的 `m8-deploy-staging`、`m8-deploy-production` 或直连 `kubectl apply` 工作流：仓库中不存在这些 overlays，且它们会绕过当前 GitOps 控制面。

## 事故处置优先级

### Centrifugo 不可用

- MySQL 与 Redis 健康时，API readiness 保持 `200`；响应会暴露 Centrifugo 降级状态，而不会触发集群级 API 就绪级联故障。
- 礼物事务仍通过 MySQL + Outbox 持久化。
- 弹幕实时发布可能失败；Kafka 持久化仍是一条独立的尽力而为链路。
- 检查 Redis 引擎健康状态、Centrifugo Pod、Ingress/LB 与 `live_realtime_publish_total`。

### Kafka 不可用

- 不要仅因 Kafka 不可用就将 API Pod 从服务端点移除。
- 礼物 Outbox 记录应积压，并在恢复后重放。
- 弹幕归档按设计允许降级；实时投递仍是优先项。
- 关注 Outbox 待处理/重试量及 Kafka 生产失败原因指标。

### MySQL 连接池饱和

- 关注 `live_db_pool_in_use_connections`、最大打开数、等待次数与等待时长。
- M7 为单进程压测选择 40 打开 / 20 空闲；Kubernetes 则有意为每个 Pod 分配更少预算（API 20/10，事件 Worker 10/5）。提高限制前必须计算 `副本数 × 单 Pod 连接池`，不要直接升到每 Pod 80。

### 热点房间过载

- 关注估算扇出与自适应采样率。
- NORMAL/HOT/PROTECT 阈值源自 M7 测量，仍是配置项，不是通用 Centrifugo 限制。
- 除非进行受控诊断，否则事故期间不要关闭自适应保护。

## 优雅终止

Kubernetes 发送 SIGTERM 后，API 停止接收新工作，内部 HTTP 关闭超时为 15 秒。Worker 组件会收到上下文取消信号，并拥有 20 秒的内部关闭窗口。Centrifugo 的优雅关闭超时为 30 秒，Pod 终止宽限期为 45 秒。

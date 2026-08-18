# M8 生产就绪说明

## M8 的改动

- 生产构建迁移到 Go 1.26.5；M8 不再使用早期里程碑所采用且已不受支持的 Go 1.24 基线。
- API and Centrifugo 的水平扩展、Worker 角色拆分等属于 M8 运行时设计；当前 GitOps demo overlay 并未引用这些工作负载。
- Redis 通过 `REDIS_URL` 支持 URL/TLS（`redis://` 或 `rediss://`）。
- Kafka 支持 TLS 以及 PLAIN / SCRAM-SHA-256 / SCRAM-SHA-512 认证。
- 数据库迁移代码已提供 `live-migrate`（checksum + MySQL advisory lock），但当前 demo GitOps overlay 不会创建该 Job。
- 当前 CI 在 PR 和 main 推送时执行格式、Lint、vet、测试及镜像构建验证；main 合并后按 demo overlay 的显式版本标签发布 GHCR 镜像，并调用 Argo CD 同步 `live-platform-demo`。

## 有意保留的边界

当前 `deploy/k8s/overlays/demo` 只引用 `sealed-runtime.yaml`，而 `../../base` 被显式禁用。因此它不是应用发布清单；集群中看到的 `live-api`、`live-worker`、MySQL、Redis、Kafka 等旧资源不应被误认为由当前 Revision 声明。先用 `make gitops-demo-preflight` 盘点，再确认 Argo CD ownership 后清理。

M8 同样不水平扩展 `live-worker-stats`。安全扩展需要房间归属/分片或分布式租约，确保任一时刻只有一个统计聚合器拥有一个房间。在此之前，单副本 `Recreate` Deployment 比可能产生重复广播的表面“高可用”配置更安全。

目标生产 overlay 需要由所选 Ingress Controller 提供 `/api`、`/connection` 的路由和 WebSocket timeout/idle 配置；当前 demo overlay 不包含 Ingress。

## 继承自 M7 的容量结论

M8 冻结 M7 结论而不继续调参。`benchmark/capacity.md` 是唯一事实来源。最终 1,000 钱包隔离实验中，观测到的礼物饱和吞吐约为 700–720 TPS，P99 约 1.8–1.9 s；最后一个实测低延迟点约为 200 TPS、P99 约 67 ms。因此 Kubernetes 将数据库连接视为集群级预算，而不会把单进程压测的 40 连接设置复制到每个 API Pod。

## 尚需按环境完成的加固

- 若要恢复应用部署，应先新建一个受审查的 GitOps overlay：明确其环境、外部依赖、迁移 Job、Secret 来源和回滚策略；不要直接取消 demo overlay 中 base 的注释。
- 生产环境还需要 NetworkPolicy、TLS/证书、镜像拉取身份与外部 MySQL/Redis/Kafka 的高可用方案。

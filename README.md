<p align="center">
  <img src="logo/live-platform.png" alt="Live Platform Logo" width="180">
</p>

<h1 align="center">live-platform</h1>

基于 Go、Centrifugo、Redis、MySQL 和 Kafka 构建的直播互动平台。项目实现互动平面：用户认证、开播/进房、实时弹幕、点赞、礼物钱包、房主管理、在线观众与礼物榜；视频采集、转码和 CDN 是后续可替换的媒体平面。

## 功能概览

- 用户注册、登录与 JWT 会话；注册时随机分配默认 SVG 头像。
- 创建直播间、开播、停播、进房与离房。
- Centrifugo WebSocket 实时推送弹幕、礼物和统计数据。
- 点赞聚合、观众心跳与显式离房；在线人数按 Redis TTL 管理。
- 大屏右上角礼物 Top 3；同分按进入房间顺序排序。点击人数气泡可展开完整在线观众列表。
- 钱包余额、开发环境充值、礼物连击、幂等送礼与事务 Outbox。
- 主播禁言、封禁管理；弹幕限流、敏感词过滤、热点房间自适应降级。
- 弹幕与成功礼物的历史回放；压测、指标、Trace 与故障演练脚本。

## 当前架构

```text
浏览器
  │ HTTP
live-api（单一入口 / 轻量 Gateway）
  ├─ live-identity-room ─ 用户、认证、房间生命周期、禁言、封禁
  ├─ live-interaction ── 进退房、心跳、弹幕、点赞、限流、热点保护
  └─ live-commerce ───── 钱包、礼物、订单、Outbox
       │                   │
       ├──── MySQL ────────┤ 业务持久化数据
       ├──── Redis ────────┤ 互动状态、限流、在线/统计
       └──── Kafka ────────┘ 领域事件
              │
         live-worker（按角色独立运行）
         ├─ Outbox Publisher / Gift Delivery
         ├─ Danmaku Archive
         └─ Stats Aggregator / Like Snapshot

浏览器 ── WebSocket ── Centrifugo（Redis engine）
```

`live-api` 保持客户端 v1 契约，通过可配置反向代理将请求切到相应服务；`live-interaction` 通过版本化内部 API 查询身份和房间治理信息，不直连 MySQL。钱包、订单和 Outbox 在同一 MySQL 本地事务中提交；Kafka 不作为 API 就绪依赖。演进状态和边界见[微服务化与分布式演进计划](<doc/微服务化与分布式演进计划.md>)。

## 快速开始

前置条件：Docker Compose、Docker，以及可选的 Go 工具链（本仓库的 `go.mod` 为准）。

```bash
git clone <your-repository-url>
cd live-platform

# 首次或保留已有数据时启动全部依赖、迁移和服务。
make compose-up
```

`make compose-reset` 会删除本地 Docker 数据卷后重新创建环境，仅在确实需要清空 MySQL、Kafka、Redis 等本地数据时使用。

启动后访问：

| 服务 | 地址 |
| --- | --- |
| 用户界面/API | http://localhost:8080/ |
| Centrifugo | http://localhost:8000/ |
| Prometheus | http://localhost:9091/ |
| Grafana | http://localhost:3000/ |

在界面中注册用户，创建并开播一个直播间；其他用户登录后进入同一房间，即可测试弹幕、点赞、礼物和在线观众。

停止并保留数据：

```bash
make compose-down
```

## 常用命令

```bash
make test                 # 运行全部 Go 测试
make vet                  # 运行 go vet
make fmt                  # 格式化 Go 文件
make compose-up           # 启动本地环境
make compose-reset        # 清除本地数据卷并重建环境
make compose-ha-up        # 启动三 Broker Kafka + 三副本 Centrifugo 的本地 HA 演练
make compose-ha-down      # 停止并删除 HA 演练数据卷
make smoke-ui             # UI 冒烟测试
make smoke-m8             # M8 本地冒烟测试
make migrate              # 单独执行数据库迁移
```

压测和容量结论请使用 `make m7-*` 命令；真实结果与结论位于 [benchmark/](benchmark/README.md)。

## 核心接口

除登录、注册、房间列表、房间详情、历史消息、Top 3 和观众列表外，接口均需要 `Authorization: Bearer <access_token>`。

| 场景 | 接口 |
| --- | --- |
| 注册/登录 | `POST /api/v1/auth/register`、`POST /api/v1/auth/login` |
| 直播间 | `GET/POST /api/v1/rooms`、`POST /api/v1/rooms/{room_id}/start` |
| 进出房 | `POST /api/v1/rooms/{room_id}/join`、`POST /api/v1/rooms/{room_id}/leave` |
| 互动 | `POST /api/v1/rooms/{room_id}/danmaku`、`like`、`gifts` |
| 实时令牌 | `POST /api/v1/realtime/token` |
| 历史消息 | `GET /api/v1/rooms/{room_id}/messages?limit=50` |
| 观众榜/列表 | `GET /api/v1/rooms/{room_id}/top-viewers`、`viewers` |
| 钱包 | `GET /api/v1/wallet`、`GET /api/v1/wallet/transactions` |

送礼必须携带唯一的 `Idempotency-Key` 请求头。单次礼物数量、用户限流和钱包余额均由后端校验。

## 配置

本地默认配置位于 [docker-compose.yml](docker-compose.yml)。常用变量：

| 变量 | 说明 |
| --- | --- |
| `MYSQL_DSN` | MySQL 连接串 |
| `REDIS_ADDR` / `REDIS_URL` | Redis 地址或 `redis://` / `rediss://` URL |
| `KAFKA_BROKERS` | Kafka Broker 列表 |
| `AUTH_JWT_SECRET` | 应用 JWT 密钥 |
| `CENTRIFUGO_API_KEY` / `CENTRIFUGO_TOKEN_SECRET` | Centrifugo 服务端与令牌密钥 |
| `VIEWER_TTL` | 观众在线 TTL；显式离房会立即清理 |
| `ENABLE_DEV_WALLET_CREDIT` | 开发钱包充值开关；生产环境必须为 `false` |

服务路由变量：`COMMERCE_BASE_URL`、`INTERACTION_BASE_URL` 与 `IDENTITY_ROOM_BASE_URL`。它们为空时，`live-api` 回退到进程内处理路径；Docker 默认值会路由到对应内部服务，适合渐进式切流与回滚。

生产环境必须替换全部开发密钥，并关闭开发充值接口；详见 [安全说明](SECURITY.md)。

## Docker 部署与 HA 演练

Docker 镜像包含 `live-api`、`live-commerce`、`live-interaction`、`live-identity-room`、`live-worker`、`live-migrate` 和默认头像资源。迁移使用 checksum 与 MySQL advisory lock，执行入口为 `make migrate`。

常规 Compose 是单机开发环境。若需要演练 Kafka Broker 故障和多副本实时接入，请使用：

```bash
make compose-ha-up
```

该 overlay 使用三节点 KRaft Kafka（RF=3、`min.insync.replicas=2`）和三副本 Centrifugo，WebSocket 统一经 `centrifugo-gateway` 的 `localhost:8000` 访问。它仍运行在单一 Docker 宿主机，Redis/MySQL 也仍为单机，因此不能作为生产 HA 结论。详见 [Docker 本地 HA 演练](<doc/operations/Docker本地HA演练.md>)。

Kubernetes 多节点本地演练文件位于 [deploy/k8s/ha-local](deploy/k8s/ha-local/README.md)。

## Kubernetes / GitOps

当前 GitOps 入口为 Argo CD Application `live-platform-demo`，指向 `deploy/k8s/overlays/demo`。该 overlay 当前仅管理 SealedSecret，不能将历史残留工作负载误认为其期望状态。部署前执行：

```bash
make m8-k8s-validate
make gitops-demo-preflight
```

详细操作见 [Kubernetes 说明](deploy/k8s/README.md) 和 [GitOps 运维手册](doc/operations/runbook.md)。

## 排障提示

- 实时连接失败：检查 `http://localhost:8000/health`、`live-interaction`、`centrifugo-gateway` 和 Centrifugo 日志；HA 演练中 `8000` 由网关而非 Centrifugo 副本直接占用。
- 弹幕刷新后缺失：确认 Kafka、`danmaku-consumer` 与 MySQL 均正常；实时弹幕可用不代表异步归档已完成。
- 在线人数不准确：检查客户端是否已升级到离房接口版本；网络强制中断时由 `VIEWER_TTL` 兜底清理。
- 礼物未实时出现：检查 Kafka、Outbox backlog 与 `gift-consumer`。

## 文档索引

- [贡献指南](CONTRIBUTING.md)
- [安全说明](SECURITY.md)
- [详细设计](doc/直播互动系统详细设计说明书.md)
- [技术方案](doc/基于%20Centrifugo%20的高并发直播互动系统技术方案书.md)
- [容量与压测报告](benchmark/README.md)
- [M8 就绪与运行手册](doc/operations/m8-production-readiness.md)
- [Docker 本地 HA 演练](<doc/operations/Docker本地HA演练.md>)
- [微服务化与分布式演进计划](<doc/微服务化与分布式演进计划.md>)

## 边界

本项目的重点是高并发直播互动系统的工程实践。它不包含真实视频推流、转码或 CDN；接入 SRS、LiveKit 或云直播服务时，应保持媒体平面与本项目互动平面分离。

<p align="center">
  <img src="logo/live-platform.png" alt="Live Platform Logo" width="180">
</p>

<h1 align="center">live-platform</h1>
# Live Platform

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

## 架构

```text
浏览器 UI
  ├─ HTTP API ────────── Go live-api ── MySQL / Redis
  └─ WebSocket ───────── Centrifugo ── Redis

Go live-worker
  ├─ Outbox Publisher ──────────────── Kafka
  ├─ Gift Consumer ──── 实时礼物推送
  ├─ Danmaku Consumer ─ 弹幕归档
  └─ Stats Aggregator ─ 观众/点赞统计广播
```

钱包、订单和 Outbox 在同一 MySQL 事务中提交；Kafka 不作为 API 就绪依赖。详情见 [设计文档](doc/直播互动系统详细设计说明书.md)。

## 快速开始

前置条件：Docker Compose、Docker，以及可选的 Go 工具链（本仓库的 `go.mod` 为准）。

```bash
git clone <your-repository-url>
cd live-platform

# 清空本地数据卷后启动全部依赖、迁移、API 和 Worker。
make compose-reset
```

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

生产环境必须替换全部开发密钥，并关闭开发充值接口；详见 [安全说明](SECURITY.md)。

## 部署说明

Docker 镜像包含 `live-api`、`live-worker`、`live-migrate` 和默认头像资源。迁移使用 checksum 与 MySQL advisory lock，执行入口为 `make migrate`。

当前 GitOps 入口为 Argo CD Application `live-platform-demo`，指向 `deploy/k8s/overlays/demo`。该 overlay 当前仅管理 SealedSecret，不能将历史残留工作负载误认为其期望状态。部署前执行：

```bash
make m8-k8s-validate
make gitops-demo-preflight
```

详细操作见 [Kubernetes 说明](deploy/k8s/README.md) 和 [GitOps 运维手册](doc/operations/runbook.md)。

## 排障提示

- 实时连接失败：检查 `http://localhost:8000/health`、浏览器 WebSocket 地址和 Centrifugo 密钥。
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

## 边界

本项目的重点是高并发直播互动系统的工程实践。它不包含真实视频推流、转码或 CDN；接入 SRS、LiveKit 或云直播服务时，应保持媒体平面与本项目互动平面分离。

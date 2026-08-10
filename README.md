# Live Platform

基于 **Go + Centrifugo + Redis + MySQL + Kafka** 的高并发直播互动系统。当前完整快照：**M0 → M6**。

> M6 目标：让系统从“能运行、能恢复”升级为“能观测、能定位、能追踪”。

## Milestones

### M0 — Foundation
- Go HTTP API、结构化日志、Graceful Shutdown
- MySQL / Redis
- Docker Compose、Makefile

### M1 — Realtime
- Centrifugo + Redis Engine
- Connection JWT / Subscription JWT
- `room:{id}:stream`、`room:{id}:stats`、`personal:user#{id}`
- Realtime event envelope

### M2 — Room & Danmaku
- 注册 / 登录 / Bearer JWT
- 房间生命周期 `PREPARING → LIVING → CLOSED`
- Join / Subscription authorization
- 弹幕、敏感词、Redis 限流、禁言、封禁

### M3 — Like & Viewer Stats
- Redis Like Counter + Delta
- `live-worker` 周期聚合
- Redis ZSET Viewer Session + heartbeat TTL
- 多设备/多 Tab 用户去重
- `room:{id}:stats` 广播

### M4 — Gift & Wallet
- Gifts / Wallet / Wallet Transactions / Gift Orders
- `Idempotency-Key`
- MySQL 本地事务
- 条件扣款，余额不允许小于 0
- 资金流水可审计

### M5 — Kafka & Transactional Outbox
- Kafka KRaft 开发环境
- `live.gift.v1` / `live.danmaku.v1`
- Transactional Outbox
- Claim / Lease / Retry / Backoff
- Gift Consumer 幂等
- 弹幕异步持久化
- `event_id` 客户端去重

### M6 — Observability & Reliability
- Prometheus 自定义业务指标
- API / Worker 独立 `/metrics`
- Centrifugo `/metrics` / `/health`
- Grafana 文件化 Provisioning
- 4 个项目 Dashboard
- OpenTelemetry Trace
- OTel Collector → Tempo
- HTTP → Gift Transaction → Outbox → Kafka → Consumer → Centrifugo 跨异步 Trace
- Trace ID / Span ID 自动进入结构化日志
- Kafka Consumer Lag / Buffered Records
- Outbox Pending / Retry
- 更严格的 API Readiness
- API / Worker Graceful Shutdown
- Centrifugo History + Recovery

---

# M6 架构

```text
                            ┌──────────────┐
                            │    Client    │
                            └──────┬───────┘
                                   │
                    ┌──────────────┴──────────────┐
                    │                             │
                   HTTP                        WebSocket
                    │                             │
                    ▼                             ▼
              ┌──────────┐                 ┌────────────┐
              │ live-api │                 │ Centrifugo │
              └────┬─────┘                 └─────┬──────┘
                   │                             │
        ┌──────────┼──────────┐                 Redis
        │          │          │
       MySQL      Redis      Kafka
        │                     │
        │                     ▼
        │               ┌─────────────┐
        └── Outbox ─────▶│ live-worker │
                         └──────┬──────┘
                                │
                                └──────▶ Centrifugo

Observability:

live-api ─────┐
live-worker ──┼── /metrics ──▶ Prometheus ──▶ Grafana
Centrifugo ───┘

live-api / live-worker
        │ OTLP/gRPC
        ▼
OpenTelemetry Collector
        │
        ▼
      Tempo ───────────────────────────────▶ Grafana Explore
```

---

# Metrics

## HTTP

```text
live_http_requests_total
live_http_request_duration_seconds
```

示例 P99：

```promql
histogram_quantile(
  0.99,
  sum(rate(live_http_request_duration_seconds_bucket{service="live-api"}[5m]))
  by (le, route)
)
```

## Realtime / Engagement

```text
live_danmaku_total{result}
live_likes_total
live_realtime_publish_total{result}
live_stats_broadcast_total{result}
```

## Gift / Outbox

```text
live_gift_orders_total{result}
live_outbox_pending
live_outbox_publish_total{result}
live_outbox_retry_total
```

## Kafka

```text
live_kafka_produce_total{topic,result}
live_kafka_produce_duration_seconds{topic}
live_kafka_consume_total{group,topic,result}
live_kafka_consumer_lag{group,topic,partition}
live_kafka_buffered_records{client,direction}
```

`live_kafka_consumer_lag` 是 Worker 根据本次 Fetch 的 High Watermark 与已处理 offset 计算的近实时估算值，用于项目级运行态观测；生产环境如需平台级权威 Lag，通常还应结合 Kafka 专用 exporter / 管理系统。

## Runtime

两个 Go 进程还暴露：

```text
go_*
process_*
```

用于观察：

- goroutine
- GC
- heap
- RSS
- CPU
- file descriptors

---

# Grafana Dashboards

启动后自动 Provision：

```text
Live Platform · System Overview
Live Platform · Realtime Overview
Live Platform · Gift / Wallet
Live Platform · Kafka / Outbox
```

配置均保存在仓库：

```text
deployments/grafana/
├── provisioning/
│   ├── datasources/
│   └── dashboards/
└── dashboards/
```

不依赖人工在 Grafana UI 中逐个创建。

---

# Distributed Tracing

M6 使用 W3C Trace Context。

普通 HTTP 请求：

```text
HTTP Server Span
    ↓
Business Span
    ↓
MySQL / Redis / Realtime
```

礼物链路会跨越异步边界：

```text
POST Gift
   │
   ▼
HTTP Span
   │
   ▼
gift.transaction
   │
   ├── MySQL
   └── outbox_events.payload.trace
                     │
                     ▼
               Outbox Worker
                     │
                     ▼
            kafka.produce Span
                     │
                     ▼
                  Kafka
                     │
                     ▼
            kafka.consume Span
                     │
                     ▼
          centrifugo.publish Span
```

Outbox 事件 envelope 会保存 Trace Carrier：

```json
{
  "event_id": "...",
  "event_type": "gift.sent",
  "trace": {
    "traceparent": "00-..."
  },
  "payload": {}
}
```

Kafka Producer 创建新 span 后会把新的 trace context 写回消息，因此 Consumer 能继续同一条 Trace。

---

# Trace-aware Logging

`slog` Handler 会从 `context.Context` 自动补：

```json
{
  "level": "INFO",
  "msg": "outbox published",
  "trace_id": "...",
  "span_id": "...",
  "event_id": "..."
}
```

这样可以从日志中的 `trace_id` 进入 Tempo 追踪整条链路。

注意：只有使用 `InfoContext` / `ErrorContext` 等携带业务 Context 的日志才能自动关联 Trace；启动日志等没有请求 Context 的日志不会伪造 trace_id。

---

# Health / Readiness

## live-api

```text
GET /health
GET /ready
GET /metrics
```

`/health`：进程活着即可返回成功。

`/ready` 当前要求：

```text
MySQL OK
Redis OK
Centrifugo Health OK
```

**Kafka 故意不进入 API Readiness。**

原因：

- Gift 通过 Transactional Outbox 可以在 Kafka 故障时正常完成资金事务；
- Danmaku 优先 Centrifugo 实时广播，Kafka 只是旁路持久化；
- MQ 故障不应该把原本可用的 API 实例从流量入口摘除。

## live-worker

```text
GET :9090/health
GET :9090/metrics
```

## Centrifugo

```text
GET :8000/health
GET :8000/metrics
```

开发环境为了方便验证映射到了宿主机；生产环境应通过内网、NetworkPolicy、防火墙或独立 internal port 隔离这些管理端点。

---

# Centrifugo Recovery

`room` Namespace：

```json
{
  "history_size": 100,
  "history_ttl": "30s",
  "force_recovery": true
}
```

目标是解决几十秒以内的短暂断线：

```text
Client receives offset N
      ↓
disconnect
      ↓
N+1 / N+2 / N+3 published
      ↓
reconnect
      ↓
Centrifugo recovery
      ↓
recover missed publications
```

History 仍然只是短期恢复窗口：

```text
Centrifugo History ≠ 持久数据库
Kafka / MySQL       = 长期业务数据
```

---

# Graceful Shutdown

## live-api

收到 SIGTERM：

```text
Stop accepting new HTTP requests
        ↓
Wait inflight requests ≤ 15s
        ↓
Flush OpenTelemetry spans
        ↓
Close dependencies
```

## live-worker

```text
Cancel worker context
       ↓
Stats / Outbox / Consumers stop
       ↓
Wait worker goroutines ≤ 15s
       ↓
Close metrics server
       ↓
Flush traces
```

Kafka Consumer 使用手动 commit；只有处理完成的 record 才会提交 offset。

---

# Local Development

要求：

```text
Docker
Docker Compose
```

建议每个新 Milestone 第一次启动清空开发 volume：

```bash
make compose-reset
```

之后：

```bash
make compose-up
```

浏览器 Demo：

```text
http://localhost:8080/demo
```

---

# Observability URLs

```text
Application Demo     http://localhost:8080/demo
API Metrics          http://localhost:8080/metrics
Worker Metrics       http://localhost:9090/metrics
Centrifugo Metrics   http://localhost:8000/metrics

Grafana              http://localhost:3000
Prometheus           http://localhost:9091
Tempo                http://localhost:3200
```

Grafana 开发环境开启 anonymous Admin 仅为了本地演示，**不能直接用于生产环境**。

---

# M6 Smoke Test

启动：

```bash
make compose-reset
```

然后：

```bash
make smoke-m6
```

测试内容：

```text
API health / ready
Worker health
Centrifugo health
API metrics
Worker metrics
Centrifugo metrics
Prometheus ready
Prometheus scrape targets
Grafana health
Grafana Prometheus datasource
Grafana Tempo datasource
Tempo ready
```

M5 业务回归仍建议执行：

```bash
make smoke-m5
make test-m5-kafka-recovery
make test-m4-concurrency
```

---

# Docker Compose Components

```text
mysql
redis
kafka
kafka-init
centrifugo
live-api
live-worker
prometheus
grafana
otel-collector
tempo
```

---

# Environment Variables Added in M6

```text
WORKER_METRICS_ADDR=:9090
OTEL_ENABLED=true
OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317
OTEL_INSECURE=true
OTEL_SAMPLE_RATIO=1
DEPLOYMENT_ENVIRONMENT=development
```

生产环境可以把：

```text
OTEL_SAMPLE_RATIO
```

降低到合适比例，避免极高流量下全量 Trace 带来的成本。

---

# Source Layout

```text
live-platform/
├── cmd/
│   ├── api/
│   └── worker/
├── internal/
│   ├── auth/
│   ├── danmaku/
│   ├── gift/
│   ├── httpapi/
│   ├── like/
│   ├── mq/
│   ├── observability/       # M6
│   ├── outbox/
│   ├── realtime/
│   ├── room/
│   ├── stats/
│   ├── viewer/
│   └── wallet/
├── deployments/
│   ├── grafana/             # M6
│   ├── otel-collector/      # M6
│   ├── prometheus/          # M6
│   └── tempo/               # M6
├── configs/
├── migrations/
├── scripts/
├── docker-compose.yml
├── Makefile
└── README.md
```

---

# M6 Done Definition

完成 M6 后应能够直接回答：

```text
现在 HTTP QPS 是多少？
HTTP P99 是多少？
Centrifugo 有多少连接？
弹幕失败/拒绝多少？
礼物请求失败多少？
Outbox 是否积压？
Kafka Consumer 是否落后？
Kafka Producer 是否持续失败？
某个礼物请求经过了哪些组件？
某个 Consumer 错误对应哪一个原始 HTTP 请求？
服务 SIGTERM 后是否能正常退出？
```

如果只能看到“CPU / Memory”，但无法回答以上业务问题，就不算完成企业级可观测性建设。

---

# Next — M7

下一阶段进入性能工程：

```text
Go WebSocket Load Generator
10K → 50K → 100K Connections
Hot Room vs Distributed Rooms
Broadcast Amplification
Like Storm
Gift TPS
Slow Consumer
Soak Test
Fault Injection
Hot Room Degradation
Capacity Model
```

M7 所有性能结论必须由真实 Benchmark 产生，禁止预填或编造数据。

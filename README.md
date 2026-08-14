# Live Platform

基于 **Go + Centrifugo + Redis + MySQL + Kafka** 的高并发直播互动系统。当前完整快照：**M0 → M7**。

> M7 目标：用可重复压测与热点治理，把“高并发”从架构描述变成可测量、可降级、可给出容量结论的工程能力。

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

### M7 — Performance Engineering & Hot Room Protection
- 独立 Go WebSocket Load Generator（官方 `centrifuge-go` SDK）
- Connection Sweep：并发连接生成、目标/实际连接速率分离；Round 2 默认先测 10K → 50K
- 单热点房间 vs 多房间分布对比
- Server-side Broadcast 并发限速压测、目标/实际 publish rate 与 fan-out delivery rate
- Slow Consumer 模拟，Fast / Slow 客户端延迟与断线分开统计
- 通用 HTTP Load Generator（Like / Gift），报告记录 target/achieved rate、并发、URL、body、token 数
- Soak Test / Fault Injection 脚本
- NORMAL / HOT / PROTECT 热点房间模式
- 基于 Viewer Count + Danmaku Rate 的实时降级
- 确定性弹幕采样，关键消息不降级
- Realtime 消息优先级标记：Gift P1 / Danmaku P3 / Stats P4（用于分类和后续策略，不宣称 Centrifugo 提供通用优先级队列）
- Centrifugo client queue 限制，保护服务端免受慢消费者无限堆积
- `benchmark/` 保存原始真实性能报告；`round1-findings.md` 记录第一轮结论与必须重测项

---

# M7 性能工程

## 热点房间治理

弹幕在完成鉴权、房间状态、禁言、用户限流和敏感词检查后，会进入 Traffic Policy：

```text
Viewer Count + Danmaku Rate
            │
            ▼
      NORMAL / HOT / PROTECT
            │
     ┌──────┴──────┐
     ▼             ▼
 broadcast       sampled
     │
     ▼
 Centrifugo
```

当前默认策略同时保留 Viewer / 请求速率安全阈值，并新增 Round 2 基准得到的 fan-out 容量阈值：

```text
HOT:     viewer >= 50,000 OR raw rate >= 500/window OR estimated fan-out >= 30K/s
PROTECT: viewer >= 100,000 OR raw rate >= 2,000/window OR estimated fan-out >= 40K/s

adaptive target fan-out: 25K/s
minimum sample rate:      5%
```

当进入 HOT / PROTECT 后，默认不再固定丢 50% / 80%，而是按 `target_fanout / estimated_fanout` 动态计算采样率。`DANMAKU_ADAPTIVE_ENABLED=false` 时才回退到旧的 50% / 20% 固定采样，主要用于 A/B 与回滚。上述 fan-out 数字来自当前 Round 2 测试环境，不是 Centrifugo 的通用上限。采样仍基于 `message_id` hash，保证多 API 实例对同一事件做出稳定决策。

可先执行 `make m7-degradation-smoke`，脚本会临时把速率阈值降低到 2/4，用真实 HTTP 弹幕链路依次触发 NORMAL → HOT → PROTECT，验收后自动恢复默认阈值。这个 Smoke 只验证降级逻辑，不代表生产阈值。

Prometheus 新增：

```text
live_danmaku_degradation_total{mode,action}
```

其中 `mode` 为 `NORMAL/HOT/PROTECT`，`action` 为 `broadcast/sampled`。

## 慢消费者保护

Centrifugo 配置：

```json
{
  "client": {
    "queue_max_size": 262144
  }
}
```

当前 M7 压测配置把单连接发送队列限制为 256 KiB；这不是生产推荐值，最终应根据消息大小、可接受积压和断连策略压测确定。slow-consumer 场景会故意阻塞部分客户端的 SDK publication callback，验证慢客户端是否被隔离，以及服务端内存是否保持可控。

## WebSocket Load Generator

工具目录：

```text
tools/loadtest/
```

该工具使用独立 Go module 和 Centrifugal 官方 Go Client SDK。它直接生成开发环境 JWT，不需要为 100K 虚拟客户端创建 100K 条 MySQL 用户数据，因此适合测量 Centrifugo 自身连接/订阅/广播容量。

示例：

```bash
cd tools/loadtest
go run . \
  --scenario hot-room \
  --clients 10000 \
  --rooms 1 \
  --connect-rate 3000 \
  --connect-concurrency 256 \
  --publish-rate 100 \
  --publish-concurrency 64 \
  --message-bytes 256 \
  --duration 60s \
  --report ../../reports/m7/hot-room.json
```

报告现在区分**目标负载**与**实际达到的负载**：

```text
connect_rate_target_per_sec / connect_rate_actual_per_sec
connection_success_rate
initial_connected / reconnect_events / connected_current
publish_rate_target_per_sec / publish_rate_actual_per_sec
fanout_delivery_actual_per_sec
message_target_bytes
fast / slow publication count
fast / slow disconnect count
fast / slow latency P50 / P95 / P99 / Max
recovery attempts / recovered
Load Generator hostname / CPU count / Go version
```

连接与 publish 都采用“限速 + 并发上限”模式，避免串行压测器本身先成为瓶颈。`connected_current` 使用每客户端原子状态维护，重连不会重复增加当前连接数。

## HTTP Load Generator

```text
tools/httpload/
```

用于 Like / Gift 等 HTTP 热路径，支持固定请求速率、并发上限、单 Bearer Token / token 文件 round-robin、唯一 Idempotency-Key、状态码分布和 P50/P95/P99。报告同时记录 target rate、achieved rate、concurrency、request body、token 数量和 Load Generator 环境，避免 JSON 离开脚本后失去测试语义。

## 推荐执行顺序

第一轮报告已经放入 `benchmark/raw/round1/`，分析见 `benchmark/round1-findings.md`。Round 2 按已发现瓶颈定向执行：

```bash
# 1. 单钱包热点 vs 多钱包并发，并保留 MySQL/InnoDB 快照
make m7-gift-compare

# 2. Hot Room：10→20→30→40→50 publish/s；再测 1K→2K→5K subscribers
make m7-hotroom-ladder

# 3. Like：20K→50K→100K logical likes/s
ROOM_ID=... TOKEN=... make m7-like-ladder

# 4. 修复统计后的 slow consumer 重测
make m7-slow-consumer

# 5. 最后再把纯连接推进到 10K→50K
make m7-connection-sweep

DURATION=6h make m7-soak
make m7-snapshot
make m7-fault
```

Round 2 默认 Connection Sweep 暂时是：

```text
10K → 50K
```

只有确认 Load Generator 的 `connect_rate_actual_per_sec`、CPU、FD 和 ephemeral port 都不是瓶颈后，再继续 100K。

多份 JSON 可以直接汇总成 Markdown 表：

```bash
python3 scripts/m7_report_table.py reports/m7/hotroom-ladder/*.json
python3 scripts/m7_report_table.py reports/m7/gift-single-wallet.json reports/m7/gift-multi-wallet.json
```

如果单台压测机的 ephemeral ports、file descriptors、CPU 或网络先成为瓶颈，应使用多台 Load Generator，而不是把客户端机器瓶颈误判成服务端瓶颈。

## Round 1 已确认的问题与 Round 2 修正

第一轮真实报告显示：

- 5K WebSocket 可以稳定保持，但旧工具没有记录 actual connect rate；
- 1K clients / 50 publish/s 下，100 rooms 的 P99 约 4.61ms，而单 Hot Room P99 约 277ms；
- Slow Consumer 出现 32s 最大延迟，但旧 `connected_current` 会被 reconnect 重复累加；
- Like 约 20K logical likes/s 时 P99 约 11ms；
- Gift 单 token 场景 P99 约 2.41s，但它只能证明单 wallet 热点，不能代表系统 Gift TPS。

因此 Round 2 不直接“优化数据库”或“扩机器”，而是先修测量方法并拆分变量。Gift Repository 现在额外生成 `gift.db.order_insert`、`gift.db.wallet_update`、`gift.db.outbox_insert`、`gift.db.commit` Span，用于在 Tempo 中判断尾延迟具体发生在哪一步。

## Benchmark 数据纪律

`benchmark/` 同时保存原始真实报告、阶段分析和最终容量模板。仓库不会出现类似“单机 100K / P99 30ms”这种未经真实实验的数据。

最终只有真实执行后才能填写：

```text
C_conn = 单节点安全连接容量
C_msg  = 单节点安全消息吞吐
C_bw   = 单节点安全网络吞吐

C_actual = min(C_conn, C_msg, C_bw)
```

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

# M7 Benchmark Execution

当前阶段进入性能工程：

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

M7 所有性能结论必须由真实 Benchmark 产生，禁止预填或编造数据。完成容量验收后，下一阶段进入 M8 集群部署与最终交付。


# M7 Environment Variables

```text
DANMAKU_USER_RATE_LIMIT=5
DANMAKU_USER_RATE_WINDOW=10s
DANMAKU_HOT_VIEWERS=50000
DANMAKU_PROTECT_VIEWERS=100000
DANMAKU_HOT_RATE=500
DANMAKU_PROTECT_RATE=2000
DANMAKU_RATE_WINDOW=1s
DANMAKU_ADAPTIVE_ENABLED=true
DANMAKU_TARGET_FANOUT_RATE=25000
DANMAKU_HOT_FANOUT_RATE=30000
DANMAKU_PROTECT_FANOUT_RATE=40000
DANMAKU_MIN_SAMPLE_RATE=0.05
DANMAKU_HOT_SAMPLE_RATE=0.5        # legacy fallback
DANMAKU_PROTECT_SAMPLE_RATE=0.2    # legacy fallback
GIFT_MAX_COUNT_PER_REQUEST=100
GIFT_USER_RATE_LIMIT=10
GIFT_USER_RATE_WINDOW=1s
```

# M7 Done Definition

M7 的代码交付完成不代表已经获得容量数字。真正完成压测验收时应同时具备：

- 可重复执行的 Connection / Broadcast / Slow Consumer / HTTP / Soak / Fault 场景；
- Grafana/Prometheus 对 CPU、Memory、Network、P99、Redis、Kafka、MySQL 的同期观测；
- HOT/PROTECT 降级在压测中实际触发；
- 压测客户端本身不存在明显资源瓶颈，或已采用多机压测；
- `benchmark/` 中填写的是实际数据而不是估算值；
- 给出最大容量、建议安全容量以及首要瓶颈。

## M7 Optimization Round

Round 2 benchmarking identified two concrete limits in the current test environment: hot-channel fan-out begins a clear tail-latency knee around 30K–40K deliveries/s, while a single wallet row saturates near 90 strong-consistency gift transactions/s under pathological same-account load. See `benchmark/round2-findings.md` for retained evidence.

The optimization round adds:

- adaptive danmaku sampling using estimated fan-out, with benchmark-derived but configurable 25K target / 30K HOT / 40K PROTECT defaults;
- per-user Gift rate limiting (`GIFT_USER_RATE_LIMIT`, default 10/s);
- bounded Gift combo requests (`GIFT_MAX_COUNT_PER_REQUEST`, default 100);
- browser-side 300 ms Gift click aggregation;
- idempotent Gift replay before rate limiting;
- A/B hot-room benchmark and multi-wallet platform Gift ladder.

Run:

```bash
make m7-gift-optimization-smoke
make m7-hotroom-adaptive-ab
make m7-gift-platform-ladder
```

For raw DB-capacity tests, `m7-gift-compare` and `m7-gift-platform-ladder` temporarily raise the per-user Gift limiter and restore the normal API configuration when finished. Do not use those relaxed limits as production defaults.

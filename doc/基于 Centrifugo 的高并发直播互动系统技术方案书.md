# 基于 Centrifugo 的高并发直播互动系统技术方案书

**文档版本：V1.0**  
**系统名称：Live Interactive Platform**  
**技术方向：Go + Centrifugo + Redis + Kafka + MySQL**  
**适用场景：直播弹幕、点赞、虚拟礼物、在线状态及实时互动**

---

## 1. 项目背景

直播业务具有明显的高并发、低延迟和突发流量特征。在大型活动、热门主播或赛事直播场景下，单直播间可能同时存在大量 WebSocket 长连接，并持续产生弹幕、点赞、礼物等互动事件。

传统业务服务直接维护大量 WebSocket 连接，会导致连接管理、广播扩散、服务扩缩容和故障恢复与核心业务逻辑高度耦合。

本方案引入 **Centrifugo 作为实时消息基础设施**，将“连接管理与消息广播”和“直播业务处理”进行解耦。

Centrifugo 本身定位为可自托管的实时消息服务器，负责客户端长连接以及基于 Channel 的发布订阅，可通过 Redis Engine 组成多节点实时消息集群。

---

# 2. 建设目标

系统主要建设目标如下：

1. 支持大规模 WebSocket 长连接；
2. 支持热门直播间高并发弹幕；
3. 支持高频点赞并控制广播放大；
4. 支持可靠、幂等的虚拟礼物交易；
5. 支持客户端短暂断线后的消息恢复；
6. 支持 Centrifugo、业务服务横向扩容；
7. 支持 Redis、Kafka、MySQL 等基础设施故障恢复；
8. 建立完整监控、告警及压测体系。

建议一期将以下指标作为**压测目标而非软件能力承诺**：

- 单直播间在线用户：100,000+
- 平台 WebSocket 并发连接：300,000+
- 互动请求峰值：10,000 QPS+
- 普通实时消息端到端 P99：≤ 300 ms
- 核心服务可用性目标：≥ 99.95%
- 礼物订单：不重复扣款、不丢订单、可追溯

具体指标最终根据部署规格与压测结果确定。

---

# 3. 设计原则

## 3.1 实时链路与业务链路分离

Centrifugo 只负责：

- WebSocket 连接维护；
- Channel Subscription；
- 消息广播；
- 短期消息 History；
- Connection Presence；
- 消息恢复。

用户、直播间、钱包、礼物订单、风控等状态仍由业务系统维护。

Centrifugo 官方同样建议将 History 看作短期、有限的消息缓存，而非最终数据源，核心数据库仍应作为业务状态的 Source of Truth。

---

## 3.2 媒体流与互动流分离

整个直播系统划分为两条独立链路。

### 媒体链路

```text
主播
 ↓
推流服务
 ↓
媒体服务器
 ↓
转码 / 录制
 ↓
CDN
 ↓
观众播放器
```

负责：

- 视频；
- 音频；
- 转码；
- 推拉流；
- CDN 分发。

### 互动链路

```text
用户
 ↓
Centrifugo / Go Backend
 ↓
Redis / Kafka / MySQL
 ↓
Centrifugo
 ↓
直播间用户
```

负责：

- 弹幕；
- 点赞；
- 礼物；
- 在线状态；
- 系统通知。

两套系统独立扩容，避免媒体带宽压力影响互动服务。

---

# 4. 总体架构

```text
                         ┌────────────────┐
                         │     Client     │
                         │ Web / App / PC │
                         └───────┬────────┘
                                 │
                   ┌─────────────┴─────────────┐
                   │                           │
              WebSocket                    HTTP/RPC
                   │                           │
                   ▼                           ▼
          ┌────────────────┐          ┌────────────────┐
          │ Load Balancer  │          │  API Gateway   │
          └───────┬────────┘          └───────┬────────┘
                  │                           │
        ┌─────────┼─────────┐                 ▼
        ▼         ▼         ▼        ┌──────────────────┐
      CF-1      CF-2      CF-N       │   Go Backend     │
        │         │         │         │                  │
        └─────────┼─────────┘         │ Auth             │
                  │                   │ Room             │
                  ▼                   │ Danmaku          │
              Redis Cluster           │ Like             │
                                      │ Gift             │
                                      │ Wallet           │
                                      └───────┬──────────┘
                                              │
                           ┌──────────────────┼───────────────┐
                           ▼                  ▼               ▼
                         Redis              Kafka           MySQL
                           │                  │               │
                           │                  ▼               │
                           │              Worker             │
                           │                  │               │
                           └──────────────────┴───────────────┘
                                              │
                                              ▼
                                        Centrifugo
                                              │
                                              ▼
                                           Client
```

Centrifugo 使用 Redis Engine 后，不同 Centrifugo 节点可通过 Redis PUB/SUB 协同工作，客户端无须固定连接至某个节点；History 和 Presence 也可以存放在 Redis 中。

---

# 5. 系统模块划分

一期不建议直接建设大量微服务。

采用 **模块化单体 + Worker**：

```text
cmd/
├── api/
└── worker/

internal/
├── auth/
├── room/
├── danmaku/
├── like/
├── gift/
├── wallet/
├── realtime/
├── repository/
└── infrastructure/
```

实际部署服务：

```text
live-api
live-worker
centrifugo
redis
kafka
mysql
```

待业务规模及团队规模扩大后，再逐步拆分：

```text
Room Service
Danmaku Service
Gift Service
Wallet Service
Realtime Service
Risk Control Service
```

避免项目初期产生不必要的分布式事务、服务发现和运维复杂度。

---

# 6. 实时 Channel 设计

不建议为每一种事件创建一个 Channel。

推荐：

```text
room:{roomId}:stream
room:{roomId}:stats
user:{userId}
```

## room:{roomId}:stream

传递需要立即展示的直播间事件：

```json
{
  "type": "danmaku",
  "data": {}
}
```

```json
{
  "type": "gift",
  "data": {}
}
```

```json
{
  "type": "system",
  "data": {}
}
```

## room:{roomId}:stats

传递状态型事件：

```json
{
  "type": "stats",
  "viewer_count": 82931,
  "like_count": 8273612
}
```

这类消息只关心最新状态，可以允许覆盖或者丢失部分中间状态。

## user:{userId}

用户私有 Channel：

- 礼物发送结果；
- 余额变化；
- 用户封禁；
- 系统通知；
- 风控提醒。

---

# 7. 弹幕系统设计

弹幕特点：

- 高频；
- 小消息；
- 延迟敏感；
- 可接受极少量消息丢失；
- 必须具备风控与限流能力。

处理链路：

```text
Client
   │
   ▼
Danmaku API / RPC
   │
   ├── 用户认证
   ├── 房间状态校验
   ├── 禁言校验
   ├── 敏感词过滤
   ├── 用户级限流
   └── 房间级限流
          │
          ▼
      Centrifugo
          │
          ▼
       房间广播

同时：

Danmaku Service
       │
       ▼
      Kafka
       │
       ▼
Danmaku Consumer
       │
       ▼
     MySQL
```

实时广播与数据库持久化解耦。

禁止采用：

```text
INSERT MySQL
     ↓
数据库成功
     ↓
实时广播
```

否则数据库延迟将直接进入用户实时链路。

---

# 8. 点赞系统设计

点赞是典型的**超高频、低价值单事件**。

如果一次点击对应一次全直播间广播，容易形成严重消息放大。

例如：

```text
10,000 次点赞
       ↓
每次广播给 100,000 用户
       ↓
理论产生巨大下行消息量
```

因此采用：

**Redis Counter + 时间窗口聚合。**

```text
Client
 ↓
Like API
 ↓
Redis INCR
 ↓
Like Aggregator
 ↓
100~500ms 聚合
 ↓
Centrifugo
 ↓
Client
```

例如 200ms 内产生：

```text
1837 次 LIKE
```

最终只产生一次：

```json
{
  "type": "like",
  "delta": 1837,
  "total": 8273612
}
```

将大量写事件转换成低频状态广播。

Redis 中维护：

```text
live:room:{id}:like_total
live:room:{id}:like_delta
```

MySQL 仅周期性持久化最终统计数据。

---

# 9. 礼物系统设计

礼物属于资产类业务，设计原则与弹幕完全不同。

必须满足：

- 幂等；
- 不重复扣款；
- 不丢订单；
- 可审计；
- 可重试；
- 最终广播可靠。

核心链路：

```text
Client
   │
   ▼
Gift Service
   │
   ├── 参数校验
   ├── request_id 幂等
   ├── 礼物状态校验
   ├── 余额检查
   │
   ▼
MySQL Transaction
   │
   ├── 扣减账户余额
   ├── 创建 gift_order
   ├── 记录账户流水
   └── 创建 outbox_event
           │
         COMMIT
           │
           ▼
     Outbox Publisher
           │
           ▼
         Kafka
           │
           ▼
     Gift Consumer
           │
           ▼
      Centrifugo
           │
           ▼
       直播间广播
```

礼物数据库核心表：

```text
gift_order
------------------------
id
request_id
user_id
anchor_id
room_id
gift_id
count
unit_price
total_amount
status
created_at
updated_at
```

其中：

```text
UNIQUE(request_id)
```

用于防止客户端超时重试导致重复扣款。

同时建立：

```text
wallet
wallet_transaction
gift_order
outbox_event
```

形成完整资金流水。

可靠事件发布采用 **Transactional Outbox** 思路，将业务数据修改和待发送事件放入同一本地数据库事务，再异步投递 Kafka，避免“数据库成功但实时广播失败”产生状态不一致。Centrifugo 官方文档也提供了 Transactional Outbox/CDC 与实时广播结合的实现思路。

---

# 10. 鉴权设计

用户首先登录业务系统：

```text
Client
 ↓
Auth Service
 ↓
JWT
 ↓
Client
 ↓
Centrifugo
```

JWT 中至少包含：

```json
{
  "sub": "10086",
  "exp": 1786330000
}
```

Centrifugo 原生支持由应用后端签发 JWT 完成客户端连接认证。

对于直播间 Subscription，应额外判断：

- 房间是否存在；
- 是否需要付费；
- 用户是否被封禁；
- 用户是否拥有观看权限。

Centrifugo 同时支持 connect、subscribe、publish、RPC 等请求通过 HTTP/GRPC Proxy 转发至业务服务，因此高频互动入口也可以通过 RPC Proxy 接入 Go 服务。

生产环境优先采用：

**JWT Connection Authentication + Go RPC/HTTP Business API**

避免每次 WebSocket 重连均请求用户 Session 服务。Centrifugo 官方指出，有效 JWT 可以在客户端重新连接时复用，因此在大规模重连场景下能够减少认证后端压力。

---

# 11. 消息恢复设计

直播用户可能由于：

- 手机网络切换；
- Wi-Fi 波动；
- Centrifugo 发布；
- Load Balancer 更新；

产生短暂掉线。

对于弹幕 Channel 开启短期 History：

```text
history_size = 100~500
history_ttl  = 30~60s
```

实际值通过压测确定。

Centrifugo History 会为消息维护 offset，并支持客户端重新订阅后恢复短时间内遗漏的消息。Redis Broker 下 History 可以存放在 Redis Stream 中。

因此：

```text
Client disconnect

18271
 ↓
18272
18273
18274
18275

Client reconnect
 ↓
recover
 ↓
18272~18275
```

但必须坚持：

```text
Centrifugo History
       =
短期消息恢复

Kafka / MySQL
       =
长期业务数据
```

History 不承担永久消息存储职责。

---

# 12. 高并发与热点直播间设计

系统最大风险不是 WebSocket 连接本身，而是**热门 Channel 的消息放大效应**。

例如：

```text
100,000 在线用户
         ×
100 条弹幕 / 秒
```

意味着极大的下行消息投递量。

因此采用以下措施。

### 12.1 客户端限流

限制单用户：

```text
弹幕 ≤ N 条 / 秒
点赞请求合并
礼物请求独立处理
```

### 12.2 房间级限流

为直播间建立 Token Bucket：

```text
room:{id}:danmaku
```

当热门直播间超出广播能力时进入降级。

### 12.3 弹幕采样

普通直播间：

```text
100% 广播
```

超级热门直播间：

```text
部分普通弹幕采样
+
礼物 / 主播 / 管理员消息优先
```

### 12.4 消息优先级

建议：

```text
P0 礼物 / 系统消息
P1 主播 / 管理员消息
P2 普通弹幕
P3 点赞动画
```

出现压力时优先丢弃低价值消息。

### 12.5 点赞聚合

通过时间窗口大幅减少消息数量。

### 12.6 Redis 水平扩展

Centrifugo Redis Engine 支持多节点部署，也支持 Redis Cluster 以及应用级 Redis Sharding；但官方同时提醒，Redis Cluster 的 PUB/SUB 并不会随着节点增加而简单线性扩展，因此超级热点直播间仍必须通过实际压力测试验证架构。

这也是本项目必须进行真实压测，而不能只根据理论连接数估算容量的原因。

---

# 13. 在线人数设计

不直接将 WebSocket Connection Count 作为直播间业务在线人数。

原因包括：

```text
同一用户多个浏览器 Tab
同一用户多个设备
客户端异常断线
网络抖动
```

因此采用：

```text
Centrifugo Presence
        +
Redis Viewer Counter
        +
周期校准
```

Redis：

```text
live:room:{id}:viewer_count
```

业务展示人数使用 Redis 聚合结果。

Presence 主要承担实时连接状态辅助判断。

---

# 14. Redis 设计

Redis 分为两个逻辑用途。

### Centrifugo Redis

负责：

- PUB/SUB；
- History；
- Presence。

### Business Redis

负责：

- 点赞计数；
- 在线人数；
- 限流；
- 热点直播间信息；
- 用户临时状态。

生产环境建议根据规模将：

```text
Centrifugo Redis

和

Business Redis
```

分开部署，避免业务大 Key 或高频 INCR 影响实时消息广播。

Centrifugo 本身也允许将 Broker 与 Presence Manager 使用不同的后端配置，为进一步隔离实时消息基础设施提供空间。

---

# 15. Kafka 设计

Kafka 主要承担：

```text
业务削峰
异步持久化
服务解耦
礼物事件
弹幕日志
行为分析
数据统计
```

Topic 示例：

```text
live.danmaku
live.gift
live.room.event
live.user.behavior
```

Partition Key 建议：

```text
room_id
```

使同一个直播间事件尽可能进入固定 Partition，从而方便维护房间级事件顺序。

礼物等关键消费端必须实现：

```text
Idempotent Consumer
```

不能假设 MQ 消息永远只消费一次。

---

# 16. MySQL 数据设计

核心业务表：

```text
users
live_rooms
live_sessions

gifts
gift_orders

wallets
wallet_transactions

danmaku_records

outbox_events
```

其中资产修改统一采用事务。

禁止：

```text
SELECT balance
↓
Go 中 balance -= amount
↓
UPDATE
```

应通过事务、条件 UPDATE 或账户流水模型避免并发超卖余额。

---

# 17. 高可用设计

## Centrifugo

```text
Load Balancer
      │
 ┌────┼────┐
 ▼    ▼    ▼
CF1  CF2  CF3
      │
      ▼
 Redis
```

节点无状态化，通过水平扩容提升 Connection Capacity。

## Go Backend

```text
Load Balancer
      │
 ┌────┼────┐
 ▼    ▼    ▼
API1 API2 API3
```

业务状态全部外置到 Redis/MySQL/Kafka。

## 故障策略

### Centrifugo 节点故障

Client 自动重连其他节点。

### Redis 故障

启用 Redis Sentinel / Cluster 等高可用机制，并验证 failover 行为。

### Kafka 故障

业务 Producer 重试；Outbox 保留未发布事件。

### MySQL 故障

采用主从/高可用数据库，并保证资金类业务故障时优先拒绝请求，而非错误扣款。

---

# 18. 安全设计

系统至少实施：

- JWT Authentication；
- Channel Authorization；
- API Rate Limit；
- WAF；
- IP Rate Limit；
- 用户 Rate Limit；
- Room Rate Limit；
- 敏感词审核；
- 礼物接口幂等；
- 管理员操作审计；
- 钱包流水审计；
- WebSocket Origin 校验；
- Redis/MySQL/Kafka 内网访问。

Centrifugo 官方也特别提醒 WebSocket 场景需要正确限制 Allowed Origins，否则不正确的跨域配置可能产生安全风险。

---

# 19. 可观测性建设

统一采用：

```text
Prometheus
+
Grafana
+
日志系统
+
Tracing
```

重点监控：

```text
centrifugo_connections
centrifugo_message_rate
centrifugo_node_cpu
centrifugo_memory

redis_ops
redis_latency
redis_memory

kafka_lag

api_qps
api_p99
api_error_rate

danmaku_qps
gift_qps
like_qps
```

Centrifugo 可直接暴露 Prometheus Metrics Endpoint 接入现有监控系统。

重点告警：

```text
Connection 突降
消息延迟升高
Redis latency 升高
Kafka Consumer Lag
Gift Error Rate
MySQL Transaction Error
系统 CPU / Memory
```

---

# 20. 压测方案

本系统上线前必须开展分层压测。

## 第一阶段：连接压测

测试：

```text
10K
50K
100K
300K
```

WebSocket Connections。

记录：

- CPU；
- Memory；
- 网络带宽；
- Connection Latency；
- Reconnect Rate。

## 第二阶段：弹幕压测

模拟：

```text
100,000 在线用户
+
1,000 / 5,000 / 10,000 弹幕请求每秒
```

观察：

```text
P50
P95
P99
Redis CPU
Centrifugo CPU
Network Throughput
```

## 第三阶段：点赞风暴

测试聚合前后：

```text
100,000 Like/s
```

验证消息聚合带来的广播数量下降。

## 第四阶段：礼物压测

重点验证：

```text
并发扣款
重复 request_id
网络超时
Kafka 重复消费
服务 Crash
```

保证：

```text
不会重复扣款
不会生成重复订单
账户流水一致
```

## 第五阶段：故障演练

主动执行：

```text
Kill Centrifugo
Kill Redis Node
Kill Kafka Broker
Kill Worker
重启 Go API
Load Balancer Reload
```

观察系统恢复情况。

---

# 21. 项目实施计划

## Phase 1：MVP

完成：

```text
用户认证
直播间
Centrifugo
WebSocket
弹幕
点赞
基础礼物
Docker Compose
```

---

## Phase 2：可靠性建设

增加：

```text
Kafka
Wallet
Gift Order
Transactional Outbox
消息幂等
History Recovery
```

---

## Phase 3：高并发建设

增加：

```text
Centrifugo Cluster
Redis Cluster
限流
点赞聚合
热门房间治理
消息优先级
弹幕降级
```

---

## Phase 4：生产能力建设

增加：

```text
Kubernetes
Prometheus
Grafana
Tracing
CI/CD
故障演练
容量规划
```

---

# 22. 技术选型总结

```text
Backend
    Go

Realtime Gateway
    Centrifugo

Realtime Broker
    Redis

Message Queue
    Kafka

Database
    MySQL

Authentication
    JWT

Container
    Docker

Orchestration
    Kubernetes

Monitoring
    Prometheus + Grafana

CI/CD
    GitHub Actions / 企业流水线
```

---

# 23. 核心架构价值

本方案不将 Centrifugo 简单作为 WebSocket Server 使用，而是将整个系统划分为：

```text
连接层
↓
实时消息层
↓
业务计算层
↓
异步事件层
↓
数据持久层
```

同时根据业务价值采用不同一致性策略：

| 场景 | 技术策略 | 一致性 |
|---|---|---|
| 弹幕 | Centrifugo + Kafka | 实时优先 |
| 点赞 | Redis + 聚合广播 | 最终一致 |
| 在线人数 | Redis 聚合 | 最终一致 |
| 礼物 | MySQL + Outbox + Kafka | 强业务一致 |
| 短暂断线 | Centrifugo History | 短期恢复 |
| 历史记录 | Kafka + MySQL | 持久化 |

最终形成一套能够水平扩展、支持流量削峰、具备资产一致性和故障恢复能力的企业级直播互动系统。

---

# 24. 后续重点技术课题

项目进入高并发阶段后，应重点研究：

**超级热点直播间治理。**

核心问题不是简单增加 Centrifugo 节点，而是：

```text
单热点 Channel
       ↓
消息广播放大
       ↓
Redis PUB/SUB 压力
       ↓
Centrifugo CPU
       ↓
网络带宽
       ↓
Client 消费速度
```

因此下一阶段需要进一步设计：

```text
热点 Channel 分片
弹幕采样
优先级队列
慢消费者治理
消息背压
区域化部署
跨区域消息分发
容量模型
```

该部分将作为系统从“可运行”升级到“真正高并发直播架构”的核心技术工作。
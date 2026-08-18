# M7 第一轮发现

> 数据来源：`benchmark/raw/round1/*.json`。这是第一轮真实压测记录。机器规格与 Docker 资源限制尚未随报告提供，因此当前结论只描述**该测试环境下的观测结果**，不能外推为生产容量。

## 1. Connection baseline

| Clients | Rooms | Initial result | Connect errors | Subscription errors |
|---:|---:|---|---:|---:|
| 1,000 | 100 | 1,000 connected | 0 | 0 |
| 5,000 | 100 | 5,000 connected | 0 | 0 |

第一轮可以确认测试环境稳定维持 5,000 个连接，但旧版 Load Generator 是串行创建客户端，报告中的 `connect_rate_per_sec` 是**目标配置**而不是实际连接建立速率，因此不能据此声称服务端达到对应 connection/s。

M7 Round 2 已将连接创建改为：

- target rate pacing；
- configurable connect concurrency；
- initial connection 与 reconnect 分离；
- `connect_rate_actual_per_sec`；
- `connection_success_rate`；
- unique `connected_current`。

因此 Connection Capacity 必须使用新工具重测。

## 2. Hot room vs distributed rooms

第一轮相同条件：1,000 clients、目标 50 publish/s。

| Scenario | Rooms | P50 | P95 | P99 | Max | Publications received |
|---|---:|---:|---:|---:|---:|---:|
| Distributed | 100 | 1.934 ms | 2.912 ms | 4.610 ms | 23.005 ms | 29,990 |
| Hot room | 1 | 44.338 ms | 163.362 ms | 277.031 ms | 977.065 ms | 2,216,000 |

这证明单热点 Channel 的 fan-out 会显著增加用户侧接收延迟，是第一轮最明确的实时链路压力点。

但旧版 publisher 使用**串行 HTTP publish**。当一次 publish 本身变慢后，压测器也无法稳定维持目标 publish rate。因此旧报告中的 `publish_success / duration` 不能直接解释为 Centrifugo 的最大 publish throughput。

M7 Round 2 已改为并发 rate-limited publisher，并新增：

- `publish_rate_target_per_sec`；
- `publish_rate_actual_per_sec`；
- `publish_concurrency`；
- `fanout_delivery_actual_per_sec`；
- message payload target bytes。

下一轮用 `make m7-hotroom-ladder` 找真正的 P99 拐点。

## 3. Slow consumer

第一轮 1,000 clients、10% slow consumers、目标 50 publish/s：

- P50: 37.587 ms
- P95: 135.442 ms
- P99: 223.408 ms
- Max: 32.065 s
- recovery attempts: 107
- recovered success: 7

其中旧报告出现 `clients=1000` 但 `connected_current=1188`，这是压测器统计 Bug：重连成功被再次累计为当前连接，而临时断线没有与之对称扣减。

Round 2 已用 per-client atomic state 修复，并把 fast / slow 客户端分别统计：

- publication count；
- disconnect count；
- P50/P95/P99/Max。

因此旧 Slow Consumer 的最大延迟仍是有价值的压力信号，但 `connected_current` 不可用于容量结论。

## 4. Like

第一轮：

- sent: 11,986 requests / ~60s
- HTTP 202: 11,985
- P50: 3.256 ms
- P95: 5.512 ms
- P99: 11.126 ms

请求体为 `count=100`，因此该轮相当于约 20K logical likes/s。当前未观察到明显尾延迟问题。

下一轮执行 20K → 50K → 100K logical likes/s 阶梯压测，并同时观察 Redis / stats broadcast。

## 5. Gift

第一轮：

- sent: 2,933 requests / ~60s
- HTTP 200: 2,931
- P50: 25.051 ms
- P95: 829.908 ms
- P99: 2.410 s

尾延迟已经明显恶化，但旧脚本只使用一个 Bearer Token，因此该测试本质上是**单 wallet row 热点**，不能代表平台整体 Gift TPS。

Round 2 增加：

1. single-wallet hotspot；
2. multi-wallet round-robin；
3. MySQL/InnoDB snapshot；
4. `gift.db.wallet_update` 等 OTel child spans。

只有当 single-wallet 的 `wallet_update` span / InnoDB lock wait 显著高于 multi-wallet，才能确认 MySQL wallet row lock 是主要原因。

## 6. 第二轮执行顺序

```text
1. make m7-gift-compare
2. make m7-hotroom-ladder
3. ROOM_ID=... TOKEN=... make m7-like-ladder
4. make m7-slow-consumer
5. make m7-connection-sweep
```

完成 Round 2 之前，不进入 M8，也不填写最终 Capacity 数字。

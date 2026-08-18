# M7 优化轮次

## 已实现改动

### 礼物保护

- 按用户的 Redis 固定窗口限流器（`GIFT_USER_RATE_LIMIT`、`GIFT_USER_RATE_WINDOW`）。
- 幂等重放查询在限流前执行，因此已提交请求的重试仍会返回原订单。
- `GIFT_MAX_COUNT_PER_REQUEST` 限制连击数量（默认 100）。
- 浏览器演示包含 300 ms 礼物连击聚合器，最多 100 次点击合并为一笔事务。
- `live_gift_rate_limited_total` 暴露被拒绝请求。

### 自适应弹幕扇出

策略估算：

`estimated_fanout_per_sec = viewer_count × observed_danmaku_rate_per_sec`

压测推导的默认设置：

- 目标扇出：25,000 次投递/s
- HOT：30,000 次投递/s
- PROTECT：40,000 次投递/s
- 最低采样率：5%

HOT/PROTECT 生效时，有效采样率约为：

`target_fanout / estimated_fanout`

clamped to `[DANMAKU_MIN_SAMPLE_RATE, 1]`.

`DANMAKU_ADAPTIVE_ENABLED=false` 时仍可使用旧版固定比例，便于 A/B 测试和回滚。

## 验证命令

```bash
make m7-gift-optimization-smoke
make m7-hotroom-adaptive-ab
make m7-gift-platform-ladder
```

A/B 压测应在优化前后使用相同硬件执行。容量阈值保持可配置，因为它们取决于主机、网络、Centrifugo 与 Redis 拓扑。

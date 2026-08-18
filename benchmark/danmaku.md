# M7 弹幕降级压测

## 策略

- NORMAL：100% 广播
- HOT：配置的确定性采样率
- PROTECT：配置的确定性采样率

## 结果

| 模式 | 接受输入/s | 广播/s | 采样/s | HTTP P99 | 发布 P99 | 出站流量 | 备注 |
|---|---:|---:|---:|---:|---:|---:|---|
| NORMAL | TBD | TBD | TBD | TBD | TBD | TBD | TBD |
| HOT | TBD | TBD | TBD | TBD | TBD | TBD | TBD |
| PROTECT | TBD | TBD | TBD | TBD | TBD | TBD | TBD |

使用 `live_danmaku_degradation_total{mode,action}` 验证实际策略决策。

# M7 连接压测

> 仅填写真实压测结果。Round 1 旧报告见 `raw/round1/`，最终容量必须使用修复后的 Round 2 Load Generator。

## 环境

- Git 提交：`TBD`
- 压测器主机名/规格：`TBD`
- Centrifugo 节点/规格：`TBD`
- Redis 节点/规格：`TBD`
- 网络 / ulimit / 临时端口范围：`TBD`

## 结果

| 客户端数 | 目标连接/s | 实际连接/s | 首次成功 | 重连次数 | CF CPU | CF 内存 | 压测器 CPU | 结果 |
|---:|---:|---:|---:|---:|---:|---:|---:|---|
| 10,000 | TBD | TBD | TBD | TBD | TBD | TBD | TBD | TBD |
| 50,000 | TBD | TBD | TBD | TBD | TBD | TBD | TBD | TBD |
| 100,000 | TBD | TBD | TBD | TBD | TBD | TBD | TBD | TBD |

## 结论

- 最大观测容量：`TBD`
- 建议安全单节点容量：`TBD`
- 首个瓶颈：`TBD`

不得将 `connect_rate_target_per_sec` 作为实际吞吐量。容量结论必须使用 `connect_rate_actual_per_sec` 并附带压测器资源证据。

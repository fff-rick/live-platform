# M7 Connection Benchmark

> 仅填写真实压测结果。Round 1 旧报告见 `raw/round1/`，最终容量必须使用修复后的 Round 2 Load Generator。

## Environment

- Git commit: `TBD`
- Load generator hostname/spec: `TBD`
- Centrifugo nodes/spec: `TBD`
- Redis nodes/spec: `TBD`
- Network / ulimit / ephemeral port range: `TBD`

## Results

| Clients | Target connect/s | Actual connect/s | Initial success | Reconnects | CF CPU | CF memory | Generator CPU | Result |
|---:|---:|---:|---:|---:|---:|---:|---:|---|
| 10,000 | TBD | TBD | TBD | TBD | TBD | TBD | TBD | TBD |
| 50,000 | TBD | TBD | TBD | TBD | TBD | TBD | TBD | TBD |
| 100,000 | TBD | TBD | TBD | TBD | TBD | TBD | TBD | TBD |

## Conclusion

- Maximum observed capacity: `TBD`
- Recommended safe capacity/node: `TBD`
- First bottleneck: `TBD`

Do not use `connect_rate_target_per_sec` as achieved throughput. Capacity claims must use `connect_rate_actual_per_sec` plus generator resource evidence.

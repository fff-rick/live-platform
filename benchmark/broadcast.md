# M7 Broadcast / Hot Room Benchmark

Raw corrected Round 2 reports are in `benchmark/raw/round2/hotroom/`.

## Publish-rate ladder — 1,000 subscribers, one room

| Subscribers | Target publish/s | Actual publish/s | Fan-out deliveries/s | P95 receive | P99 receive | Reconnects |
|---:|---:|---:|---:|---:|---:|---:|
| 1,000 | 10 | 9.98 | 9,991 | 24.4 ms | 33.9 ms | 0 |
| 1,000 | 20 | 19.98 | 19,983 | 25.7 ms | 36.4 ms | 0 |
| 1,000 | 30 | 29.98 | 29,983 | 27.7 ms | 44.7 ms | 0 |
| 1,000 | 40 | 39.97 | 39,974 | 56.7 ms | 124.3 ms | 0 |
| 1,000 | 50 | 49.68 | 49,683 | 120.2 ms | 222.6 ms | 0 |

With a P99 objective below 100 ms, the corrected baseline shows a clear knee between roughly 30K and 40K deliveries/s in this environment.

## Subscriber ladder — fixed 20 publish/s

| Subscribers | Actual publish/s | Fan-out deliveries/s | P95 receive | P99 receive | Reconnects | Client errors |
|---:|---:|---:|---:|---:|---:|---:|
| 1,000 | 19.98 | 19,983 | 25.3 ms | 34.2 ms | 0 | 0 |
| 2,000 | 19.98 | 39,967 | 102.2 ms | 230.0 ms | 0 | 0 |
| 5,000 | 13.27 | 57,249 measured | 17.72 s | 23.10 s | 2,462 | 4,954 |

The 5,000-subscriber case is an unstable overload result, not a safe capacity figure. It also reinforces that subscriber count is an independent burst dimension: equal average fan-out does not guarantee equal latency.

## Optimization policy

M7 Optimization therefore uses **configurable** defaults derived from this environment:

- target fan-out: 25K deliveries/s
- HOT threshold: 30K/s
- PROTECT threshold: 40K/s
- minimum sample rate: 5%

The adaptive controller estimates `viewer_count × rolling_danmaku_rate` and chooses an effective deterministic sample rate approximately equal to `target / estimated`. Run `make m7-hotroom-adaptive-ab` to measure baseline vs adaptive behavior using the same business Danmaku API path.

These values are not universal Centrifugo limits. Re-run after machine/network/topology changes.

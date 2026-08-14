# M7 Optimization Round

## Implemented changes

### Gift protection

- Per-user Redis fixed-window limiter (`GIFT_USER_RATE_LIMIT`, `GIFT_USER_RATE_WINDOW`).
- Idempotent replay lookup occurs before rate limiting, so a retry of a committed request still returns the original order.
- `GIFT_MAX_COUNT_PER_REQUEST` bounds combo size (default 100).
- Browser demo includes a 300 ms gift-combo accumulator; up to 100 clicks are sent as one transaction.
- `live_gift_rate_limited_total` exposes rejected requests.

### Adaptive danmaku fan-out

Policy estimates:

`estimated_fanout_per_sec = viewer_count × observed_danmaku_rate_per_sec`

Default benchmark-derived settings:

- target fan-out: 25,000 deliveries/s
- HOT: 30,000 deliveries/s
- PROTECT: 40,000 deliveries/s
- minimum sample rate: 5%

When HOT/PROTECT is active, effective sample rate is approximately:

`target_fanout / estimated_fanout`

clamped to `[DANMAKU_MIN_SAMPLE_RATE, 1]`.

Legacy fixed percentages remain available when `DANMAKU_ADAPTIVE_ENABLED=false`, which is useful for A/B testing and rollback.

## Verification commands

```bash
make m7-gift-optimization-smoke
make m7-hotroom-adaptive-ab
make m7-gift-platform-ladder
```

The A/B benchmark should be run on the same hardware before and after optimization. Capacity thresholds remain configurable because they depend on host/network/Centrifugo/Redis topology.

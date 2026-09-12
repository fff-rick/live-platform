# live-interaction

`live-interaction` owns join/leave, heartbeat, danmaku, likes, viewer state, rate limiting, and hot-room adaptive protection. It uses a versioned internal API for identity and room governance instead of reading MySQL directly.

## Dependencies and degradation

- Redis stores viewer, like, rate-limit, and hot-room state.
- Centrifugo is the realtime publishing path.
- Kafka persistence for danmaku is best effort; realtime delivery remains the priority path.
- `live-identity-room` supplies identity, room, mute, and ban decisions.

## Investigation pivots

Correlate errors with `live_danmaku_degradation_total`, estimated fanout, sampling rate, `live_realtime_publish_total`, Redis health, and the identity-room internal call. NORMAL/HOT/PROTECT thresholds are measured configuration, not universal Centrifugo limits. Do not disable adaptive protection during an incident except for a controlled diagnostic.

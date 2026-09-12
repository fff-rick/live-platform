# live-api

`live-api` is the public `/api/v1` entry point and lightweight gateway. It preserves the client contract and proxies commerce, interaction, and identity/room routes when `COMMERCE_BASE_URL`, `INTERACTION_BASE_URL`, and `IDENTITY_ROOM_BASE_URL` are configured.

## Dependencies and failure boundaries

- MySQL and Redis support the remaining local fallback handlers.
- `live-commerce`, `live-interaction`, and `live-identity-room` are downstream HTTP services in the Compose deployment.
- Kafka is used for best-effort danmaku persistence and is deliberately not a readiness dependency.
- Centrifugo failure is exposed as degraded realtime state; it must not automatically make the whole API unready while MySQL and Redis are healthy.

## Investigation pivots

For HTTP 5xx, split by route and status first, then correlate `trace_id` across `live-api` and the routed service. Check releases for both `live-api` and the downstream owner. Clearing one route base URL is the documented rollback switch for that route, but only propose it after identifying the failing downstream and obtaining change approval.

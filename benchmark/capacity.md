# M7 Capacity Decision

This file separates **measured baseline facts** from capacity still pending Optimization Round re-test. Hardware/topology-specific thresholds must not be presented as universal framework limits.

## Preliminary safe capacity from corrected Round 2 baseline

- Centrifugo safe connections/node: **not established yet**. 5,000 total connections were previously stable, but the load generator and SUT must be separated before a node-capacity claim.
- Hot-room subscribers: **1,000 subscribers at 20 publish/s validated** with P99 ~34 ms. 2,000 at the same rate reached P99 ~230 ms and is outside the current <100 ms objective.
- Hot-room publish rate: **30 publish/s validated at 1,000 subscribers** with P99 ~45 ms. 40/s reached ~124 ms.
- Baseline safe fan-out working target: **~25K–30K deliveries/s** for current environment/headroom; corrected test knee appears between 30K and 40K/s.
- Like accepted rate: preliminary Round 1 result ~20K logical likes/s with API P99 ~11 ms; 50K/100K ladder still pending.
- Gift platform TPS: **~200 TPS across 100 wallets validated** with P99 ~92 ms. 500/1000/2000 TPS pending.
- Single-wallet Gift throughput: saturates around **~90 TPS** under pathological same-account load; this is a per-wallet serialization boundary, not platform capacity.

## Bottleneck order observed so far

1. Single hot-channel fan-out / network-and-client delivery pressure.
2. Same-wallet MySQL row serialization under abusive per-user Gift rates.
3. Platform-level Gift / Redis / Kafka / MySQL bottleneck: **TBD; next ladder required**.

## Optimization policy

- Danmaku target fan-out: 25K/s (configurable)
- HOT: 30K/s (configurable)
- PROTECT: 40K/s (configurable)
- Adaptive sample floor: 5%
- Gift per-user request guard: 10 requests/s by default
- Gift combo max: 100 per transaction by default

## Re-test conditions

Re-run capacity/A-B tests when any of these change materially:

- Centrifugo node count or Redis topology;
- NIC / host CPU / kernel limits;
- message size or history settings;
- API / DB instance count or MySQL storage;
- load generator moves to a separate host (required before final connection-capacity claim).

## Remaining decision gates

1. `make m7-hotroom-adaptive-ab` — prove optimization reduces fan-out/tail latency versus baseline.
2. `make m7-gift-platform-ladder` — find platform Gift knee at 500/1000/2000 TPS.
3. Like 20K/50K/100K ladder.
4. Separate load-generator host, then 10K → 50K → 100K connection campaign.

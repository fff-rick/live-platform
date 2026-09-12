# Realtime and hot-room degradation

If Centrifugo is unavailable while MySQL and Redis are healthy, API readiness should remain healthy and report realtime degradation. Gift transactions remain durable through MySQL and Outbox; realtime gift or danmaku publication may fail independently.

For hotspot overload, inspect estimated fanout, adaptive sample rate, room traffic, Redis health, and `live_realtime_publish_total`. NORMAL/HOT/PROTECT thresholds come from M7 measurements and remain configuration values. Keep adaptive protection enabled during incidents unless a bounded diagnostic explicitly requires otherwise.

After recovery, verify Centrifugo health, publish success, sampling returning toward normal, and worker backlog draining. Do not infer historical persistence from realtime delivery alone.

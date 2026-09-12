# live-worker

`live-worker` runs independently selectable roles: stats aggregation, like snapshots, Outbox publishing, gift delivery, and danmaku archive. In Kubernetes these roles can be separate Deployments but continue to report the `live-worker` service identity.

## Dependencies and safety

- Stats depends on Redis and Centrifugo and remains single-active until a room-ownership lease exists.
- Like snapshots depend on Redis and MySQL.
- Outbox publishing depends on MySQL and Kafka.
- Gift delivery and danmaku archive depend on Kafka and MySQL; gift delivery also publishes through Centrifugo.
- Kafka consumers use at-least-once delivery with event idempotency. Scaling beyond available partitions does not improve consumption.

## Investigation pivots

Use role names in logs, then check Outbox age/retry metrics, Kafka producer errors, consumer lag by group/topic/partition, and MySQL pool pressure. During rollback, restore only the affected role; never delete Outbox rows, topics, consumer groups, or offsets as an incident shortcut.

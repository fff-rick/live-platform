# Outbox and Kafka backlog

## Signals

Check `live_outbox_pending`, `live_outbox_oldest_pending_age_seconds`, `live_outbox_publish_total`, `live_outbox_retry_total`, `live_kafka_produce_errors_total`, and `live_kafka_consumer_lag` by group, topic, and partition. Correlate them with `live-worker` role logs.

## Decision path

1. If Outbox age rises with Kafka produce errors, verify broker reachability and producer error reasons. The commerce transaction may still be durable in MySQL.
2. If publishing succeeds but consumer lag rises, inspect consumer readiness, processing errors, partition assignment, and poison-record handling.
3. After Kafka recovery, verify backlog age and lag trend down and that idempotent consumers do not duplicate business effects.
4. Scale consumers only after checking partition count and MySQL connection budget. Stats remains single-active without a lease.

Never delete pending Outbox rows, reset consumer offsets, or recreate topics during initial diagnosis.

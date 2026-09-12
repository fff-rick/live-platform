# MySQL connection pool contention

Check `live_db_pool_in_use_connections`, open and maximum connections, `live_db_pool_wait_total`, and `live_db_pool_wait_duration_seconds_total` per service and pool. Correlate waits with request latency, slow transactions, worker backlog, and recent replica or pool changes.

Compute the whole application budget as `replicas × per-pod maximum` plus migration, monitoring, administration, and database reserve. The measured single-process M7 setting of 40 open / 20 idle must not be copied to every Pod. Kubernetes budgets are intentionally smaller: API 20/10 and MySQL-using worker roles 10/5.

Before proposing a pool increase, identify whether the cause is a leaked/slow transaction, excess replicas, or true database capacity. Verify recovery by observing the wait-duration rate and request latency return to baseline.

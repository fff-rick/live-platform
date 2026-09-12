# live-commerce

`live-commerce` owns the gift catalog, wallet balance and transactions, development credit, gift orders, idempotency, and transaction Outbox. `live-api` proxies the corresponding public routes to this service.

## Dependencies and consistency boundary

- MySQL is authoritative for balances, orders, wallet transactions, and `outbox_events`.
- Balance deduction, order creation, wallet transaction, and Outbox insertion commit in one local MySQL transaction.
- Redis supports viewer/rate-limit projections.
- The current migration stage still reads room state from the shared MySQL schema; it must not add writes to identity or room ownership tables.

## Investigation pivots

For gift failures, correlate the `live-commerce` HTTP trace with MySQL pool waits and the resulting Outbox record. Repeating an `Idempotency-Key` must not deduct twice. Do not treat delayed realtime gift delivery as proof that the commerce transaction failed; the Outbox and worker path are separate evidence.

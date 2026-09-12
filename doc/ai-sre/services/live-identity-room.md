# live-identity-room

`live-identity-room` owns registration and authentication, room lifecycle, host governance, mute/ban decisions, and the internal identity/room API consumed by `live-interaction`.

## Dependencies

- MySQL stores users, rooms, and governance facts.
- Redis supports active-room and related projections.
- Centrifugo tokens and publish integration participate in realtime-facing flows.

## Investigation pivots

For authorization or room-state errors, distinguish token validation failures from missing users/rooms and governance denial. Correlate MySQL pool waits and Redis errors before attributing failures to `live-api`, because the gateway preserves the external path while this service owns the decision.

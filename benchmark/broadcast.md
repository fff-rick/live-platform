# M7 Broadcast / Hot Room Benchmark

## Scenarios

Compare the same connection count under one hot room and evenly distributed rooms.

| Clients | Rooms | Target publish/s | Actual publish/s | Fanout deliveries/s | Msg bytes | P99 receive | CF CPU | Redis CPU | Egress | Disconnects |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| TBD | 1 | TBD | TBD | TBD | TBD | TBD | TBD | TBD | TBD | TBD |
| TBD | 100 | TBD | TBD | TBD | TBD | TBD | TBD | TBD | TBD | TBD |

## Round 2 ladders

1. Fixed 1,000 subscribers, publish: `10 → 20 → 30 → 40 → 50 /s`.
2. Fixed 20 publish/s, subscribers: `1K → 2K → 5K`.

Use `make m7-hotroom-ladder`.

## Amplification

`application_bytes_per_second ≈ subscribers × publishes_per_second × average_message_bytes`

Record measured NIC throughput next to the theoretical value. `fanout_delivery_actual_per_sec` is measured by the load clients and should be compared with the expected fan-out.

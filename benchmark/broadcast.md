# M7 Broadcast / Hot Room Benchmark

## Scenarios

Compare the same connection count under:

1. one hot room;
2. evenly distributed rooms.

| Clients | Rooms | Publish/s | Msg bytes | P99 receive | CF CPU | Redis CPU | Egress | Disconnects |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| TBD | 1 | TBD | TBD | TBD | TBD | TBD | TBD | TBD |
| TBD | 100 | TBD | TBD | TBD | TBD | TBD | TBD | TBD |

## Amplification

`application_bytes_per_second ≈ subscribers × publishes_per_second × average_message_bytes`

Record measured NIC throughput next to the theoretical value; do not treat the formula as benchmark data.

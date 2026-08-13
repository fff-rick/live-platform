# M7 Like Storm Benchmark

| Logical likes/s | Target API req/s | Actual API req/s | Batch/request | API P99 | Redis ops/s | stats broadcasts/s | Network | Result |
|---:|---:|---:|---:|---:|---:|---:|---:|---|
| 20,000 | 200 | TBD | 100 | TBD | TBD | TBD | TBD | TBD |
| 50,000 | 500 | TBD | 100 | TBD | TBD | TBD | TBD | TBD |
| 100,000 | 1,000 | TBD | 100 | TBD | TBD | TBD | TBD | TBD |

Use `ROOM_ID=... TOKEN=... make m7-like-ladder`.

Compare accepted like rate with the much lower `room:{id}:stats` broadcast rate to quantify aggregation gain.

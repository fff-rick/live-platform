# M7 点赞风暴压测

| 逻辑点赞/s | 目标 API 请求/s | 实际 API 请求/s | 每请求批量数 | API P99 | Redis 操作/s | 统计广播/s | 网络 | 结果 |
|---:|---:|---:|---:|---:|---:|---:|---:|---|
| 20,000 | 200 | TBD | 100 | TBD | TBD | TBD | TBD | TBD |
| 50,000 | 500 | TBD | 100 | TBD | TBD | TBD | TBD | TBD |
| 100,000 | 1,000 | TBD | 100 | TBD | TBD | TBD | TBD | TBD |

使用 `ROOM_ID=... TOKEN=... make m7-like-ladder`。

将接受点赞速率与更低的 `room:{id}:stats` 广播速率比较，以量化聚合收益。

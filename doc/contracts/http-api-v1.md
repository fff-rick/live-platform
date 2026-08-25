# HTTP API v1 兼容契约

**基路径：** `/api/v1`  
**认证：** 除下表明确“公开”的接口外，使用 `Authorization: Bearer <access_token>`。  
**格式：** 请求和响应均为 JSON；错误响应保持既有 `{ "error": { "code", "message" } }` 语义。

## 路由冻结表

| 方法 | 路径 | 认证 | 迁移归属 |
| --- | --- | --- | --- |
| POST | `/auth/register`、`/auth/login` | 公开 | 账户-房间服务 |
| GET | `/me` | 必需 | 账户-房间服务 |
| POST | `/realtime/token` | 必需 | 账户-房间服务 / Gateway |
| GET/POST | `/rooms` | GET 公开；POST 必需 | 账户-房间服务 |
| GET | `/rooms/{room_id}`、`/messages`、`/top-viewers`、`/viewers`、`/stats` | 公开 | 房间、归档或统计读模型 |
| POST | `/rooms/{room_id}/start`、`/stop` | 必需 | 账户-房间服务 |
| POST | `/rooms/{room_id}/join`、`/leave`、`/heartbeat` | 必需 | 互动接入服务 |
| POST | `/rooms/{room_id}/danmaku`、`/like` | 必需 | 互动接入服务 |
| GET | `/gifts` | 公开 | 钱包-礼物服务 |
| GET | `/wallet`、`/wallet/transactions` | 必需 | 钱包-礼物服务 |
| POST | `/wallet/dev-credit` | 必需；仅开发开关启用 | 钱包-礼物服务 |
| POST | `/rooms/{room_id}/gifts` | 必需；必须携带 `Idempotency-Key` | 钱包-礼物服务 |
| GET | `/gift-orders/{order_no}` | 必需 | 钱包-礼物服务 |
| POST/DELETE | `/rooms/{room_id}/mutes`、`/mutes/{user_id}` | 必需 | 账户-房间服务 |
| POST/DELETE | `/rooms/{room_id}/bans`、`/bans/{user_id}` | 必需 | 账户-房间服务 |

## 迁移要求

- Gateway 必须保留以上 URI、方法、认证、请求头及状态码；内部服务地址不暴露给浏览器。
- 送礼请求的 `Idempotency-Key` 在整个迁移期间保持原有去重范围与冲突语义；不得因路由切换产生重复扣款。
- 实时令牌与订阅令牌的 JWT claim 和失效时间在客户端升级完成前不得改变。
- 新增字段应为可选字段。字段改名、JSON number/string 互换、将同步成功改为异步受理都属于破坏性变更。

响应示例与业务错误码以 `internal/httpapi/server.go` 及其测试为实现基线；每次改变该文件的外部响应前，必须同步修改本契约并新增对应测试。

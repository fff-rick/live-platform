# LiveFlow 展示界面

面向用户的 Web 应用嵌入在 `live-api` 中，并通过 `/` 提供服务。

## 产品流程

```text
直播大厅
  -> 注册 / 登录
  -> 创建并开播（主播）或进入直播间（观众）
  -> 加入 API 返回频道专属订阅令牌
  -> 连接 Centrifugo WebSocket
  -> 流频道：弹幕与礼物事件
  -> 统计频道：观众与点赞聚合数据
  -> 心跳刷新业务观众在线状态
```

## 真实后端集成

| 界面能力 | 后端路径 |
| --- | --- |
| 注册/登录 | `/api/v1/auth/*` |
| 大厅 | `GET /api/v1/rooms` |
| 创建/开播/停播 | `/api/v1/rooms*` |
| 实时鉴权 | `/api/v1/realtime/token` + 进房订阅 JWT |
| 弹幕 | `/api/v1/rooms/{id}/danmaku` |
| 点赞 | `/api/v1/rooms/{id}/like` |
| 观众心跳 | `/api/v1/rooms/{id}/heartbeat` |
| 礼物 | `/api/v1/rooms/{id}/gifts` |
| 钱包 | `/api/v1/wallet*` |
| 内容管理 | 房间禁言/封禁 API |

## 媒体平面边界

当前仓库有意实现互动平面，而非视频采集、转码或 CDN。播放器外壳因此是明确的集成点，不是伪造的视频后端。后续接入 SRS、LiveKit 或云直播服务时，无需改变实时互动模型。

## 部署

Docker Compose：打开 `http://localhost:8080/`。

Kubernetes 目标部署中：Ingress 将 `/connection` 路由到 Centrifugo，将 `/api` 和 `/` 路由到 `live-api`，以保持浏览器 API 调用和 WebSocket 鉴权同源。当前 demo overlay 不包含该 Ingress。

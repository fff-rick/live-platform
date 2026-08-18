# 安全说明

请勿在公开 Issue 中提交真实凭据。任何被提交、记录到日志或粘贴到公开渠道的凭据都必须立即轮换。

生产部署必须替换全部开发密钥，并应使用外部密钥管理服务或 Kubernetes Secret 集成。仓库中的示例仅包含占位值。

演示钱包充值接口在生产环境必须关闭（`ENABLE_DEV_WALLET_CREDIT=false`）。Centrifugo HTTP API 和指标端点必须仅在内网开放；公网 Ingress 只能暴露客户端连接端点和应用 API 路由。

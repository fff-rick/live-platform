# 契约目录

本目录冻结在微服务迁移开始时已经对客户端和异步消费者可见的契约。它不是新 API 的设计草案；任何破坏性修改都必须新建版本，并完成生产者、消费者与灰度计划评审。

| 契约 | 当前版本 | 规范 |
| --- | --- | --- |
| HTTP API | `/api/v1` | [http-api-v1.md](http-api-v1.md) |
| Centrifugo 实时消息 | `room:*` | [realtime-v1.md](realtime-v1.md) |
| Kafka 领域事件 | Topic `*.v1` | [kafka-events-v1.md](kafka-events-v1.md) |

## 兼容性规则

1. 已发布 JSON 对象只能新增可选字段，不能删除或改变字段含义、类型和单位。
2. HTTP 成功响应、错误码和认证要求属于外部契约；服务迁移只能改变路由实现，不能改变这些语义。
3. Kafka 消费者按至少一次投递实现幂等；`event_id` 是去重键，不保证全局顺序。
4. 实时事件允许重复和短暂乱序。客户端使用 `event_id` 去重，并在重连后以 HTTP 读模型恢复状态。
5. 新的破坏性事件使用新 `event_type` 和新 Topic 版本；旧消费者保留至迁移窗口结束。

Go 代码中的 `internal/mq` 对房间范围 Kafka envelope 执行基础结构校验；`go test ./...` 会覆盖该兼容契约。Payload 的业务字段由各消费者校验。

## 本阶段的明确边界

本阶段不强制引入 OpenAPI 代码生成、Schema Registry、gRPC 或服务网格。现有服务仍为同一仓库、同一发布单元；这些契约先用于阻止迁移时的无意破坏。待至少一个服务真正独立发布后，再评估 OpenAPI/Protobuf 与 Schema Registry 的投入回报。

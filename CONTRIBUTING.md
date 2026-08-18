# 贡献指南

## 本地开发

```bash
make test
make vet
make fmt
make compose-reset
```

提交 Pull Request 前，请保持改动范围清晰，为正确性敏感行为补充测试，且绝不编造压测数据。性能结论必须包含工作负载、机器/环境、目标与实际速率、延迟分位数，并在可行时附上 `benchmark/raw/` 中的原始报告。

## 架构规则

- Centrifugo 是实时传输层，不是业务事实来源。
- 钱包与礼物的正确性必须由 MySQL 事务保证。
- 礼物可靠投递采用事务 Outbox；普通弹幕归档按设计保持尽力而为。
- Kafka 不能成为 API 就绪检查的依赖。
- 在具备归属或租约语义前，不要水平扩展统计 Worker。

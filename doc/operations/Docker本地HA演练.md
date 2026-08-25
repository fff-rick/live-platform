# Docker 本地 HA 演练

使用 `docker-compose.ha.yml` 叠加标准 Compose 文件，演练三节点 KRaft Kafka 与多副本 Centrifugo。Centrifugo 副本不再各自发布宿主机端口；`centrifugo-gateway` 是唯一的 `localhost:8000` WebSocket/API 入口。所有容器和卷仍在同一宿主机，因此它不代表生产高可用。

## 启动

```bash
docker compose -f docker-compose.yml -f docker-compose.ha.yml up --build --scale centrifugo=3
```

若已使用旧版 HA overlay 启动并遇到 `Bind for 0.0.0.0:8000 failed`，先仅重建
Centrifugo 与网关（不删除 Kafka/MySQL 数据卷）：

```bash
docker compose -f docker-compose.yml -f docker-compose.ha.yml up -d --build \
  --force-recreate --scale centrifugo=3 centrifugo centrifugo-gateway
```

应用会使用 `kafka-1:19092,kafka-2:19092,kafka-3:19092`。两个业务 Topic 都以复制因子 3 和 `min.insync.replicas=2` 创建。

## 验证与故障演练

```bash
docker compose -f docker-compose.yml -f docker-compose.ha.yml ps
docker compose -f docker-compose.yml -f docker-compose.ha.yml exec kafka \
  /opt/kafka/bin/kafka-topics.sh --bootstrap-server kafka-1:19092 --describe --topic live.gift.v1
docker compose -f docker-compose.yml -f docker-compose.ha.yml stop kafka-2
docker compose -f docker-compose.yml -f docker-compose.ha.yml start kafka-2
```

Broker 下线期间继续执行已有 Kafka 冒烟或送礼测试，确认消费者能恢复，且 Topic 的 ISR 在恢复后回到 3。

清理演练数据：

```bash
docker compose -f docker-compose.yml -f docker-compose.ha.yml down -v
```

## 明确限制

- Docker Compose 的 `deploy.replicas` 在非 Swarm 模式不保证扩容；命令中的 `--scale centrifugo=3` 才是本演练的有效设置。
- Redis 和 MySQL 仍是单机依赖。应用 Redis 客户端尚未配置 Sentinel，不能把 Redis 容器重启当成自动 failover 验证。
- 单宿主机故障、磁盘损坏和跨可用区恢复必须在 Kind 多节点演练或托管环境验证。

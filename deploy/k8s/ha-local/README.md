# 本地 HA 演练

此目录用于验证 Pod、Kafka Broker 和 Redis 主节点故障后的行为，不等同于生产 HA：Kind 的所有节点和 PVC 都在一台宿主机上。

## 前置条件

安装 `docker`、`kubectl`、`kind` 和 `helm`；不要在当前 `docker-desktop` context 中执行。

## 启动

```bash
kind create cluster --config deploy/k8s/ha-local/kind-config.yaml
kubectl config use-context kind-live-platform-ha

# 安装 Kafka operator（固定版本，避免不受控升级）。
kubectl create namespace live-platform
kubectl create namespace strimzi
kubectl apply -n strimzi -f https://github.com/strimzi/strimzi-kafka-operator/releases/download/0.48.0/strimzi-cluster-operator-0.48.0.yaml
kubectl wait -n strimzi --for=condition=Available deployment/strimzi-cluster-operator --timeout=180s

# Redis Sentinel 密码只用于本地；不要提交真实密码。
kubectl -n live-platform create secret generic live-platform-redis --from-literal=redis-password=local-dev-only
helm repo add bitnami https://charts.bitnami.com/bitnami
helm upgrade --install redis bitnami/redis -n live-platform -f deploy/k8s/ha-local/redis-values.yaml

kubectl apply -f deploy/k8s/ha-local/kafka.yaml
kubectl wait -n live-platform kafka/kafka --for=condition=Ready --timeout=300s
```

`centrifugo.yaml` 需要配套的 Sentinel 地址、账号和 `centrifugo-ha-config` 后才能应用；本次先不把它接入现有 base，防止和单副本开发 Redis 混用。

## 演练

```bash
kubectl -n live-platform get pods -w
kubectl -n live-platform delete pod kafka-dual-role-0
kubectl -n live-platform get kafka kafka -o yaml
```

确认 Topic 仍可写入、消费者恢复且 ISR 不低于 2。清理：`kind delete cluster --name live-platform-ha`。

## MySQL

本地 MySQL HA 暂不部署。它需要独立 Operator、备份目标和恢复演练；在决定使用 Percona、Oracle MySQL Operator 或托管 MySQL 前，手写主从 StatefulSet 不应进入此演练环境。

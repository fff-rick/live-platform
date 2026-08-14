# Kubernetes 演示环境部署说明

此 Overlay 仅用于演示和联调，不是正式容量压测环境。它以单副本部署业务必需的服务，并关闭本地链路追踪栈，以适应小规格单节点 Kubernetes 集群的资源预算。

## 一次性集群初始化

请在本地生成运行时配置，使用集群的 Sealed Secrets 公钥加密后提交生成的 `SealedSecret`；**禁止提交明文 `Secret`**。

`live-platform-runtime` 必须包含以下键：

- `MYSQL_PASSWORD`、`MYSQL_ROOT_PASSWORD`、`MYSQL_DSN`
- `CENTRIFUGO_API_KEY`、`CENTRIFUGO_TOKEN_SECRET`、`AUTH_JWT_SECRET`

`MYSQL_DSN` 必须连接集群内的 MySQL Service，例如：

```text
live:<password>@tcp(mysql:3306)/live?parseTime=true&charset=utf8mb4
```

已检查的集群中，Sealed Secrets Controller 的名称为 `sealed-secrets-controller`，命名空间为 `kube-system`，版本为 `0.27.3`。仓库根目录中的 `sealed-secrets-cert.pem` 是该集群的公钥。请在本地准备包含上述键的 `deploy/k8s/runtime.env` 文件（不要提交），然后执行：

```bash
cp deploy/k8s/runtime.env.example deploy/k8s/runtime.env
# 编辑 deploy/k8s/runtime.env，替换所有 REPLACE_WITH_... 占位值
chmod 600 deploy/k8s/runtime.env
./deploy/k8s/generate-sealed-runtime.sh
```

脚本优先使用系统中的 `kubeseal`。若未安装，会自动使用 Go 执行版本匹配的官方命令 `go run github.com/bitnami-labs/sealed-secrets/cmd/kubeseal@v0.27.3`，因此不需要 root 权限安装系统软件。

脚本只会将加密后的 YAML 写到 `deploy/k8s/overlays/demo/sealed-runtime.yaml`，不会创建或保存明文 Secret。生成后，将 `sealed-runtime.yaml` 加入 Overlay 的 `resources` 列表，再提交到仓库。

如果 GHCR 镜像包为私有包，还需要在 `live-platform` 命名空间创建名为 `ghcr-secret` 的镜像拉取 Secret。

默认分支已包含真实镜像标签和加密运行时密钥后，只需首次应用一次 Argo CD Application：

```bash
kubectl apply -f deploy/k8s/argocd/live-platform-app.yaml
```

演示环境通过 NodePort 暴露 API 与 Centrifugo：

- API：`30082`
- Centrifugo：`30083`

请在云防火墙/安全组中仅允许可信来源 IP 访问这两个端口。Redis、MySQL、Kafka 与 Worker Metrics 均保持集群内部可见。

## 交付流程

每个 PR，以及每次合并到 `main` 后，都会运行同一套 `Quality gate`：格式检查、Lint、`go vet`、测试，以及 Buildx `cacheonly` 预构建。预构建会验证 Dockerfile 和镜像构建流程，但不会导出、加载或推送镜像产物。

镜像版本由功能 PR 直接指定：修改 `deploy/k8s/overlays/demo/kustomization.yaml` 中的 `newTag`，例如从：

```yaml
newTag: v1.0
```

改为：

```yaml
newTag: v1.1
```

不使用交互式手动输入版本。PR 通过并合并后，CI 会再次执行质量门，然后读取该版本、校验版本格式、拒绝覆盖仓库中已有版本，最后推送：

```text
ghcr.io/fff-rick/live-platform:v1.1
```

镜像推送成功后，CI 会要求 Argo CD 同步 Application 配置中跟踪的当前 `main` Revision。

该 Application **刻意关闭了自动同步**：若 Argo CD 自动监听 `main`，它可能在 CI 推送镜像前先尝试拉取 PR 指定的新标签。请在 GitHub 仓库配置以下 Secrets：

- `ARGOCD_SERVER`：可从 GitHub Actions Runner 访问的 Argo CD API 地址，例如 `43.136.82.118:30080`。
- `ARGOCD_AUTH_TOKEN`：最小权限 Argo CD Token，仅允许同步 `default/live-platform-demo`。

工作流只会在镜像推送成功后调用 Argo CD；它不通过 SSH 或 `kubectl` 改动集群，实际滚动更新仍由 Argo CD 执行。

## GitHub 分支保护

为 `main` 启用分支保护，并配置：

- 必须通过 Pull Request 合并；
- 必须通过 `Quality gate`；
- 要求分支在合并前保持最新；
- 禁止直接推送，也不要为工作流配置绕过分支保护的权限。

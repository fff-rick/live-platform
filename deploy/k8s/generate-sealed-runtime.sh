#!/usr/bin/env bash
# Generate the encrypted runtime Secret used by the demo Overlay.
# Plaintext credentials are read from an ignored env file and are never written
# to the repository by this script.
set -euo pipefail
umask 077

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(CDPATH= cd -- "$script_dir/../.." && pwd)"
env_file="$script_dir/runtime.env"
cert_file="$repo_root/sealed-secrets-cert.pem"
output_file="$script_dir/overlays/demo/sealed-runtime.yaml"
namespace="live-platform"
secret_name="live-platform-runtime"

usage() {
  cat <<'EOF'
用法：
  ./deploy/k8s/generate-sealed-runtime.sh [选项]

选项：
  --env-file PATH    明文运行时环境变量文件（默认：deploy/k8s/runtime.env）
  --cert PATH        Sealed Secrets 集群公钥（默认：sealed-secrets-cert.pem）
  --output PATH      加密 YAML 输出路径（默认：deploy/k8s/overlays/demo/sealed-runtime.yaml）
  --namespace NAME   Secret 命名空间（默认：live-platform）
  --name NAME        Secret 名称（默认：live-platform-runtime）
  -h, --help         显示此帮助

环境变量文件必须包含：
  MYSQL_PASSWORD
  MYSQL_ROOT_PASSWORD
  MYSQL_DSN
  CENTRIFUGO_API_KEY
  CENTRIFUGO_TOKEN_SECRET
  AUTH_JWT_SECRET
EOF
}

while (($#)); do
  case "$1" in
    --env-file) env_file="$2"; shift 2 ;;
    --cert) cert_file="$2"; shift 2 ;;
    --output) output_file="$2"; shift 2 ;;
    --namespace) namespace="$2"; shift 2 ;;
    --name) secret_name="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "未知参数：$1" >&2; usage >&2; exit 2 ;;
  esac
done

for command in kubectl openssl; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "缺少命令：$command" >&2
    exit 1
  }
done

# 与远程集群的 sealed-secrets-controller:0.27.3 保持同一版本。
# 没有系统级 kubeseal 时，Go 会从模块缓存运行官方命令，不需要 root 权限。
if command -v kubeseal >/dev/null 2>&1; then
  kubeseal_command=(kubeseal)
elif command -v go >/dev/null 2>&1; then
  kubeseal_command=(go run github.com/bitnami-labs/sealed-secrets/cmd/kubeseal@v0.27.3)
else
  echo "缺少 kubeseal，且未找到可用的 Go 命令" >&2
  exit 1
fi

[[ -f "$env_file" ]] || { echo "未找到环境变量文件：$env_file" >&2; exit 1; }
[[ -f "$cert_file" ]] || { echo "未找到集群公钥：$cert_file" >&2; exit 1; }
openssl x509 -in "$cert_file" -noout >/dev/null

required_keys=(
  MYSQL_PASSWORD
  MYSQL_ROOT_PASSWORD
  MYSQL_DSN
  CENTRIFUGO_API_KEY
  CENTRIFUGO_TOKEN_SECRET
  AUTH_JWT_SECRET
)
for key in "${required_keys[@]}"; do
  grep -qE "^${key}=" "$env_file" || {
    echo "环境变量文件缺少必需键：$key" >&2
    exit 1
  }
done

output_dir="$(dirname -- "$output_file")"
mkdir -p "$output_dir"
tmp_file="$(mktemp "$output_dir/.sealed-runtime.XXXXXX")"
trap 'rm -f "$tmp_file"' EXIT

kubectl -n "$namespace" create secret generic "$secret_name" \
  --from-env-file="$env_file" \
  --dry-run=client -o yaml |
  "${kubeseal_command[@]}" --format yaml --cert "$cert_file" > "$tmp_file"

mv -f "$tmp_file" "$output_file"
trap - EXIT

echo "已生成加密配置：$output_file"
echo "请将 $(basename -- "$output_file") 加入 deploy/k8s/overlays/demo/kustomization.yaml 的 resources 列表后再提交。"

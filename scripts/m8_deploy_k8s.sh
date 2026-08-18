#!/usr/bin/env bash
# Report the current GitOps deployment state without mutating the cluster.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP="${ARGOCD_APP:-live-platform-demo}"
NAMESPACE="${K8S_NAMESPACE:-live-platform}"

"$ROOT/scripts/m8_k8s_validate.sh"

echo
echo "GitOps target: Argo CD Application/$APP -> deploy/k8s/overlays/demo (main)"
echo "Desired workload state: only SealedSecret/live-platform-runtime."
echo "The legacy base is intentionally not referenced by the demo overlay."

if command -v argocd >/dev/null 2>&1; then
  echo
  argocd app get "$APP"
else
  echo "argocd CLI unavailable: skipped Application status query"
fi

if command -v kubectl >/dev/null 2>&1; then
  echo
  echo "Resources currently present in namespace $NAMESPACE:"
  kubectl -n "$NAMESPACE" get deploy,statefulset,job,svc,pvc -o wide 2>&1 || true
  cat <<'EOF'

If legacy workloads are listed, inspect their Argo CD ownership before removal.
Use the Argo CD UI/CLI to sync with prune only after confirming that
live-platform-demo is their controller. This script intentionally never
syncs, prunes, or deletes resources.
EOF
else
  echo "kubectl unavailable: skipped cluster inventory"
fi

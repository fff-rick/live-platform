#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

python3 "$ROOT/scripts/m8_k8s_static_validate.py" "$ROOT"

if command -v kustomize >/dev/null 2>&1; then
  out="$(mktemp)"
  trap 'rm -f "$out"' EXIT
  kustomize build "$ROOT/deploy/k8s/overlays/demo" >"$out"
  echo "rendered demo GitOps overlay: $(grep -c '^kind:' "$out") resources"
else
  echo "kustomize not installed: skipped rendered-overlay validation"
fi

echo "GitOps demo manifest validation: PASS"

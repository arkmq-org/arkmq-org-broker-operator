#!/bin/bash
# Clear finalizers from all bapp and bsvc resources across all namespaces.
# Usage:
#   ./hack/clear-finalizers.sh          # list only (dry run)
#   ./hack/clear-finalizers.sh --apply  # list and patch

set -euo pipefail

APPLY=false
if [[ "${1:-}" == "--apply" ]]; then
  APPLY=true
fi

for KIND in bsvc bapp; do
  echo "=== $KIND ==="
  kubectl get "$KIND" --all-namespaces --no-headers | while read -r NAMESPACE NAME REST; do
    FINALIZERS=$(kubectl get "$KIND" "$NAME" -n "$NAMESPACE" -o jsonpath='{.metadata.finalizers}')
    if [[ -z "$FINALIZERS" || "$FINALIZERS" == "[]" ]]; then
      echo "  $NAMESPACE/$NAME  (no finalizers)"
      continue
    fi
    echo "  $NAMESPACE/$NAME  finalizers=$FINALIZERS"
    if $APPLY; then
      kubectl patch "$KIND" "$NAME" --namespace "$NAMESPACE" --type=merge -p '{"metadata":{"finalizers":null}}'
      echo "    -> cleared"
    fi
  done
  echo
done

if ! $APPLY; then
  echo "Dry run — re-run with --apply to clear finalizers."
fi

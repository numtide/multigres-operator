#!/usr/bin/env bash
# Delete a pool pod by label selector (picks the first match).
#
# Required env vars:
#   KUBECONFIG, NAMESPACE
#   POD_SELECTOR (optional, defaults to app.kubernetes.io/component=shard-pool)
#   TARGET_POD (optional, if set deletes this specific pod instead of first match)
#
# Usage:
#   NAMESPACE=default ./delete-pool-pod.sh
#   TARGET_POD=pool-rw-cell-0 NAMESPACE=default ./delete-pool-pod.sh

set -euo pipefail

SELECTOR="${POD_SELECTOR:-app.kubernetes.io/component=shard-pool}"

if [ -n "${TARGET_POD:-}" ]; then
  kubectl delete pod "$TARGET_POD" -n "$NAMESPACE"
else
  POD=$(kubectl get pods -n "$NAMESPACE" -l "$SELECTOR" -o jsonpath='{.items[0].metadata.name}')
  if [ -z "$POD" ]; then
    echo "ERROR: no pool pods found with selector $SELECTOR" >&2
    exit 1
  fi
  echo "Deleting pod: $POD"
  kubectl delete pod "$POD" -n "$NAMESPACE"
fi

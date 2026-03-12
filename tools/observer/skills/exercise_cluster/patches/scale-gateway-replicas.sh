#!/usr/bin/env bash
# Scale multigateway replicas for a cell.
#
# Required env vars:
#   KUBECONFIG, CLUSTER_NAME, NAMESPACE, GATEWAY_PATH, REPLICAS
#
# GATEWAY_PATH is the JSON pointer to multigateway replicas, e.g.:
#   /spec/cells/0/spec/multigateway/replicas
#
# Usage:
#   GATEWAY_PATH=/spec/cells/0/spec/multigateway/replicas \
#   REPLICAS=2 CLUSTER_NAME=test NAMESPACE=default ./scale-gateway-replicas.sh

set -euo pipefail

kubectl patch multigrescluster "$CLUSTER_NAME" -n "$NAMESPACE" \
  --type=json \
  -p "[{\"op\":\"replace\",\"path\":\"${GATEWAY_PATH}\",\"value\":${REPLICAS}}]"

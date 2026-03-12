#!/usr/bin/env bash
# Scale pool replicasPerCell.
#
# Required env vars:
#   KUBECONFIG, CLUSTER_NAME, NAMESPACE, POOL_PATH, REPLICAS
#
# POOL_PATH is the JSON pointer to replicasPerCell, e.g.:
#   /spec/databases/0/tablegroups/0/shards/0/spec/pools/read-write/replicasPerCell
#
# Usage:
#   POOL_PATH=/spec/databases/0/tablegroups/0/shards/0/spec/pools/read-write/replicasPerCell \
#   REPLICAS=4 CLUSTER_NAME=test NAMESPACE=default ./scale-pool-replicas.sh

set -euo pipefail

kubectl patch multigrescluster "$CLUSTER_NAME" -n "$NAMESPACE" \
  --type=json \
  -p "[{\"op\":\"replace\",\"path\":\"${POOL_PATH}\",\"value\":${REPLICAS}}]"

#!/usr/bin/env bash
# Update postgres container resource limits for a pool.
#
# Required env vars:
#   KUBECONFIG, CLUSTER_NAME, NAMESPACE, POOL_PATH
#   CPU_REQUEST, MEM_REQUEST, CPU_LIMIT, MEM_LIMIT
#
# POOL_PATH is the JSON pointer to the pool, e.g.:
#   /spec/databases/0/tablegroups/0/shards/0/spec/pools/read-write
#
# Usage:
#   POOL_PATH=/spec/databases/0/tablegroups/0/shards/0/spec/pools/read-write \
#   CPU_REQUEST=250m MEM_REQUEST=384Mi CPU_LIMIT=500m MEM_LIMIT=512Mi \
#   CLUSTER_NAME=test NAMESPACE=default ./update-resource-limits.sh

set -euo pipefail

kubectl patch multigrescluster "$CLUSTER_NAME" -n "$NAMESPACE" \
  --type=json \
  -p "[{\"op\":\"add\",\"path\":\"${POOL_PATH}/postgres/resources\",\"value\":{\"requests\":{\"cpu\":\"${CPU_REQUEST}\",\"memory\":\"${MEM_REQUEST}\"},\"limits\":{\"cpu\":\"${CPU_LIMIT}\",\"memory\":\"${MEM_LIMIT}\"}}}]"

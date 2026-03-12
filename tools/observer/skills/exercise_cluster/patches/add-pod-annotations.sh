#!/usr/bin/env bash
# Add a pod annotation to a shard's multiorch.
#
# Required env vars:
#   KUBECONFIG, CLUSTER_NAME, NAMESPACE, SHARD_PATH, KEY, VALUE
#
# SHARD_PATH is the JSON pointer to the shard spec, e.g.:
#   /spec/databases/0/tablegroups/0/shards/0/spec
#
# Usage:
#   SHARD_PATH=/spec/databases/0/tablegroups/0/shards/0/spec \
#   KEY=chaos.multigres.com/test VALUE=exerciser \
#   CLUSTER_NAME=test NAMESPACE=default ./add-pod-annotations.sh

set -euo pipefail

kubectl patch multigrescluster "$CLUSTER_NAME" -n "$NAMESPACE" \
  --type=json \
  -p "[{\"op\":\"add\",\"path\":\"${SHARD_PATH}/multiorch/podAnnotations\",\"value\":{\"${KEY}\":\"${VALUE}\"}}]"

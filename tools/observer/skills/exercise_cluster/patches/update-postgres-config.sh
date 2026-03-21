#!/usr/bin/env bash
# update-postgres-config.sh — Update the postgres config ConfigMap to trigger a rolling update.
#
# Environment:
#   CONFIGMAP_NAME  — ConfigMap name (default: custom-pg-config)
#   CONFIGMAP_KEY   — key within the ConfigMap (default: postgresql.conf)
#   NAMESPACE       — namespace (default: default)
#   SHARED_BUFFERS  — new shared_buffers value (default: 512MB)
#   WORK_MEM        — new work_mem value (default: 16MB)
#   MAX_CONNECTIONS — new max_connections value (default: 50)
#
# Usage:
#   SHARED_BUFFERS=1GB ./patches/update-postgres-config.sh
#   SHARED_BUFFERS=512MB WORK_MEM=32MB ./patches/update-postgres-config.sh

set -euo pipefail

: "${CONFIGMAP_NAME:=custom-pg-config}"
: "${CONFIGMAP_KEY:=postgresql.conf}"
: "${NAMESPACE:=default}"
: "${SHARED_BUFFERS:=512MB}"
: "${WORK_MEM:=16MB}"
: "${MAX_CONNECTIONS:=50}"

CONFIG_CONTENT="# Custom PostgreSQL configuration (updated by exerciser)
shared_buffers = '${SHARED_BUFFERS}'
work_mem = '${WORK_MEM}'
max_connections = ${MAX_CONNECTIONS}
"

KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl create configmap "${CONFIGMAP_NAME}" \
  --from-literal="${CONFIGMAP_KEY}=${CONFIG_CONTENT}" \
  --namespace="${NAMESPACE}" \
  --dry-run=client -o yaml | \
  KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl apply -f -

echo "Updated ConfigMap ${CONFIGMAP_NAME} in ${NAMESPACE}: shared_buffers=${SHARED_BUFFERS} work_mem=${WORK_MEM} max_connections=${MAX_CONNECTIONS}"

#!/usr/bin/env bash
set -euo pipefail

# Resolve script directory for path-independent execution
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

kubectl apply -f "${SCRIPT_DIR}/kind-job-topo-registration.yaml"

kubectl apply -f "${SCRIPT_DIR}/multiadmin.yaml"

kubectl create configmap grafana-dashboard-multigres \
    --from-file=multigres.json="${SCRIPT_DIR}/grafana/dashboard.json" \
    --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f "${SCRIPT_DIR}/observability.yaml"

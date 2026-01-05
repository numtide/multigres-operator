#!/usr/bin/env bash
# Helper script to register cells and database in the topology server
# Uses the multigres CLI to properly encode topology data as protobuf.
#
# Usage: ./register-topology.sh <cells> <database-name> <topo-service> <namespace> <kubeconfig>
# Example: ./register-topology.sh zone-a postgres kind-toposerver-sample default /tmp/kind-kubeconfig.yaml

set -euo pipefail

if [ "$#" -ne 5 ]; then
  echo "Error: Requires exactly 5 arguments"
  echo "Usage: $0 <cells> <database-name> <topo-service> <namespace> <kubeconfig>"
  echo "Example: $0 zone-a postgres kind-toposerver-sample default /tmp/kind-kubeconfig.yaml"
  echo ""
  echo "Note: For multiple cells, use comma-separated: zone-a,zone-b"
  exit 1
fi

CELLS="$1"
DATABASE="$2"
TOPO_SERVICE="$3"
NAMESPACE="$4"
KUBE_CONFIG="$5"

GLOBAL_ROOT="/multigres/global"
TOPO_ADDRESS="${TOPO_SERVICE}:2379"

echo "Registering topology metadata..."
echo "  Cells: ${CELLS}"
echo "  Database: ${DATABASE}"
echo "  Topo Service: ${TOPO_ADDRESS}"
echo "  Namespace: ${NAMESPACE}"
echo "  Kubeconfig: ${KUBE_CONFIG}"
echo ""

# Find a pod with the multigres binary to run the command
# Try multiorch pod first, fallback to any pod with multigres image
EXEC_POD=$(kubectl --kubeconfig="$KUBE_CONFIG" get pods -n "$NAMESPACE" \
  -l app.kubernetes.io/component=multiorch \
  -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

if [ -z "$EXEC_POD" ]; then
  echo "Error: No multiorch pod found. Make sure Shard resources are deployed first."
  exit 1
fi

echo "Using pod '${EXEC_POD}' to run multigres CLI..."
echo ""

# Run createclustermetadata command
kubectl --kubeconfig="$KUBE_CONFIG" exec -n "$NAMESPACE" "$EXEC_POD" -- \
  /multigres/bin/multigres createclustermetadata \
    --global-topo-address="$TOPO_ADDRESS" \
    --global-topo-root="$GLOBAL_ROOT" \
    --cells="$CELLS" \
    --backup-location=/backup \
    --durability-policy=none

echo ""
echo "✓ Topology registered successfully"
echo ""
echo "Verify cell registration:"
echo "  kubectl --kubeconfig=$KUBE_CONFIG exec -n $NAMESPACE ${EXEC_POD} -- /multigres/bin/multigres getcellnames --global-topo-address=$TOPO_ADDRESS --global-topo-root=$GLOBAL_ROOT"
echo ""
echo "Verify database registration:"
echo "  kubectl --kubeconfig=$KUBE_CONFIG exec -n $NAMESPACE ${EXEC_POD} -- /multigres/bin/multigres getdatabasenames --global-topo-address=$TOPO_ADDRESS --global-topo-root=$GLOBAL_ROOT"

# Kind Cluster Topology Awareness

**Default kind clusters have a single node with NO topology labels.** This matters when cells specify `zone` or `region` fields:

- The operator translates `CellConfig.Zone` into `nodeSelector: topology.kubernetes.io/zone: <value>` on all pods in that cell.
- If no node has the matching label, pods stay **Pending forever**. The webhook warns about this (`no nodes currently match zone=X`) — treat that warning as a hard stop.
- The `multi-cell-quorum` fixture uses logical cell names only (no `zone`/`region`), so it works on a default single-node kind cluster.

## For Topology Testing

Zone-aware scheduling, verifying pods land on correct nodes:

1. Destroy any existing kind cluster: `make kind-down`
2. Deploy with topology zones: `make kind-deploy-topology`
   - Creates a 3-worker kind cluster where each worker has a distinct `topology.kubernetes.io/zone` label (us-east-1a, us-east-1b, us-east-1c)
   - The control-plane node has no zone label
3. Use the `multi-cell-topology` fixture, which sets `zone` on each cell to match.

## On EKS/GKE

Nodes already have `topology.kubernetes.io/zone` labels automatically — no special cluster setup needed. Use the real availability zones from `kubectl get nodes --show-labels | grep topology`.

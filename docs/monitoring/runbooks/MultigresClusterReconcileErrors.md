# MultigresClusterReconcileErrors

## Meaning

The MultigresCluster or TableGroup controller is failing to reconcile resources. The `controller_runtime_reconcile_errors_total` counter is increasing.

## Impact

Child resources (Cells, Shards, TopoServers) may not be created, updated, or deleted correctly. The cluster may drift from the desired state.

## Investigation

```bash
# Check operator logs for errors
kubectl logs -n multigres-operator deploy/multigres-operator-controller-manager --tail=100 | grep -i error

# Check the cluster's status and events
kubectl describe multigrescluster <name> -n <namespace>

# Check if the controller is running
kubectl get pods -n multigres-operator -l control-plane=controller-manager
```

## Remediation

1. **Transient errors** — If errors are caused by temporary API server issues, they will self-resolve. Monitor for 5–10 minutes.
2. **Configuration errors** — Check if the MultigresCluster spec references templates or resources that don't exist.
3. **Resource limits** — Ensure the operator pod has sufficient CPU and memory. Check for OOMKilled events.
4. **Restart** — If the operator is wedged, restart the controller manager: `kubectl rollout restart deploy/multigres-operator-controller-manager -n multigres-operator`.

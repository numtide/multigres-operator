# MultigresClusterDegraded

## Meaning

A MultigresCluster has been in a non-Healthy phase (e.g., `Provisioning`, `Error`, `Degraded`) for more than 10 minutes. The `multigres_operator_cluster_info` gauge reports the current phase.

## Impact

The cluster may not be fully operational. Some cells or shards may be unavailable, and database traffic may be affected depending on the phase.

## Investigation

```bash
# Check the cluster's current phase and conditions
kubectl get multigrescluster <name> -n <namespace> -o yaml | grep -A 20 status:

# Check events on the cluster
kubectl describe multigrescluster <name> -n <namespace>

# Check the status of child resources
kubectl get cells,shards,toposervers -n <namespace> -l multigres.com/cluster=<name>

# Check operator logs for this cluster
kubectl logs -n multigres-operator deploy/multigres-operator-controller-manager --tail=200 | grep <name>
```

## Remediation

1. **Provisioning phase** — If the cluster is new, allow additional time. Check if all child resources are being created.
2. **Error phase** — Inspect the cluster's status conditions and events for the specific error. Common causes: missing templates, invalid spec, insufficient cluster resources.
3. **Degraded phase** — Some sub-resources are unhealthy. Check individual Cell and Shard statuses for the degraded component.

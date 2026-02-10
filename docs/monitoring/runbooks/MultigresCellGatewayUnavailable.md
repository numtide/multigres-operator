# MultigresCellGatewayUnavailable

## Meaning

A Cell's MultiGateway has zero ready replicas. The `multigres_operator_cell_gateway_replicas{state="ready"}` gauge is 0 for this cell.

## Impact

**Critical.** No traffic can be routed to this cell. All database connections through this cell's gateway will fail. This is a data-plane outage for the affected cell.

## Investigation

```bash
# Check the MultiGateway deployment
kubectl get deploy -n <namespace> -l multigres.com/cell=<cell-name>,multigres.com/component=multigateway

# Check pod status
kubectl get pods -n <namespace> -l multigres.com/cell=<cell-name>,multigres.com/component=multigateway

# Check pod events for crash/scheduling issues
kubectl describe pod -n <namespace> -l multigres.com/cell=<cell-name>,multigres.com/component=multigateway

# Check the Cell resource status
kubectl describe cell <cell-name> -n <namespace>

# Check node resources
kubectl top nodes
```

## Remediation

1. **CrashLoopBackOff** — Check container logs for the failing pod: `kubectl logs <pod> -n <namespace>`. Common causes: misconfigured connection strings, missing secrets, image pull errors.
2. **Pending pods** — Check for insufficient resources (CPU/memory requests exceeding node capacity) or node affinity/taint issues.
3. **Image issues** — Verify the MultiGateway image exists and is pullable from the cluster.
4. **Scale to zero** — If the deployment was accidentally scaled down, the operator should reconcile it back. If not, check the Cell spec's replica count.

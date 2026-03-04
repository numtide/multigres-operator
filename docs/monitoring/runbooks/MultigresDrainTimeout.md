# MultigresDrainTimeout

## Meaning

Drain operations are timing out for a shard. The `multigres_operator_drain_operations_total{result="timeout"}` counter has been incrementing for more than 10 minutes.

## Impact

Pods targeted for removal (during scale-down or rolling updates) are stuck in the drain state machine. This blocks scale-down completion, rolling update progress, and may leave stale entries in the topology server.

## Investigation

```bash
# Check for pods with drain annotations
kubectl get pods -n <namespace> -l multigres.com/shard=<shard-name> \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.drain\.multigres\.com/state}{"\t"}{.metadata.annotations.drain\.multigres\.com/requested-at}{"\n"}{end}'

# Check shard controller logs for drain errors
kubectl logs -n multigres-operator deploy/multigres-operator-controller-manager --tail=200 | grep -i drain

# Check topology server connectivity
kubectl get toposerver -n <namespace>
kubectl describe toposerver <toposerver-name> -n <namespace>

# Check etcd pod health (if using managed etcd)
kubectl get pods -n <namespace> -l multigres.com/component=etcd

# Check the Shard status for pool details
kubectl describe shard <shard-name> -n <namespace>
```

## Remediation

1. **Topology server unreachable** — If the etcd cluster is unhealthy, drain cannot complete because it needs to unregister the pod from topology. Fix the TopoServer first.
2. **Stuck in `requested` state** — The shard controller's data-plane reconciliation may be failing. Check operator logs for errors in `reconcileDataPlane`.
3. **Stuck in `draining` state** — The sync standby removal may have failed. Check if the primary pod is healthy and accepting RPC calls.
4. **Stuck in `acknowledged` state** — The verification step may be failing. Check if `pg_stat_replication` on the primary still lists the drained standby.
5. **Manual cleanup** — As a last resort, manually remove the drain annotation from the stuck pod: `kubectl annotate pod <pod> drain.multigres.com/state-`. The shard controller will re-evaluate on the next reconcile.

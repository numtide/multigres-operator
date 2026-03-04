# MultigresRollingUpdateStuck

## Meaning

A pool's rolling update has been in progress for more than 30 minutes. The `multigres_operator_rolling_update_in_progress` gauge has been 1 for the affected pool continuously.

## Impact

The pool is running a mix of old and new pod specs. Depending on the change, this may cause inconsistent behavior across replicas. New pods may be stuck in Pending or CrashLoopBackOff, blocking the rollout from completing.

## Investigation

```bash
# Check which pods have spec-hash mismatch (drifted)
kubectl get pods -n <namespace> -l multigres.com/shard=<shard-name>,multigres.com/pool=<pool-name> \
  --show-labels | grep spec-hash

# Check for pods stuck in non-Ready state
kubectl get pods -n <namespace> -l multigres.com/shard=<shard-name>,multigres.com/pool=<pool-name>

# Check for pods in drain state
kubectl get pods -n <namespace> -l multigres.com/shard=<shard-name> \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.drain\.multigres\.com/state}{"\n"}{end}'

# Check pod events for scheduling or image pull issues
kubectl describe pods -n <namespace> -l multigres.com/shard=<shard-name>,multigres.com/pool=<pool-name>

# Check the Shard status
kubectl describe shard <shard-name> -n <namespace>
```

## Remediation

1. **Pod stuck Pending** — Check for insufficient resources or node affinity mismatches. Scale the node pool or adjust resource requests.
2. **Pod stuck CrashLoopBackOff** — Check container logs. The new spec may have a configuration error. Consider reverting the template change.
3. **Drain stuck** — If a pod is stuck in a drain state, check topology server connectivity. See `MultigresDrainTimeout`.
4. **Health gate blocking** — Rolling updates are deferred when the pool is already degraded. Resolve the degraded state first (see `MultigresShardPoolDegraded`).

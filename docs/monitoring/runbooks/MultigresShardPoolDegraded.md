# MultigresShardPoolDegraded

## Meaning

A Shard's pool has fewer ready replicas than desired for more than 10 minutes. The `multigres_operator_shard_pool_replicas` gauge shows a mismatch between `state="ready"` and `state="desired"`.

## Impact

Reduced redundancy for database connections in the affected shard. Depending on the pool type and remaining replicas, query latency may increase or some read traffic may fail.

## Investigation

```bash
# Check the pool deployment
kubectl get deploy -n <namespace> -l multigres.com/shard=<shard-name>,multigres.com/pool=<pool-name>

# Check pod status
kubectl get pods -n <namespace> -l multigres.com/shard=<shard-name>,multigres.com/pool=<pool-name>

# Check for scheduling or resource issues
kubectl describe pods -n <namespace> -l multigres.com/shard=<shard-name>,multigres.com/pool=<pool-name>

# Check the Shard resource status
kubectl describe shard <shard-name> -n <namespace>
```

## Remediation

1. **Pending pods** — Check for insufficient cluster resources. Consider scaling the node pool or reducing resource requests.
2. **CrashLoopBackOff** — Check container logs. Common causes: database connection failures, misconfigured pool settings.
3. **PVC issues** — If pools use persistent storage, check PVC status: `kubectl get pvc -n <namespace> -l multigres.com/shard=<shard-name>`.
4. **Rolling update** — If an update is in progress, allow time for the rollout to complete.

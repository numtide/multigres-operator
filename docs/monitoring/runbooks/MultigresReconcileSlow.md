# MultigresReconcileSlow

## Meaning

The p99 reconcile duration for a controller has exceeded 30 seconds for more than 5 minutes. This uses the built-in `controller_runtime_reconcile_time_seconds` histogram.

## Impact

Slow reconciles delay the operator's ability to converge on the desired state. Changes to MultigresCluster specs will take longer to propagate.

## Investigation

```bash
# Check operator resource usage
kubectl top pod -n multigres-operator -l control-plane=controller-manager

# Check operator logs for slow operations
kubectl logs -n multigres-operator deploy/multigres-operator-controller-manager --tail=200 | grep duration

# Check API server latency
kubectl get --raw /metrics | grep apiserver_request_duration

# Check if the cluster has many resources
kubectl get multigrescluster,cell,shard,toposerver --all-namespaces | wc -l
```

## Remediation

1. **API server latency** — If the Kubernetes API server is slow, reconciles will be slow. Check API server health and latency metrics.
2. **Large clusters** — Clusters with many cells/shards take longer to reconcile. This may be expected behavior. Consider increasing the slow threshold.
3. **Resource limits** — Increase CPU/memory limits on the operator pod if it's being throttled.
4. **Template resolution** — If templates involve many lookups, consider caching improvements.

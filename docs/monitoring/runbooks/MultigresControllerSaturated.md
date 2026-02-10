# MultigresControllerSaturated

## Meaning

The controller's work queue has been deeper than 50 items for more than 10 minutes. The `workqueue_depth` metric shows the controller cannot process incoming events fast enough.

## Impact

Reconciliation of MultigresCluster or TableGroup resources is lagging behind changes. Users will experience increased latency between applying changes and seeing them reflected in child resources.

## Investigation

```bash
# Check the current queue depth in metrics
kubectl port-forward -n multigres-operator deploy/multigres-operator-controller-manager 8443:8443
curl -sk https://localhost:8443/metrics | grep workqueue_depth

# Check operator resource usage
kubectl top pod -n multigres-operator -l control-plane=controller-manager

# Check the number of managed resources
kubectl get multigrescluster --all-namespaces --no-headers | wc -l

# Check if there's a storm of events (e.g., many updates at once)
kubectl logs -n multigres-operator deploy/multigres-operator-controller-manager --tail=500 | grep "reconcile started" | tail -20
```

## Remediation

1. **Increase concurrency** — Increase `MaxConcurrentReconciles` for the saturated controller in the operator's configuration.
2. **Scale resources** — Increase CPU limits on the operator pod to allow faster reconciliation.
3. **Event storm** — If many resources were updated simultaneously (e.g., a bulk template change), the queue will drain naturally. Monitor and wait.
4. **Excessively frequent reconciles** — Check if a resource is rapidly flapping (constantly being updated), causing the controller to re-queue continuously. Inspect the resource's events and generation number.

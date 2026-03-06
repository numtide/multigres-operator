# Exerciser Reference (Level 1)

The exerciser runs predefined scenarios that perform normal operational tasks a user would do, then validates the outcome using the observer. It answers the question: **"Does the operator handle day-to-day operations correctly?"**

Deploy with:

```bash
CHAOS_LEVEL=1 make kind-deploy-chaos
```

---

## How It Works

Each scenario follows a four-phase lifecycle:

```
Setup → Execute → Validate → Teardown
```

1. **Setup:** Snapshot current state, verify preconditions
2. **Execute:** Perform the operation (patch a CRD, delete a pod, etc.)
3. **Validate:** Run observer checks continuously until the cluster converges to the expected state or timeout
4. **Teardown:** Restore original state for the next scenario

**Dry-run mode** (`--dry-run`): Logs what each scenario would do without executing any mutations. Use this on first deployment to a real cluster.

### Result Reporting

Each scenario result is emitted as a structured JSON log:

```json
{
  "ts": "2026-03-05T12:05:00Z",
  "level": "info",
  "check": "scenario-result",
  "scenario": "scale-up-pool-replicas",
  "result": "PASS",
  "duration": "45s",
  "details": {
    "operation": "Scaled pool replicas from 1 to 3",
    "validations_passed": 12,
    "validations_failed": 0
  }
}
```

At Level 1+, results are also persisted as `ChaosReport` CRDs for programmatic access. Old reports are garbage-collected after 24h.

---

## Scenario Catalog

### Scale Scenarios (`exerciser/scale.go`)

| Scenario | Operation | Validation | Timeout |
|----------|----------|------------|---------|
| **Scale up pool replicas** | Increase `pools[0].replicasPerCell` by 1 | New pod appears, becomes Ready, registers in etcd, service endpoints update, `readyReplicas` increments, `podRoles` updated | 5min |
| **Scale down pool replicas** | Decrease `pools[0].replicasPerCell` by 1 | Full drain lifecycle fires, pod terminates, PVC behavior matches `pvcDeletionPolicy.whenScaled`, pooler deregisters | 7min |
| **Scale MultiOrch** | Change `spec.multiorch.replicas` | Deployment scales, pods Ready, `orchReady` updates | 5min |
| **Scale MultiGateway** | Change `cell.spec.multigateway.replicas` | Deployment scales, pods Ready, service endpoints update | 5min |
| **Scale TopoServer** | Change TopoServer replicas | StatefulSet scales, etcd cluster health maintained | 5min |
| **Scale to zero and back** | Scale pool to 0, wait, scale back | All pods drain, then all come back healthy | 10min |

### Config Mutation Scenarios (`exerciser/config.go`)

| Scenario | Operation | Validation | Timeout |
|----------|----------|------------|---------|
| **Update resource limits** | Patch pool spec with new CPU/memory | Rolling update triggers (spec-hash changes), new pods have updated resources | 5min |
| **Expand storage** | Patch pool spec with larger storage | PVC resize event fires, PVC capacity increases | 5min |
| **Add/remove labels** | Patch pool/gateway spec with labels | Labels propagated to pods, no restart | 2min |
| **Add/remove annotations** | Patch pool/gateway spec with annotations | Annotations propagated, no restart | 2min |
| **Change MultiGateway args** | Modify gateway configuration | Rolling update of gateway deployment | 5min |

### Restart Scenarios (`exerciser/restart.go`)

| Scenario | Operation | Validation | Timeout |
|----------|----------|------------|---------|
| **Delete replica pod** | Delete a non-primary pool pod | Pod recreated, becomes Ready, re-registers, data intact (PVC preserved) | 5min |
| **Delete primary pod** | Delete the primary (from `podRoles`) | Failover happens, new primary elected, old pod recreated as standby | 5min |
| **Delete operator pod** | Delete the operator deployment's pod | Operator restarts, resumes reconciliation, no state lost | 3min |
| **Delete MultiGateway pod** | Delete one gateway pod | Deployment recreates it, connectivity restored | 3min |
| **Delete TopoServer pod** | Delete one etcd pod | StatefulSet recreates it, etcd cluster health recovers | 5min |

### Lifecycle Scenarios (`exerciser/lifecycle.go`)

| Scenario | Operation | Validation | Timeout |
|----------|----------|------------|---------|
| **Create second cluster** | Apply a new MultigresCluster CR | Entire resource hierarchy created, cluster becomes Healthy | 10min |
| **Delete and recreate** | Delete cluster, wait for cleanup, recreate | Clean start, no conflicts from old resources | 15min |

---

## Cooldown

A configurable cooldown period (`--scenario-cooldown`, default 30s) runs between scenarios. This allows the reconciler to quiesce before the next scenario starts.

---

## ChaosReport CRD

Results are stored as CRDs for programmatic access:

```yaml
apiVersion: multigres.com/v1alpha1
kind: ChaosReport
metadata:
  name: scale-up-pool-replicas-20260305-120500
  namespace: multigres-operator
spec:
  scenario: scale-up-pool-replicas
  level: 1
  startedAt: "2026-03-05T12:05:00Z"
status:
  result: Pass  # Pass | Fail | Skipped | Error
  completedAt: "2026-03-05T12:05:45Z"
  duration: 45s
  validationsPassed: 12
  validationsFailed: 0
  findings: []  # Populated on failures
```

Query results:

```bash
kubectl get chaosreports -n multigres-operator
kubectl get chaosreport scale-up-pool-replicas-20260305-120500 -o yaml
```

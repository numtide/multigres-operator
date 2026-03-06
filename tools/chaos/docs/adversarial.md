# Adversarial Reference (Level 3)

The adversarial level tests the most hostile scenarios: concurrent conflicting mutations, cascading failures, state corruption, and operator upgrades under load. It answers the question: **"Does the operator maintain correctness under adversarial conditions?"**

Deploy with:

```bash
CHAOS_LEVEL=3 make kind-deploy-chaos
```

---

## Scenario Catalog

### Concurrent Mutations (`adversarial/concurrent.go`)

These scenarios test the operator's behavior when multiple conflicting operations happen simultaneously.

| Scenario | Operations (in parallel) | Validation |
|----------|----------------------|------------|
| **Simultaneous scale up + down** | Scale shard A up, scale shard B down | Both complete correctly, no cross-contamination |
| **Modify CR during drain** | Start scale-down; while in `draining`, patch the CR | Drain completes, config change applies after drain, no state corruption |
| **Delete shard during scale-up** | Start scaling up; before pods ready, delete the shard | All resources cleaned up, no orphaned pods/PVCs |
| **Rapid spec mutations** | Patch MultigresCluster 5 times in quick succession | Operator converges to final desired state, no intermediate state leaks |

### Cascade Failures (`adversarial/cascade.go`)

These scenarios combine multiple failures to validate resilience under compound stress.

| Scenario | Failures Combined | Validation |
|----------|------------------|------------|
| **etcd kill + drain** | Kill TopoServer pod while drain is in progress | Drain handles topology unavailability, etcd recovers, drain completes |
| **Operator kill during drain** | Start scale-down, kill operator mid-drain | Operator picks up drain from where it left off (drain state annotations persist) |
| **Network partition + pod kill** | Partition a pool pod from etcd, then kill it | Pod recreated, re-registers when partition heals |
| **Delete cluster while degraded** | Introduce a fault, then delete MultigresCluster | Cluster deletion completes despite degraded state, all resources cleaned up |

### State Corruption (`adversarial/corruption.go`)

These scenarios intentionally corrupt state to verify the operator's self-healing capability.

| Scenario | Corruption | Validation |
|----------|-----------|------------|
| **Remove drain annotation mid-drain** | Delete `drain.multigres.com/state` while pod is `draining` | Operator re-applies or handles gracefully |
| **Add drain annotation to healthy pod** | Set `drain.multigres.com/state: requested` on a running pod | Operator detects and handles (proceeds with drain or removes annotation) |
| **Delete PVC while pod is running** | Delete the data PVC of a running pool pod | Pod fails, operator recreates both PVC and pod |
| **Directly modify child CRD** | Edit a Shard CR directly (bypassing MultigresCluster) | Operator reverts the change (child CRDs are owned by parent) |

### Operator Upgrade Under Load (`adversarial/upgrade.go`)

Tests zero-downtime operator upgrade while workloads are actively serving traffic.

| Phase | Action | Validation |
|-------|--------|------------|
| **Setup** | Ensure cluster Healthy, start `SELECT 1` background workload | Cluster fully operational |
| **Execute** | Patch operator Deployment to trigger rolling update | Old pod terminates, new one starts |
| **Validate** | Monitor throughout | No SQL errors during restart, ObservedGeneration catches up, no duplicate resources, cluster never leaves Healthy (or briefly Progressing then back) |
| **Teardown** | Stop background workload | Cluster remains Healthy |
| **Timeout** | — | 5min |

---

## Resilience Guarantees Tested

Level 3 scenarios collectively validate:

1. **Ordering:** The operator handles out-of-order events correctly (e.g., drain state changes during a concurrent spec update)
2. **Idempotency:** Repeated reconciliation of the same state produces the same result
3. **Crash recovery:** Operator restarts mid-operation don't corrupt state
4. **Ownership:** OwnerReferences ensure cascade deletion works even under compound failures
5. **State persistence:** Drain state annotations survive operator restarts and are re-evaluated correctly
6. **Self-healing:** Corrupted state is detected and corrected within a bounded time window

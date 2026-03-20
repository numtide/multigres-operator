# Core Mode Scenarios

These scenarios form the **core** execution mode — the default when the user says "exercise the cluster" without specifying a mode.

---

### scale-up-pool-replicas
**Tier:** standard | **Fast-path:** yes
**Tests:** Pool replica increase, new pod creation, replication setup
**Success criteria:** Pool pod count per cell == new replicasPerCell, all pods Running+Ready

**How to patch:**
1. Find all pools in the CR. They may be at:
   - `.spec.databases[].tablegroups[].shards[].spec.pools.<name>` (inline spec)
   - `.spec.databases[].tablegroups[].shards[].overrides.pools.<name>` (override-based)
2. Pick the first readWrite pool. Note its name and current `replicasPerCell`.
3. JSON patch to increment replicasPerCell by 1.

**What to observe:**
- New pod appears with correct labels and joins the StatefulSet-like set
- Observer connectivity checks include the new pod after startup
- Observer replication check shows the new pod as a connected replica
- No split-brain findings at any point

**Teardown:** Patch replicasPerCell back to original value.

### scale-down-pool-replicas
**Tier:** standard | **Fast-path:** no (drain machine)
**Tests:** Drain state machine, graceful pod removal, PVC handling
**Negative assertion:** Resource Removal — pod count decreased by 1 after drain completes

**How to patch:**
1. Same discovery as scale-up. Current replicasPerCell must be > 3.
2. JSON patch to decrement by 1.

> **IMPORTANT:** Never scale below `replicasPerCell: 3`. Multigres uses `AT_LEAST_2` synchronous quorum by default, which requires at least 3 poolers. Scaling to 2 or fewer causes write stalls and spurious recovery actions unrelated to operator logic.

**What to observe:**
- Drain state annotations progress: `DrainStateRequested` → `DrainStateDraining` → `DrainStateAcknowledged` → `DrainStateReadyForDeletion`
- Observer `drain-state` findings during the drain are EXPECTED and normal
- Drain should complete within timeouts (30s / 5min / 30s per state)
- After drain completes, the pod should terminate
- **If drain gets stuck** (same state beyond its timeout) — that is a bug
- After full stabilization, no drain-state errors should remain

**Teardown:** Patch replicasPerCell back to original value.

### scale-multigateway-replicas
**Tier:** standard | **Fast-path:** yes
**Tests:** Gateway Deployment scaling
**Success criteria:** Deployment `.status.readyReplicas` == target

**How to patch:**
1. Read `.spec.cells[0].name` to get the cell name.
2. Determine if the cell uses `.spec.multigateway` or `.overrides.multigateway`.
3. Read current `replicas` value.
4. Merge patch to increment by 1 (use the correct path: spec or overrides).

**What to observe:**
- New gateway pod appears in the cell's MultiGateway Deployment
- Observer connectivity checks cover the new gateway endpoint

**Teardown:** Patch replicas back to original.

### update-resource-limits
**Tier:** standard | **Fast-path:** yes
**Tests:** Rolling update triggered by resource change, pod recreation order
**Success criteria:** All pool pods Running+Ready with updated resource values

**How to patch:**
1. Read the live CR and find the first pool. Resources are on the **container sub-objects** (`postgres` and `multipooler`), NOT at pool level. The correct path is `pools.<name>.postgres.resources` (for the postgres container) or `pools.<name>.multipooler.resources`.
2. Merge patch example (adjust the shard/pool path to match your fixture):
   ```bash
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{
     "spec": {"databases": [{"name": "<db>", "default": true, "tablegroups": [{"name": "<tg>", "default": true, "shards": [{"name": "<shard>", "spec": {"pools": {"<pool>": {"postgres": {"resources": {"requests": {"cpu": "50m", "memory": "128Mi"}, "limits": {"cpu": "200m", "memory": "256Mi"}}}}}}}]}]}]}
   }'
   ```
   > **Common mistake:** patching `pools.<name>.resources` directly — this field does not exist. The `PoolSpec` has `postgres ContainerConfig` and `multipooler ContainerConfig` sub-objects, each with their own `resources`.

**What to observe:**
- Pods restart one by one (rolling update via the resource-handler)
- **Brief connectivity errors during each pod's restart are expected** — these should resolve as each pod comes back up
- After all pods restart, full connectivity restored
- Replication fully re-established across all replicas
- The key bug signal: errors that persist AFTER all pods are Ready and past grace period

**Teardown:** Remove the resources field or restore original values.

### delete-pool-pod
**Tier:** standard | **Fast-path:** yes
**Tests:** Pod recreation, replication recovery, potential failover
**Success criteria:** Replacement pod Running+Ready, total pod count unchanged

**How to execute:**
1. List pool pods: `kubectl get pods -n <ns> -l app.kubernetes.io/component=shard-pool`
2. Check which pod is primary (from observer's replication probe data or pod labels).
3. Delete the pod: `kubectl delete pod <name> -n <ns>`

**What to observe:**
- Pod is recreated by the operator
- **If primary was deleted**: multiorch should perform failover. Brief write unavailability is expected. A new primary should be elected. Observer should detect the failover sequence.
- **If replica was deleted**: primary maintains writes, replica should rejoin replication after restart.
- After stabilization: replication fully healthy, no split-brain, connectivity restored

**Teardown:** Not needed — Kubernetes recreates the pod.

# Lifecycle & Drain Scenarios

Scenarios involving cluster lifecycle operations, the drain state machine, and rolling update paths.

---

### delete-and-recreate-cluster
**Tier:** lifecycle | **Fast-path:** no
**Tests:** Full teardown, cascade deletion via ownerReferences, fresh bootstrap
**Also in e2e:** Basic deletion + cleanup in `test/e2e/shared/deletion/`. Exerciser adds data-plane stability verification via observer after recreation.
**Negative assertion:** Cleanup Verification — all managed pods and child CRs gone before recreate

**How to execute:**
1. Save current CR: `kubectl get multigrescluster <name> -n <ns> -o yaml > /tmp/cluster-backup.yaml`
2. Delete: `kubectl delete multigrescluster <name> -n <ns>`
3. Wait for all managed pods to terminate: `kubectl get pods -n <ns> -l app.kubernetes.io/part-of=multigres`
4. Recreate: `kubectl apply -f /tmp/cluster-backup.yaml`

**What to observe:**
- During deletion: all child resources (Cell, Shard, TopoServer, pods, services) should cascade-delete via ownerReferences. The operator uses NO finalizers.
- If any child resources linger after the parent is gone, that's an ownerReference bug.
- During recreation: full bootstrap sequence — TopoServer, Cells, Shards, pool pods
- After recreation: full stability verification with lifecycle tier (10 min timeout, 90s observation)

**Teardown:** Not needed — cluster is back.

### multiadmin-scale
**Tier:** standard | **Fast-path:** yes
**Tests:** MultiAdmin deployment replica scaling up and down
**Applicable fixtures:** `overrides-complex`, `templated-full`

**How to execute:**
1. Read current multiadmin replica count: `kubectl get multigrescluster <name> -n <ns> -o jsonpath='{.spec.multiadmin.spec.replicas}'`
2. **Mutation A** (scale up): Merge patch `spec.multiadmin.spec.replicas` to current+1.
   ```
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"multiadmin":{"spec":{"replicas":<current+1>}}}}'
   ```
3. Wait for multiadmin deployment to scale: `kubectl get deployment -n <ns> -l app.kubernetes.io/component=multiadmin`
4. Verify all multiadmin pods are Running+Ready.
5. **Mutation B** (scale down): Patch replicas back to original value.
6. Verify deployment scales down cleanly.

**Success criteria:**
- Multiadmin deployment matches requested replica count after each mutation
- All multiadmin pods Running+Ready
- No operator error logs during scaling
- Cluster remains Healthy throughout

**Teardown:** Restore original replica count if not already done.

---

### drain-abort-on-scale
**Tier:** standard | **Fast-path:** no
**Tests:** Scaling back up while a drain is in progress

**How to execute:**
1. Record baseline replicasPerCell (must be >= 2 for scale-down).
2. **Mutation A**: Scale down pool replicas by 1 to trigger a drain.
3. **Watch** drain annotations on pods: `kubectl get pods -n <ns> -l multigres.com/shard=<shard> -o jsonpath='{range .items[*]}{.metadata.name}: {.metadata.annotations.drain\.multigres\.com/state}{"\n"}{end}'`
4. **While drain is in `draining` state** (within 30s-60s of scale-down), **Mutation B**: Scale replicas back to original value.
5. Run full Stability Verification Protocol.

**Success criteria:**
- Drain either completes or aborts cleanly — no stuck drain annotations
- Final pod count matches the restored replicasPerCell
- All pods Running+Ready with correct roles
- Observer drain-state check shows no persistent findings
- No pods in `ready-for-deletion` state without corresponding deletion

**What to observe:**
- The operator should handle the conflicting intent gracefully
- Watch for pods stuck in draining state indefinitely
- Check for race conditions: drain deleting a pod while scale-up is creating one

**Teardown:** If replicas don't match original, restore manually.

### drain-primary-scale-down
**Tier:** lifecycle | **Fast-path:** no
**Tests:** Primary pod drain state machine when forced by scale-down
**Applicable fixtures:** `minimal-retain`, `minimal-delete` (replicasPerCell >= 4)

**How to execute:**
1. Identify the primary pod:
   ```
   kubectl get pods -n <ns> -l multigres.com/shard=<shard>,multigres.com/pool=<pool> -o jsonpath='{range .items[*]}{.metadata.name}: {.metadata.annotations.role\.multigres\.com/postgres}{"\n"}{end}'
   ```
2. Record the primary pod name.
3. **Mutation**: Scale down replicasPerCell by 1 to force at least one pod to drain. If the primary pod is selected for drain, the operator must perform a primary drain (failover + drain).
4. **Watch** drain annotations progress through all 4 states:
   - `requested` → `draining` → `acknowledged` → `ready-for-deletion`
5. After drain completes, verify a new primary has been elected.
6. Run full Stability Verification Protocol.

**Success criteria:**
- Drain annotations progress through all 4 states without getting stuck
- If primary was drained, a new primary is elected
- Final pod count matches the new replicasPerCell
- All remaining pods Running+Ready
- Observer drain-state check shows no persistent findings
- Replication re-establishes with the new primary

**What to observe:**
- Watch for primary drain taking longer than replica drains (expected — involves failover)
- Check that no two pods are in drain state simultaneously (operator drains one at a time)
- Watch for replication lag spikes during primary failover

**Teardown:** Scale replicasPerCell back to original value.

### rolling-update-drain-path
**Tier:** standard | **Fast-path:** no
**Tests:** Config changes trigger proper drain→replace cycle through the drain state machine

**How to execute:**
1. Record baseline pod names and their creation timestamps.
2. **Mutation**: Change pool resource limits to trigger a rolling update:
   ```
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"shards":[{"name":"<shard>","pools":[{"name":"<pool>","resources":{"limits":{"memory":"512Mi"}}}]}]}}'
   ```
3. **Monitor** drain state transitions on pods during the rolling update:
   ```
   watch -n2 'kubectl get pods -n <ns> -l multigres.com/shard=<shard> -o custom-columns=NAME:.metadata.name,DRAIN:.metadata.annotations.drain\.multigres\.com/state,READY:.status.conditions[?\(@.type==\"Ready\"\)].status,AGE:.metadata.creationTimestamp'
   ```
4. Verify pods are replaced one at a time (no concurrent drains on same shard).
5. Run full Stability Verification Protocol after all pods are replaced.

**Success criteria:**
- Each old pod goes through: requested → draining → acknowledged → ready-for-deletion → deleted
- New pod is created after old pod is deleted (serial replacement)
- No concurrent drains on the same shard at any point
- All new pods have the updated resource limits
- Observer drain-state check shows proper transitions with no timeouts

**What to observe:**
- Drain state machine must progress forward-only
- Watch for stuck drains (same state beyond timeout)
- Primary pod should be drained last (if applicable)
- Replication should remain healthy throughout (check observer replication findings)

**Teardown:** None needed — new state is valid.

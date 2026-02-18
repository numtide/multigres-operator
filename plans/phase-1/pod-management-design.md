# Pod Management Design: StatefulSet → Direct Pod Management

> **Status**: Draft — For Review by Multigres Team
>
> **Goal**: Document the design for replacing StatefulSets with direct pod management in the operator, covering pod identity, decommissioning, failure scenarios, and scope limitations.

---

## Table of Contents

1. [Background](#1-background)
2. [Resource Topology: Before & After](#2-resource-topology-before--after)
3. [What We Gain & Lose (StatefulSet → Pods)](#3-what-we-gain--lose-statefulset--pods)
4. [Scope Limitations (v1alpha1)](#4-scope-limitations-v1alpha1)
5. [Pod Identity: Primary vs Replica](#5-pod-identity-primary-vs-replica)
6. [Pod Lifecycle & Decommissioning](#6-pod-lifecycle--decommissioning)
7. [Failure Scenarios](#7-failure-scenarios)
8. [Scaling Operations](#8-scaling-operations)
9. [Rolling Updates](#9-rolling-updates)
10. [pgBackRest & Backup Infrastructure](#10-pgbackrest--backup-infrastructure)
11. [Rename Prevention](#11-rename-prevention)
12. [Open Questions](#12-open-questions)

---

## 1. Background

The operator currently uses StatefulSets to manage pool pods. However, multigres has its own identity and discovery system (via etcd topology) that is completely independent of Kubernetes StatefulSet identity. The operator uses `ParallelPodManagement` and does not leverage ordered deployment/scaling — the two main features that StatefulSets provide over Deployments.

The key motivation is that StatefulSets introduce limitations that conflict with multigres's own management model:
- Cannot target individual pods for decommissioning (must scale from the tail)
- Cannot manage pods across zones or cells independently
- Rename/replace operations become ambiguous with StatefulSet ordinal identity
- `PersistentVolumeClaimRetentionPolicy` is the only mechanism for PVC lifecycle

### How Multigres Works (Key Concepts)

Multigres manages its own cluster state through an **etcd topology store**. Each component discovers others by reading etcd — not through Kubernetes pod names or services:

- **Multipooler**: Registers itself in etcd at startup using `ts.RegisterMultiPooler()` with its `--service-id` (currently set to `$(POD_NAME)`). On shutdown, it sets `ServingStatus = NOT_SERVING` in etcd but **never deletes its registration** — the code comment says: *"If they are actually deleted, they need to be cleaned up outside the lifecycle of starting/stopping."*

- **Multiorch**: Discovers poolers by querying etcd (`GetMultiPoolersByCell()`), not the Kubernetes API. It health-checks them via gRPC, detects problems (dead primary, broken replication), and executes recovery actions (failover, replication fix). It does **not** know what a StatefulSet or a K8s Pod is.

- **Multigateway**: Discovers poolers via etcd topology watches (`WatchRecursive("poolers")`). It connects to poolers using the `Hostname:GRPCPort` from their etcd registration (`MultiPoolerInfo.Addr()`), completely independent of Kubernetes service discovery or DNS.

---

## 2. Resource Topology: Before & After

### Current (StatefulSets)

```ascii
[MultigresCluster] 🚀 (Root CR - user-editable)
      │
      ├── 📍 Defines [TemplateDefaults] (Cluster-wide default templates)
      │
      ├── 🌍 [GlobalTopoServer] (Child CR) ← 📄 Uses [CoreTemplate] OR inline [spec]
      │
      ├── 🤖 MultiAdmin Resources ← 📄 Uses [CoreTemplate] OR inline [spec]
      │
      ├── 💠 [Cell] (Child CR) ← 📄 Uses [CellTemplate] OR inline [spec]
      │    │
      │    ├── 🚪 MultiGateway Resources (Deployment + Service)
      │    └── 📡 [LocalTopoServer] (Child CR, optional)
      │
      └── 🗃️ [TableGroup] (Child CR)
           │
           └── 📦 [Shard] (Child CR) ← 📄 Uses [ShardTemplate] OR inline [spec]
                │
                ├── 🧠 MultiOrch Resources (Deployment)
                └── 🏊 Pools (per cell):
                     ├── StatefulSet  ← owns pods, owns data PVCs
                     ├── Headless Service  ← required by StatefulSet spec
                     └── Backup PVC (shared across pods in cell)
```

### After (Direct Pod Management)

```ascii
[MultigresCluster] 🚀 (Root CR - user-editable)
      │
      ├── 📍 Defines [TemplateDefaults] (Cluster-wide default templates)
      │
      ├── 🌍 [GlobalTopoServer] (Child CR) ← 📄 Uses [CoreTemplate] OR inline [spec]
      │
      ├── 🤖 MultiAdmin Resources ← 📄 Uses [CoreTemplate] OR inline [spec]
      │
      ├── 💠 [Cell] (Child CR) ← 📄 Uses [CellTemplate] OR inline [spec]
      │    │
      │    ├── 🚪 MultiGateway Resources (Deployment + Service)
      │    └── 📡 [LocalTopoServer] (Child CR, optional)
      │
      └── 🗃️ [TableGroup] (Child CR)
           │
           └── 📦 [Shard] (Child CR) ← 📄 Uses [ShardTemplate] OR inline [spec]
                │
                ├── 🧠 MultiOrch Resources (Deployment)
                └── 🏊 Pools (per cell):
                     ├── Pod-0  ← operator-managed, owns data PVC-0
                     ├── Pod-1  ← operator-managed, owns data PVC-1
                     ├── PVC-0  ← operator-managed (data)
                     ├── PVC-1  ← operator-managed (data)
                     └── Backup PVC (shared across pods in cell)
```

### Key Differences

| Aspect | Before (StatefulSet) | After (Direct Pods) |
|---|---|---|
| **Pod creation** | StatefulSet controller creates pods | Operator reconciler creates pods directly |
| **Pod recreation** | StatefulSet auto-recreates on deletion | Operator reconciler detects missing pods and recreates |
| **Data PVC creation** | StatefulSet `volumeClaimTemplates` | Operator creates PVCs explicitly |
| **PVC retention** | `PersistentVolumeClaimRetentionPolicy` | Operator decides based on `PVCDeletionPolicy` |
| **Headless Service** | Required by `spec.serviceName` | **Still needed for hostname resolution** — see note below |
| **Scaling** | Change `replicas` on StatefulSet | Operator creates/deletes individual pods |
| **Target-specific deletion** | Not possible (must scale from tail) | Delete any specific pod |

> [!WARNING]
> **Headless service is still needed for hostname resolution.** While multigateway discovers poolers via etcd (not K8s DNS), it connects using the `Hostname:GRPCPort` from the topology. The `Hostname` value comes from multipooler's `FullyQualifiedHostname()`, which resolves via DNS: `os.Hostname()` → `net.LookupHost()` → `net.LookupAddr()` → FQDN. With a headless service, this produces a resolvable FQDN (e.g., `pod-0.headless-svc.ns.svc.cluster.local`). Without it, the hostname may not be DNS-resolvable from other pods.
>
> **Options**: (a) Keep a headless service for pod-to-pod DNS resolution, or (b) have the operator set `--hostname=$(POD_IP)` so the multipooler registers its IP address instead of a DNS name. Option (b) is simpler (removes the headless service) but means etcd entries contain IPs that become stale if pods get new IPs.

---

## 3. What We Gain & Lose (StatefulSet → Pods)

### What We Gain

| Benefit | Detail |
|---|---|
| **Targeted decommissioning** | Can delete specific pods without affecting others. StatefulSets can only scale from ordinal N-1 downward. |
| **Independent zone/cell management** | Each cell's pods can be managed independently. No need for one StatefulSet per cell (which is what we already do). |
| **Simpler rename/replace** | Pods can be replaced with new names without fighting StatefulSet ordinal conventions. |
| **Direct PVC lifecycle control** | Operator explicitly creates and deletes PVCs instead of relying on `PersistentVolumeClaimRetentionPolicy` semantics. |
| **No GitOps drift** | StatefulSets report `replicas: N` in status which causes constant drift. Direct pods + operator-managed count avoids this. |
| **Better observability** | Operator has full visibility into which specific pods exist and their states, rather than relying on StatefulSet status. |
| **Headless service only for DNS** | Without StatefulSets, the headless service is only needed for pod hostname resolution (see §2 note). Can be eliminated if operator sets `--hostname=$(POD_IP)`. |

### What We Lose

| Loss | Mitigation |
|---|---|
| **Automatic pod recreation** | Operator's reconcile loop must detect missing pods and recreate them. This is straightforward — reconciler compares desired vs actual pod count. |
| **Rolling update coordination** | Operator must implement its own rolling update logic. However, since multigres already handles replication and failover, the operator just needs to delete/recreate pods one at a time. |
| **Stable network identity** | Not needed for identity — multigres uses etcd, not K8s DNS. However, the hostname registered in etcd must be resolvable (see §2 note on headless service). |
| **Ordered startup** | Not used — we already use `ParallelPodManagement`. |
| **PVC template auto-creation** | Operator must explicitly create PVCs for each pod. |

### Impact Assessment

**Low risk.** The critical functionality (replication, failover, backup/restore) is entirely within multigres and etcd — not in Kubernetes StatefulSet features. The operator's role is limited to:
1. Creating/deleting pods and PVCs
2. Passing the correct configuration to each pod
3. Reconciling desired state vs actual state

---

## 4. Scope Limitations (v1alpha1)

These constraints are already enforced or need to be enforced via CEL validation rules and webhook logic. We have enforced these to prevent users from doing things not supported by multigres or the operator at the current stage. For example, we don't support resharding or even multiple shards yet, so we prevent users from doing it.

### Already Enforced

| Constraint | Enforcement | CEL Rule |
|---|---|---|
| **Single database: `postgres`** | CEL on `databases` array | `self.all(db, db.name == 'postgres' && db.default == true)` |
| **Single shard: `0-inf`** | CEL on `shardName` | `self == '0-inf'` |
| **Pools are append-only** | CEL on pools map | `oldSelf.all(k, k in self)` — prevents removal or rename |

### Not Yet Enforced (Needs Review)

| Constraint | Status | Risk |
|---|---|---|
| **Cell rename prevention** | ❌ **No CEL rule exists** | Renaming a cell would create pods with new names while orphaning old ones. The old pods' etcd registrations would become stale. **Action needed: add CEL rule.** |
| **Pool replica count floor** | ❌ **Not validated** | User could set `replicasPerCell: 0`, which would delete all pods but leave etcd registrations behind. Consider minimum of 1. |

### Explicitly NOT Supported (v1alpha1)

These operations are **not supported by the operator** and should be documented as such:

| Operation | Why Not Supported |
|---|---|
| **Resharding** | Not currently supported by multigres upstream either. The focus is exclusively on single shard for now. |
| **Adding new shards** | Not supported upstream. Multigres MVP enforces single shard `0-inf` via `ValidateMVPTableGroupAndShard()` in `constants/multischema.go`. |
| **Multiple databases** | Not supported upstream. Multigres MVP enforces single `default` tablegroup with shard `0-inf`. |
| **Renaming databases** | Would require re-registering the database in etcd under a new name and migrating all topology references. |
| **Renaming shards** | Same issue — all topology references are keyed by shard name. |
| **Renaming pools** | Already blocked by CEL append-only rule. Renaming would orphan pods and their PVCs. |
| **Renaming cells** | Not yet blocked but must be (see above). Same orphaning problem. |
| **Removing pools** | Already blocked by CEL. Would require draining connections, removing pods, cleaning etcd topology entries, and cleaning PVCs. |
| **Removing cells from a pool** | Not yet blocked. Would orphan pods and etcd registrations. |

### What Multigres Upstream Supports vs What the Operator Supports

> [!IMPORTANT]
> Multiple shards, multiple databases, and resharding are **not currently supported by multigres upstream** either. The upstream codebase enforces single shard `0-inf` and single tablegroup `default` via `ValidateMVPTableGroupAndShard()` in `go/common/constants/multischema.go`. The operator's CEL rules mirror this upstream MVP constraint. S3 backup support was added upstream recently (commit `62e1d94`).

| Feature | Multigres Upstream | Operator (v1alpha1) |
|---|---|---|
| Multiple databases | ❌ (MVP: single `default` tablegroup) | ❌ (locked to `postgres`) |
| Multiple shards | ❌ (MVP: locked to `0-inf`) | ❌ (locked to `0-inf`) |
| Multiple cells/zones | ✅ | ✅ |
| Multiple pools | ✅ | ✅ (append-only) |
| Resharding | ❌ (not in current roadmap) | ❌ |
| Backup to S3 | ✅ (recently added) | ❌ (filesystem only, see [§9](#9-pgbackrest--backup-infrastructure)) |
| Backup to filesystem | ✅ | ✅ (via shared PVC) |
| Failover (auto) | ✅ (via multiorch) | ✅ (handled by multiorch) |
| pgBackRest TLS certs | ✅ (via cert-manager in demo) | ⚠️ Not fully wired |

---

## 5. Pod Identity: Primary vs Replica

### How Multigres Determines Primary vs Replica

**The operator does NOT need to distinguish primary from replica in pod naming.** Multigres determines roles internally:

1. **During bootstrap**: `multiorch`'s `BootstrapShardAction` acquires a distributed lock, selects a candidate from reachable poolers, and calls `InitializeEmptyPrimary` on it (with `ConsensusTerm: 1`). The other pods become standbys.
2. **After bootstrap**: Each multipooler reports its role to etcd (PRIMARY, REPLICA, DRAINED). Multiorch reads this from etcd. The Kubernetes pod name is irrelevant to the role assignment.
3. **During failover**: `multiorch`'s `AppointLeaderAction` promotes a standby to primary. Pod names don't change — only the `PoolerType` in etcd changes.

### Does the Operator Need to Know the Primary?

**Yes, in specific situations.** While the operator doesn't need to know for normal operations (multigres handles replication and failover internally), the operator DOES need primary awareness for:

1. **Scale-down**: To avoid accidentally deleting the primary (which would trigger an unnecessary failover)
2. **Rolling updates**: To update replicas first and the primary last (minimizing downtime)
3. **Status reporting**: To report primary/replica counts and roles in the MultigresCluster status

**How the operator can determine primary/replica**: The operator's data-handler already has an etcd client connection. It can read the pooler topology to check each pooler's `PoolerType` (PRIMARY, REPLICA, DRAINED). The `--service-id` registered in etcd matches the pod name (`$(POD_NAME)`), so the operator can correlate etcd entries with K8s pods.

### Pod Naming Scheme

The operator currently uses a **hierarchical naming scheme** with safety hashes defined in `pkg/util/name`. The naming pattern for pool resources is:

```
{cluster}-{db}-{tg}-{shard}-pool-{pool}-{cell}-{hash}
```

With StatefulSets, pods get an ordinal suffix appended by Kubernetes: `{sts-name}-{ordinal}`. StatefulSet names are constrained to 52 characters (reserving 11 chars for pod ordinal suffix and controller hash). When the name exceeds 52 chars, it's truncated with a `---` marker followed by the FNV-1a hash.

**Examples from the operator's naming package:**
- **StatefulSet**: `inline-postgres-default-0-inf---d8f1a` (truncated)
- **Pod (from STS)**: `inline-postgres-default-0-inf---d8f1a-0` (ordinal appended)
- **Short cluster**: `mycluster-postgres-default-0inf-pool-main-z1-a7c3d9e2`

**After removing StatefulSets**, pods can use the full 63-character DNS label limit since there's no STS ordinal suffix. The operator would create pods directly with names like:

```
{cluster}-{db}-{tg}-{shard}-pool-{pool}-{cell}-{hash}-{index}
```

Where `{index}` is a numeric index (0, 1, 2...) that the operator tracks. The hash ensures uniqueness across different pool/cell combinations even after truncation (see [implementation-notes.md § Resource Naming Strategy](./implementation-notes.md#resource-naming-strategy) for the full naming design).

### Do NOT Name Pods by Role

Naming pods like `primary-0` or `replica-1` would be misleading because:
- After a failover, `replica-1` would actually be the primary
- The operator would need to rename pods after failover, which means deleting and recreating the pod (and potentially losing data if PVC is tied to the old name)
- Multigres does not use pod names to determine roles

---

## 6. Pod Lifecycle & Decommissioning

### What Happens on Multipooler Shutdown (SIGTERM)

When a pod receives SIGTERM (e.g., from `kubectl delete pod` or operator-initiated deletion):

1. Multipooler's `Shutdown()` is called:
   - Calls `toporeg.Unregister()` → which executes the `unregisterFunc`
   - `unregisterFunc` sets `ServingStatus = NOT_SERVING` in etcd
   - Closes the topology store connection
2. The multipooler process exits
3. Kubernetes terminates the pod

**Critical: the multipooler does NOT delete its registration from etcd.** The entry persists with `ServingStatus = NOT_SERVING`. The rationale in the code is: *"For poolers, we don't un-register them on shutdown (they are persistent component). If they are actually deleted, they need to be cleaned up outside the lifecycle of starting/stopping."*

### Stale Etcd Entry Cleanup

There is **no active cleanup mechanism** for etcd topology entries of deleted pods:

| Mechanism | What It Cleans | Scope | Timing |
|---|---|---|---|
| `forgetLongUnseenInstances()` | Multiorch's **internal** `poolerStore` (in-memory) | Per-multiorch instance | 4 hours after last health check |
| Etcd topology entries | **Nothing cleans them automatically** | Global | Never |

This means:
- When the operator deletes a pod permanently (scale-down), the etcd topology still has an entry for that pod's service ID
- Multiorch will keep trying to health-check that entry (it'll fail with connection refused)
- After 4 hours, multiorch removes it from its internal store and stops health-checking
- But the etcd entry remains forever unless explicitly deleted

### Current Behavior with StatefulSets

> [!NOTE]
> The stale etcd entry problem **already exists today with StatefulSets**. When a user reduces `replicasPerCell` on a pool, the StatefulSet controller deletes the tail pod, but the operator does nothing to clean up that pod's etcd registration. The same 4-hour stale window applies. Moving to direct pod management does not make this worse — it just gives us the opportunity to do better.

### Implications for the Operator

> [!IMPORTANT]
> The operator SHOULD clean up etcd topology entries when permanently removing a pod. This is an improvement over the current StatefulSet behavior.

Options:
1. **Operator calls `ts.DeleteMultiPooler()` on scale-down** — but this function doesn't exist yet in upstream multigres
2. **Operator calls `ts.UpdateMultiPoolerFields()` to set type to DRAINED** — this exists, but the stale entry still persists
3. **Wait for upstream multigres to add a deregister RPC** — the multipooler could accept a "decommission" command via gRPC before being killed
4. **Accept the 4-hour stale window** — multiorch handles it eventually, but during those 4 hours it'll log errors trying to reach the dead pooler

**Recommendation**: For Phase 1, start with option 4 (same behavior as today with StatefulSets). Track option 1 or 3 as an improvement item. The stale entries don't cause data corruption — multiorch just logs warnings trying to health-check unreachable poolers.

### Graceful Scale-Down Sequence

Scale-down is a multi-step process that spans several reconcile loops. Because the operator must coordinate with the Postgres replication layer (removing a standby from `synchronous_standby_names` before deleting its pod), doing everything in a single reconcile pass is fragile — if the operator crashes mid-sequence, the cluster could be left in an inconsistent state.

#### Drain State Machine

The recommended approach uses **pod annotations** to track the decommissioning state across reconcile loops. This ensures that each step is durable and resumable:

```
                    ┌──────────────────────────────────────┐
                    │          Scale-down detected         │
                    │ (desired replicas < actual replicas) │
                    └──────────────┬───────────────────────┘
                                   │
                                   ▼
                    ┌──────────────────────────────────────┐
                    │          Select pod to drain         │
                    │ (see Pod Selection Algorithm below)  │
                    └──────────────┬───────────────────────┘
                                   │
                    ┌──────────────▼───────────────────────┐
                    │  State: DRAINING                     │
                    │  Annotation: drain.multigres.com/    │
                    │    state=draining                    │
                    │                                      │
                    │  Action: Call UpdateSynchronous-     │
                    │    StandbyList(REMOVE) on primary    │
                    │  Then: Requeue                       │
                    └──────────────┬───────────────────────┘
                                   │
                    ┌──────────────▼───────────────────────┐
                    │  State: ACKNOWLEDGED                 │
                    │  Annotation: drain.multigres.com/    │
                    │    state=acknowledged                │
                    │                                      │
                    │  Guard: Verify the standby is        │
                    │    actually removed from sync list   │
                    │  Then: Requeue                       │
                    └──────────────┬───────────────────────┘
                                   │
                    ┌──────────────▼───────────────────────┐
                    │  State: FINISHED                     │
                    │  Annotation: drain.multigres.com/    │
                    │    state=finished                    │
                    │                                      │
                    │  Action: Delete pod, optionally      │
                    │    delete PVC (PVCDeletionPolicy)    │
                    └──────────────────────────────────────┘
```

**Key invariant**: At most one pod per pool should be in `FINISHED` state at a time. This guarantees that scale-down cannot accidentally delete multiple pods in one reconcile if the operator processes events faster than expected.

**Why annotation-based?** Each state transition is persisted to the Kubernetes API before proceeding. If the operator crashes at any point, the next reconcile reads the annotation and resumes from the correct state — no in-memory state is lost.

> [!NOTE]
> `UpdateSynchronousStandbyList` with `STANDBY_UPDATE_OPERATION_REMOVE` exists in the multipooler gRPC API. However, **multiorch never calls it** — its `FixReplicationAction` only handles ADD operations. This means the operator is the only component that performs standby removal today.

#### Scale-Down of a Primary Pod

When the operator wants to scale down and the target IS the primary:
1. Trigger a failover first (tell multiorch to appoint a new leader)
2. Wait for failover to complete
3. Then follow the drain state machine above for the now-demoted replica

---

## 7. Failure Scenarios

### 7.1 Replica Pod Deleted (PVC Retained)

**Trigger**: `kubectl delete pod <replica-pod>` or node failure with PVC on networked storage

**What happens**:
1. Pod is deleted → multipooler sets `NOT_SERVING` in etcd (if graceful shutdown)
2. Operator's reconcile loop detects missing pod
3. Operator recreates the pod with the same PVC
4. Pod starts → multipooler discovers existing PGDATA on the PVC
5. Multipooler's `MonitorPostgres` finds `dirInitialized = true` → starts PostgreSQL
6. Multiorch detects the pooler is back, configures streaming replication if needed
7. **Result: automatic recovery, no data loss, fast (seconds)**

### 7.2 Replica Pod + PVC Deleted

**Trigger**: `kubectl delete pod <replica-pod> && kubectl delete pvc <replica-pvc>` or node permanent failure with local storage

**What happens**:
1. Pod and PVC are gone
2. Operator's reconcile loop detects missing pod
3. Operator creates a new PVC and a new pod
4. Pod starts with empty PGDATA
5. Multipooler's `MonitorPostgres` finds `dirInitialized = false`, checks for backups
6. If backups exist → `pgbackrest restore --type=standby` → restores from latest backup
7. PostgreSQL starts, streaming replication catches up from primary
8. **Result: automatic recovery, no data loss, slower (depends on backup size)**

### 7.3 Primary Pod Deleted (PVC Retained)

**Trigger**: `kubectl delete pod <primary-pod>`

**What happens**:
1. Pod goes down → multipooler sets `NOT_SERVING` (if graceful)
2. Multiorch's `PrimaryIsDeadAnalyzer` detects the primary is unreachable
3. **Smart check**: If replicas are still connected to the primary's Postgres (verified via `PrimaryConnInfo` + `LastReceiveLsn` from health checks), multiorch assumes Postgres is still running and waits — it logs *"operator should restart pooler process"*
4. If Postgres is truly down: after the **failover grace period** (configurable, default ~4-12 seconds with jitter), multiorch triggers `AppointLeaderAction`
5. `AppointLeaderAction` selects the most up-to-date replica and promotes it to primary via consensus protocol
6. Meanwhile, operator's reconcile loop recreates the old primary pod with the same PVC
7. The old primary pod restarts — but by now a new primary exists
8. Multiorch detects `ProblemReplicaNotReplicating` on the old primary → configures it as a standby
9. If timeline divergence prevents direct replication: multiorch attempts `pg_rewind`. If that fails, marks the pooler as `DRAINED`.
10. **Result: automatic failover + recovery, potential brief write interruption**

### 7.4 Primary Pod + PVC Deleted

**Trigger**: Catastrophic loss of primary

**What happens**:
1. Same as 7.3 for failover (steps 2-5)
2. Operator creates a new PVC and new pod
3. New pod starts with empty PGDATA
4. Restore from pgBackRest backup → become standby of the new primary
5. **Result: automatic failover + restore from backup, data loss limited to unreplicated transactions**

### 7.5 All Pods Deleted (All PVCs Retained)

**What happens**:
1. Operator recreates all pods
2. All pods start with existing PGDATA
3. Multiorch detects `StalePrimary` or similar → bootstraps the shard again
4. Previous primary is typically re-elected if it has the most recent data

### 7.6 All Pods + All PVCs Deleted

**What happens**:
1. Operator recreates all pods with new PVCs
2. All pods start with empty PGDATA
3. If the **shared backup PVC** still exists → multiorch runs `BootstrapShardAction` → picks one as primary → `InitializeEmptyPrimary` → backup → others restore
4. If the backup PVC is also deleted → same bootstrap but **from an empty database** — all data is lost
5. **Result: full cluster re-bootstrap, data loss depends on backup PVC survival**

> [!CAUTION]
> The shared backup PVC is critical for disaster recovery. If using filesystem-based backups (current implementation), losing the backup PVC means total data loss. S3-based backups (see §10) eliminate this single point of failure.

---

## 8. Scaling Operations

### Scale Up (Add Replicas)

1. Operator creates a new PVC for the new replica
2. Operator creates a new pod referencing the new PVC and the shared backup PVC
3. Pod starts → multipooler registers in etcd → `MonitorPostgres` detects no PGDATA
4. Checks backup availability → restores from latest backup → starts as standby
5. Multiorch discovers new pooler → configures streaming replication → adds to `synchronous_standby_names`
6. **No operator-driven orchestration needed** — multigres handles everything automatically

### Scale Down (Remove Replicas)

Scale-down uses the drain state machine described in [§6](#graceful-scale-down-sequence):

1. Operator selects which pod to remove using the pod selection algorithm below
2. Operator annotates the pod to begin the drain sequence
3. Drain progresses over multiple reconcile loops: remove from sync standby list → verify → delete
4. Operator deletes the PVC (if `PVCDeletionPolicy` says so)
5. After 4 hours, multiorch's `forgetLongUnseenInstances` cleans its internal store
6. Etcd topology entry persists (see [§6](#6-pod-lifecycle--decommissioning))

### Pod Selection Algorithm

When scaling down, the operator must choose which pod to delete. The selection algorithm is:

1. **Never select the primary.** Read etcd topology to identify the current primary's `service-id` (which matches the pod name).
2. **Prefer pods that are not ready.** If any non-primary pods are in a non-ready state, prefer those (they're already not serving traffic).
3. **Among ready, non-primary pods: select the highest-index pod.** This makes the selection deterministic and predictable. The index refers to the numeric suffix in the pod name (e.g., pod `...-2` is preferred over `...-1`).
4. **If no deletable pod is found** (e.g., all pods are the primary or not ready), **do not scale down.** Requeue and try again later.

> [!WARNING]
> The operator must avoid deleting the primary pod during scale-down. Since multigres determines primary/replica roles internally, the operator must query the etcd topology to identify the current primary before choosing which pod to delete. If the operator accidentally deletes the primary, multiorch will handle failover automatically (see §7.3), but this causes unnecessary downtime.

---

## 9. Rolling Updates

When the operator detects that a pod's spec has drifted from the desired state (e.g., image change, environment variable change, resource limit change), it must perform a rolling update. Since pod specs are **immutable** after creation, the only way to update a pod is to delete it and recreate it with the new spec.

### Drift Detection

The operator detects drift by comparing the **running pod's spec** against the **desired pod spec** generated from the Shard CR. The comparison should be limited to operator-managed fields:

| Field | Triggers Rolling Update? |
|---|---|
| Container images | Yes |
| Container command/args | Yes |
| Environment variables (operator-set) | Yes |
| Resource requests/limits | Yes |
| Volume mounts | Yes |
| Security context | Yes |
| Affinity/tolerations/node selector | Yes |
| Labels/annotations | No (updated in-place via patch) |
| Fields set by admission controllers (sidecars, injected env vars) | No |

> [!NOTE]
> Only fields that the operator explicitly sets should be compared. Admission controllers (e.g., Istio, Linkerd, Vault Agent) may inject sidecars, environment variables, or volume mounts that the operator didn't set. Comparing these would trigger unnecessary rollouts every time the operator reconciles. If this becomes a problem in practice, a hash-based approach (hashing only operator-controlled fields into a pod annotation at creation time) can be adopted to make detection reliable.

### Update Order: Replicas First, Primary Last

To minimize downtime during rolling updates:

1. **Collect all pods** in the pool that need updating (drift detected)
2. **Exclude the primary** from the initial pass
3. **Update replicas** one at a time, starting with the most lagged replica (least valuable data):
   - Delete the replica pod
   - Requeue (return from reconcile)
   - On next reconcile: the operator detects the missing pod and recreates it with the new spec
   - Wait for the new pod to become ready before proceeding to the next replica
4. **Update the primary** last:
   - Trigger a controlled switchover: tell multiorch to promote a healthy, up-to-date replica to primary
   - Wait for the switchover to complete (old primary becomes a replica)
   - Delete the old primary pod (now a replica) and recreate it with the new spec

### Delete-Then-Recreate Pattern

The operator does **not** attempt in-place pod updates. The flow is:

1. Delete the pod (Kubernetes terminates it; multipooler runs its graceful shutdown → sets `NOT_SERVING` in etcd)
2. The reconciler detects the pod is missing on the next loop
3. The reconciler creates a new pod with the updated spec, referencing the existing PVC
4. The new pod starts → multipooler discovers existing PGDATA → PostgreSQL starts → streaming replication resumes

This is safe because PVCs persist across pod deletions. The data is never lost during a rolling update.

### One Pod at a Time

The operator should update **at most one pod per reconcile loop** per pool. After deleting a pod, the reconciler returns early (requeue). On the next reconcile, it verifies the replacement pod is ready before moving to the next pod. This natural rate-limiting prevents cascading failures from updating too many pods simultaneously.

---

## 10. pgBackRest & Backup Infrastructure

### Current State (Filesystem-Based)

The operator currently uses a **shared PVC** (`backup-data-<name>`) mounted at `/backups` on all pods in a cell. pgBackRest writes backups to this PVC and reads from it during replica initialization.

The backup location is written to etcd by the data-handler controller as `BackupLocation{filesystem: "/backups"}` (hardcoded in `shard_controller.go:getBackupLocation()`).

This works for single-node clusters (kind) because all pods share the same node and use `ReadWriteOnce`. For multi-node production clusters, this requires either:
- A storage class that supports `ReadWriteMany` (NFS, EFS, etc.)
- Switching to S3-based backups

### Backup Scheduling

> [!NOTE]
> Multigres does **NOT** have automatic periodic backup scheduling (no cron or timer). Backups are taken **on-demand** as part of specific operations:
> 1. **During shard bootstrap**: The primary takes an initial full backup after `InitializeEmptyPrimary`
> 2. **During replica initialization**: pgBackRest restores from the latest available backup
> 3. The multipooler exposes a `Backup` gRPC RPC that multiorch can call as part of recovery actions
>
> If periodic scheduled backups are needed, this would need to be implemented either in the operator (as a CronJob or a timer in the shard controller) or upstream in multigres.

### Future: S3-Based Backups

Multigres upstream recently added S3 support (commit `62e1d94`). The `BackupLocation` protobuf supports `S3Backup{bucket, region, endpoint, keyPrefix, useEnvCredentials}`. The multipooler's `backup.Config` generates the correct pgBackRest config for either filesystem or S3 automatically.

To enable S3 in the operator:

1. **Add backup config to the CRD** — new fields under `PoolSpec` or at the cluster level for S3 bucket, region, optional endpoint, and credentials secret reference
2. **Change `registerDatabaseInTopology`** — write `BackupLocation_S3{...}` to etcd instead of `BackupLocation_Filesystem`
3. **Inject AWS credentials as env vars** — either from a K8s Secret or via IRSA (IAM Roles for Service Accounts) with `key-type=auto`
4. **Remove the shared backup PVC** — not needed with S3
5. **No changes to upstream multigres** — the existing `initPgBackRest`, `backupLocked`, and `restoreFromBackupLocked` functions handle S3 transparently

### Why S3 Matters for Pod Management

With filesystem backups, the shared backup PVC is a single point of failure and imposes ReadWriteMany requirements. With S3:
- Pods can be on any node in any zone
- Backup data survives complete cluster destruction
- Replica initialization works across zones/regions
- No shared storage dependency

**Recommendation**: S3 support should be a high-priority follow-up.

---

## 11. Rename Prevention

### Currently Blocked

| Entity | How Prevented | Where |
|---|---|---|
| **Pools** | CEL: `oldSelf.all(k, k in self)` | `multigrescluster_types.go`, `shardtemplate_types.go` |
| **Shard name** | CEL: `self == '0-inf'` | `multigrescluster_types.go` |
| **Database name** | CEL: `self.all(db, db.name == 'postgres' && db.default == true)` | `multigrescluster_types.go` |

### NOT Blocked (Action Required)

| Entity | Risk | Recommendation |
|---|---|---|
| **Cell names** | Renaming a cell would orphan pods and their etcd registrations. Old pods would continue running with old names while new pods start with new names. | **Add CEL rule**: cells should be treated as append-only, same as pools. |
| **Removing cells from a pool** | Same orphaning problem. Old pods in the removed cell continue running. | **Add CEL rule**: cells array should be append-only within each pool. |

---

## 12. Open Questions

> [!IMPORTANT]
> These questions need to be resolved before implementation.

### For the Multigres Team

1. **Etcd cleanup on scale-down**: Should the operator implement topology cleanup when permanently removing a pod? Currently there is no `DeleteMultiPooler` API. Would the multigres team be open to adding one, or should we just accept the 4-hour stale window? (Note: this is the same behavior as today with StatefulSets — scale-down does not clean up etcd.)

2. **Graceful decommission RPC**: Would it be valuable for the operator to call a "decommission" RPC on the multipooler before killing its pod? This could:
   - Drain active connections gracefully
   - Remove itself from `synchronous_standby_names`
   - Set its type to `DRAINED` in etcd
   - Close cleanly

3. **Primary awareness**: The operator needs to know which pod is the primary to avoid deleting it during scale-down and for status reporting. Preferred approach is reading etcd topology (data-handler already has etcd client). Are there other options the multigres team recommends?

4. **S3 backup support timeline**: When should S3 be implemented? Is it a prerequisite for multi-zone deployments or can we start with filesystem and add S3 later?

5. **Periodic backup scheduling**: Multigres currently only takes backups during bootstrap and specific recovery actions. Should periodic scheduled backups be added? If so, where — in the operator (CronJob/timer) or upstream?

6. **pgBackRest TLS certificates**: The demo uses cert-manager for pgBackRest TLS. The operator's current pod spec doesn't mount TLS certificates for the pgbackrest-server sidecar. Is TLS required for filesystem-based backups (same-node shared PVC) or only for cross-node operations?

### Implementation Questions

7. **Pod naming convention**: Pods should continue using the `pkg/util/name` hierarchical naming with hash suffixes, plus a numeric index for the replica ordinal. This gives us the 63-char DNS label limit (vs 52-char for StatefulSets) and maintains the collision-prevention hash.

8. **Headless service**: Multigateway uses etcd, not K8s DNS, but the multipooler's `FullyQualifiedHostname()` relies on DNS to resolve to a pod FQDN. Either keep a headless service for pod hostname DNS, or have the operator set `--hostname=$(POD_IP)` to bypass DNS entirely. Which approach is preferred?

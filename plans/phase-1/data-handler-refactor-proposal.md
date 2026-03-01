# Data-handler Refactor Proposal (v2)

**Date:** 2026-03-01
**Status:** Proposal
**Scope:** Internal controller restructuring (no API/CRD changes)

---

## TL;DR

We have **two controllers fighting over every Shard and Cell** in the system.
They each add their own finalizer, race during deletion, and one finishes
cleanup before the other even starts — leaving pods un-drained, topo entries
orphaned, and a comment in the code that literally says *"the drain state
machine cannot run reliably here."*

On top of that, **child CRs are registering their own topology entries** — and
in some cases, registering entries that belong to a completely different level
of the hierarchy. The Shard controller registers the database entry in topo,
but databases are a cluster-level concept defined in
`MultigresCluster.Spec.Databases[]` with no CR of their own. The Cell
controller registers its own cell entry in topo.

This is an anti-pattern. **The parent controller that creates a CR should be
the one registering its topology entry**, not the child itself. This is because:

- The parent **outlives** the child — it survives child deletion/recreation
- The parent can unregister from topo **before** deleting the child CR
- No cleanup is needed during deletion, eliminating the need for finalizers
- The parent has visibility into the full desired state for pruning

When the child registers itself, it must clean up during deletion — exactly
when controllers are racing, resources are disappearing, and the topo server
may already be dying.

**The fix:**

1. **One controller per CRD** — delete the data-handler controllers (which
   have no CRDs of their own) and convert the data-handler module into a
   set of **utility packages** that any controller can call.

2. **Parent registers, parent prunes** — MultigresCluster registers databases
   and cells in topo (it owns both concepts). TableGroup prunes Shard CRs
   (already does this). Shard controller prunes stale pooler entries.

3. **"Make it safe, then delete"** — parent marks child as `PendingDeletion`,
   child drains and unregisters, sets `Idle=True`, parent then calls
   `Delete()`. By the time deletion happens, there is nothing left to clean
   up.

4. **Zero finalizers** — if all cleanup happens *before* deletion, finalizers
   have nothing to do. No finalizer chains, no races, no "stuck in
   Terminating" states. `kubectl delete cluster` and Kubernetes GC cascades
   everything instantly.

**What changes:** Internal controller wiring, data-handler becomes utility
packages. **What doesn't change:** CRDs, API, user-facing behaviour.
**What it unblocks:** Individual shard deletion, cell removal, shard migration,
and any future multi-shard/multi-DB work.

---

## Table of Contents

1. [Summary](#1-summary)
2. [How Things Work Today](#2-how-things-work-today)
3. [Why This Is a Problem](#3-why-this-is-a-problem)
4. [Topology Registration: What's Wrong and How to Fix It](#4-topology-registration-whats-wrong-and-how-to-fix-it)
5. [Upstream TopoClient API Coverage](#5-upstream-topoclient-api-coverage)
6. [Proposed Solution](#6-proposed-solution)
7. [Implementation Plan](#7-implementation-plan)
8. [Migration Strategy](#8-migration-strategy)
9. [Risk Assessment](#9-risk-assessment)
10. [To Prune or Not to Prune](#10-to-prune-or-not-to-prune)
11. [Questions for the Multigres Team](#11-questions-for-the-multigres-team)

---

## 1. Summary

The operator currently uses two separate controllers (reconcilers) for the same
Custom Resource on both Cell and Shard. This violates the Kubernetes operator
convention of one reconciliation loop per CRD, creates complex finalizer
coordination problems, and places topology registration at the wrong level of
the controller hierarchy.

This document proposes:

1. Converting the data-handler controllers into **utility packages** (not
   controllers) for topology operations
2. Moving all topo registration to the **parent controller** that owns each
   entity, not the child CR that represents it
3. Adopting a "make it safe before deleting it" pattern for resource teardown
4. Eliminating all finalizers from the system

The external API (CRDs) does not change. No user-facing behaviour changes for
the current single-shard model.

---

## 2. How Things Work Today

### 2.1 The Two-Controller Pattern

For both Cell and Shard, we run **two independent reconcilers** watching the
same Custom Resource, in the same binary:

```
                    ┌──────────────────────────────┐
                    │           Shard CR           │
                    │                              │
                    │  Watched by TWO controllers: │
                    │                              │
          ┌─────────┴────────┐          ┌──────────┴───────────────┐
          │ resource-handler │          │       data-handler       │
          │                  │          │                          │
          │ Creates:         │          │ Does:                    │
          │  - Pods          │          │  - Topology registration │
          │  - PVCs          │          │  - Drain state machine   │
          │  - Services      │          │  - PodRoles tracking     │
          │  - Deployments   │          │  - Backup health checks  │
          │                  │          │                          │
          │ Finalizer:       │          │ Finalizer:               │
          │  shard-resource- │          │  shard-data-             │
          │  protection      │          │  protection              │
          └──────────────────┘          └──────────────────────────┘
```

The same pattern applies to Cell:

```
                    ┌──────────────────────────────┐
                    │           Cell CR            │
                    │                              │
          ┌─────────┴────────┐          ┌──────────┴───────────────┐
          │ resource-handler │          │       data-handler       │
          │                  │          │                          │
          │ Creates:         │          │ Does:                    │
          │  - MultiGateway  │          │  - Topology registration │
          │    Deployment    │          │  - Topology cleanup      │
          │  - MultiGateway  │          │                          │
          │    Service       │          │                          │
          └──────────────────┘          └──────────────────────────┘
```

The data-handler **does not own any child resource**. It has no CR of its own.
It is a second controller bolted onto someone else's resource.

### 2.2 The Controller Hierarchy

The actual controller hierarchy in the operator:

```
MultigresCluster controller (cluster-handler)
  ├── Creates Cell CRs
  ├── Creates TableGroup CRs
  ├── Owns databases (MultigresCluster.Spec.Databases[] — no Database CR)
  ├── Finalizer: multigres.com/cluster-cleanup
  │
  ├── TableGroup controller (cluster-handler)
  │     ├── Creates Shard CRs
  │     ├── Prunes orphan Shard CRs
  │     └── Zero topo awareness
  │
  ├── Cell controller (resource-handler + data-handler)
  │     ├── resource-handler: creates MultiGateway Deployment/Service
  │     └── data-handler: registers/unregisters cell in topo
  │
  └── Shard controller (resource-handler + data-handler)
        ├── resource-handler: creates pods, PVCs, services, deployments
        │   Finalizer: multigres.com/shard-resource-protection
        └── data-handler: registers DATABASE in topo ⚠️ wrong level
            Finalizer: multigres.com/shard-data-protection

TopoServer controller (resource-handler)
  ├── Creates etcd StatefulSet/Services
  └── Finalizer: multigres.com/topo-deletion-ordering
```

### 2.3 How Deletion Works Today (Full Cluster Teardown)

When a user deletes a MultigresCluster, a carefully choreographed finalizer
chain runs:

```
Step 1: MultigresCluster controller sees DeletionTimestamp
        ┌────────────────────────────────────────────────────────┐
        │  MultigresCluster                                      │
        │  Finalizer: "multigres.com/cluster-cleanup"            │
        │                                                        │
        │  handleDeletion():                                     │
        │    a) Deletes all Cell CRs                             │
        │    b) Deletes all TableGroup CRs (cascades to Shards)  │
        │    c) Polls every 2s: any Cells or Shards still exist? │
        │       YES → requeue (keep waiting)                     │
        │       NO  → remove own finalizer                       │
        │                                                        │
        │  While waiting, the MultigresCluster's own finalizer   │
        │  prevents Kubernetes GC from cascading to TopoServer.  │
        │  This keeps etcd alive for children to clean up against│
        └────────────────────────────────────────────────────────┘
                    │
                    │ Deletes Cell and TableGroup CRs
                    │ (TableGroup deletion cascades to Shards via OwnerRef)
                    │
        ┌───────────┴───────────────────────────────────────────┐
        │                                                       │
        ▼                                                       ▼
Step 2: Cell enters deletion              Step 2: Shard enters deletion
        (DeletionTimestamp set)                    (DeletionTimestamp set)
        │                                         │
        ▼                                         │
  Cell data-handler:                              ├──► Shard data-handler:
   - Unregisters cell from topo                   │     - Unregisters DB from topo
   - Removes its finalizer                        │     - Removes its finalizer
                                                  │
                                                  └──► Shard resource-handler:
                                                        - Deletes Deployments
                                                        - Deletes PVCs (per policy)
                                                        - Removes pod finalizers
                                                          (SKIPS drain — see below)
                                                        - Removes its finalizer

Step 3: All Cells and Shards are gone.
        MultigresCluster controller sees 0 remaining.
        Removes its own finalizer.

Step 4: Kubernetes GC now cascades to TopoServer.
        ┌────────────────────────────────────────────────────┐
        │  TopoServer                                        │
        │  Finalizer: "multigres.com/topo-deletion-ordering" │
        │                                                    │
        │  handleDeletion():                                 │
        │    Any Shards or Cells still exist?                │
        │    NO → remove finalizer → etcd StatefulSet deleted│
        └────────────────────────────────────────────────────┘
```

**Why drains are skipped during shard deletion (the critical detail):**

During full cluster teardown, the resource-handler's shard deletion code has
this comment (`shard_controller.go` lines 354-369):

> "During shard deletion, remove pod finalizers directly and skip the drain
> state machine. The data-handler has already unregistered the database from
> topology in its own handleDeletion, so pooler entries are cleaned up at the
> database level. The drain state machine cannot run reliably here because the
> data-handler has already removed its finalizer and stopped reconciling
> before the resource-handler gets a chance to set drain annotations."

The data-handler finishes first and stops reconciling. The resource-handler
then has nobody to drive the drain state machine, so it force-removes pod
finalizers and deletes pods directly.

This works for full cluster teardown only because the topo server is also being
destroyed — any residual per-pooler entries in etcd are wiped with it.

---

## 3. Why This Is a Problem

### 3.1 Violation of Operator Best Practices

The Operator SDK best practices documentation states:

> "Inside an Operator, multiple controllers should be used if multiple CRDs
> are managed. This helps in separation of concerns and code readability.
> Note that this doesn't necessarily mean that we need to have one container
> image per controller, but rather **one reconciliation loop per CRD**."

We have **two reconciliation loops per CRD** for both Cell and Shard. The
data-handler does not have its own CRD — it is a second controller watching
someone else's resource.

### 3.2 No Ordering Guarantee Between Controllers

When a Shard CR is marked for deletion, **both controllers race**:

```
  Shard CR gets DeletionTimestamp
         │
         ├──────────────────────────────────┐
         │                                  │
         ▼                                  ▼
  data-handler fires                resource-handler fires
  (unregisters from topo)           (wants to drain pods)
  (removes its finalizer)           (but nobody drives drain!)
```

The Kubernetes controller-runtime does not guarantee which reconciler runs
first. Both see the DeletionTimestamp and enter their cleanup paths
independently. There is no sequencing, no communication channel, and no way
for one to tell the other "wait, I'm not done yet."

### 3.3 Dual Finalizers Create Complex Coordination

Each Shard has **two finalizers** from two different controllers:

- `multigres.com/shard-data-protection` (data-handler)
- `multigres.com/shard-resource-protection` (resource-handler)

Each controller independently adds, checks, and removes its own finalizer.
Neither controller knows what state the other's cleanup is in. The resource-
handler's code explicitly acknowledges this:

> "The drain state machine cannot run reliably here because the data-handler
> has already removed its finalizer and stopped reconciling before the
> resource-handler gets a chance to set drain annotations."

### 3.4 Status Conflicts

Both controllers write to the same `shard.Status` field. The resource-handler
uses SSA with field owner `"multigres-resource-handler"` and must manually
NIL out fields owned by the data-handler:

```go
patchObj.Status.PodRoles = nil         // Owned by data-handler
patchObj.Status.LastBackupTime = nil   // Owned by data-handler
patchObj.Status.LastBackupType = ""    // Owned by data-handler
```

Adding a new data-handler status field requires updating the resource-handler's
exclusion list. There is no compile-time safety for this.

### 3.5 Individual Shard Deletion Is Broken

When the operator supports multiple shards per database, users will remove
individual shards from a live cluster. The cluster stays alive — only one
shard is being deleted.

**What should happen:**

1. All pods in the shard are gracefully drained (unregistered from topo)
2. The database entry is unregistered from topo
3. Pods, PVCs, and Services are cleaned up
4. The Shard CR is deleted

**What actually happens with the current architecture:**

1. TableGroup controller deletes the Shard CR
2. data-handler sees DeletionTimestamp, unregisters DB from topo, removes its
   finalizer, stops reconciling
3. resource-handler sees DeletionTimestamp, wants to drain pods, but the
   data-handler (which drives the drain state machine) has already left
4. Falls back to force-removing pod finalizers — pods die without draining
5. Individual pooler entries are left orphaned in topo permanently

### 3.6 Finalizers Are Making Everything More Complicated Than It Needs To Be

The current system uses **six finalizers** across four resource types:

| Resource | Finalizer | Purpose |
|----------|-----------|---------|
| MultigresCluster | `multigres.com/cluster-cleanup` | Blocks GC so children clean topo before TopoServer dies |
| Shard (data) | `multigres.com/shard-data-protection` | Blocks deletion until topo unregistration is done |
| Shard (resource) | `multigres.com/shard-resource-protection` | Blocks deletion until pods/PVCs are cleaned |
| TopoServer | `multigres.com/topo-deletion-ordering` | Blocks deletion until children are gone |
| Pod | `multigres.com/pool-pod-protection` | Blocks pod deletion until drain is complete |

These finalizers exist because the architecture tries to do cleanup **during
deletion**. But deletion is the worst time to do cleanup: controllers are
racing, resources are disappearing, and the topo server may already be dying.

The root cause is that **child controllers are doing topo cleanup during
deletion** — which requires finalizers to gate the deletion. But if the
**parent** handles all topo registration, it can unregister the child from
topo *before* calling `Delete()`. There's nothing left for a finalizer to do.

Mature production operators managing stateful distributed databases achieve
**zero finalizers** by pushing all topo registration to the parent level and
doing cleanup before deletion, not during it.

---

## 4. Topology Registration: What's Wrong and How to Fix It

### 4.1 The Self-Registration Anti-Pattern

In our operator, **child CRs register their own topo entries** — and in the
Shard controller's case, it registers an entry that doesn't even belong to
its level:

- **Shard controller** registers the **database** entry in topo. But databases
  are defined in `MultigresCluster.Spec.Databases[]` — a cluster-level concept
  with no CR. The Shard is reaching up to register a parent-level entity.
- **Cell controller** registers the **cell** entry in topo. The Cell CR is
  created by MultigresCluster, yet it's registering its own topo entry.

The correct principle: **the parent controller that creates a CR should
register its topo entry.** This is because the parent outlives the child —
when the child needs to be removed, the parent can unregister the topo entry
*before* deleting the child CR, eliminating the need for finalizers.

The hierarchy:

```
MultigresCluster (CR)
  └── Databases[]         (config in cluster spec, no CR)
  └── Cells[]             (creates Cell CRs)
        └── TableGroups[] (creates TableGroup CRs)
              └── Shards[]  (creates Shard CRs)
```

MultigresCluster owns both the database concept and creates Cell CRs, so it
should register both databases and cells in topo.

**Topo registration** (who creates/deletes etcd entries):

| Level | Our Controller | Currently Registers in Topo | Should Register in Topo |
|-------|----------------|----------------------------|------------------------|
| Top | MultigresCluster | Nothing | **Database, Cell** |
| Mid | TableGroup | Nothing | Nothing (no shard entity in upstream topo API) |
| Low | Shard | **Database** ⚠️ (wrong level) | Nothing |
| Low | Cell | **Cell** ⚠️ (self-registration) | Nothing |

> **Note:** The upstream `topoclient` has no `CreateShard`/`DeleteShard` API.
> Shards are not a standalone entity in the multigres topology. They exist
> implicitly under the database path in etcd — the shard lock path is
> `databases/{db}/{tablegroup}/{shard}/` (see `shard_lock.go:44`), and pooler
> entries reference their shard via `ShardKey{Database, TableGroup, Shard}`.
> Since `CreateDatabase()` is the API that establishes the parent path under
> which shards live, and databases are defined in
> `MultigresCluster.Spec.Databases[]`, the MultigresCluster controller is the
> natural place for database topo registration.
>
> If upstream multigres adds explicit shard CRUD to the topoclient in the
> future (likely needed for multi-shard discovery and key-range management),
> the TableGroup controller — which creates and prunes Shard CRs today —
> would be the natural place to register shards in topo, following the
> "parent registers children" principle.

**Topo pruning** (who removes stale etcd entries) and **CR pruning** (who
removes orphan Kubernetes resources):

| Level | Our Controller | Currently Prunes | Should Prune |
|-------|----------------|------------------|-------------|
| Top | MultigresCluster | Nothing | Stale databases/cells in topo |
| Mid | TableGroup | Orphan Shard CRs ✅ | Orphan Shard CRs ✅ |
| Low | Shard | Nothing | **Stale pooler entries** in topo |
| Low | Cell | Nothing | Nothing |

### 4.2 What Can Go Wrong With Self-Registration

1. **Deletion requires finalizers.** If the Cell registers itself, it must
   unregister during deletion — which requires a finalizer to block the
   deletion until cleanup is done. If MultigresCluster registers the cell,
   it unregisters *before* calling `Delete()` on the Cell CR. No finalizer
   needed.

2. **Multi-shard future.** If multiple shards belong to one database, which
   Shard controller owns the database entry? They all race to create it. If
   the MultigresCluster controller owns the database record instead, this
   problem disappears.

3. **Database outlives individual shards.** A database is a cluster-wide entity
   describing backup policy, cells, and durability. It shouldn't be tied to the
   lifecycle of one shard. If the Shard CR is force-deleted, the database entry
   gets orphaned.

4. **Deletion ordering.** When a Shard is deleted, the data-handler unregisters
   the database from topo before the resource-handler can drain pods. But the
   database isn't the shard's to delete — the MultigresCluster should decide
   when the database entry goes away.

5. **Parent has full visibility.** MultigresCluster knows the entire desired
   state (all databases, all cells). It can compare desired vs actual in topo
   and prune anything that shouldn't be there. Child controllers only see
   their own CR.

### 4.3 The Correct Pattern

Each level should manage topo at its own scope:

```
MultigresCluster controller
  └─ Register/unregister database in topo (owns databases in spec)
  └─ Register/unregister cell in topo (creates Cell CRs)
  └─ Prune stale cells/databases

TableGroup controller
  └─ Prune orphan Shard CRs (already does this at Kubernetes level)
  └─ No topo operations (no topo entity corresponds to a TableGroup)

Shard controller
  └─ Prune stale pooler entries (list → compare → delete)
  └─ Read pooler status for drain state machine

Cell controller
  └─ No topo operations (MultigresCluster registers cells)
  └─ Purely resource reconciliation (MultiGateway Deployment/Service)
```

### 4.4 Self-Registering Components (What the Operator Should NOT Touch)

Three of the four topo entity types are self-managed by the upstream multigres
components themselves:

| Component | Registers on Startup | Unregisters on Graceful Shutdown |
|-----------|:---:|:---:|
| **MultiPooler** | ✅ `RegisterMultiPooler()` | ❌ Only marks `NOT_SERVING` on **graceful shutdown**, **never deletes** its etcd key. If the pod is killed (OOMKill, node failure, force-delete), the entry stays with its last state (`SERVING`). Comment: *"For poolers, we don't un-register them on shutdown (they are persistent component)"* |
| **MultiGateway** | ✅ `RegisterMultiGateway()` | ✅ `UnregisterMultiGateway()` deletes the key on shutdown |
| **MultiOrch** | ✅ `RegisterMultiOrch()` | ✅ `UnregisterMultiOrch()` deletes the key on shutdown |

The operator should NOT register or unregister gateways or orchs. They handle
their own lifecycle.

For poolers, the operator **only unregisters** dead entries (via
`UnregisterMultiPooler()` in the drain state machine) — it **never registers**
them. The pods register themselves on startup. This is the current behavior
and should remain unchanged. The reason the operator must handle pooler
unregistration is that the pooler intentionally never cleans up its own etcd
key on shutdown.

---

## 5. Upstream TopoClient API Coverage

### 5.1 GlobalStore Interface

| Method | Used by Operator | Needed? |
|--------|:---:|---------|
| `GetCellNames()` | ❌ | Yes — needed for prune-on-reconcile (compare topo vs spec) |
| `GetCell()` | ❌ | Not needed — operator creates cells, doesn't read them back |
| `CreateCell()` | ✅ | Yes — currently Cell controller, should be MultigresCluster |
| `UpdateCellFields()` | ❌ | Not yet — would be needed if cell config changes post-creation |
| `DeleteCell()` | ✅ | Yes — currently Cell controller, should be MultigresCluster |
| `GetDatabaseNames()` | ❌ | Yes — needed for prune-on-reconcile (compare topo vs spec) |
| `GetDatabase()` | ❌ | Not needed |
| `CreateDatabase()` | ✅ | Yes — currently Shard, should be MultigresCluster |
| `UpdateDatabaseFields()` | ✅ | Yes — updates backup location and cells |
| `DeleteDatabase()` | ✅ | Yes — currently Shard, should be MultigresCluster |

### 5.2 CellStore — MultiPooler Methods

| Method | Used by Operator | Needed? |
|--------|:---:|---------|
| `GetMultiPooler()` | ❌ | Not needed — individual pooler reads not required |
| `GetMultiPoolerIDsByCell()` | ❌ | Not needed — `GetMultiPoolersByCell` returns full info |
| `GetMultiPoolersByCell()` | ✅ | Yes — drain state machine and PodRoles |
| `CreateMultiPooler()` | ❌ | No — **pod creates its own entry on startup** |
| `UpdateMultiPooler()` | ❌ | No — **pod updates its own entry** |
| `UpdateMultiPoolerFields()` | ❌ | No — **pod updates its own fields** |
| `UnregisterMultiPooler()` | ✅ | Yes — operator cleans up dead pooler entries during drain |
| `RegisterMultiPooler()` | ❌ | No — **pod registers itself on startup** |

### 5.3 CellStore — MultiGateway and MultiOrch Methods

**Not used and not needed.** All 16 methods (8 per component) are handled by
the components themselves. Gateways and orchs self-register on startup and
self-unregister on graceful shutdown. No operator intervention required.

### 5.4 Store-Level Methods

| Method | Used | Needed? |
|--------|:---:|---------|
| `LockShard()` | ❌ | Not yet — would be needed for shard migration |
| `TryLockShard()` | ❌ | Not yet — would be needed for shard migration |
| `ConnForCell()` | ❌ | Used internally by store, not needed directly |
| `Close()` | ✅ | Yes — proper cleanup |

---

## 6. Proposed Solution

### 6.1 Convert Data-Handler to Utility Packages

Instead of deleting the data-handler entirely, **keep it as a module but
convert its controllers into utility packages** that any controller can call.
The pattern is a shared package of topo helper functions — not a controller.

The data-handler module becomes:

```
pkg/data-handler/
├── topo/
│   ├── database.go      ← RegisterDatabase(), UnregisterDatabase()
│   ├── cell.go           ← RegisterCell(), UnregisterCell()
│   ├── pooler.go         ← PrunePoolers(), GetPoolerStatus()
│   └── store.go          ← TopoStoreFactory (shared connection logic)
├── drain/
│   └── drain.go          ← ExecuteDrainStateMachine()
└── backuphealth/
    └── backuphealth.go   ← EvaluateBackupHealth()
```

The data-handler controller directories
(`pkg/data-handler/controller/cell/` and
`pkg/data-handler/controller/shard/`) are deleted.

Each existing controller calls these packages at the correct level:

```
MultigresCluster controller
  └─ calls topo.RegisterDatabase() / topo.UnregisterDatabase()
  └─ calls topo.RegisterCell() / topo.UnregisterCell()
     (parent registers children — databases and cells are both cluster-level)

Shard controller (merged, single reconciler)
  ├── reconcileResources()         ← existing resource-handler logic
  ├── reconcilePodRoles()          ← calls topo.GetPoolerStatus()
  ├── reconcileDrainState()        ← calls drain.ExecuteDrainStateMachine()
  ├── reconcileBackupHealth()      ← calls backuphealth.EvaluateBackupHealth()
  └── reconcilePoolerPrune()       ← calls topo.PrunePoolers() (NEW)

Cell controller (merged, single reconciler)
  └── reconcileResources()         ← existing resource-handler logic only
      (no topo — MultigresCluster handles cell topo registration)
```

### 6.2 No Finalizers

The proposed architecture eliminates **all** finalizers. This is possible
because the "make it safe, then delete" pattern ensures that by the time a
resource is actually deleted, there is nothing left to clean up.

| Resource | Current Finalizer | Why No Longer Needed |
|----------|-------------------|----------------------|
| MultigresCluster | `multigres.com/cluster-cleanup` | Full teardown uses Kubernetes GC (ownerReferences cascade). Topo entries die with the managed etcd. |
| Shard (data) | `multigres.com/shard-data-protection` | Merged into single controller. With PendingDeletion, database is unregistered and pods drained before `Delete()`. |
| Shard (resource) | `multigres.com/shard-resource-protection` | Same. When parent calls `Delete()`, the shard is already idle. |
| TopoServer | `multigres.com/topo-deletion-ordering` | Children don't clean topo during deletion (they clean before via PendingDeletion or topo dies with managed etcd). |
| Pod | `multigres.com/pool-pod-protection` | Single controller never calls `Delete()` on a pod until drain is complete. No need for a finalizer gate. |

**Why this works — the key insight:**

Finalizers exist to run cleanup code during deletion. But if you do all the
cleanup *before* deletion, there's nothing left for the finalizer to do:

```
  CURRENT (with finalizers):
    Parent calls Delete() → CR gets DeletionTimestamp → finalizer runs cleanup
    → finalizer removed → CR gone
    Problem: cleanup is racing against destruction

  PROPOSED (without finalizers):
    Parent sets PendingDeletion → controller drains/unregisters → sets Idle=True
    → Parent sees Idle → Parent calls Delete() → CR gone immediately
    No race: everything is safe BEFORE deletion
```

**What about direct `kubectl delete`?**

If someone bypasses the parent and runs `kubectl delete shard` directly, the
shard will be deleted immediately without draining. This is acceptable because:

1. Direct deletion of child resources is operator misuse — the documented way
   to remove a shard is to update the cluster spec.
2. Mature production operators handle this identically: direct deletion of
   children skips their turndown process.
3. The shard controller's `handleDeletion` still runs (Kubernetes calls the
   reconciler one final time with DeletionTimestamp set) and does best-effort
   cleanup. It just doesn't block if cleanup fails.
4. With pooler pruning, stale entries self-heal if the parent recreates the
   shard (i.e. the shard is still in the TableGroup spec) and the new
   controller runs its prune logic.

### 6.3 "Make It Safe, Then Delete" Pattern

Instead of trying to drain during deletion (when the CR is dying and
controllers are racing), adopt this pattern:

**For shard deletion:**

```
  TableGroup controller                    Shard controller
  ┌────────────────────────┐              ┌─────────────────────────┐
  │                        │              │                         │
  │ Shard "100-200" is     │  annotate    │ Sees PendingDeletion:   │
  │ no longer in spec      │─────────────►│  1. Drain all pods      │
  │                        │              │  2. Set Idle=True       │
  │ Set annotation:        │              │                         │
  │ PendingDeletion        │              │                         │
  │                        │◄─────────────│ Status: Idle=True       │
  │ Check: Idle=True?      │  status      │                         │
  │ YES → delete Shard CR  │              │                         │
  │                        │              │ CR deleted immediately  │
  │                        │              │ (no finalizer to block) │
  └────────────────────────┘              └─────────────────────────┘
```

Note: The **MultigresCluster** controller owns all topo registration
(databases and cells). The TableGroup only manages Shard CRs at the
Kubernetes level. Database unregistration happens when a database is removed
from `MultigresCluster.Spec.Databases[]` — **not** when an individual shard
is deleted. In a multi-shard scenario, deleting one shard leaves the database
entry intact because other shards still use it.

**For cell deletion:**

```
  MultigresCluster controller     Shard controllers
  ┌────────────────────────┐     ┌──────────────────┐
  │                        │     │                  │
  │ User removes "zone-b"  │     │                  │
  │ from spec              │     │                  │
  │                        │     │                  │
  │ 1. Update all shard    │────►│ Drain zone-b     │
  │    specs: remove       │     │ pods (normal     │
  │    zone-b pools        │     │ scale-down path) │
  │                        │     │                  │
  │ 2. Wait for shards to  │◄────│ Pools drained    │
  │    finish draining     │     │                  │
  │                        │     └──────────────────┘
  │ 3. Unregister cell     │
  │    from topo           │
  │    (parent does this)  │
  │                        │
  │ 4. Delete Cell CR      │
  │    (nothing to clean   │
  │     up, no finalizer)  │
  └────────────────────────┘
```

**For full cluster teardown:**

Full cluster teardown is radically simpler without finalizers. Kubernetes
garbage collection does all the work:

```
  User: kubectl delete multigrescluster my-cluster

  1. Kubernetes sets DeletionTimestamp on MultigresCluster
  2. Kubernetes GC cascades via ownerReferences:
     - Deletes Cell CRs → GC deletes their MultiGateway resources
     - Deletes TableGroup CRs → GC deletes their Shard CRs →
       GC deletes shard pods/PVCs/services
     - Deletes TopoServer CR → GC deletes etcd StatefulSet
  3. Each controller gets a final reconcile with DeletionTimestamp.
     handleDeletion does best-effort cleanup but never blocks.
  4. Done. No polling loops, no 2-second requeue cycles.
```

**Why topo cleanup is unnecessary during full teardown:**

The TopoServer (etcd) is owned by the MultigresCluster and is being deleted
in the same GC cascade. Any pooler entries, cell entries, or database entries
in the topology will be destroyed with it. Trying to unregister them first
is wasted work — like carefully removing files from a hard drive that is
about to be shredded.

This is the correct approach: the cluster controller needs no complex
`handleDeletion` logic during full teardown. All topo entries die with the
managed etcd. Kubernetes GC shreds everything together.

### 6.4 Pooler Pruning (Self-Healing Topo)

The best practice is to prune topo on every reconcile. We should do this for
poolers, since poolers never clean up after themselves.

On every Shard controller reconcile, the new `reconcilePoolerPrune()` phase:

1. Calls `store.GetMultiPoolersByCell()` — list all poolers in topo for each
   cell this shard uses
2. Filters to poolers belonging to this shard's database
3. Compares to actual running pods
4. Calls `store.UnregisterMultiPooler()` for any pooler that doesn't have a
   matching pod

This uses APIs the operator already imports and uses. No new upstream API
needed. This is a standard list-compare-delete pattern implemented as a helper
function in our operator code, not an upstream API endpoint.

This pruning logic solves multiple problems:
- Orphaned pooler entries from force-deleted shards
- Poolers left behind after ungraceful pod termination
- Stale entries after controller crashes
- The external topo gap (section 10)

---

## 7. Implementation Plan

### Phase 1: Create Topo Utility Packages

**Goal:** Extract topo operations from data-handler controllers into reusable
utility packages in the data-handler module.

Create:
- `pkg/data-handler/topo/database.go` — `RegisterDatabase()`,
  `UnregisterDatabase()`
- `pkg/data-handler/topo/cell.go` — `RegisterCell()`, `UnregisterCell()`
- `pkg/data-handler/topo/pooler.go` — `PrunePoolers()`, `GetPoolerStatus()`
- `pkg/data-handler/topo/store.go` — shared `TopoStoreFactory`
- `pkg/data-handler/drain/drain.go` — `ExecuteDrainStateMachine()` (extract
  from current controller)
- `pkg/data-handler/backuphealth/backuphealth.go` —
  `EvaluateBackupHealth()` (extract from current controller)

### Phase 2: Move Topo Registration to MultigresCluster Controller

**Goal:** Parent registers children. Databases and cells are both registered
by MultigresCluster (the controller that owns both concepts). Pruning logic
is also added here so the controller can remove stale entries.

Modify:
- `pkg/cluster-handler/controller/multigrescluster/multigrescluster_controller.go`
  — call `topo.RegisterDatabase()` for each database in the spec during
  reconciliation. Call `topo.RegisterCell()` for each cell. Implement
  prune-on-reconcile: call `GetDatabaseNames()`/`GetCellNames()`, compare
  to the spec, and call `DeleteDatabase()`/`DeleteCell()` for any entries
  that no longer exist in the spec. This ensures that when the append-only
  CEL restriction on `Spec.Cells` is eventually lifted, the topo cleanup
  logic is already in place.

### Phase 3: Merge Cell Controllers

**Goal:** One reconciler for Cell, no data-handler Cell controller. The Cell
controller becomes purely a resource reconciler — it no longer does any topo
operations (cell topo registration moved to MultigresCluster in Phase 2).

Modify:
- `pkg/resource-handler/controller/cell/cell_controller.go` — remove topo
  logic entirely. Cell controller only reconciles MultiGateway resources.

Delete:
- `pkg/data-handler/controller/cell/cell_controller.go`
- `pkg/data-handler/controller/cell/cell_controller_test.go`
- `pkg/data-handler/controller/cell/cell_controller_internal_test.go`

Remove from `cmd/multigres-operator/main.go`:
- The `datahandlercellcontroller.CellReconciler` registration.

### Phase 4: Merge Shard Controllers

**Goal:** One reconciler for Shard, no data-handler Shard controller.

Modify:
- `pkg/resource-handler/controller/shard/shard_controller.go` — add phases
  for drain, PodRoles, backup health, and pooler pruning. Import and call
  the utility packages.

Delete:
- `pkg/data-handler/controller/shard/shard_controller.go`
- `pkg/data-handler/controller/shard/shard_controller_test.go`
- `pkg/data-handler/controller/shard/shard_controller_internal_test.go`

Remove from `cmd/multigres-operator/main.go`:
- The `datahandlershardcontroller.ShardReconciler` registration.

Reconciler structure after merge:

```go
func (r *ShardReconciler) Reconcile(ctx, req) (Result, error) {
    shard := &Shard{}
    r.Get(ctx, req.NamespacedName, shard)

    if !shard.DeletionTimestamp.IsZero() {
        return r.handleDeletion(ctx, shard)
    }

    // Check if shard is marked for turndown by parent
    if shard.Annotations[AnnotationPendingDeletion] != "" {
        return r.prepareTurndown(ctx, shard)
    }

    // Phase 1: Reconcile Kubernetes resources (pods, PVCs, services)
    r.reconcileResources(ctx, shard)

    // Phase 2: Update PodRoles from topology
    r.reconcilePodRoles(ctx, shard)

    // Phase 3: Execute drain state machine for any draining pods
    r.reconcileDrainState(ctx, shard)

    // Phase 4: Evaluate backup health
    r.reconcileBackupHealth(ctx, shard)

    // Phase 5: Prune stale pooler entries from topo
    r.reconcilePoolerPrune(ctx, shard)

    return ctrl.Result{}, nil
}
```

Note: the Shard controller **no longer registers the database**. That
responsibility moved to the MultigresCluster controller in Phase 2.

### Phase 5: Remove All Finalizers

- Remove `multigres.com/cluster-cleanup` from MultigresCluster
- Simplify MultigresCluster `handleDeletion` to a no-op (GC cascades)
- Remove `multigres.com/shard-data-protection`
- Remove `multigres.com/shard-resource-protection`
- Remove `multigres.com/pool-pod-protection` (pod finalizer)
- Remove `multigres.com/topo-deletion-ordering` from TopoServer
- Remove all Cell finalizers
- Verify ownerReferences are correctly set on all child resources

**Note on topo registration conditions:** The current `DatabaseRegistered`
condition on Shard and `TopologyRegistered` condition on Cell exist to
distinguish "never registered" from "temporarily unreachable" during
deletion. Since topo registration moves to MultigresCluster, these conditions
can be removed from Cell and Shard. MultigresCluster may need equivalent
tracking (e.g. a `TopoRegistrationHealthy` condition) to know whether
registration succeeded before allowing deletion flows.

### Phase 6: Add PendingDeletion Support

- Add `AnnotationPendingDeletion` annotation and `Idle` condition
- Update TableGroup controller to use PendingDeletion instead of direct delete
- Update MultigresCluster controller for cell deletion flow
- Add tests for individual shard deletion and cell removal

### Phase 7: Delete Data-Handler Controllers

- Delete `pkg/data-handler/controller/` directory entirely
- Keep `pkg/data-handler/topo/`, `pkg/data-handler/drain/`,
  `pkg/data-handler/backuphealth/` as utility packages
- Update main.go references
- Update documentation

---

## 8. Migration Strategy

Since there are no external users yet, this can be done as a single
refactoring effort. Recommended order:

1. **Create topo utility packages** (Phase 1) — smallest scope, no behaviour
   change, proves the extraction pattern.
2. **Merge Cell controllers** (Phase 3) — smaller controller, proves the
   merge pattern. **Note:** at this point the merged Cell controller still
   contains topo registration code temporarily. This is a deliberate stepping
   stone — the topo logic moves to MultigresCluster in the next step.
3. **Move topo registration to MultigresCluster** (Phase 2) — fixes the
   level problem. Database and cell registration both move to the parent.
   The Cell controller's temporary topo code from step 2 is removed here.
4. **Merge Shard controllers** (Phase 4) — larger scope, more moving parts.
5. **Remove finalizers** (Phase 5) — requires confidence in the merged
   controllers.
6. **Add PendingDeletion** (Phase 6) — enables future workflows.
7. **Clean up** (Phase 7) — delete old controllers.

---

## 9. Risk Assessment

### What could go wrong

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Regression in drain state machine | Low | Drain code is extracted, not rewritten. All existing tests move with it. |
| Regression in topology registration | Low | Registration logic is simple and well-tested. |
| Status update conflicts during transition | Low | Moving from two controllers to one eliminates conflicts, doesn't create them. |
| Direct `kubectl delete` of child CRs | Low | Documented as misuse. handleDeletion does best-effort. Pooler pruning self-heals on recreation. |
| Orphaned topo entries after full teardown | None (managed topo) | Topo entries die with managed etcd. External topo is a known gap (section 10). |
| OwnerReference missing on a child resource | Low | Unit tests verify ownerReferences. Without them, GC won't cascade. |

### What we gain

- **No finalizers at all** — eliminates the entire category of finalizer bugs,
  races, coordination problems, and "stuck in Terminating" states
- **Correct topo ownership** — parent registers children. Databases and cells
  registered at MultigresCluster level. TableGroup prunes Shards. Shard prunes
  poolers. Each level manages what it owns.
- **Self-healing topo** — pooler pruning on every reconcile cleans stale entries
  automatically
- **Deterministic deletion** — one controller, one handleDeletion, no races
- **Simpler code** — no SSA field ownership juggling, no dual finalizers,
  no cross-controller assumptions
- **Instant cluster teardown** — no polling loops, no 2-second requeue cycles.
  `kubectl delete` and GC does the rest.
- **Reusable topo utilities** — any controller can call the topo packages

### What we lose

- **Handler separation as code boundary** — the resource-handler/data-handler
  split goes away at the controller level. Mitigation: the data-handler module
  survives as utility packages, maintaining logical separation.
- **Protection against direct child deletion** — without finalizers, a direct
  `kubectl delete shard` deletes immediately without draining. Mitigation:
  documented as operator misuse. Pooler pruning self-heals.

---

## 10. To Prune or Not to Prune

### Why Prune

Today the operator **registers** topo entries (cells, databases) but never
**prunes** stale ones. If anything goes wrong — a crash, a force-delete, an
ungraceful shutdown, or a spec change — orphaned entries stay in topo forever.
The system has no self-healing capability.

Pruning on every reconcile fixes this. On each cycle the controller:

1. Lists what exists in topo
2. Compares it to the desired state (the spec or running pods)
3. Removes anything that shouldn't be there

This is a standard list-compare-delete pattern. It uses APIs the operator
already imports (`GetMultiPoolersByCell`, `UnregisterMultiPooler`,
`GetCellNames`, `GetDatabaseNames`, `DeleteCell`, `DeleteDatabase`). No new
upstream dependencies are needed.

### What Pruning Solves

| Problem | Without Pruning | With Pruning |
|---------|----------------|-------------|
| Orphaned pooler entries from force-deleted shards | Permanent | Self-heals on next Shard reconcile |
| Stale poolers after ungraceful pod termination | Permanent | Self-heals on next Shard reconcile |
| Database removed from spec | Entry stays in topo | MultigresCluster prunes on reconcile |
| Cell removed from spec | Entry stays in topo | MultigresCluster prunes on reconcile |
| External topo: cluster deleted and recreated | Stale entries linger | Self-heals on first reconcile of new cluster |
| External topo: cluster deleted and never recreated | Permanent orphans | Still permanent (see below) |

### The External Topo Edge Case

The operator API supports external topology servers via
`ExternalTopoServerSpec`. When configured, no TopoServer CR is created — the
operator connects to a user-provided etcd endpoint. Topology data survives
cluster deletion.

Pruning handles the **recreation** case naturally: when a new cluster is
created against the same external topo, stale entries are cleaned on the first
reconcile cycle.

For cell and database entries, `topo.RegisterCell()` and
`topo.RegisterDatabase()` use create-or-update semantics
(`allowUpdate=true`), so stale entries are naturally overwritten.

The **one case pruning cannot fix** is: cluster deleted with external topo and
**never recreated**. There is no controller running to prune. Two future
options exist:

**Option A: Conditional finalizer on MultigresCluster**

Only when external topo is configured, add a single finalizer that drives
ordered cleanup:

```
1. Mark all children as PendingDeletion
2. Wait for all shards to drain (pods unregistered from topo)
3. Unregister all databases from topo
4. Unregister all cells from topo
5. Remove finalizer → cluster deleted
```

**Option B: Cluster-level prune on delete**

In `handleDeletion`, do a one-shot prune of all cells and databases from topo
before letting the CR die. This is simpler than Option A but doesn't wait for
shards to drain.

### Current Stance

Pruning should be implemented as part of this refactor. It closes the
self-healing gap, requires no new upstream APIs, and the implementation is
straightforward. The permanent-orphan-with-external-topo edge case is a
documented known limitation that can be addressed later with Option A or B
when external topo is actually in use, but will NOT be addressed in this
refactor.

---

## 11. Questions for the Multigres Team

Before implementing, the following questions should be clarified with the
upstream multigres team:

### 11.1 Shard Registration in Topology

The upstream `topoclient` currently has no `CreateShard`/`DeleteShard` API.
Shards exist implicitly under the database path
(`databases/{db}/{tablegroup}/{shard}/`) and are only materialised when
`LockShard()` creates the lock path.

Our operator currently registers databases via `CreateDatabase()`, and the
shard's existence is implicit — tracked through the Shard CR at the
Kubernetes level and the pooler entries that reference a `ShardKey`.

**Questions:**

1. Is there a plan to add explicit shard CRUD to the `topoclient` for
   multi-shard support (e.g. `CreateShard`, `GetShardNames`,
   `DeleteShard`)? If so, what data would a shard record contain (key
   range, serving state, primary tablet, etc.)?

2. In the meantime, is it correct for the operator to continue registering
   only the database via `CreateDatabase()` and let shards remain implicit?
   Or should we be creating shard directory nodes explicitly?

### 11.2 Operator-Side Pruning

We plan to implement a prune-on-reconcile pattern where the operator:

- **Shard controller:** lists pooler entries in topo via
  `GetMultiPoolersByCell()`, compares to running pods, and calls
  `UnregisterMultiPooler()` for any stale entries.
- **MultigresCluster controller:** lists databases/cells in topo via
  `GetDatabaseNames()`/`GetCellNames()`, compares to the spec, and calls
  `DeleteDatabase()`/`DeleteCell()` for entries no longer in the spec.

**Questions:**

1. Are there any concerns with the operator calling `UnregisterMultiPooler()`
   to remove stale pooler entries, beyond what the drain state machine
   already does? Could this interfere with upstream pooler health-checking
   or connection routing?

2. Are there any upstream components that rely on the existence of a database
   or cell entry in topo and could break if the operator prunes it? For
   example, does `multiorch` assume the database entry exists before
   attempting to bootstrap a shard?


---

## Appendix: Key File References

### Current Architecture

| File | What it does |
|------|-------------|
| `cmd/multigres-operator/main.go:437-458` | Registers both data-handler controllers |
| `pkg/data-handler/controller/cell/cell_controller.go` | Cell topo registration + cleanup |
| `pkg/data-handler/controller/shard/shard_controller.go` | Shard topo registration + drain + PodRoles |
| `pkg/data-handler/controller/shard/drain.go` | Drain state machine |
| `pkg/data-handler/controller/shard/backup_health.go` | Backup health evaluation |
| `pkg/resource-handler/controller/shard/shard_controller.go:247-399` | Shard resource-handler handleDeletion |
| `pkg/resource-handler/controller/shard/status.go:80-92` | Manual NIL-ing of data-handler fields |
| `pkg/cluster-handler/controller/multigrescluster/multigrescluster_controller.go:242-317` | Cluster deletion finalizer chain |
| `pkg/cluster-handler/controller/tablegroup/tablegroup_controller.go` | TableGroup creates/prunes Shard CRs |
| `pkg/resource-handler/controller/toposerver/toposerver_controller.go:166-217` | TopoServer deletion ordering |

### Design Patterns Used

| Pattern | Description |
|---------|-----------|
| Parent registers children | MultigresCluster registers databases and cells in topo, not the children themselves |
| Parent prunes children | TableGroup prunes Shard CRs. Shard prunes stale pooler entries. MultigresCluster prunes stale databases/cells. |
| PrepareForTurndown gate | Parent checks `Idle=True` condition before calling `Delete()` |
| Topo utility package | Shared helper functions (list → compare → delete), not a controller |
| Zero finalizers | Safe deletion guaranteed by pre-deletion checks, not finalizer chains |

# Pod Management Architecture

> This document explains how the multigres-operator manages PostgreSQL pool pods directly, replacing the original StatefulSet-based approach. It covers the "why", "how", code structure, current feature set, and known gaps.

---

## Table of Contents

1. [Overview](#1-overview)
2. [Why Direct Pod Management](#2-why-direct-pod-management)
3. [Resource Topology](#3-resource-topology)
4. [Pod Lifecycle](#4-pod-lifecycle)
5. [Drain State Machine](#5-drain-state-machine)
6. [Rolling Updates](#6-rolling-updates)
7. [Spec-Hash Drift Detection](#7-spec-hash-drift-detection)
8. [PVC Management](#8-pvc-management)
9. [Pod Disruption Budgets](#9-pod-disruption-budgets)
10. [Status Aggregation](#10-status-aggregation)
11. [Feature Summary](#11-feature-summary)
12. [Code Organization](#12-code-organization)
13. [Naming Conventions](#13-naming-conventions)
14. [Interaction with Multigres](#14-interaction-with-multigres)
15. [Known Gaps and Future Work](#15-known-gaps-and-future-work)

---

## 1. Overview

The operator manages **individual Pods and PersistentVolumeClaims** for each pool replica instead of delegating to Kubernetes StatefulSets. The shard controller (`pkg/resource-handler/controller/shard/`) owns Pod, PVC, PDB, headless Service, and ConfigMap resources directly, reconciling them on every loop.

This architectural change was motivated by the fact that multigres has its own identity and discovery system (etcd topology) that is completely independent of Kubernetes StatefulSet identity. The operator previously used `ParallelPodManagement` and did not leverage ordered deployment or scaling — the two main features that StatefulSets provide over Deployments.

---

## 2. Why Direct Pod Management

### Problems with StatefulSets

| Problem | Impact |
|---|---|
| **No targeted decommissioning** | StatefulSets can only scale from ordinal N-1 downward. Deleting a specific replica (e.g., one whose data is corrupt or whose zone is draining) required external hacks. |
| **No independent zone/cell management** | Each cell already needed its own StatefulSet. The operator gained no simplification from StatefulSet identity guarantees. |
| **PVC lifecycle is opaque** | The only mechanism was `PersistentVolumeClaimRetentionPolicy`, which is limited and version-dependent. |
| **Rename/replace ambiguity** | StatefulSet ordinal identity conflicted with multigres's own etcd-based identity. Replacing a pod meant fighting the controller. |
| **GitOps drift** | StatefulSets report `replicas: N` in status, causing constant "configured" drift when using `kubectl apply`. |
| **Blocked rolling updates** | StatefulSet rolling update strategy (`OnDelete` or `RollingUpdate`) doesn't coordinate with multigres replication — it can't remove a standby from `synchronous_standby_names` before killing the pod. |

### What We Gained

| Benefit | Detail |
|---|---|
| **Targeted decommissioning** | Any specific pod can be drained and removed without affecting others. |
| **Simpler PVC lifecycle** | PVC creation/deletion is handled directly in the reconcile loop. Previously delegated to StatefulSet's `PersistentVolumeClaimRetentionPolicy`, which had version and behavior quirks. |
| **Drain state machine** | Scale-down coordinates with the data-handler to safely remove standbys from the sync standby list and clean up etcd entries. |
| **Rolling updates with primary awareness** | Updates replicas first, primary last, with switchover coordination. |
| **Sequential provisioning** | New pods are created one at a time, waiting for readiness before creating the next. This prevents multiple pods from simultaneously trying to restore from backup. |
| **Pod Disruption Budgets** | Per-pool PDBs with `maxUnavailable: 1` protect against voluntary evictions. |
| **Better observability** | Status aggregation directly from pod readiness conditions; no intermediate StatefulSet `.status` layer. |

### What We Didn't Lose

- **Automatic pod recreation**: The reconcile loop detects missing pods and recreates them.
- **Stable network identity**: Pods still get DNS-resolvable names via the headless service (hostname + subdomain).
- **Ordered startup**: Was never needed — `ParallelPodManagement` was already in use. The new sequential provisioning is actually superior.
- **PVC auto-creation**: Operator creates PVCs for each pod index, mirroring `volumeClaimTemplates` behavior.

---

## 3. Resource Topology

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
                ├── 🧠 MultiOrch (Deployment + Service, per-cell)
                └── 🏊 Pools (per pool × per cell):
                     ├── Pod-0  ← operator-managed, has finalizer
                     ├── Pod-1  ← operator-managed, has finalizer
                     ├── PVC-0  ← operator-managed (per-pod data)
                     ├── PVC-1  ← operator-managed (per-pod data)
                     ├── Backup PVC (shared across pods in cell)
                     ├── Headless Service (DNS resolution)
                     └── PodDisruptionBudget (maxUnavailable: 1)
```

Key points:
- **No StatefulSets** in the resource tree for pool pods.
- **Headless Service** is still required for DNS resolution. Multigres's `FullyQualifiedHostname()` uses DNS reverse lookup, so pods need resolvable FQDNs.
- **Shared Backup PVC** is per-shard-per-cell for filesystem backups; replaced with EmptyDir for S3.
- **PDB** is per-pool-per-cell to limit voluntary disruption.

---

## 4. Pod Lifecycle

### Creation (Scale-Up)

The `createMissingResources` function handles pod creation:

1. For each desired index `0..replicasPerCell-1`, check if a PVC and Pod exist.
2. Create missing PVCs first, then Pods that reference them.
3. **Sequential provisioning**: After creating a pod, the reconciler returns early and requeues. On the next reconcile, it checks if the pod is Ready before creating the next one. This prevents multiple replicas from simultaneously restoring from backup, which could exhaust I/O bandwidth.
4. Terminal pods (`Failed` or `Succeeded`) are cleaned up and their index is reused.

### Deletion (Scale-Down)

Scale-down uses the [drain state machine](#5-drain-state-machine). Extra pods beyond the desired count are identified, and the highest-index non-primary pod is selected for draining.

### External Deletion

When a pod is deleted externally (e.g., `kubectl delete pod`):
- If the pod was never scheduled (no `spec.nodeName`), the finalizer is removed immediately.
- If the pod was scheduled (had a running container), it enters the drain state machine to ensure etcd cleanup.

### Finalizer

Every pool pod gets a `multigres.com/pool-pod-protection` finalizer at creation. This prevents Kubernetes from garbage-collecting the pod before the operator can:
1. Coordinate with the data-handler for etcd cleanup.
2. Delete the associated data PVC (if the deletion policy says so).

The Shard itself has a `multigres.com/shard-resource-protection` finalizer to ensure all child pod finalizers are removed before the Shard is deleted.

---

## 5. Drain State Machine

The drain state machine coordinates pod removal between two controllers:

| Controller | Responsibility |
|---|---|
| **resource-handler** (shard controller) | Selects which pod to drain, sets `drain.multigres.com/state=requested`, removes finalizer and deletes PVC after `ready-for-deletion` |
| **data-handler** (shard controller) | Watches for the `requested` annotation, removes the pod from `synchronous_standby_names`, unregisters from etcd, sets `ready-for-deletion` |

### State Flow

```
                    ┌──────────────────────────────────┐
                    │       Scale-down detected        │
                    └──────────────┬───────────────────┘
                                   │
                    ┌──────────────▼───────────────────┐
                    │     Pod selected for drain       │
                    │  (non-ready > non-primary >      │
                    │   highest index)                  │
                    └──────────────┬───────────────────┘
                                   │
                    ┌──────────────▼───────────────────┐
                    │  Annotation: state=requested     │
                    │  + drain-requested-at timestamp   │
                    │  (set by resource-handler)        │
                    └──────────────┬───────────────────┘
                                   │
                    ┌──────────────▼───────────────────┐
                    │  Data-handler drains the pod:    │
                    │  1. Remove from sync standby     │
                    │  2. Unregister from etcd         │
                    │  3. Set state=ready-for-deletion │
                    └──────────────┬───────────────────┘
                                   │
                    ┌──────────────▼───────────────────┐
                    │  Resource-handler cleans up:     │
                    │  1. Remove finalizer             │
                    │  2. Delete data PVC (if policy)  │
                    │  3. Pod is garbage collected     │
                    └──────────────────────────────────┘
```

### Pod Selection Algorithm

When choosing which pod to remove during scale-down:

1. **Never select the primary.** The pod role is read from `shard.Status.PodRoles`, which the data-handler populates by reading etcd topology.
2. **Prefer non-ready pods.** Pods that are already failing are better candidates.
3. **Among ready non-primary pods, select the highest index.** This makes order deterministic and predictable.
4. **If no suitable pod is found, defer.** Requeue and try again.

### Safety Guarantees

- At most **one pod per pool** can be in the drain state at any time.
- Scale-down and rolling-update operations do not run concurrently — if a drain is in progress, rolling updates are deferred.
- If the data-handler is unavailable, the drain annotation sits untouched and the resource-handler retries on the next reconcile.

---

## 6. Rolling Updates

When a pod's spec has drifted from the desired state (detected via spec-hash mismatch), the reconciler performs a rolling update:

1. **Identify drifted pods** — `podNeedsUpdate` compares each pod's `multigres.com/spec-hash` annotation against the hash of the currently desired spec.
2. **Skip if drain in progress** — Rolling updates are deferred if any pod is currently being drained (scale-down takes precedence).
3. **Update replicas first** — Non-primary drifted pods are selected first.
4. **Primary last** — When only the primary remains, a controlled switchover is needed before draining and recreating. (Note: the switchover is coordinated via the data-handler using the same drain annotation mechanism.)
5. **One at a time** — Only one pod is drained per reconcile cycle. The reconciler returns early after initiating a drain, waiting for the pod to reach `ready-for-deletion` before proceeding to the next.
6. **RollingUpdate status condition** — A `RollingUpdate` condition is set on the Shard to track progress (e.g., `"2/5 pods updated"`).

---

## 7. Spec-Hash Drift Detection

Since most pod spec fields are immutable after creation, the operator uses a **hash-based approach** to detect drift:

1. At pod creation, `ComputeSpecHash` produces an FNV-1a hex string over all operator-managed fields:
   - Container images, commands, args, env vars, resources, volume mounts, security contexts
   - Pod affinity, node selector, volume definitions, termination grace period
2. The hash is stored as annotation `multigres.com/spec-hash` on the pod.
3. On each reconcile, `podNeedsUpdate` builds the desired pod and computes its hash. If it doesn't match the existing pod's annotation, the pod is flagged for update.

This avoids false positives from admission controllers (Istio, Linkerd, Vault) that inject sidecars or environment variables — only fields the operator explicitly sets are included in the hash.

---

## 8. PVC Management

### Data PVCs

Each pool pod gets its own data PVC named `data-{base-name}-{index}`. These PVCs:
- Are created before the pod (if missing).
- Persist across pod deletions — a restarted pod reattaches to its existing PVC and finds its PGDATA intact.
- Are deleted during scale-down only if `PVCDeletionPolicy.WhenScaled` is `Delete`.
- Are deleted during cluster teardown only if `PVCDeletionPolicy.WhenDeleted` is `Delete`.

### Shared Backup PVC

One shared PVC per shard per cell for filesystem-based backups, mounted at `/backups` on all pods. For S3 backups, this is replaced with an `EmptyDir` volume. See the backup architecture section in `implementation-notes.md` for details.

### PVC Deletion Policy

The `PVCDeletionPolicy` type (`whenDeleted`, `whenScaled`) controls lifecycle. Both default to `Retain` for maximum data safety. This is no longer tied to Kubernetes's `StatefulSetPersistentVolumeClaimRetentionPolicy` — the operator manages PVC deletion directly.

---

## 9. Pod Disruption Budgets

For each pool-cell combination, the operator creates a `PodDisruptionBudget` with:
- `maxUnavailable: 1` — At most one pod can be voluntarily evicted at a time.
- Label selector matching pool pods in that cell.

This protects against node drains or Kubernetes upgrades taking down too many replicas simultaneously. The PDB is owned by the Shard CR for garbage collection.

---

## 10. Status Aggregation

The `updatePoolsStatus` function directly lists pods by label selector and aggregates:

| Status Field | Source |
|---|---|
| `shard.Status.PoolsReady` | `true` when `readyPods == totalPods > 0` |
| `shard.Status.ReadyReplicas` | Count of pods with `PodReady=True` condition |
| `shard.Status.OrchReady` | MultiOrch deployment readiness |
| `shard.Status.Phase` | `Healthy` when both pools and orch are ready; `Progressing` otherwise |
| `shard.Status.Cells` | Observed cells from pod labels |
| `shard.Status.PodRoles` | Pod → role mapping (populated by data-handler from etcd) |
| `shard.Status.Conditions` | `Available` condition with `AllPodsReady` or `NotAllPodsReady` |

Terminating pods (with a non-zero `DeletionTimestamp`) are excluded from counts.

Metrics are emitted per pool via `monitoring.SetShardPoolReplicas()`. A `PoolEmpty` warning event is emitted when a cell has zero ready replicas but the desired count is > 0.

---

## 11. Feature Summary

### Implemented

| Feature | Details |
|---|---|
| **Direct pod creation and recreation** | Pods created per-index, auto-recreated on failure |
| **Sequential provisioning** | Pods created one-at-a-time, waiting for readiness |
| **Per-pod data PVCs** | Explicitly created, named by index |
| **Shared backup PVC** | Per-shard-per-cell, skipped for S3 |
| **Headless Service for DNS** | Pod hostname + subdomain for FQDN resolution |
| **PodDisruptionBudgets** | `maxUnavailable: 1` per pool per cell |
| **Drain state machine** | Annotation-based, coordinated across resource/data handlers |
| **Pod selection for scale-down** | Primary avoidance, prefer non-ready, highest index |
| **Rolling updates** | Spec-hash drift detection, replicas first, primary last |
| **Finalizer-based cleanup** | Pod finalizer prevents premature GC |
| **Shard deletion handling** | Strips all pod finalizers, deletes deployments, waits for termination |
| **Status aggregation from pods** | Direct pod count, no StatefulSet intermediary |
| **Zone/region scheduling** | `nodeSelector` injection from `CellTopologyLabels` |
| **Cell topology propagation** | Labels carry cluster/db/tg/shard/pool/cell hierarchy |
| **pgBackRest TLS certificates** | Auto-generated or user-provided (cert-manager compatible) |
| **S3 and filesystem backup support** | Full backup configuration propagation |
| **PVC deletion policy** | Hierarchical merge, Retain/Delete per whenDeleted/whenScaled |
| **Observability** | Events, conditions, metrics, tracing spans |

### Not Yet Implemented (Blocked on Upstream Multigres)

These features are designed and documented in `pod-management-design.md` but require upstream multigres changes:

| Feature | Gap | Notes |
|---|---|---|
| **WAL archiving failure detection** | Gap 1 | Needs multipooler to expose `pg_stat_archiver` via etcd |
| **Standby waiting-for-backup surfacing** | Gap 4 | Needs multipooler to expose `monitor_reason` via etcd |
| **Graceful decommission RPC** | Gap 8 | Nice-to-have; drain state machine covers safety |
| **Point-in-Time Recovery** | Gap 9 | Separate product feature, not urgent |

### Not Implemented (Design Decision)

| Feature | Reason |
|---|---|
| **Scheduled base backups** | Per design review meeting, kept peripheral to operator |
| **Etcd topology cleanup** | API exists (`UnregisterMultiPooler`), wired through data-handler drain flow |
| **Backup health reporting** | `GetBackups` RPC exists; data-handler integration is scoped for future work |
| **DRAINED pod replacement** | Detection and replacement flow designed; requires data-handler etcd reads at runtime |

---

## 12. Code Organization

All pod management code lives in `pkg/resource-handler/controller/shard/`:

| File | Purpose |
|---|---|
| `shard_controller.go` | Main reconciler. Orchestrates pool reconciliation, handles deletion, sets up watches. |
| `reconcile_pool_pods.go` | Core pod lifecycle: `reconcilePoolPods`, `createMissingResources`, `handleScaleDown`, `handleRollingUpdates`, `selectPodToDrain`, `cleanupDrainedPod`, `podNeedsUpdate`. |
| `pool_pod.go` | Pod builder: `BuildPoolPod`, `BuildPoolPodName`, `ComputeSpecHash`. |
| `pool_pvc.go` | PVC builders: `BuildPoolDataPVC`, `BuildPoolDataPVCName`, `BuildSharedBackupPVC`. |
| `pool_pdb.go` | PDB builder: `BuildPoolPodDisruptionBudget`. |
| `pool_service.go` | Headless service builder for DNS resolution. |
| `drain_helpers.go` | Drain utilities: `resolvePodRole` (reads `PodRoles` from shard status), `initiateDrain` (sets drain annotation). |
| `labels.go` | Label builder: `buildPoolLabelsWithCell` — creates the standard label set for pool resources. |
| `status.go` | Status aggregation: `updateStatus`, `updatePoolsStatus` (counts pods directly), `updateMultiOrchStatus`, `setConditions`. |
| `containers.go` | Container builders for pgctld and multipooler (reused from the StatefulSet era, unchanged). |
| `reconcile_shared_infra.go` | Shared infrastructure: pgBackRest TLS certs, pg_hba ConfigMap, postgres password Secret, shared backup PVC. |
| `multiorch.go` | MultiOrch deployment and service builders. |
| `doc.go` | Package documentation. |

### Test Files

| File | Coverage |
|---|---|
| `pool_pod_test.go` | Pod builder, spec-hash computation, naming |
| `pool_pvc_test.go` | PVC builder, naming, storage class handling |
| `pool_service_test.go` | Headless service labels and selector |
| `shard_controller_test.go` | Unit tests for reconcile behavior (envtest-based) |
| `shard_controller_internal_test.go` | Internal function tests (podNeedsUpdate, etc.) |
| `integration_test.go` | Full integration tests: scale up/down, PDB creation, ConfigMap, services |
| `containers_test.go` | Container builder tests |
| `configmap_test.go` | pg_hba ConfigMap generation |
| `multiorch_test.go` | MultiOrch deployment/service builders |
| `secret_test.go` | Postgres password secret |
| `ports_test.go` | Port constant tests |

### Data-Handler Side

The drain annotations are consumed by the data-handler shard controller (`pkg/data-handler/controller/shard/`), which:
- Watches pods with drain annotations.
- Reads etcd topology to determine pod roles.
- Calls `UpdateSynchronousStandbyList(REMOVE)` on the primary.
- Calls `UnregisterMultiPooler` to delete the etcd entry.
- Sets the pod annotation to `ready-for-deletion`.

---

## 13. Naming Conventions

### Pod Names

```
{cluster}-{db}-{tg}-{shard}-pool-{pool}-{cell}-{hash}-{index}
```

Generated by `BuildPoolPodName` using `PodConstraints` (60 chars max for the base name, then `-{index}` suffix appended). The hash is an 8-character FNV-1a hex suffix ensuring uniqueness even after truncation.

### Data PVC Names

```
data-{cluster}-{db}-{tg}-{shard}-pool-{pool}-{cell}-{hash}-{index}
```

Same structure as pod names with a `data-` prefix.

### Shared Backup PVC Names

```
backup-data-{cluster}-{db}-{tg}-{shard}-{cell}-{hash}
```

### PDB Names

```
{cluster}-{db}-{tg}-{shard}-pool-{pool}-{cell}-{hash}-pdb
```

### Headless Service Names

```
{cluster}-{db}-{tg}-{shard}-pool-{pool}-{cell}-{hash}-headless
```

Pods reference this via `spec.subdomain`, combined with `spec.hostname` (set to the pod name) to produce FQDNs like `pod-name.headless-svc.namespace.svc.cluster.local`.

---

## 14. Interaction with Multigres

### Etcd Topology

The operator reads from etcd topology (via the data-handler) to:
- Determine pod roles (`PRIMARY`, `REPLICA`, `DRAINED`) for scale-down and rolling-update decisions.
- Clean up stale topology entries on permanent pod removal (`UnregisterMultiPooler`).

The operator writes to etcd topology (via the data-handler) to:
- Register databases, shards, and cells during initial setup.
- Unregister poolers during drain cleanup.

### gRPC Operations

Through the data-handler:
- `UpdateSynchronousStandbyList(REMOVE)` — Removes a standby before pod deletion.
- `UnregisterMultiPooler` — Deletes the etcd entry for a permanently removed pod.

### What Multigres Handles Autonomously

| Operation | Component | Notes |
|---|---|---|
| Primary election (bootstrap) | multiorch | `BootstrapShardAction` via consensus |
| Failover | multiorch | `AppointLeaderAction` on primary failure |
| Replication setup | multiorch | Detects new poolers, configures streaming replication |
| Sync standby list (ADD) | multiorch | `FixReplicationAction` |
| WAL archiving | PostgreSQL | `archive_command` → pgBackRest |
| Backup execution | multiadmin | Selects a replica, calls `Backup` RPC |
| Replica restore | multipooler | `MonitorPostgres` detects empty PGDATA, restores from backup |

### What the Operator Handles

| Operation | How |
|---|---|
| Pod creation and recreation | Direct pod management via reconcile loop |
| Scale-up (new replicas) | Create PVC + Pod, multigres handles the rest |
| Scale-down | Drain state machine with standby removal |
| Rolling update | Spec-hash detection, ordered recreation |
| PVC lifecycle | Direct creation/deletion per policy |
| Certificate provisioning | `pkg/cert` for pgBackRest TLS |
| Status reporting | Aggregate from pod conditions and etcd roles |

---

## 15. Known Gaps and Future Work

### Upstream Dependencies (GitHub Issues Filed)

| Gap | Issue | Description |
|---|---|---|
| WAL archiving failure detection | [#654](https://github.com/multigres/multigres/issues/654) | Multipooler should report `pg_stat_archiver` status via etcd |
| Standby stuck waiting for backup | [#652](https://github.com/multigres/multigres/issues/652) | Multipooler should expose `monitor_reason` in etcd |
| Graceful decommission RPC | [#653](https://github.com/multigres/multigres/issues/653) | Nice-to-have; drain state machine covers safety |

### Operator-Side Future Work

- **Backup health reporting**: The `GetBackups` gRPC RPC exists and can be called to populate `ShardStatus` with backup metadata. Designed but not yet integrated.
- **Scheduled base backups**: Designed as a controller-timer approach (like CloudNativePG) but deferred per team decision.
- **DRAINED pod replacement**: The design calls for detecting DRAINED pods via etcd and creating replacements while cleaning up the drained pod. The scale-up logic already handles the "create replacement" part; the detection and cleanup need the data-handler to surface DRAINED status at runtime.
- **Primary switchover for rolling updates**: Currently, rolling update of the primary defers to the same drain mechanism. A dedicated switchover flow (request switchover → wait for data-handler confirmation → then drain) would be more graceful and is mostly plumbed.

### Design Constraints (v1alpha1)

These constraints are enforced via CEL validation and prevent unsupported operations:

| Constraint | Enforcement |
|---|---|
| Single database (`postgres`) | CEL on databases array |
| Single shard (`0-inf`) | CEL on shard name |
| Pools are append-only | CEL prevents pool removal or rename |
| Cells are append-only | CEL prevents cell removal (on MultigresCluster and Shard) |
| Zone/region immutability | CEL on Cell spec fields |

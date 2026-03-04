# Changelog

All notable changes to the Multigres Operator are documented in this file.

---

## [v0.4.0] — 2026-03-03

**Previous release:** v0.3.2 (2026-03-01)

This release delivers the **data-handler refactor** — a major architectural change that merges the separate data-handler controllers into the resource-handler controllers, eliminating cross-controller race conditions, removing all finalizers, and centralizing topology management. It also adds PVC volume expansion support, pod tolerations, and fixes 14+ bugs across drain, topology, deletion, and backup health paths.

**97 files changed, ~7,800 insertions, ~9,800 deletions across 5 PRs (#328, #330–#334).**

### Breaking Changes

- **Finalizers removed from all controllers.** MultigresCluster, Shard, and TopoServer controllers no longer use finalizers. Resource cleanup now relies on Kubernetes garbage collection via owner references. Existing resources with stale finalizers will have them removed on the next reconcile.
- **Data-handler controllers deleted.** The separate cell and shard data-handler controllers (`pkg/data-handler/controller/cell/`, `pkg/data-handler/controller/shard/`) have been removed. Their responsibilities are now handled by the cluster-handler and resource-handler controllers respectively.
- **TopologyPruningConfig added to MultigresClusterSpec.** Topology pruning (cells, databases, poolers) is now managed by the MultigresCluster controller with a new `TopologyPruningConfig` field (enabled by default).

### Features

- Operator now supports in-place PVC resizing: update the storage size in the spec and the operator patches data PVCs when `desired > current`.
- Automatic pod drain triggered when pending filesystem resize is detected (`FileSystemResizePending` condition).
- Webhook rejects storage shrink attempts with a validation error.
- Warning event emitted on Shard when PVC expansion fails (e.g., StorageClass doesn't support expansion).
- New `Tolerations` field on `PoolSpec`, wired into `BuildPoolPod`, `ComputeSpecHash`, and `mergePoolSpec`.
- New `AnnotationPendingDeletion` and `ConditionReadyForDeletion` constants for graceful shard deletion.
- TableGroup controller now uses a 3-step deletion: set PendingDeletion annotation → wait for ReadyForDeletion condition → delete Shard CR.
- Tracking labels (`uses-core-template`, `uses-cell-template`, `uses-shard-template`) applied via `MergeFrom` patch. Webhook `TemplateValidator.ValidateDelete` now uses indexed label lookups.

### Bug Fixes

- **Drain fallthrough on topo unavailability:** The drain state machine fell through with `myPooler=nil` when topo was unavailable. Now requeues instead.
- **Drain error swallowed:** `reconcileDrainState` was silently discarding errors. Errors are now returned.
- **Primary drain check blocked replicas:** `IsPrimaryDraining` returned true for `DrainStateReadyForDeletion`. Now excludes completed drains.
- **Drain timestamp parsing:** Malformed annotations now log a warning and fall back to `time.Now()`.
- **Pooler pruning FQDN mismatch:** `PrunePoolers` compared FQDN hostnames against short pod names. Now uses FQDN-aware helper.
- **Pooler status key garbled:** `GetPoolerStatus` hostname fallback now prefers `Id.Name`.
- **Selector cross-shard interference:** Four functions used only cluster+shard labels. Now include database and tablegroup labels.
- **Orphan deletion races:** `IsNotFound` guards added to prevent duplicate deletion events.
- **Stuck terminating pods:** Pods stuck >60s during graceful deletion are now treated as gone.
- **PVC deletion policy enforcement:** PVCs with Delete policy now get controller ownerRefs for GC cascade-deletion.
- **Nil panic:** `EvaluateBackupHealth` could panic on nil gRPC response. Nil guard added.
- **Backup event spam:** `BackupStale` event now gated on healthy-to-unhealthy transition only.
- **S3 backup fields:** `MinLength=1` validation added to `Bucket` and `Region`.
- **Multipooler hostname:** `--hostname=$(POD_NAME)` now passed to multipooler container.

### Refactoring

- Topo, drain, and backup health logic extracted into standalone packages.
- Shard controller split into `reconcile_deletion.go` and `reconcile_data_plane.go`.
- Resolver split: validation functions extracted into `validation.go`.

---

## [v0.3.2] — 2026-03-01

Safety improvements for shard scale-down operations, more robust topology cleanup during deletion, and significant maintenance updates.

### Features

- Health gate for scale-down: blocks drain operations if remaining pool replicas are unhealthy. Includes `ScaleDownBlocked` warning event.

### Bug Fixes

- Smart topology cleanup during deletion: 2-minute retry for transient topology server unavailability. `TopologyRegistered` / `DatabaseRegistered` conditions for accurate tracking.
- Track drain timeout from drain request start via `drain.multigres.com/requested-at` annotation instead of `DeletionTimestamp`.
- Prevent status patch errors from being swallowed in Shard and Cell controllers.

### Refactoring

- Extract shared `SetCondition`/`IsConditionTrue` helpers into `pkg/util/status/conditions.go`.
- Add missing `doc.go` files to several packages.
- Increase code coverage across all modules.

---

## [v0.3.1] — 2026-02-27

### Features

- IRSA (IAM Roles for Service Accounts) support for S3 authentication.
- Enforce minimum `replicasPerCell` and add quorum loss warnings.

### Bug Fixes

- Resolve status ownership conflicts during reconciliation.
- Ensure backup PVCs are reconciled for all pool-active cells.
- Fix deadlock during scale-down when extra pods are externally deleted.
- Skip drain state machine reconciliation for pods marked for deletion.
- Resolve multiple topo-ordering and reconciliation issues found during bug audit.
- Fix infinite loop during PRIMARY pod drain operations.

### Chores

- Pin multigres container images to specific SHA tags for production stability.

---

## [v0.3.0] — 2026-02-26

### Highlights

- Direct pod management replaces StatefulSet-based pool management.
- Full E2E testing framework with Kind clusters and psql connectivity verification.
- Container images pinned to SHA tags for reproducible builds.

### Features

- Replace StatefulSet pool management with direct Pods: pod, PVC, and PDB builders; sequential pooler pod management; rolling updates and drained pod replacement.
- Implement drain state machine for scale-down.
- Add backup health reporting.
- Add cell append-only validation rules.
- Add E2E testing framework with Kind clusters and psql connectivity verification.

### Bug Fixes

- Prevent PVC deletion during rolling updates.
- Reorder PVC deletion and finalizer removal.
- Implement manual PVC GC to respect retention policies.
- Prevent nil pointer panic in drain state machine.
- Include `ValueFrom` and `EnvFrom` in spec-hash calculation.
- Resolve cross-shard state bleed and status memory leak.
- Correct Shard status to use desired replica counts.
- Fix drain state matching and topology store residency.
- Resolve deletion deadlock and PDB scale subresource warning.

### Refactoring

- Split monolithic shard controller into focused files.
- Remove legacy StatefulSet management and cleanup RBAC.
- Consolidate image defaults to central source of truth.

---

## [v0.2.6] — 2026-02-24

### Features

- Strict node scheduling for cell failure domains: injects `nodeSelector` constraints on MultiGateway Deployments, Pool StatefulSets, and MultiOrch Deployments based on cell zone/region topology. Webhook emits warnings when no matching nodes exist.

### Bug Fixes

- Guard against infinite recursion in PKI: `ensureCA` and `ensureServerCert` now have max recursion depth of 3 with clear error messages.

### Documentation

- Add pod management design document covering resource topology changes, pod identity management, drain state machine, rolling updates, and failure recovery.

---

## [v0.2.5] — 2026-02-23

### Features

- Filter template-triggered reconciliations by resolved refs so updating one template no longer enqueues every cluster in the namespace.
- Add `ResolvedTemplates` status field tracking which templates each cluster uses.
- Gate `localTopoServer` field behind CEL validation with "not yet supported" message.

### Bug Fixes

- Add `app.kubernetes.io/managed-by` label to CA and TLS secrets generated by the CertRotator, fixing informer cache misses leading to infinite Create/AlreadyExists recursion.
- Add `watch` verb to StorageClass RBAC role.

### Refactoring

- Remove dead `Enable`, `CertStrategy`, and `CertDir` fields from webhook options.

---

## [v0.2.4] — 2026-02-21

### Bug Fixes

- Aggregate pool metrics across cells instead of reporting only the last cell's count.
- Merge `StorageSpec` field-by-field in `mergePoolSpec` and `mergeEtcdSpec` to preserve unrelated base fields on partial overrides.
- Generate unique ConfigMap and Secret names per Shard to prevent cross-shard ownership conflicts.
- Remove dead pgctld `--backup-*` CLI flags that caused CrashLoopBackOff on all pool pods.

### Documentation

- Expand secret access patterns guide with decision table and three documented options.
- Document silent HA loss with RWO filesystem backups and `WaitForFirstConsumer` node pinning.

---

## [v0.2.3] — 2026-02-20

### Bug Fixes

- Listen on dual-stack for multiadmin-web: set `HOSTNAME=::` so Next.js listens on both IPv4 and IPv6, fixing liveness probe failures on dual-stack EKS clusters.

---

## [v0.2.2] — 2026-02-20

### Bug Fixes

- Survive SSA cert wipe on upgrade: `PatchWebhookCABundle` now uses Server-Side Apply with a dedicated field owner (`multigres-operator-cert`). `multigres.com/cert-strategy` annotation set at runtime for reliable self-signed mode detection.
- Handle `AlreadyExists` race in secret creation when multiple reconcilers create certs concurrently.

---

## [v0.2.1] — 2026-02-20

### Features

- StorageClass validation in the admission webhook: rejects `MultigresCluster` creation when no default StorageClass exists and no explicit `storage.class` is set.

### Bug Fixes

- Skip topo cleanup on deletion when cell/shard controllers cannot reach the topo server, preventing finalizer deadlocks.

---

## [v0.2.0] — 2026-02-20

### Highlights

- Backup & Restore with filesystem and S3 backend support.
- Generic TLS certificate lifecycle module (`pkg/cert`).
- pgBackRest TLS with auto-generated and user-provided certificate modes.

### Features

- Backup configuration API with filesystem and S3 backend support.
- S3 backup support with configurable endpoint, bucket, region, key prefix, and env-based credentials.
- pgBackRest TLS certificate handling with dual-mode support (auto-generated and user-provided/cert-manager compatible).
- Projected volume key renaming for upstream compatibility.
- Backup configuration resolution with filesystem/S3 backend validation.
- Per-shard backup PVC name generation.

### Refactoring

- Migrate webhook certificate handling from `pkg/webhook/cert` to generic `pkg/cert` module.
- Wire backup configuration into pgctld and multipooler container args.
- Pass S3 environment variables to data plane containers.
- Use APIReader for uncached Secret lookups.

---

## [v0.1.0] — 2026-02-17

First release.

# Changelog

All notable changes to the Multigres Operator are documented in this file.

---

## [v0.5.3] — 2026-03-13

**Previous release:** v0.5.2 (2026-03-12)

Accelerates cluster bootstrap with parallel pod and PVC creation, hardens admission validation for pool names and resource limits, restructures the E2E test suite for parallel execution, and pins multigres images to upstream fixes for startup deadlocks and connection recycling. The observer gains a spec-compliance check for drift detection between CRD intent and running pod state.

**13 commits, 63 files changed, ~2,910 insertions.**

### Bug Fixes

- **Parallel pod and PVC creation:** Sequential pod creation required one reconcile loop per pod, making bootstrap time proportional to replica count. Removed `actionTaken` and unready-blocks-creation gates in `createMissingResources` so all missing pods and PVCs are created in a single pass. Deletion, drain, filesystem-resize, and rolling-update operations remain strictly sequential and health-gated. Reduces N-replica bootstrap from N reconcile loops to 1.
- **Pool name map key validation:** Kubernetes CRD structural schema does not enforce kubebuilder validation markers on map keys, allowing invalid pool names like `INVALID_NAME!` through the API server. These invalid names then caused PVC creation failures. Added `ValidatePoolName()` in `pkg/resolver/validation.go` with DNS label regex and 25-char length check, called after `ResolveShard` in the webhook validator. Extended `ShardTemplate` webhook to validate pool names on create, update, and delete.
- **Resource limit validation:** The webhook accepted resource specs where limits < requests, causing pods to fail at creation and clusters to get stuck in Progressing. Added `validateResourceRequirements` helper checking limits ≥ requests for all component resource specs (etcd, multiadmin, multiadmin-web, multigateway, multiorch, pool postgres, pool multipooler).

### Dependencies

- Upgrade multigres image `sha-f02e476` → `sha-bb174f9` and pgctld image `sha-f02e476` → `sha-9b51b0f`. Upstream fixes: HTTP server startup deadlock (server now starts before run hooks), connection recycle bugfix (`RESET ROLE`/`SESSION AUTHORIZATION` before `RESET ALL`), and `POSTGRES_DB` env var support.

### Refactoring

- **Stale TODO cleanup:** Removed resolved or obsolete TODO comments from RBAC markers in cell/shard/tablegroup/toposerver types, upstream dependency note in `main.go`, empty `main_test.go` placeholder, duplicate-cell TODOs resolved by webhook validation, and stale comments in `resource_watcher_match.go`. Consolidated multipooler/multiorch container TODOs to a single `--log-level` item each. Added explanatory comment on `IsPrimaryNotReady` optimistic default in `drain.go`.

### Testing

- **E2E test suite restructure:** Split monolithic E2E tests into dedicated and shared test directories (`test/e2e/dedicated/`, `test/e2e/shared/`) with per-scenario `main_test.go` files for parallel execution. Extracted shared helpers into `test/e2e/framework/` package (cluster management, helpers, loader). Updated CI workflows and Makefile targets for the new structure.
- **CI cache fix:** Fixed invalid cache name in the reusable test-coverage workflow causing cache misses.

### Observer

- **Spec-compliance check:** New `spec_compliance.go` with 4 sub-checks — pod resources, tolerations, images, and PVC sizes — compared against fully resolved Shard specs. Detects drift between CRD intent and actual pod state after template merging.
- **Multi-cell primary count fix:** New `countPrimariesForPool` helper correctly counts primaries across all cells when `len(poolSpec.Cells) > 1`, eliminating false positive findings for multi-cell clusters.
- **External etcd endpoint discovery:** `findEtcdAddress` now checks `GlobalTopoServer.External.Endpoints` before managed etcd lookup. `probeTopoServerServices` probes external etcd endpoints.
- **New exerciser fixtures:** Added `multiadmin-web`, `tolerations-affinity`, `multiadmin-lifecycle`, and `namespace-default-template` fixtures with 9 new verification scenarios (resource-limits-violated, add-tolerations, add-affinity, drain-primary-scale-down, multiadmin-scale, expand-pvc-storage, multiadminweb, pdb, durability-policy).

---

## [v0.5.2] — 2026-03-12

**Previous release:** v0.5.1 (2026-03-11)

Hardens data-plane reliability with startup probes on all remaining containers to prevent premature liveness kills during initialization, adds RFC 1123 DNS label validation on all user-provided name fields, and fixes cell gateway service name population and stale podRoles after primary election. The observer gains history tracking, on-demand check endpoints, 14 new health check categories, and a comprehensive exerciser skill for cluster smoke-testing.

**13 commits, 63 files changed, ~5,249 insertions.**

### Bug Fixes

- **Startup probes for remaining containers:** Without startup probes, pgctld's liveness probe could kill the container during slow initdb or WAL replay, and multiadmin/multiadmin-web used `InitialDelaySeconds` as a workaround. Added startup probes to pgctld (`/live` :15400), multiadmin (`/ready` :18000), and multiadmin-web (`/` :18100) with a 150s startup window (period=5s, failureThreshold=30). Removed now-redundant `InitialDelaySeconds` from multiadmin and multiadmin-web probes.
- **DNS name validation:** User-provided name fields accepted any characters (e.g., `INVALID_NAME`), which could produce invalid Kubernetes resource names downstream. Added RFC 1123 DNS label pattern validation to `DatabaseName`, `TableGroupName`, `ShardName`, `PoolName`, and `CellName` CRD types via CEL pattern constraints.
- **Cell gateway service name:** Cell status never populated `GatewayServiceName`, leaving clients with an empty string for gateway service discovery. Now set via `BuildMultiGatewayServiceName()` in the cell status reconciler.
- **Stale podRoles requeue:** The shard controller's reconciliation burst completed before multiorch elected a primary, leaving `podRoles` with 0 primaries indefinitely. Added a `hasPrimary` check with 10s requeue in `reconcileDataPlane` when the shard is Healthy but topology has no primary.

### Dependencies

- Upgrade multigres container images from `sha-8278fad` to `sha-f02e476`. Upstream changes: pprof endpoints enabled by default (no-op for operator), PrimaryObservation wired into multipooler health stream for faster gateway primary discovery.

### Testing

- **Startup probe tests:** Updated unit and integration test expected specs for startup probes on pgctld, multiadmin, and multiadmin-web containers.
- **Samples bump:** All sample manifests updated to `replicasPerCell: 4` minimum.

### Observer

- **History and on-demand check endpoints:** New `/api/history` endpoint with ring buffer tracking findings across cycles, classifying them as persistent, transient, or flapping. New `/api/check` endpoint for immediate targeted checks via channel-based dispatch. Added `--history-capacity` flag (default 30 cycles).
- **Coverage gap checks:** 14 new health check categories — PVC validation (missing/unbound/orphaned), service endpoint validation, backup staleness thresholds, status message checks on Degraded/Unknown phases, cell status field cross-checks, stuck-Progressing and invalid phase transition detection, and operator metrics HTTPS probe on port 8443.
- **Stale podRoles detection:** New `detectStalePodRoles` replication check runs `pg_is_in_recovery()` SQL probes when CRD shows 0 primaries and flags the mismatch.
- **Log pattern refinements:** Narrowed panic/runtime error patterns to avoid false positives during rolling restarts. Added filter for benign Go runtime shutdown panics. Fixed metrics probe to treat HTTP 401/403 as reachable. Fixed PVC orphan false positive during pod grace period.
- **Exercise cluster skill:** New comprehensive exerciser skill with 10 fixtures, 5 mutation patches, 11 scenarios, stability verification protocol, and operator-knowledge reference. Includes topology-aware kind cluster config with zone-labeled workers.
- **Documentation:** Rewrote observer README with skills as first-class citizens and example prompts section. Updated architecture, configuration, and diagnostic skill docs. Added `docs/gitops-and-webhook-defaults.md` documenting webhook default materialisation, intentionally dynamic fields, and GitOps compatibility.

---

## [v0.5.1] — 2026-03-11

**Previous release:** v0.5.0 (2026-03-10)

Hardens cluster reliability with etcd health probes, etcd replica immutability validation, full TemplateDefaults override-chain propagation, and OTEL sampling config support across all data-plane components. Also fixes observer false positives from probe-induced log noise and bumps multigres images to sha-8278fad.

**9 commits, 28 files changed, ~649 insertions.**

### Bug Fixes

- **Etcd health probes:** The etcd StatefulSet had no health probes, so Kubernetes could not detect or recover from unhealthy members. Added startup probe on `/readyz` (30 failure threshold for bootstrap/WAL replay), liveness probe on `/livez`, and readiness probe on `/readyz` — all via HTTP GET on the etcd client port (2379).
- **TemplateDefaults propagation:** The 4-level override chain (Component > Cluster Default Template > Namespace Default > Hardcoded) was broken for CoreTemplate and CellTemplate. `ResolveGlobalTopo`, `ResolveMultiAdmin`, and `ResolveMultiAdminWeb` now fall back to `TemplateDefaults.CoreTemplate`. CellTemplate default is propagated before `ResolveCell`. The webhook validator now resolves `TemplateDefaults.ShardTemplate` for accurate orphan pool checks.
- **Etcd replica immutability:** CEL rule rejects even replica counts. Webhook warns on `replicas=1` (single-node etcd is not HA) and rejects replica changes on Update to prevent cluster ID mismatch from static bootstrap re-initialization.
- **OTEL sampling config propagation:** `SamplingConfigRef` was accepted by the API but never wired through. Now sets `OTEL_TRACES_SAMPLER_CONFIG` env var and mounts the ConfigMap volume into pool pods, multigateway, multiadmin, and multiorch containers. Fixes `CrashLoopBackOff` when using `multigres_custom` sampler.
- **Multiorch sampling volume:** The multiorch container had an `otel-sampling-config` VolumeMount but the corresponding Volume was never added to the Deployment pod spec. Volume is now conditionally attached when `SamplingConfigRef` is configured.
- **Observer probe noise:** TCP health probes to the gateway PG port caused EOF log entries at ERROR level, which the observer then reported as findings. New `isProbeNoise()` filter suppresses these false positives. Grace period bumped from 60s to 2min with multiorch and gateway probes skipped during grace.
- **Observer JSON log parsing:** Log severity is now extracted from JSON `level` fields. Severity is elevated for `ShardNeedsBootstrap` and quorum errors.
- **Observer RBAC:** Added `watch` verb for nodes (controller-runtime cached client requires watch, not just list).

### Dependencies

- Upgrade multigres container images from `sha-d6c1048` to `sha-8278fad` (includes fix for admin conn action lock starvation).

### Testing

- **Etcd probe tests:** Unit tests for probe configuration and integration tests verifying probe fields on the generated StatefulSet.
- **Resolver tests:** Test cases for TemplateDefaults propagation across MultiAdmin, MultiAdminWeb, GlobalTopo, and the webhook validator.
- **Etcd validation tests:** Test cases for `getEffectiveEtcdReplicas` helper in resolver validation.

---

## [v0.5.0] — 2026-03-10

**Previous release:** v0.4.2 (2026-03-09)

Adds configurable durability policy, graceful deletion for orphan TableGroups and Cells, and consolidates the repository from 11 Go modules into one. Fixes native sidecar crash detection, multiorch cross-shard watch leakage, and health probe endpoints for multipooler and pgctld.

**17 commits, 90 files changed, ~1,551 insertions.**

### Features

- **Configurable durability policy:** New `DurabilityPolicy` field on `MultigresClusterSpec` (cluster-wide default) and `DatabaseConfig` (per-database override), propagated through TableGroup → Shard → topology registration. Webhook materializes `"ANY_2"` as default. Enables future cross-AZ replication quorum (`MULTI_CELL_ANY_2`) and user-defined policies.
- **Graceful orphan TableGroup/Cell deletion:** Orphan pruning now follows a 3-step `PendingDeletion` flow (annotate → wait for `ReadyForDeletion` → delete) instead of immediate deletion, routing through the drain state machine to prevent data loss during database or cell removal.
- **TopoServer cleanup on external mode switch:** Switching from embedded to external topo mode now deletes the orphaned TopoServer resource in `reconcile_global.go`.

### Bug Fixes

- **Native sidecar crash detection:** `IsCrashLooping` now checks `InitContainerStatuses` in addition to `ContainerStatuses`, correctly surfacing crash-looping native sidecars (e.g., multipooler) as `PhaseDegraded`.
- **Multiorch cross-shard watch leakage:** `--watch-targets` was hardcoded to the database level, causing each multiorch to discover multipoolers across all shards. Now builds a fully qualified target from database/tablegroup/shard fields.
- **Multipooler health probes:** Startup and readiness probes now use `/live` instead of `/ready`, matching upstream endpoint changes (multigres commit `309da86`).
- **pgctld health probes:** Liveness and readiness probes added using the `/live` endpoint on port 15400, with `--http-port=15400` flag wired into the container args.
- **Malformed backup ID ages:** `EvaluateBackups` now guards against zero-time from `ParseBackupTime`, preventing ~56-year ages in Prometheus metrics and misleading `BackupStale` conditions.
- **PVC ownerRef update conflicts:** Replaced `r.Update()` with `client.MergeFrom` patch for PVC ownerRef changes, reducing conflict errors during concurrent reconciliation.
- **Hardcoded label string:** Replaced hardcoded `"multigres.com/cluster"` string with `metadata.LabelMultigresCluster` constant.

### Dependencies

- Upgrade multigres container images from `sha-8bfe693` to `sha-d6c1048` (pgctld, multigres core, multiorch, multipooler, multigateway).
- Intermediate builder image updated in Dockerfile and CI workflow.

### Refactoring

- **Single Go module consolidation:** Merged 11 Go modules (root + 10 sub-modules under `api/` and `pkg/`) into a single `go.mod`, eliminating ~3,500 lines of duplicated dependency management. Removed `go.work` workspace, simplified Makefile and CI workflows. No external consumers of the sub-modules exist.
- **Webhook test fixture isolation:** Replaced shared test fixtures with `DeepCopy` to prevent cross-test data races.
- **Comment and label fixes:** Fixed copy-paste comment typo in `labels.go`, removed stale `hasInline` comments from `defaulter.go`, added CEL exclusion comment to `buildCellNodeSelector`.

### Testing

- **Test coverage increase:** New tests for `topology.go`, `store_test.go`, `conditions_test.go`, `phase_test.go`, `shard_controller_internal_test.go`, `multiorch_test.go`, `database_test.go`, and `builders_test.go`.

### Documentation

- New `docs/durability-policy.md` explaining configurable replication quorum.
- Updated API design doc with `DurabilityPolicy` field specification.
- Updated admission controller doc with durability policy defaulting.
- Updated `cell-topology.md` with durability policy propagation path.

---

## [v0.4.2] — 2026-03-09

**Previous release:** v0.4.1 (2026-03-05)

Introduces the Multigres Observer — a standalone cluster health monitoring tool — along with degraded phase detection across all controllers, drain state machine hardening, topology pruning propagation, and materialized webhook defaults.

**34 commits, 95 files changed, ~8,300 insertions.**

### Features

- **Multigres Observer tool:** New standalone tool (`tools/observer/`) for continuous cluster health monitoring with 10 check categories (pod health, resource validation, CRD status, drain state, connectivity, replication, log monitoring, events, topology validation, and operator logs). Includes Prometheus metrics, structured JSON logging, `/api/status` endpoint, startup grace period, Dockerfile, and Kustomize deploy manifests. New Makefile targets: `kind-build-observer`, `kind-load-observer`, `kind-deploy-observer`, `kind-undeploy-observer`.
- **Degraded phase detection:** All controllers (Cell, TopoServer, Shard) now detect crash-looping pods (`CrashLoopBackOff`, `OOMKilled`, `ImagePullBackOff`, or terminated with ≥3 restarts) and report `PhaseDegraded` instead of `PhaseProgressing`. MultigresCluster aggregates `PhaseDegraded` from child Cells, TableGroups, and TopoServers.
- **Periodic status re-reconciliation:** Cell and TopoServer controllers requeue after 30s when not `Healthy`, enabling detection of pod-level issues (e.g., CrashLoopBackOff) that don't trigger watch events.
- **Materialized cluster defaults:** Webhook defaulter now materializes `TopologyPruning`, `PVCDeletionPolicy`, `Backup`, etcd `RootPath`, and external topo `Implementation` into the persisted spec, solving the "invisible defaults" problem for these fields.
- **Stale drain cancellation:** Drains in `Requested` state are automatically cancelled when the desired state changes (e.g., scale-down reversed), preventing stuck `Progressing` phases.
- **Topology pruning propagation:** `TopologyPruning` config propagated from MultigresCluster → TableGroup → Shard via the builder chain and new `TopologyPruning` fields on `ShardSpec` and `TableGroupSpec`. CRD schemas updated accordingly.
- **TopoServer status aggregation:** MultigresCluster status now includes TopoServer phase in its health aggregation alongside Cells and TableGroups.

### Bug Fixes

- **Drain: check primary pod readiness before RPCs.** New `IsPrimaryNotReady` guard in `DrainStateDraining` and `DrainStateAcknowledged` prevents sending RPCs to a primary whose containers are not ready. Drains are delayed and requeued instead.
- **Drain: reload PostgreSQL config after sync standby removal.** `ReloadConfig: true` added to `UpdateSynchronousStandbyList` requests so PostgreSQL reloads `synchronous_standby_names` immediately.
- **PodRoles keying fix.** `GetPoolerStatus` now keys by actual Kubernetes pod name (via `PodMatchesPooler` FQDN-aware matching) instead of topology hostname, preventing stale entries from orphaned topology data.
- **Multipooler service ID shortening.** Shorten `--service-id` value to prevent `application_name` overflow at the 63-character `NAMEDATALEN` limit, which was causing synchronous replication matching failures.
- **pgctld `POSTGRES_PASSWORD` env var.** Switch from `PGPASSWORD` to `POSTGRES_PASSWORD` for the pgctld container.
- **Topology defaults fix.** `getGlobalTopoRef` properly branches on `External` vs `Etcd` instead of unconditionally defaulting `rootPath` and `implementation`.
- **Draining pods excluded from ready counts.** Pods with drain annotations are no longer counted as ready in shard status, preventing inflated readiness metrics.
- **Configurable topology pruning.** `reconcilePoolerPrune` now respects the `TopologyPruning.Enabled` flag and skips pruning when disabled.

### Dependencies

- Upgrade multigres container images from `sha-4f39f4a` to `sha-8bfe693` (pgctld, multigres core, multiadmin-web, multiorch, multipooler, multigateway).
- Internal Go module dependencies updated across all modules.

### Documentation

- New `docs/development/phase-lifecycle.md` explaining phase computation across all resources.
- Updated `pod-management-architecture.md` drain state machine and status aggregation sections.
- Fixed stale StatefulSet references in `operator-capability-levels.md` and observer docs.
- Updated `CONTRIBUTING.md` with observer Makefile targets and project structure.

---

## [v0.4.1] — 2026-03-05

**Previous release:** v0.4.0 (2026-03-03)

Maintenance release with container image upgrades, observability improvements, bug fixes, and a major documentation reorganization. No breaking changes.

**69 files changed across 10 PRs (#335–#344).**

### Features

- Upgrade multigres container images from `sha-b0a47e1` to `sha-4f39f4a` (pgctld, multigres core, multiorch, multipooler, multigateway).
- New Grafana dashboard (`grafana-dashboard-cluster.json`) with panels for backup age, drain operations, rolling update progress, and spec-hash drifted pods.
- Three new Prometheus alerting rules: `MultigresBackupStale` (backup older than 24h), `MultigresRollingUpdateStuck` (rolling update >30min), `MultigresDrainTimeout` (drain timeouts for 10min).
- Prometheus alert and saturation rules now cover all five controllers (multigrescluster, tablegroup, shard, cell, toposerver) instead of only multigrescluster and tablegroup.
- E2E test timeout increased from 20m to 30m with parallelism set to 2.

### Bug Fixes

- **Multipooler `--hostname` flag removed:** The `--hostname=$(POD_NAME)` flag added in v0.4.0 caused multipooler to register with a short name that collided with FQDN-based pooler topology entries. Removed to restore correct registration behavior.
- **Pod cache filter added:** Pods were not included in the split-brain cache filter, causing the operator to cache all pods in the cluster. Pods are now filtered by `app.kubernetes.io/managed-by` label like other high-volume resources.
- **Deprecated `result.Requeue` removed:** Replaced deprecated `result.Requeue` with `result.RequeueAfter > 0` in the shard controller data-plane reconciliation path, fixing a staticcheck SA1019 warning.

### Testing

- E2E tests adapted to pod-based pool management: replaced `waitForStatefulSetWithContainer` assertions with `waitForPodWithContainer` to match the v0.4.0 architecture change from StatefulSets to direct Pods.
- Removed zone constraints from E2E test cell configs to allow scheduling on Kind nodes without zone topology labels.

### Documentation

- **README slimmed down:** Moved detailed sections (cert-manager integration, observability, webhook demo, local development) to dedicated files under `demo/` and `docs/`.
- **New user-facing docs:** `docs/storage.md` (PVC lifecycle and volume expansion), `docs/configuration.md` (operator flags), `docs/observability.md` (monitoring setup), `docs/backup-restore.md` (backup guide), `docs/operator-capability-levels.md` (capability assessment with diagram).
- **New developer docs:** `docs/development/` now contains 12 focused documents covering caching strategy, cell topology, certificate management, controller patterns, naming strategy, observability internals, pod management architecture, PostgreSQL image strategy, PVC lifecycle, template propagation, backup architecture, and known behaviors.
- **New demo directories:** `demo/cert-manager/README.md`, `demo/observability/README.md`, `demo/webhook/demo.md` with full step-by-step guides.
- **New runbooks:** `docs/monitoring/runbooks/` with runbooks for `MultigresBackupStale`, `MultigresDrainTimeout`, and `MultigresRollingUpdateStuck` alerts.
- **CONTRIBUTING.md** created with local development setup, testing, and PR guidelines.
- Fixed 15 stale references across all documentation.
- Removed obsolete `docs/architecture.md`, `docs/implementation-guide.md`, `docs/interface-design.md`, and `plans/phase-1/implementation-notes.md`.
- Removed stale E2E config files (`pkg/testutil/e2e-config/`) that referenced the old StatefulSet-based architecture.

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

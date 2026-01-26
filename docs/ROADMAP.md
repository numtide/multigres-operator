# Gap Analysis and Technical Roadmap

## 1. Missing Implementation Items (Phase 1)

These items represent immediate gaps between the codebase and the design specifications. Addressing these is critical for a clean handover.

### 📦 Project Hygiene & Packaging

* **User-Facing Documentation:** While `docs/README.md` exists, it is currently a "Developer Guide" describing the project structure. We need a "User Guide" that documents how to install the operator (via Kustomize/Helm) and how to construct the `MultigresCluster` CRD for the first time.
* **RBAC Marker Cleanup:** The `shard_types.go` file currently contains RBAC markers with a `TODO` explicitly stating they should be moved to the controller implementation to follow Kubebuilder conventions. This should be cleaned up to ensure permissions are generated correctly during the build.
* **Packaging:** Define the distribution artifacts. The file list shows Kustomize configs, but we should confirm if a Helm Chart is required for the client, or if raw manifests/Kustomize is sufficient.
* **Versioning Strategy:** Establish a semantic versioning policy for the Operator and its CRDs.
* **Repository Migration:** Move the repository from `numtide` organization to the Multigres organization.

### 🛠️ API & Controller Refinements

* **Duplicate Cell Validation (Pools):** The `shard_controller.go` contains a specific `TODO(#91)` noting that `Pool.Cells` may contain duplicates and needs `+listType=set` validation added to the API. This is a potential bug vector if a user lists the same cell twice.
* **Standardize Server-Side Apply (SSA):** The `shard_controller.go` currently uses client-side `Create` and `Update` logic. To prevent conflicts with the Mutation Webhook (which applies defaults) and potential external editors (like the future DDL sync controller), we should standardize on Server-Side Apply (Patch). NOTE: Done for `cluster-handler` already, needs to be done in `resource-handler` too
* **Event Coverage:** The `shard_controller.go` logs errors via the logger, but there are no calls to a generic `EventRecorder`. Kubernetes Events (`Normal` and `Warning`) must be emitted for significant state changes (e.g., "PoolScaled", "ShardReady") to be visible to the user via `kubectl describe`.
* **End-to-End testing:**: Currently our integration tests use `envtest`. We should add full end-to-end testing using something like kind.

### 🔍 Quality & Observability

* **ValidatingAdmissionPolicy Manifests:** The design document `admission-controller-and-defaults.md` specifies that `ValidatingAdmissionPolicy` (Level 3 validation) should be implemented as separate YAML files in `config/policies/`. These files are currently missing from the file list. Although the logic exists in the fallback webhook, shipping the policies is part of the design specification. This isn't critical at this point and could be omitted. The reason why it wasn't implemented yet is because `ValidatingAdmissionPolicy` is a relatively new feature in kubernetes and doesn't have extensive backwards compatibility, but it's worth looking into in the near future. For now the webhook does this as a fallback.
* **Code Sweep:** Review the code and ensure not bugs and clean code everywhere.
* **Monitoring:** While `metrics_service.yaml` exists, explicit custom metrics (e.g., "shards_provisioned_total") are not yet instrumented in the controller code.
* **Remove Duplicated Logic:** We have some duplicate logic in the `resource handler` right now that we need to remove and centralize. For example the `metadata` package and some other defaults.

---

## 2. Technical Roadmap & Future Work

The following initiatives outline the strategy for the next phase, focusing on "Enterprise Operations" and "Production Hardening" to pitch for contract extension.

### 🧪 Initiative 1: Real-World Verification & Cloud Testing

**Objective:** Move beyond local `envtest` to proven production capability.

* **Cloud Validation:** Validate the Operator on managed Kubernetes services (EKS, GKE, AKS). Specific attention is needed for the `StorageClass` definitions in `CoreTemplate` and `ShardTemplate`, which are currently hardcoded to generic names like `standard-gp3` in examples.
* **Correctness Testing:** Verify data routing logic (Multigres Gateway) and write paths under load.
* **Stress Testing:** Simulate high-churn scenarios (pod deletion, node failure) to ensure the `shard_controller`'s reconciliation loop recovers correctly without manual intervention.

### 🔄 Initiative 2: Advanced Data Operations

**Objective:** Support complex lifecycle management for stateful data.

* **Coordinated "Rollout" Strategy (Smart Upgrades):** currently, we rely on standard Kubernetes `StatefulSets` for managing pods. In the future, we aim to transition to direct Pod management (or advanced controller logic) to enable **Gated Upgrades**. Standard StatefulSet rolling updates are often too aggressive for databases. Moving to Pod-level control will allow the Operator to "Schedule" an update but wait for a "Release" signal (manual or automated) and perform **Shard-by-Shard Rollouts** (canary upgrades) to verify health before cascading the update to the rest of the cluster.
* **Explicit "Drain" Protocol:** Implement a formal, annotation-based handshake (`Started` -> `Acknowledged` -> `Finished`) for maintenance. While the current plan uses `PreStop` hooks, these are often insufficient for complex actions like active reparenting or failover. A controller-mediated Drain Protocol ensures the Operator explicitly evacuates leadership from a node *before* giving Kubernetes permission to terminate the Pod.
* **Topology Garbage Collection:** Implement logic to explicitly remove shard and cell metadata from the `GlobalTopoServer` (Etcd) when their corresponding CRs are deleted. Relying solely on Kubernetes OwnerReferences handles the pods but leaves "ghost" entries in the topology, which can lead to zombie routing paths.
* **Shard Migration & Renaming:** Support moving data between shards and renaming shards. Currently, the API enforces a rigid `0-inf` naming scheme.
* **Flexible Database Support:** Relax the current v1alpha1 constraint in `multigrescluster_types.go` that explicitly restricts usage to a single `postgres` database.

### 🚀 Initiative 3: The "Postgres-Native" Workflow (DDL Sync)

**Objective:** Bridge the gap between Platform Engineering and DBAs.

* **Synchronization Controller:** As hinted in the `implementation-history` of the API design, the next big step is a controller that watches the System Catalog (default `postgres` DB).
* **Bi-Directional Sync:** When `CREATE DATABASE` is run via SQL, automatically adopt it into the `MultigresCluster` CR. This solves the "Zombie Resources" problem described in the design doc drawbacks.

### 🛡️ Initiative 4: Managed Backup & Recovery (PITR)

**Objective:** Provide enterprise-grade data protection.

* **Backup Schedule CRD:** Introduce a `BackupSchedule` CRD (wrapping Kubernetes `CronJobs`) to provide a standard, declarative interface for defining periodic backup policies.
* **Backup & Restore CRDs:** Introduce declarative APIs for on-demand backups and restores.
* **Integration:** Utilize Kubernetes `VolumeSnapshot` APIs. The `ShardTemplate` already includes a `BackupStorage` field in `PoolSpec`, but the logic to utilize this for WAL archiving (e.g., Wal-G sidecars) needs implementation.

### 🏢 Initiative 5: Scalability & Isolation (RBAC 2.0) - Potentially, far down the road

**Objective:** Support large-scale multi-tenant environments.

* **Claim Model (Multi-Tenancy):** Introduce `MultigresDatabaseClaim` CRDs. This was considered and rejected for Phase 1, but remains a valid requirement for large-scale multi-tenancy where developers need self-service in their own namespaces.
* **Status Aggregation:** Implement a scalable status reporting architecture. The current `MultigresClusterStatus` map will hit etcd object size limits if thousands of databases are created. We need an Aggregator pattern or Custom Metrics API.

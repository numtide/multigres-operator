---
title: MultigresCluster API with Child Resources based on Multigres Architecture Alternative
state: ready
---

# This is a WIP

## Summary

This proposal defines the `v1alpha1` API for the Multigres Operator. The design is centered on two user-editable Custom Resources (CRs):

1.  **`MultigresCluster`**: The root resource that defines the desired state (intent) of the entire cluster.
2.  **`ShardTemplate`**: A reusable, namespaced resource for defining common shard pool configurations (e.g., "standard-ha-shard", "analytics-shard").

All other resources (`TopoServer`, `Cell`, `TableGroup`, `Shard`) are implemented as read-only child CRs owned by the `MultigresCluster`. These child CRs reflect the *realized state* of the system and are managed by their own dedicated controllers. This enables a clean, hierarchical, and observable architecture.

## Motivation

Managing a distributed, sharded database system across multiple failure domains (cells) is inherently complex. A single, monolithic CRD for this task would be unmanageable, difficult to debug, and lead to a complex, monolithic controller.

This proposal advocates for a parent/child CRD model to address these challenges. The primary motivations for this design are:

  * **Separation of Concerns:** Splitting logic into child CRs (`Cell`, `TableGroup`, `Shard`) results in simple, specialized controllers that are easier to build, test, and maintain.
  * **Hierarchical Observability:** This model provides a clean, hierarchical tree for status aggregation. A failure in a specific `Shard` can be clearly observed on the `Shard` CR, its `TableGroup` parent, and finally aggregated up to the root `MultigresCluster` status.
  * **Efficient, Event-Driven Reconciliation:** This architecture enables cascading reconciliation. A change to the `MultigresCluster` only triggers its controller, which may then update a `Cell` child CR. This, in turn, triggers the `Cell` controller, and so on.
  * **Idempotency and Single Source of Intent:** By making the `MultigresCluster` the single editable source of truth, we prevent unstable conflicts. Any manual edits to read-only child CRs are automatically reverted by their controller, ensuring the cluster's *realized state* always converges with the parent CR's *desired state*.

## Goals

  * Provide a declarative, Kubernetes-native API for deploying a multi-cell, sharded Multigres cluster.
  * Separate user-facing *intent* (`MultigresCluster`) from operator-managed *realized state* (child CRs).
  * Enable configuration reuse for shard definitions via a `ShardTemplate` resource.
  * Establish a clear, hierarchical ownership model using `OwnerReferences` for clean garbage collection and status aggregation.
  * Define a clear API contract for managing global and cell-local topology servers (etcd), as well as global and cell-local components (`MultiOrch`, `MultiGateway`).
  * **V1 Simplification:** Simplify the database-level API to assume **one shard per database**, while retaining the underlying `TableGroup` and `Shard` child CRs to allow for easy expansion to multi-shard support in a future API version.

## Non-Goals

  * This document does not define the specific implementation details of each controller's reconciliation logic or admission webhooks.
  * This document does not cover Day 2 operations such as database backup, restore, monitoring, or alerting.
  * This document does not cover automated, in-place version upgrades.
  * This document does **not** support multi-shard or explicit `TableGroup` management in the `v1alpha1` user-facing API. This is deferred to a future version.

## Proposal: API Architecture and Resource Topology

  * The operator can create a managed global etcd topology server and/or a managed local topology server. The same `TopoServer` CRD is used for both. The global `TopoServer` will belong to the `MultigresCluster` CR directly, whereas the local `TopoServer` belongs to the `Cell` CR.
  * A user can choose to point the cluster to an external etcd topology server, in which case the `TopoServer` resource will not be provisioned.
  * If no local topology server is configured for a cell, it will use the global topology server by default.
  * **Component Placement:**
      * `MultiAdmin` and `MultiOrch` are global components, managed as direct children of `MultigresCluster`.
      * `MultiGateway` is a cell-local component, managed by its parent `Cell` CR.
      * `MultiPooler` and `Postgres` are shard-level components, managed by their parent `Shard` CR.
  * **V1 Sharding:** The user defines a "database." The operator implicitly creates one `TableGroup` and one `Shard` child CR for that database. This abstraction simplifies the v1 API while preserving the sharding-ready architecture.

<!-- end list -->

```ascii
[MultigresCluster] 🚀 (The root CR - user-editable)
      │
      ├── 🌍 [GlobalTopoServer] (Child CR if managed)
      │    │
      │    └── 🏛️ etcd Resources (if managed)
      │
      ├── 🤖 MultiAdmin Resources - Deployment, Services, Etc
      │
      ├── 🧠 MultiOrch Resources (Global) - Deployment, Etc
      │    
      │
      ├── 💠 [Cell] (Child CR)
      │    │
      │    ├── 🚪 MultiGateway Resources (Deployment, Service, etc.)
      │    │    
      │    │
      │    └── 📡 [LocalTopoServer] (Child CR if managed and not using global)
      │         │
      │         └── 🏛️ etcd Resources (if managed)
      │
      └── 🗃️ [TableGroup] (Child CR - *implicitly created*)
           │
           └── 📦 [Shard] (Child CR - *implicitly created*)
                │
                └── 🏊 MultiPooler and postgres resources (pods or statefulset)
                    


📋 [ShardTemplate] (Separate CR - user-editable, NOT a child)
   ├── Contains spec sections for:
   │   └── pools: [...]
   │
   └── Watched by MultigresCluster controller ONLY when referenced
       └── Resolved into child CRs (children are unaware of templates)
```

## Design Details: API Specification

This section provides the comprehensive definitions and examples for each Custom Resource.

### User Managed CR: MultigresCluster

  * This and the `ShardTemplate` are the only two editable entries for the end-user. All other child CRs will be owned by this top-level CR, and any manual changes to those child CRs will be reverted.
  * Images are defined globally to ensure version consistency.
  * `MultiOrch` is now a global component defined at this level.
  * `Cell` definitions now include `zone`/`region` and a `MultiGateway` config with `static` or `dynamic` options.
  * The `databases` spec is simplified. The `tablegroups` block is removed. Users now define shard configuration directly under the database using either an inline `pools` list or a `shardTemplateRef`.

<!-- end list -->

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: example-multigres-cluster
  namespace: example
spec:

  # ----------------------------------------------------------------
  # Global Images Cluster Configuration
  # ----------------------------------------------------------------
  images:
    imagePullPolicy: "IfNotPresent"
    imagePullSecrets:
      - name: "my-registry-secret"
    multigateway: "multigres/multigres:latest"
    multiorch: "multigres/multigres:latest"
    multipooler: "multigres/multigres:latest"
    multiadmin: "multigres/multigres:latest"
    postgres: "postgres:15.3"

  # ----------------------------------------------------------------
  # globalTopoServer Configuration
  # ----------------------------------------------------------------
  globalTopoServer:
    rootPath: "/multigres/global"
    # Templating removed for simplicity. Define inline or use external.
    managedSpec:
      image: quay.io/coreos/etcd:v3.5.17
      replicas: 3
      dataVolumeClaimTemplate:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: "10Gi"
    # external:
    #   address: "my-external-etcd-client.etcd.svc:2379"

  # ----------------------------------------------------------------
  # MultiAdmin Configuration (Global)
  # ----------------------------------------------------------------
  multiadmin:
    # Templating removed for simplicity. Define inline.
    replicas: 1
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"
      limits:
        cpu: "200m"
        memory: "256Mi"

  # ----------------------------------------------------------------
  # MultiOrch Configuration (Global)
  # ----------------------------------------------------------------
  multiorch:
    # Moved from Cell to global. Define inline.
    replicas: 1
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"
      limits:
        cpu: "200m"
        memory: "256Mi"

  # ----------------------------------------------------------------
  # Cells Configuration
  # ----------------------------------------------------------------
  cells:
    - name: "us-east-1a"
      spec:
        # Define topology. Set ONLY one of zone or region.
        # Controller maps this to node labels
        # (e.g., topology.kubernetes.io/zone)
        zone: "us-east-1a"
        # region: "us-east-1"
        
        # MultiGateway config is per-cell.
        # Choose one: static or dynamic.
        multiGateway:
          # --- Option 1: Static (Original Model) ---
          static:
            replicas: 2
            resources:
              requests:
                cpu: "500m"
                memory: "512Mi"
              limits:
                cpu: "1"
                memory: "1Gi"
          # --- Option 2: Dynamic (Client Model) ---
          # dynamic:
          #   replicasPerCell: 1
          #   resourceMultiplier: 1.0

        # Note: This cell uses the global topoServer by default.
        # topoServer: {}

    - name: "us-east-1b"
      spec:
        zone: "us-east-1b"

        multiGateway:
          dynamic:
            replicasPerCell: 1
            resourceMultiplier: 1.0
        
        topoServer: # This cell uses a managed local topo server
          rootPath: "/multigres/us-east-1b"
          managedSpec:
            image: quay.io/coreos/etcd:v3.5.17
            replicas: 1
            dataVolumeClaimTemplate:
              accessModes: ["ReadWriteOnce"]
              resources:
                requests:
                  storage: "5Gi"

  # ----------------------------------------------------------------
  # Database Configuration (V1 - One Shard per DB)
  # ----------------------------------------------------------------
  databases:
    - name: "production_db"
      spec:
        # Simplified spec. 'tablegroups' is removed.
        # Define the DB's single shard using ONE of the following options.
        # An admission webhook MUST reject if both are set.

        # --- OPTION 1: Use a ShardTemplate ---
        shardTemplateRef: "standard-ha-shard"

    - name: "analytics_db"
      spec:
        # --- OPTION 1 (with Overrides): Use a ShardTemplate + Overrides ---
        shardTemplateRef: "standard-ha-shard"
        
        # Overrides are deep-merged onto the template's pools
        # using (type, cell) as the unique key.
        overrides:
          # This item MATCHES (replica, us-east-1a) in the template
          # and will be deep-merged.
          - type: "replica"
            cell: "us-east-1a"
            dataVolumeClaimTemplate:
              resources:
                requests:
                  storage: "1000Gi" # Patches the storage size
            postgres:
              resources:
                requests:
                  cpu: "4" # Patches the CPU request

          # This item does NOT MATCH any pool in the template
          # and will be ADDED as a new pool.
          - type: "readOnly"
            cell: "us-east-1a"
            replicas: 1
            dataVolumeClaimTemplate:
              accessModes: ["ReadWriteOnce"]
              resources:
                requests:
                  storage: "500Gi"
            postgres:
              resources:
                requests:
                  cpu: "1"
            multipooler:
              resources:
                requests:
                  cpu: "100m"

    - name: "custom_db"
      spec:
        # --- OPTION 2: Inline Definition (mutually exclusive) ---
        # This defines the *entire* shard spec.
        pools:
          - type: "replica"
            cell: "us-east-1b"
            replicas: 2 
            dataVolumeClaimTemplate:
              accessModes: ["ReadWriteOnce"]
              resources:
                requests:
                  storage: "75Gi"
            postgres:
              resources:
                requests:
                  cpu: "1"
                  memory: "1Gi"
            multipooler:
              resources:
                requests:
                  cpu: "100m"
                  memory: "128Mi"

# --- Status ---
status:
  observedGeneration: 1
  globalTopoServer:
    etcd:
      available: "True"
  conditions:
    - type: Available
      status: "True"
    - type: Progressing
      status: "False"
  cells:
    us-east-1a:
      gatewayAvailable: "True"
      topoServerAvailable: "True" # Using global
    us-east-1b:
      gatewayAvailable: "True"
      topoServerAvailable: "True" # Using local managed
  databases:
    production_db:
      desiredInstances: 6 # 2 cells * 3 replicas/cell
      readyInstances: 6
      servingWrites: "True"
    analytics_db:
      desiredInstances: 7 # (3 from us-east-1a) + (3 from us-east-1b) + (1 readOnly)
      readyInstances: 7
      servingWrites: "True"
    custom_db:
      desiredInstances: 2
      readyInstances: 2
      servingWrites: "True"
  multiadmin:
    available: "True"
    serviceName: "example-multigres-cluster-multiadmin"
  multiorch:
    available: "True"
    serviceName: "example-multigres-cluster-multiorch"
```

### User Managed CR: ShardTemplate

  * This CR is renamed from `DeploymentTemplate`.
  * It is **not** a child of any other resource.
  * Its scope is **reduced** to *only* defining shard pools. Templating for other components (`MultiAdmin`, `MultiOrch`, etc.) is removed for v1 simplicity.
  * The `spec` now contains a `pools` key, which holds a list of pool definitions. This structure is identical to the inline `pools` block in the `MultigresCluster` CR.

<!-- end list -->

```yaml
# This defines a reusable template named "standard-ha-shard".
# It is renamed from DeploymentTemplate and now ONLY contains pool specs.
apiVersion: multigres.com/v1alpha1
kind: ShardTemplate
metadata:
  name: "standard-ha-shard"
  namespace: example
spec:
  # The spec now consists of a list of pools.
  # This structure is required for the (type, cell)
  # override logic to function.
  pools:
    # --- Pool 1: Replicas in us-east-1a ---
    - type: "replica"
      cell: "us-east-1a"
      replicas: 3
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 100
            podAffinityTerm:
              labelSelector:
                matchLabels:
                  app.kubernetes.io/component: shard-pool
              topologyKey: "kubernetes.io/hostname"
      dataVolumeClaimTemplate:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: "250Gi"
      postgres:
        resources:
          requests:
            cpu: "2"
            memory: "4Gi"
      multipooler:
        resources:
          requests:
            cpu: "500m"
            memory: "256Mi"

    # --- Pool 2: Replicas in us-east-1b ---
    - type: "replica"
      cell: "us-east-1b"
      replicas: 3
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 100
            podAffinityTerm:
              labelSelector:
                matchLabels:
                  app.kubernetes.io/component: shard-pool
              topologyKey: "kubernetes.io/hostname"
      dataVolumeClaimTemplate:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: "250Gi"
      postgres:
        resources:
          requests:
            cpu: "2"
            memory: "4Gi"
      multipooler:
        resources:
          requests:
            cpu: "500m"
            memory: "256Mi"

# status:
  # No status is currently defined for this resource.
  # The original discussion about status (e.g., listing consumers)
  # is still valid and will be considered for future implementation.
```

### Child CRs of MultigresCluster

These CRs are **read-only** and managed by their respective controllers.

#### Child CR of MultigresCluster OR Cell: TopoServer

  * **No change.** This CRD is still sound. It is owned by `MultigresCluster` (if global) or `Cell` (if local). It is not created if `external` config is used.

<!-- end list -->

```yaml
apiVersion: multigres.com/v1alpha1
kind: TopoServer
metadata:
  name: "example-multigres-cluster-global"
  namespace: example
  ownerReferences:
  - apiVersion: multigres.com/v1alpha1
    kind: MultigresCluster
    name: "example-multigres-cluster"
    uid: "a1b2c3d4-1234-5678-90ab-f0e1d2c3b4a5"
spec:
  rootPath: "/multigres/global"
  image: "quay.io/coreos/etcd:v3.5.17"
  replicas: 3
  # ... other resolved spec fields ...
status:
  conditions:
  - type: Available
    status: "True"
  replicas: 3
  readyReplicas: 3
  clientServiceName: "example-multigres-cluster-global-client"
```

#### Child CR of MultigresCluster: Cell

  * This CR is owned by the `MultigresCluster`.
  * It no longer manages `MultiOrch`.
  * It still owns `MultiGateway` resources and the `LocalTopoServer` CR (if configured).

<!-- end list -->

```yaml
apiVersion: multigres.com/v1alpha1
kind: Cell
metadata:
  name: "example-multigres-cluster-us-east-1a"
  namespace: example
  labels:
    multigres.com/cluster: "example-multigres-cluster"
    multigres.com/cell: "us-east-1a"
  ownerReferences:
  - apiVersion: multigres.com/v1alpha1
    kind: MultigresCluster
    name: "example-multigres-cluster"
    uid: "a1b2c3d4-1234-5678-90ab-f0e1d2c3b4a5"
spec:
  name: "us-east-1a"
  
  # The controller populates the resolved topology info
  zone: "us-east-1a" 

  images:
    multigateway: "multigres/multigres:latest"

  # The controller resolves the static or dynamic config
  # into a concrete spec for the Cell controller to act on.
  multiGateway:
    replicas: 2
    resources:
      requests:
        cpu: "500m"
        memory: "512Mi"
      limits:
        cpu: "1"
        memory: "1Gi"

  # 'multiOrch' spec is REMOVED from Cell.
  
  # This cell uses the global topo server
  globalTopoServer:
    rootPath: "/multigres/global"
    clientServiceName: "example-multigres-cluster-global-client"
  topoServer: {}

  allCells:
  - "us-east-1a"
  - "us-east-1b"
status:
  conditions:
  - type: Available
    status: "True"
  gatewayReplicas: 2
  gatewayReadyReplicas: 2
  gatewayServiceName: "example-multigres-cluster-us-east-1a-gateway"
  # 'multiorchAvailable' is REMOVED from Cell status.
```

#### Child CR of MultigresCluster: TableGroup

  * **No change to CRD definition.**
  * This CR is still owned by the `MultigresCluster`.
  * The `MultigresCluster` controller will create **one** of these for each entry in the `spec.databases` list.
  * The controller will resolve the user's `pools` or `shardTemplateRef`+`overrides` from the database spec and populate the `spec.shardTemplate` block below.
  * For v1, `spec.partitioning.shards` will always be set to `1` by the controller.

<!-- end list -->

```yaml
apiVersion: multigres.com/v1alpha1
kind: TableGroup
metadata:
  name: "production-db-default" # e.g., <db-name>-<default-tg>
  namespace: example
  ownerReferences:
  - apiVersion: multigres.com/v1alpha1
    kind: MultigresCluster
    name: "example-multigres-cluster"
    uid: "a1b2c3d4-1234-5678-90ab-f0e1d2c3b4a5"
spec:
  images:
    multipooler: "multigres/multigres:latest"
    postgres: "postgres:15.3"

  # Controller will set this to 1 for v1
  partitioning:
    shards: 1

  # This block is populated by the MultigresCluster controller
  # after resolving the database-level spec (template or inline).
  shardTemplate:
    pools:
      - type: "replica"
        cell: "us-east-1a"
        replicas: 3
        dataVolumeClaimTemplate: { ... }
        postgres: { ... }
        multipooler: { ... }
      - type: "replica"
        cell: "us-east-1b"
        replicas: 3
        dataVolumeClaimTemplate: { ... }
        postgres: { ... }
        multipooler: { ... }
status:
  conditions:
  - type: Available
    status: "True"
  # This will be 1 for v1
  shards: 1
  readyShards: 1
```

#### Child of TableGroup: Shard

  * **No change to CRD definition.**
  * This CR is owned by the `TableGroup`.
  * The `TableGroup` controller will create **one** of these (since `partitioning.shards: 1`).
  * The `spec.pools` here is the final, resolved, read-only spec that drives the creation of `Postgres` and `MultiPooler` resources.

<!-- end list -->

```yaml
apiVersion: multigres.com/v1alpha1
kind: Shard
metadata:
  name: "production-db-default-0"
  namespace: example
  ownerReferences:
  - apiVersion: multigres.com/v1alpha1
    kind: TableGroup
    name: "production-db-default"
    uid: "d4e5f6a7-1234-5678-90ab-f0e1d2c3b4a7"
spec:
  images:
    multipooler: "multigres/multigres:latest"
    postgres: "postgres:15.3"
  # This is the fully resolved spec from the TableGroup parent
  pools:
    - type: "replica"
      cell: "us-east-1a"
      replicas: 3
      # ... full resolved spec ...
    - type: "replica"
      cell: "us-east-1b"
      replicas: 3
      # ... full resolved spec ...
status:
  conditions:
  - type: Available
    status: "True"
  primaryCell: "us-east-1a"
  totalPods: 6
  readyPods: 6
```

## Open Issues / Design Questions

  * **V1 Upgrade Path:** Is the v1 simplification (hiding `TableGroup` from the user) clear, and is the upgrade path to re-introducing the `tablegroups` block in `v1alpha2` or `v1beta1` well-defined?
  * **Gateway Config Validation:** Should the `static` and `dynamic` `MultiGateway` configs be enforced as a formal `oneOf` in the CRD validation webhook? (Strongly recommended).
  * **Override Logic:** Verify the `(type, cell)` deep-merge logic for `overrides` is intuitive for users. Does it need to be a strategic merge patch instead? (Deep-merge is simpler to implement).
  * What fields should be defaulted if the user provides minimal inline configuration?
  * [Decide](https://github.com/numtide/multigres-operator/pull/33#discussion_r2472393853) whether the operator should reconcile labels and use them to keep track of its resources or we should add spec fields that do this.

## Implementation History

  * **2025-10-08:** Initial proposal to create individual, user-managed CRDs for each component.
  * **2025-10-14:** Second proposal introduced a top-level `MultigresCluster` CR as the primary user-facing API.
  * **2025-10-28:** "Parent/child" model formalized. `MultigresCluster` becomes the single source of truth, and `DeploymentTemplate` was introduced.
  * **2025-11-05 (Consolidated):** This design was created based on client feedback.
      * Renamed `DeploymentTemplate` to `ShardTemplate` and scoped it *only* to shard definitions.
      * Simplified the `databases` spec to one-shard-per-database (hiding `TableGroup` from the v1 user-facing API).
      * Moved `MultiOrch` to be a global component.
      * Updated `MultiGateway` to be cell-local with `static` and `dynamic` config options.
      * Added `zone`/`region` topology keys to the `Cell` spec.

## Drawbacks

  * **Potential User Confusion:** Users accustomed to editing any Kubernetes resource might be confused when their manual edits to a child `Cell` or `Shard` CR are immediately reverted by the operator.
  * **Abstraction of `ShardTemplate`:** The `ShardTemplate` CR adds a layer of indirection. A user must look in two places (`MultigresCluster` and `ShardTemplate`) to fully understand a shard's configuration.
  * **Increased Number of CRDs:** This model introduces more CRDs (`Cell`, `TableGroup`, `Shard`, etc.) than a single monolithic approach.

## Alternatives

(All original alternatives remain considered and rejected for the same reasons.)

  * **Alternative 1: Single Monolithic CRD:** Rejected. Lacks observability and separation of concerns.
  * **Alternative 2: Component CRDs Only (No Parent):** Rejected. Makes the common case (a full cluster) too complex.
  * **Alternative 3: Hybrid Model with `managed: true/false` Flag:** Rejected. Lifecycle complexity and "split-brain" source of truth are too risky.
  * **Alternative 4: Operator as a Platform-Agnostic Delegator:** Rejected. Too much scope for v1alpha1; can be considered later.
  * **Alternative 5: Helm Charts Instead of CRDs:** Rejected. Lacks active reconciliation and Day 2 operations critical for a stateful database.
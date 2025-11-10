# MultigresCluster API v1alpha1

## Summary

This proposal defines the `v1alpha1` API for the Multigres Operator. The design is centered on a root `MultigresCluster` resource that acts as the single source of truth, supported by two specifically scoped template resources:

1.  **`MultigresCluster`**: The root resource defining the desired state (intent) of the entire cluster.
2.  **`CellTemplate`**: A reusable, namespaced resource for defining standard configurations for Cell-level components (`MultiGateway` and optionally `LocalTopoServer`).
3.  **`ShardTemplate`**: A reusable, namespaced resource for defining standard configurations for Shard-level components (`MultiOrch` and `Pools`).

All other resources (`TopoServer`, `Cell`, `TableGroup`, `Shard`) are implemented as read-only child CRs owned by the `MultigresCluster`. These child CRs reflect the *realized state* of the system and are managed by their own dedicated controllers.

## Motivation

Managing a distributed, sharded database system across multiple failure domains is inherently complex. Previous iterations explored monolithic CRDs (too complex), purely composable CRDs (too manual), and "managed" flags (unstable state).

The formalized parent/child model addresses these by ensuring:

  * **Separation of Concerns:** Splitting logic into child CRs results in simple, specialized controllers.
  * **Single Source of Truth:** The `MultigresCluster` is the only editable entry point for cluster topology, preventing conflicting states.
  * **Scoped Reusability:** By splitting templates into `CellTemplate` and `ShardTemplate`, we avoid N:1 mapping conflicts.
  * **Explicit Topology:** Removing "shard count" partitioning in favor of explicit shard list definitions provides deterministic control over where exactly data lives.

## Proposal: API Architecture and Resource Topology

  * **Globals (Inline Only):** `MultiAdmin` and `GlobalTopoServer` are critical singletons and must be defined inline within the `MultigresCluster` to prevent template ambiguity.
  * **Cells:** Explicitly defined in the root CR; must be associated mutually exclusively with a `zone` OR a `region`.
  * **Databases:** Follows a strict physical hierarchy: `Database` -\> `TableGroup` -\> `Shard`.
  * **MultiOrch:** Located at the **Shard** level to provide dedicated orchestration per Raft group.

<!-- end list -->

```ascii
[MultigresCluster] 🚀 (Root CR - user-editable)
      │
      ├── 🌍 [GlobalTopoServer] (Child CR, defined INLINE only)
      │
      ├── 🤖 MultiAdmin Resources (Deployment, Services, defined INLINE only)
      │
      ├── 💠 [Cell] (Child CR) ← 📄 Uses [CellTemplate] OR inline [spec]
      │    │
      │    ├── 🚪 MultiGateway Resources
      │    └── 📡 [LocalTopoServer] (Child CR, optional)
      │
      └── 🗃️ [TableGroup] (Child CR)
           │
           └── 📦 [Shard] (Child CR) ← 📄 Uses [ShardTemplate] OR inline [spec]
                │
                ├── 🧠 MultiOrch Resources (Deployment/Pod)
                └── 🏊 Pools (StatefulSets for Postgres+MultiPooler)

📄 [CellTemplate] (User-editable, scoped config)
   ├── multigateway
   └── localTopoServer (optional)

📄 [ShardTemplate] (User-editable, scoped config)
   ├── multiorch
   └── pools (postgres + multipooler)
```

## Design Details: API Specification

### User Managed CR: MultigresCluster

  * This CR and the two scoped templates (`CellTemplate`, `ShardTemplate`) are the *only* editable entries for the end-user.
  * All other child CRs will be owned by this top-level CR. Any manual changes to those child CRs will be automatically reverted by their respective controllers to match the `MultigresCluster` definition.
  * Every field that uses a `template` also comes with an `overrides` option, allowing specific deviations from the standard configuration.
  * **Atomic Overrides:** To ensure safety, highly interdependent fields are grouped into **Atomic Blocks** (e.g., `resources`, `storage`). When using `overrides`, you must replace the *entire* block, not just individual sub-fields (e.g., you cannot override just `cpu limit` without also providing `cpu request`).
  * Images are defined globally to avoid the danger of running multiple incongruent versions at once. This implies the operator handles upgrades.
  * The `MultigresCluster` does not create its grandchildren directly; for example, shard configuration is passed to the `TableGroup` CR, which then creates its own children `Shard` CRs.

<!-- end list -->

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: example-cluster
  namespace: example
spec:
  # ----------------------------------------------------------------
  # Global Images Cluster Configuration
  # ----------------------------------------------------------------
  # Images are defined globally to ensure version consistency across
  # all cells and shards.
  images:
    imagePullPolicy: "IfNotPresent"
    imagePullSecrets:
      - name: "my-registry-secret"
    multigateway: "multigres/gateway:latest"
    multiorch: "multigres/orch:latest"
    multipooler: "multigres/pooler:latest"
    multiadmin: "multigres/admin:latest"
    postgres: "postgres:15.3"

  # ----------------------------------------------------------------
  # Global Components (INLINE ONLY - No Templates)
  # ----------------------------------------------------------------
  
  # Global TopoServer is a singleton. It MUST be defined inline (or defaulted).
  # It supports EITHER a 'managedSpec' OR an 'external' configuration.
  globalTopoServer:
    # --- OPTION 1: Managed by Operator ---
    managedSpec:
      replicas: 3
      storage:
        size: "10Gi"
        class: "standard-gp3"
      resources:
        requests:
          cpu: "500m"
          memory: "1Gi"
        limits:
          cpu: "1"
          memory: "2Gi"
    # --- ALTERNATIVE OPTION 2: External Etcd ---
    # If you use an external Etcd, the operator will NOT create a 
    # TopoServer child CR.
    #
    # external:
    #   endpoints: 
    #     - "https://etcd-1.infra.local:2379"
    #     - "https://etcd-2.infra.local:2379"
    #   caSecret: "etcd-ca-secret"
    #   clientCertSecret: "etcd-client-cert-secret"

  # MultiAdmin is a singleton and must be defined inline.
  multiadmin:
    replicas: 2
    resources:
      requests: 
        cpu: "200m"
        memory: "256Mi"
      limits:
        cpu: "500m"
        memory: "512Mi"

  # ----------------------------------------------------------------
  # Cells Configuration
  # ----------------------------------------------------------------
  cells:
    # --- CELL 1: Using a Template ---
    - name: "us-east-1a"
      # Location must be strictly one of: 'zone' OR 'region'
      zone: "us-east-1a" 
      # region: "us-east-1"
      
      # Refers to a 'CellTemplate' CR in the same namespace.
      cellTemplate: "standard-cell-ha"
      
      # Optional overrides applied ON TOP of the template.
      # Useful for slight deviations like hardware differences in one zone.
      overrides:
        multiGateway:
           replicas: 3

    # --- CELL 2: Using Inline Spec (No Template) ---
    - name: "us-east-1b"
      zone: "us-east-1b"
      # If 'cellTemplate' is omitted, you MUST provide the 'spec' inline.
      # This is functionally equivalent to what a CellTemplate contains.
      spec:
         multiGateway:
           replicas: 2
           resources:
             requests:
               cpu: "500m"
               memory: "1Gi"
             limits:
               cpu: "1"
               memory: "2Gi"
         # --- Optional Local TopoServer ---
         # If omitted, this cell uses the GlobalTopoServer.
         # localTopoServer:
         #   managedSpec:
         #      replicas: 3
         #      storage:
         #        size: "5Gi"
         #        class: "standard-gp3"

  # ----------------------------------------------------------------
  # Database Topology (Database -> TableGroup -> Shard)
  # ----------------------------------------------------------------
  databases:
    - name: "production_db"
      tablegroups:
        - name: "orders_tg"
          # Shards are strictly explicitly defined. 
          # No 'partitioning: { count: 10 }' auto-generation.
          shards:
            # --- SHARD 0: Using a Template ---
            - name: "0"
              # Refers to a 'ShardTemplate' CR.
              shardTemplate: "standard-shard-ha"
              # Overrides are crucial here to pin pools to specific cells
              # if the template uses generic cell names.
              overrides:
                 # MAP STRUCTURE: Keyed by pool name for safe targeting.
                 pools:
                   # Overriding the pool named 'primary' from the template
                   # to ensure it lives in a specific cell for this shard.
                   primary:
                     cell: "us-east-1a"
            
            # --- SHARD 1: Using Inline Spec (No Template) ---
            - name: "1"
              # If 'shardTemplate' is omitted, you MUST provide the 'spec' inline.
              spec:
                multiOrch:
                  replicas: 1
                  resources:
                     requests:
                       cpu: "100m"
                       memory: "128Mi"
                     limits:
                       cpu: "200m"
                       memory: "256Mi"
                pools:
                  primary:
                    type: "readWrite"
                    cell: "us-east-1b"
                    replicas: 2
                    storage:
                       size: "100Gi"
                       class: "standard-gp3"
                    postgres:
                       requests:
                         cpu: "2"
                         memory: "4Gi"
                       limits:
                         cpu: "4"
                         memory: "8Gi"
                    multipooler:
                       requests:
                         cpu: "500m"
                         memory: "1Gi"
                       limits:
                         cpu: "1"
                         memory: "2Gi"

status:
  observedGeneration: 1
  conditions:
    - type: Available
      status: "True"
      lastTransitionTime: "2025-11-07T12:00:00Z"
      message: "All components are available."
  # Aggregated status for high-level visibility
  cells:
    us-east-1a: 
      ready: True
      gatewayReplicas: 3
    us-east-1b: 
      ready: True
      gatewayReplicas: 2
  databases:
    production_db:
      readyShards: 2
      totalShards: 2
```

### User Managed CR: CellTemplate

  * This CR is NOT a child resource. It is purely a configuration object.
  * It is namespaced to support RBAC scoping (e.g., platform team owns templates, dev team owns clusters).
  * When created, templates are not reconciled until referenced by a `MultigresCluster`.

<!-- end list -->

```yaml
apiVersion: multigres.com/v1alpha1
kind: CellTemplate
metadata:
  name: "standard-cell-ha"
  namespace: example
spec:
  # Template strictly defines only Cell-scoped components.
  multiGateway:
    replicas: 2
    resources:
      requests:
        cpu: "500m"
        memory: "512Mi"
      limits:
        cpu: "1"
        memory: "1Gi"
    affinity:
      podAntiAffinity:
        preferredDuringSchedulingIgnoredDuringExecution:
        - weight: 100
          podAffinityTerm:
             labelSelector:
               matchLabels:
                 app.kubernetes.io/component: multigateway
             topologyKey: "kubernetes.io/hostname"

  # --- OPTIONAL: Local TopoServer ---
  # Define if cells using this template should have their own dedicated ETCD.
  # If omitted, cells use the Global TopoServer by default.
  #
  # localTopoServer:
  #   managedSpec:
  #     replicas: 3
  #     storage:
  #       class: "standard-gp3"
  #       size: "5Gi"
  #     resources:
  #       requests:
  #         cpu: "500m"
  #         memory: "1Gi"
  #       limits:
  #         cpu: "1"
  #         memory: "2Gi"
```

### User Managed CR: ShardTemplate

  * Similar to `CellTemplate`, this is a pure configuration object.
  * It defines the "shape" of a shard: its orchestration and its data pools.
  * **Important:** `pools` is a **MAP**, keyed by the pool name. This ensures that overrides can safely target a specific pool without relying on brittle list array indices.

<!-- end list -->

```yaml
apiVersion: multigres.com/v1alpha1
kind: ShardTemplate
metadata:
  name: "standard-shard-ha"
  namespace: example
spec:
  # Template strictly defines only Shard-scoped components.
  
  # MultiOrch is a shard-level component (one per Raft group).
  multiOrch:
    replicas: 1 
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"
      limits:
        cpu: "200m"
        memory: "256Mi"

  # MAP STRUCTURE: Keyed by pool name for safe targeting.
  pools:
    primary:
      type: "readWrite"
      # 'cell' can be left empty here. It MUST be overridden in the 
      # MultigresCluster CR if left empty.
      # Alternatively, it can be set to a generic value if this template 
      # is specific to a region (e.g., "us-east-template").
      cell: "" 
      replicas: 2
      storage:
        class: "standard-gp3"
        size: "100Gi"
      postgres:
        requests:
          cpu: "2"
          memory: "4Gi"
        limits:
          cpu: "4"
          memory: "8Gi"
      multipooler:
        requests:
          cpu: "1"
          memory: "512Mi"
        limits:
          cpu: "2"
          memory: "1Gi"

    dr-replica:
      type: "readOnly"
      cell: "us-west-2a" # Hardcoded cell example in template
      replicas: 1
      storage:
         class: "standard-gp3"
         size: "100Gi"
      postgres:
         requests:
           cpu: "1"
           memory: "2Gi"
         limits:
           cpu: "2"
           memory: "4Gi"
      multipooler:
         requests:
           cpu: "500m"
           memory: "512Mi"
         limits:
           cpu: "1"
           memory: "1Gi"
```

### Child Resources (Read-Only)

These resources are created and reconciled by the `MultigresCluster` controller.

#### Child CR: TopoServer

  * Applies to both Global (owned by `MultigresCluster`) and Local (owned by `Cell`) topology servers.
  * This CR does *not* exist if the user configures an `external` etcd connection in the parent.

<!-- end list -->

```yaml
apiVersion: multigres.com/v1alpha1
kind: TopoServer
metadata:
  name: "example-cluster-global"
  namespace: example
  ownerReferences:
    - apiVersion: multigres.com/v1alpha1
      kind: MultigresCluster
      name: "example-cluster"
      controller: true
spec:
  # Fully resolved 'managedSpec' from MultigresCluster inline definition
  replicas: 3
  storage:
    size: "10Gi"
    class: "standard-gp3"
  image: "quay.io/coreos/etcd:v3.5"
  resources:
    requests:
      cpu: "500m"
      memory: "1Gi"
    limits:
      cpu: "1"
      memory: "2Gi"
status:
  conditions:
    - type: Available
      status: "True"
      lastTransitionTime: "2025-11-07T12:01:00Z"
  clientService: "example-cluster-global-client"
  peerService: "example-cluster-global-peer"
```

#### Child CR: Cell

  * Owned by `MultigresCluster`.
  * Strictly contains `MultiGateway` and optional `LocalTopoServer`. `MultiOrch` is NO LONGER here (moved to Shard).
  * The `allCells` field is used for discovery by gateways.

<!-- end list -->

```yaml
apiVersion: multigres.com/v1alpha1
kind: Cell
metadata:
  name: "example-cluster-us-east-1a"
  namespace: example
  labels:
    multigres.com/cluster: "example-cluster"
    multigres.com/cell: "us-east-1a"
  ownerReferences:
    - apiVersion: multigres.com/v1alpha1
      kind: MultigresCluster
      name: "example-cluster"
      uid: "a1b2c3d4-1234-5678-90ab-f0e1d2c3b4a5"
      controller: true
spec:
  name: "us-east-1a"
  zone: "us-east-1a"

  # Images passed down from global configuration
  images:
    multigateway: "multigres/gateway:latest"

  # Resolved from CellTemplate + Overrides
  multiGateway:
    replicas: 3
    resources:
      requests:
        cpu: "500m"
        memory: "512Mi"
      limits:
        cpu: "1"
        memory: "1Gi"
  
  # A reference to the GLOBAL TopoServer.
  # Always populated by the parent controller if no local server is used.
  globalTopoServer:
    rootPath: "/multigres/global"
    clientServiceName: "example-cluster-global-client"

  # Option 1: Using the Global TopoServer (Default)
  #
  topoServer: {}

  # Option 2: Inline Definition (External)
  #
  # topoServer:
  #   external:
  #     address: "etcd-us-east-1a.my-domain.com:2379"
  #     rootPath: "/multigres/us-east-1a"

  # Option 3: Managed Local
  #
  # topoServer:
  #   managedSpec:
  #     rootPath: "/multigres/us-east-1a"
  #     image: "quay.io/coreos/etcd:v3.5"
  #     replicas: 3
  #     storage:
  #       size: "5Gi"
  #       class: "standard-gp3"

  # List of all cells in the cluster for discovery.
  allCells:
  - "us-east-1a"
  - "us-east-1b"

  # Topology flags for the Cell controller to act on.
  topologyReconciliation:
    registerCell: true
    pruneTablets: true

status:
  conditions:
  - type: Available
    status: "True"
    lastTransitionTime: "2025-11-07T12:01:00Z"
  gatewayReplicas: 3
  gatewayReadyReplicas: 3
  gatewayServiceName: "example-cluster-us-east-1a-gateway"
```

#### Child CR: TableGroup

  * Owned by `MultigresCluster`.
  * Acts as the middle-manager for Shards. It MUST contain the full resolved specification for all shards it manages, enabling it to be the single source of truth for creating its child `Shard` CRs.

<!-- end list -->

```yaml
apiVersion: multigres.com/v1alpha1
kind: TableGroup
metadata:
  name: "production-db-orders-tg"
  namespace: example
  labels:
    multigres.com/database: "production_db"
    multigres.com/tablegroup: "orders_tg"
  ownerReferences:
    - apiVersion: multigres.com/v1alpha1
      kind: MultigresCluster
      name: "example-cluster"
      controller: true
spec:
  databaseName: "production_db"
  tableGroupName: "orders_tg"
  
  # The list of FULLY RESOLVED shard specifications.
  # This is pushed down from the MultigresCluster controller.
  shards:
    - name: "0"
      multiOrch:
        replicas: 1
        image: "multigres/orch:latest"
        resources:
          requests:
            cpu: "100m"
            memory: "128Mi"
          limits:
            cpu: "200m"
            memory: "256Mi"
      pools:
        primary:
          cell: "us-east-1a"
          type: "readWrite"
          replicas: 2
          storage:
             size: "100Gi"
             class: "standard-gp3"
          postgres:
              requests:
                cpu: "2"
                memory: "4Gi"
              limits:
                cpu: "4"
                memory: "8Gi"
          multipooler:
              requests:
                cpu: "1"
                memory: "512Mi"
              limits:
                cpu: "2"
                memory: "1Gi"
    
    - name: "1"
      multiOrch:
        replicas: 1
        image: "multigres/orch:latest"
        resources:
          requests:
            cpu: "100m"
            memory: "128Mi"
          limits:
            cpu: "200m"
            memory: "256Mi"
      pools:
        primary:
          cell: "us-east-1b"
          type: "readWrite"
          replicas: 2
          storage:
             size: "100Gi"
             class: "standard-gp3"
          postgres:
              requests:
                cpu: "2"
                memory: "4Gi"
              limits:
                cpu: "4"
                memory: "8Gi"
          multipooler:
              requests:
                cpu: "1"
                memory: "512Mi"
              limits:
                cpu: "2"
                memory: "1Gi"

status:
  readyShards: 2
  totalShards: 2
```

#### Child CR: Shard

  * Owned by `TableGroup`.
  * Now contains `MultiOrch` (Raft leader helper) AND `Pools` (actual data nodes).
  * Represents one entry from the `TableGroup` shards list.

<!-- end list -->

```yaml
apiVersion: multigres.com/v1alpha1
kind: Shard
metadata:
  name: "production-db-orders-tg-0"
  namespace: example
  labels:
     multigres.com/shard: "0"
     multigres.com/database: "production_db"
     multigres.com/tablegroup: "orders_tg"
  ownerReferences:
    - apiVersion: multigres.com/v1alpha1
      kind: TableGroup
      name: "production-db-orders-tg"
      controller: true
spec:
  shardName: "0"
  
  # Fully resolved from parent TableGroup spec
  multiOrch:
    replicas: 1
    image: "multigres/orch:latest"
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"
      limits:
        cpu: "200m"
        memory: "256Mi"

  pools:
    primary:
      cell: "us-east-1a" 
      type: "readWrite"
      replicas: 2
      storage:
         size: "100Gi"
         class: "standard-gp3"
      postgres:
           requests:
             cpu: "2"
             memory: "4Gi"
           limits:
             cpu: "4"
             memory: "8Gi"
      multipooler:
           requests:
             cpu: "1"
             memory: "512Mi"
           limits:
             cpu: "2"
             memory: "1Gi"
status:
  primaryCell: "us-east-1a"
  orchReady: True
  poolsReady: True
```

## Defaults & Webhooks

To simplify user experience and ensure cluster stability, a mutating admission webhook will apply strictly opinionated defaults if fields are omitted.

### Global Defaults

If `multiadmin` or `globalTopoServer` are omitted from the `MultigresCluster` spec, the webhook will inject them with these production-ready defaults:

  * **GlobalTopoServer:**
      * Replicas: 3
      * Storage: 10Gi (standard PVC)
      * Resources: Requests {cpu: 500m, memory: 1Gi}, Limits {cpu: 1, memory: 2Gi}
  * **MultiAdmin:**
      * Replicas: 2
      * Resources: Requests {cpu: 200m, memory: 256Mi}, Limits {cpu: 500m, memory: 512Mi}

### "Default" Named Templates

We propose reserving the name `default` for templates in the same namespace to act as implicit defaults.

  * **Cells:** If a user creates a `Cell` entry without providing `cellTemplate` OR `spec`, the webhook will attempt to patch the CR with `cellTemplate: default`.
  * **Shards:** If a user creates a `Shard` entry without providing `shardTemplate` OR `spec`, the webhook will attempt to patch the CR with `shardTemplate: default`.

This allows an organization to publish standard "golden" templates once, enabling highly concise user manifests.

## End-User Examples

### 1\. The Minimalist (Relying on Defaults)

This creates Multigres cluster with one cell and one database, one tablegroup and one shard.

The defaults for this ultra-minimalistic example can be fetched in two ways:

1.  All components are defaulted by the operator's webhook.
2.  If a `CellTemplate` and `ShardTemplate` named `default` exists within the same namespace it will take these as its default values.

> Notice that the `cells` field is still necessary but we are not naming the cell, this is because we are not sure yet if we should take a default zone or region at random from the cluster to define this, but if we can do this safely this field also won't be needed

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: minimal-cluster
spec:
  cells:
    - zone: "us-east-1a"
```

When the user does a `kubectl get multigrescluster minimal-cluster -o yaml` after apply this they would see all the values materialized, the default will be applied via webhook.

### 2\. The Power User (Explicit Templates & Atomic Overrides)

This example shows full control, demonstrating safe targeting via maps and atomic resource replacement.

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: power-cluster
spec:
  # Explicit external ETCD instead of managed default
  globalTopoServer:
    external:
      endpoints: 
        - "https://my-etcd-1.infra:2379"
        - "https://my-etcd-2.infra:2379"
        - "https://my-etcd-3.infra:2379"
      caSecret: "etcd-ca"
      clientCertSecret: "etcd-client-cert"

  # Explicit MultiAdmin sizing for dev environment
  multiadmin:
    replicas: 1
    resources:
      requests:
        cpu: "100m"
        memory: "256Mi"
      limits:
        cpu: "200m"
        memory: "512Mi"

  cells:
    - name: "us-east-1a"
      zone: "us-east-1a"
      # Using a specific high-performance template
      cellTemplate: "high-throughput-gateway"
    - name: "us-west-2a"
      zone: "us-west-2a"
      cellTemplate: "standard-gateway"
      overrides:
        multiGateway:
          resources:
             requests:
               cpu: "500m"
               memory: "2Gi"
             limits:
               cpu: "1"
               memory: "4Gi"

  databases:
    - name: "users_db"
      tablegroups:
        - name: "auth"
          shards:
            - name: "0"
              shardTemplate: "geo-distributed-shard"
              overrides:
                # MAP-BASED OVERRIDE: Safely targeting 'primary' pool
                pools:
                  primary:
                    # Partial override of a simple field
                    cell: "us-east-1a"
                    # ATOMIC OVERRIDE of Postgres compute for this specific shard
                    postgres:
                       requests:
                         cpu: "8"
                         memory: "16Gi"
                       limits:
                         cpu: "8"
                         memory: "16Gi"
            - name: "1"
              shardTemplate: "geo-distributed-shard"
              overrides:
                pools:
                  primary:
                    cell: "us-west-2a"
```

## Implementation History

  * **2025-10-08:** Initial proposal to create individual, user-managed CRDs for each component (`MultiGateway`, `MultiOrch`, etc.).
  * **2025-10-14:** A second proposal introduced a top-level `MultigresCluster` CR as the primary user-facing API.
  * **2025-10-28:** The current "parent/child" model was formalized, designating `MultigresCluster` as the single source of truth with read-only children.
  * **2025-11-05:** Explored a simplified V1 API limited to a single shard. Rejected to ensure the API is ready for multi-shard from day one.
  * **2025-11-06:** Explored a single "all-in-one" `DeploymentTemplate`. Rejected due to N:1 conflicts when trying to apply one template to both singular Cell components and multiplied Shard components.
  * **2025-11-07:** Finalized the "Scoped Template" model (`CellTemplate` & `ShardTemplate`) and restored full explicit `database` -\> `tablegroup` -\> `shard` hierarchy.
  * **2025-11-10:** Refactored `pools` to use Maps instead of Lists and introduced "Atomic Blocks" for `resources` and `storage` to ensure safer template overrides.
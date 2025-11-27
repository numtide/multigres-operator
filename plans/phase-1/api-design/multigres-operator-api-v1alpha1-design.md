# MultigresCluster API v1alpha1

This is the final design we will be using to implement the operator based on discussions with the Multigres team.

## Summary

This proposal defines the `v1alpha1` API for the Multigres Operator. The design is centered on a root `MultigresCluster` resource that acts as the single source of truth, supported by three specifically scoped template resources:

1.  **`MultigresCluster`**: The root resource defining the desired state (intent) of the entire cluster.
2.  **`CoreTemplate`**: A reusable, namespaced resource for defining standard configurations for core control plane components (`GlobalTopoServer` and `MultiAdmin`).
3.  **`CellTemplate`**: A reusable, namespaced resource for defining standard configurations for Cell-level components (`MultiGateway` and optionally `LocalTopoServer`).
4.  **`ShardTemplate`**: A reusable, namespaced resource for defining standard configurations for Shard-level components (`MultiOrch` and `Pools`).

All other resources (`TopoServer`, `Cell`, `TableGroup`, `Shard`) should be considered read-only child CRs owned by the `MultigresCluster`. These child CRs reflect the *realized state* of the system and are managed by their own dedicated controllers. If the user edits them directly, they will get immediately reverted by the parent CR.

## Motivation

Managing a distributed, sharded database system across multiple failure domains is inherently complex. Previous iterations explored monolithic CRDs (too complex), purely composable CRDs (too manual), and "managed" flags (unstable state).

The formalized parent/child model addresses these by ensuring:

  * **Separation of Concerns:** Splitting logic into child CRs results in simple, specialized controllers.
  * **Single Source of Truth:** The `MultigresCluster` is the only editable entry point for cluster topology, preventing conflicting states.
  * **Scoped Reusability:** By splitting templates into `CoreTemplate`, `CellTemplate`, and `ShardTemplate`, we provide clear, reusable configurations.
  * **Explicit Topology:** Removing "shard count" partitioning in favor of explicit shard list definitions provides deterministic control over where exactly data lives.
  * **Consistent Override Chain:** All components follow a predictable 4-level override chain, providing maximum flexibility while maintaining a clear and consistent API pattern.

## Proposal: API Architecture and Resource Topology

  * **Core Components:** `MultiAdmin` and `GlobalTopoServer` are defined as top-level fields in the `MultigresCluster`. Each can be configured inline or by referencing a `CoreTemplate`.
  * **Cells:** Explicitly defined in the root CR; can be specified inline or via a `CellTemplate`.
  * **Databases:** Follows a strict physical hierarchy: `Database` -\> `TableGroup` -\> `Shard`.
  * **Shards:** Can be specified inline or via a `ShardTemplate`.
  * **Multiorch** Located at the **Shard** level to provide dedicated orchestration.

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
      │    ├── 🚪 MultiGateway Resources
      │    └── 📡 [LocalTopoServer] (Child CR, optional)
      │
      └── 🗃️ [TableGroup] (Child CR)
           │
           └── 📦 [Shard] (Child CR) ← 📄 Uses [ShardTemplate] OR inline [spec]
                │
                ├── 🧠 MultiOrch Resources (Deployment/Pod)
                └── 🏊 Pools (StatefulSets for Postgres+MultiPooler)

📄 [CoreTemplate] (User-editable, scoped config)
   ├── globalTopoServer
   └── multiadmin

📄 [CellTemplate] (User-editable, scoped config)
   ├── multigateway
   └── localTopoServer (optional)

📄 [ShardTemplate] (User-editable, scoped config)
   ├── multiorch
   └── pools (postgres + multipooler)
```

## Design Details: API Specification

### The `default` Flag (System Resources)

Both `Database` and `TableGroup` entries support a boolean `default` flag (defaulting to `false`). This flag maps the definition to the **System Default** infrastructure created during the cluster's bootstrap phase.

  * **On a Database (`default: true`):** Indicates this entry defines the configuration for the system-level database (typically named `postgres`) that contains the global catalog. There can be only one default database per cluster.
  * **On a TableGroup (`default: true`):** Indicates this entry defines the configuration for the "Catch-All" or "Unsharded" group of that database. Every database has exactly one Default TableGroup where tables land by default.

Defining these entries allows the user to explicitly configure the resources (replicas, storage, compute) allocated to these system components, rather than relying on hardcoded operator defaults.

### User Managed CR: MultigresCluster

  * This CR and the three scoped templates (`CoreTemplate`, `CellTemplate`, `ShardTemplate`) are the *only* editable entries for the end-user.
    * All other child CRs will be owned by this top-level CR. Any manual changes to those child CRs will be immediately reverted by the `MultigresCluster` cluster controller.
  * All component configurations (`globalTopoServer`, `multiadmin`, `cells`, `shards`) follow a consistent pattern: they can be defined via an inline `spec` or by referencing a template (`templateRef`). Providing both is a validation error.
  * **Override Chain:** All components use the following 4-level precedence chain for configuration:
    1.  **Component-Level Definition:** An inline `spec` or an explicit `templateRef` on the component itself.
    2.  **Cluster-Level Default:** The corresponding template defined in `spec.templateDefaults` (e.g., `templateDefaults.coreTemplate` or `templateDefaults.cellTemplate`).
    3.  **Namespace-Level Default:** A template of the correct kind (e.g., `CoreTemplate`) named `default` in the same namespace.
    4.  **Operator Hardcoded Defaults:** A final fallback applied by the operator's admission webhook.
  * **Atomic Overrides:** To ensure safety, highly interdependent fields are grouped (e.g., `resources`, `storage`). When using `overrides`, you must replace the *entire* group, not just individual sub-fields (e.g., you cannot override just `cpu limit` without also providing `cpu request`).
  * Images are defined globally to avoid the danger of running multiple incongruent versions at once. This implies the operator handles upgrades.
  * The `MultigresCluster` does not create its grandchildren directly; for example, shard configuration is passed to the `TableGroup` CR, which then creates its own children `Shard` CRs.

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
  # NOTE: Perhaps one day we can template these as well.
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
  # Cluster-Level Template Defaults
  # ----------------------------------------------------------------
  # These are the default templates to use for any component that
  # does not have an inline 'spec' or an explicit 'templateRef'.
  # These are optional.
  # if they don't exist the controller will pick whichever is named 'default'
  # or use the controller defaults.
  templateDefaults:
    coreTemplate: "cluster-wide-core"
    cellTemplate: "cluster-wide-cell"
    shardTemplate: "cluster-wide-shard"

  # ----------------------------------------------------------------
  # Global Components
  # ----------------------------------------------------------------
  
  # Global TopoServer is a singleton. It follows the 4-level override chain.
  # It supports EITHER a 'etcd' OR 'external' OR 'templateRef'.
  globalTopoServer:
    # --- OPTION 1: Inline Etcd Spec ---
    etcd:
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

    # --- OPTION 2: Inline External Spec ---
    # external:
    #   endpoints: 
    #     - "https://etcd-1.infra.local:2379"
    #     - "https://etcd-2.infra.local:2379"
    #   caSecret: "etcd-ca-secret"
    #   clientCertSecret: "etcd-client-cert-secret"

    # --- OPTION 3: Explicit Template Reference ---
    # templateRef: "my-explicit-core-template"

  # MultiAdmin is a singleton. It follows the 4-level override chain.
  # It supports EITHER a 'spec' OR a 'templateRef'.
  multiadmin:
    # --- OPTION 1: Inline Spec ---
    spec:
      replicas: 2
      resources:
        requests: 
          cpu: "200m"
          memory: "256Mi"
        limits:
          cpu: "500m"
          memory: "512Mi"

    # --- OPTION 2: Explicit Template Reference ---
    # templateRef: "my-explicit-core-template"

  # ----------------------------------------------------------------
  # Cells Configuration
  # ----------------------------------------------------------------
  cells:
    # --- CELL 1: Using an Explicit Template ---
    - name: "us-east-1a"
      # Location must be strictly one of: 'zone' OR 'region'
      zone: "us-east-1a" 
      # region: "us-east-1"
      
      cellTemplate: "standard-cell-ha"
      
      # Optional overrides applied ON TOP of the template.
      overrides:
        multiGateway:
           replicas: 3

    # --- CELL 2: Using Inline Spec (No Template) ---
    - name: "us-east-1b"
      zone: "us-east-1b"
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
         #   etcd:
         #      replicas: 3
         #      storage:
         #        size: "5Gi"
         #        class: "standard-gp3"
    
    # --- CELL 3: Using Cluster Default Template ---
    - name: "us-east-1c"
      zone: "us-east-1c"
      # 'spec' and 'cellTemplate' are omitted.
      # This will use 'spec.templateDefaults.cellTemplate' ("cluster-wide-cell")
      # (if that is not set, it will look for 'CellTemplate' named 'default',
      # and if that is not found, it will use the webhook default).

  # ----------------------------------------------------------------
  # Database Topology (Database -> TableGroup -> Shard)
  # ----------------------------------------------------------------
  databases:
    # --- EXAMPLE 1: Configuring the System Default Database ---
    # This entry targets the system-level database created during bootstrap.
    # We mark it as 'default: true' to apply this configuration to the 
    # bootstrap resources (instead of creating a new user database).
    - name: "postgres"
      default: true
      tablegroups:
        - name: "default"
          default: true
          shards:
            - name: "0"
              # define resources for the system default shard
              shardTemplate: "standard-shard-ha"

    # --- EXAMPLE 2: A User Database ---
    - name: "production_db"
      # default: false (Implicit) - This creates a new logical database
      tablegroups:
        # The default unsharded group for this specific database.
        # Default tablegroups can only have one shard.
        # It handles all tables not explicitly moved to 'orders_tg'.
        - name: "main_unsharded"
          default: true
          shards:
            - name: "0"
              spec:
                multiorch:
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
                    cells: 
                      - "us-east-1b"
                    replicasPerCell: 2
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

        # A custom sharded group for high-volume data
        - name: "orders_tg"
          # default: false (Implicit)
          shards:
            # --- SHARD 0: Using Inline Spec (No Template) ---
            - name: "0"
              spec:
                multiorch:
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
                    cells: 
                      - "us-east-1b"
                    replicasPerCell: 2
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

            # --- SHARD 1: Using an Explicit Template ---
            - name: "1"
              shardTemplate: "standard-shard-ha"
              # Overrides are crucial here to pin pools to specific cells
              # if the template uses generic cell names.
              overrides:
                 # MAP STRUCTURE: Keyed by pool name for safe targeting.
                 pools:
                   # Overriding the pool named 'primary' from the template
                   # to ensure it lives in a specific cell for this shard.
                   primary:
                     cells: 
                       - "us-east-1a"
                     
            # --- SHARD 2: Using Cluster Default Template ---
            - name: "2"
              # 'spec' and 'shardTemplate' are omitted.
              # This will use 'spec.templateDefaults.shardTemplate' ("cluster-wide-shard")
              # (or 'default' template, or webhook default).
              # We still must provide overrides for required fields.
              overrides:
                 pools:
                   primary:
                     cells: 
                       - "us-east-1c"

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
    us-east-1c:
      ready: True
      gatewayReplicas: 2 # (Assuming default is 2)
  databases:
    production_db:
      readyShards: 3
      totalShards: 3
```

### User Managed CR: CoreTemplate

  * This CR is NOT a child resource. It is purely a configuration object.
  * It is namespaced to support RBAC scoping (e.g., platform team owns templates, dev team owns clusters).
  * It defines the shape of the cluster's core control plane. A `CoreTemplate` can contain definitions for *both* components. When a component (e.g., `globalTopoServer`) references this template, the controller will extract the relevant section.

```yaml
apiVersion: multigres.com/v1alpha1
kind: CoreTemplate
metadata:
  name: "standard-core-ha"
  namespace: example
spec:
  # Defines the Global TopoServer component
  globalTopoServer:
    # --- OPTION 1: Managed by Operator ---
    etcd:
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
    # external:
    #   endpoints: 
    #     - "https://etcd-1.infra.local:2379"
    #   caSecret: "etcd-ca-secret"
    #   clientCertSecret: "etcd-client-cert-secret"

  # Defines the MultiAdmin component
  multiadmin:
    spec:
      replicas: 2
      resources:
        requests: 
          cpu: "200m"
          memory: "256Mi"
        limits:
          cpu: "500m"
          memory: "512Mi"
```

### User Managed CR: CellTemplate

  * This CR is NOT a child resource. It is purely a configuration object.
  * It is namespaced to support RBAC scoping (e.g., platform team owns templates, dev team owns clusters).
  * When created, templates are not reconciled until referenced by a `MultigresCluster`.

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
  # If omitted, cells use the GlobalTopoServer by default.
  #
  # localTopoServer:
  #   etcd:
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
  * **MultiOrch Placement:** `multiorch` is deployed to the cells listed in `multiorch.cells`. If this list is empty, it defaults to all cells where pools are defined.
  * **Pool Placement:** `pools` now uses a `cells` list. For `readWrite` pools (Primary), this list typically contains a single cell. For `readOnly` pools (Replicas), this list can contain multiple cells to apply the same configuration across multiple zones.

```yaml
apiVersion: multigres.com/v1alpha1
kind: ShardTemplate
metadata:
  name: "standard-shard-ha"
  namespace: example
spec:
  # Template strictly defines only Shard-scoped components.

  # MultiOrch is a shard-level component.
  # The Operator will deploy one instance of this Deployment into EVERY Cell 
  # listed in 'cells'. If 'cells' is empty, it defaults to all cells 
  # where pools are defined.
  multiorch:
    # replicas: 1 # replicas per cell and pool this multiorch is deployed
    cells: [] # Defaults to all populated cells
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
      # 'cells' can be left empty here or omitted entirely. It MUST be overridden in the 
      # MultigresCluster CR if left empty or missing.
      # Alternatively, it can be set to a generic value if this template 
      # is specific to a region (e.g., "us-east-template").
      cells: [] 
      replicasPerCell: 2
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
      # This pool will be deployed to all cells listed here.
      cells: 
        - "us-west-2a" 
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

> NOTE: At some point we may want to consider adding status fields for the children to say what template the config is coming from, for simplicity not defining that now.

#### Child CR: TopoServer

  * Applies to both Global (owned by `MultigresCluster`) and Local (owned by `Cell`) topology servers.
  * This CR does *not* exist if a separate, `external` etcd definition is used in the parent (e.g. using etcd-operator to provision one).

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
  # Resolved from MultigresCluster.
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
  * Strictly contains `MultiGateway` and optional `LocalTopoServer`.
  * The `allCells` field is used for discovery by gateways.

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
    multigateway: "multigres/multigres:latest"

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
    address: "example-cluster-global-client.example.svc.cluster.local:2379"
    rootPath: "/multigres/global"
    implementation: "etcd2"

  # Option 1: Using the Global TopoServer (Default)
  #
  topoServer: {}

  # Option 2: Inline Definition (External)
  #
  # topoServer:
  #   external:
  #     address: "my-etcd.some-namespace.svc.cluster.local:2379"
  #     rootPath: "/multigres/us-east-1a"
  #     implementation: "etcd2"

  # Option 3: Managed Local
  #
  # topoServer:
  #   etcd:
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

  # Images passed down from global configuration
  images:
    multiorch: "multigres/multigres:latest"
    multipooler: "multigres/multigres:latest"
    postgres: "postgres:15.3"

  # A reference to the GLOBAL TopoServer.
  globalTopoServer:
    address: "example-cluster-global-client.example.svc.cluster.local:2379"
    rootPath: "/multigres/global"
    implementation: "etcd2"
  
  # The list of FULLY RESOLVED shard specifications.
  # This is pushed down from the MultigresCluster controller.
  shards:
    - name: "0"
      multiorch:
        cells: 
          - "us-east-1a"
          - "us-east-1b"
        resources:
          requests:
            cpu: "100m"
            memory: "128Mi"
          limits:
            cpu: "200m"
            memory: "256Mi"
      pools:
        primary:
          cells: 
            - "us-east-1a"
          type: "readWrite"
          replicasPerCell: 2
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
      multiorch:
        cells: 
           - "us-east-1b"
        resources:
          requests:
            cpu: "100m"
            memory: "128Mi"
          limits:
            cpu: "200m"
            memory: "256Mi"
      pools:
        primary:
          cells: 
            - "us-east-1b"
          type: "readWrite"
          replicasPerCell: 2
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
    
    # This shard's spec is resolved from a template
    # (e.g., "cluster-wide-shard")
    - name: "2"
      multiorch:
        cells: 
          - "us-east-1c"
        resources:
          requests:
            cpu: "100m"
            memory: "128Mi"
          limits:
            cpu: "200m"
            memory: "256Mi"
      pools:
        primary:
          cells: 
            - "us-east-1c" # Resolved from override
          type: "readWrite"
          replicasPerCell: 2 # Assuming '2' from template
          storage:
             size: "100Gi" # Assuming '100Gi' from template
             class: "standard-gp3" # Assuming 'standard-gp3' from template
          postgres:
              requests: # Assuming values from template
                cpu: "2"
                memory: "4Gi"
              limits:
                cpu: "4"
                memory: "8Gi"
          multipooler:
              requests: # Assuming values from template
                cpu: "1"
                memory: "512Mi"
              limits:
                cpu: "2"
                memory: "1Gi"
        
status:
  readyShards: 3
  totalShards: 3
```

#### Child CR: Shard

  * Owned by `TableGroup`.
  * Contains `MultiOrch` (consensus management) and `Pools` (actual data nodes).
  * Represents one entry from the `TableGroup` shards list.

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

  # Images passed down from global configuration
  images:
    multiorch: "multigres/multigres:latest"
    multipooler: "multigres/multigres:latest"
    postgres: "postgres:15.3"

  # A reference to the GLOBAL TopoServer.
  globalTopoServer:
    address: "example-cluster-global-client.example.svc.cluster.local:2379"
    rootPath: "/multigres/global"
    implementation: "etcd2"
  
  # Fully resolved from parent TableGroup spec
  multiorch:
    cells: 
      - "us-east-1a"
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"
      limits:
        cpu: "200m"
        memory: "256Mi"

  pools:
    primary:
      cells: 
        - "us-east-1a" 
      type: "readWrite"
      replicasPerCell: 2
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

To simplify user experience and ensure cluster stability, the operator uses a combination of **Mutating Webhooks** (for applying defaults) and **Validating Webhooks** (for synchronous checks), alongside **Controller Finalizers** (for asynchronous protection).

### 1\. Configuration Defaults (Mutating Webhook)

A mutating admission webhook applies a strict **4-Level Override Chain** to resolve configurations. This logic is applied consistently for Cluster Components (`GlobalTopoServer`, `MultiAdmin`, `Cells`) and Database Shards.

#### Cluster Component Override Chain

*Applies to `GlobalTopoServer`, `MultiAdmin`, and `Cells` defined in `MultigresCluster`.*

1.  **Component-Level Definition (Highest):** An inline `spec` or an explicit `templateRef` / `cellTemplate` on the component itself.
2.  **Cluster-Level Default Template:** The corresponding template defined in `spec.templateDefaults` (e.g., `templateDefaults.coreTemplate` or `templateDefaults.cellTemplate`).
3.  **Namespace-Level Default Template:** A template of the correct kind (e.g., `CoreTemplate`) named `default` in the same namespace as the cluster.
4.  **Operator Hardcoded Defaults (Lowest):** A final fallback applied by the operator code (e.g., default resources, default replicas).

#### Shard Override Chain

*Applies to every Shard defined in `spec.databases[].tablegroups[].shards[]`.*

1.  **Shard-Level Definition (Highest):** An inline `spec` or an explicit `shardTemplate` defined on the specific shard entry in the `MultigresCluster` YAML.
2.  **Cluster-Level Default Template:** The `spec.templateDefaults.shardTemplate` field in the root `MultigresCluster` CR.
3.  **Namespace-Level Default Template:** A `ShardTemplate` named `default` in the same namespace as the cluster.
4.  **Operator Hardcoded Defaults (Lowest):** A final fallback applied by the operator.
5.  **List Replacement Behavior:** When overriding the `cells` field (in pools or multiorch), the new list specified in the override *completely replaces* the list defined in the template. It is not merged or appended.
-----

### 2\. Synchronous Validating Webhooks

Webhooks are used *only* for fast, synchronous, and semantic validation to prevent invalid configurations from being accepted by the API server.

#### `MultigresCluster`

  * **On `CREATE` and `UPDATE`:**
      * **Template Existence:** Validates that all templates referenced in `spec.templateDefaults` or explicitly in components (Core, Cell, or Shard templates) exist *at the time of application*.
      * **Component Spec Mutex:** Enforces that `etcd`, `external`, and `templateRef` are mutually exclusive for components like `GlobalTopoServer`.
      * **Uniqueness:** Validates that all names are unique within their respective scopes:
          * Cell names in `spec.cells`.
          * Database names in `spec.databases`.
          * TableGroup names within a Database.
          * Shard names within a TableGroup.
      * **Topology Integrity:** Verifies that if a Shard is pinned to a specific cell (via overrides), that cell exists in the `spec.cells` list.

#### `CoreTemplate`, `CellTemplate`, `ShardTemplate`

  * **On `CREATE` and `UPDATE`:**
      * **Schema Validation:** Ensures the template spec contains valid configuration for its specific type (e.g., a `CellTemplate` cannot contain `multiAdmin` config).

-----

### 3\. Asynchronous Controller and Finalizer Logic

Asynchronous logic is used for operations that depend on external state or require blocking deletion, handled by controllers and finalizers.

#### `MultigresCluster`

  * **Deletion Protection (Finalizer):**
    1.  The `MultigresCluster` controller adds a finalizer (e.g., `multigres.com/cleanup`) to the CR upon creation.
    2.  On deletion, the controller ensures all child resources (StatefulSets, Services) are properly terminated before removing the finalizer. NOTE: We should consider optional flag to delete PVCs too, default will be to keep them.
    3.  *Note:* Since databases are embedded in the cluster CR, deleting the cluster implies deleting all databases. No extra "claim" check is needed here.

#### `CoreTemplate`, `CellTemplate`, `ShardTemplate`

  * **In-Use Protection (Finalizer):**
    1.  These templates are protected by a "fan-out" finalizer pattern to prevent accidental deletion of templates actively used by production clusters.
    2.  **Logic:** When a `MultigresCluster` controller reconciles and sees it is using "prod-cell-template", it adds a finalizer (e.g., `multigres.com/in-use-by-example-cluster`) *to the `CellTemplate` object*.
    3.  **Cleanup:** When the `MultigresCluster` is deleted (or updated to stop using that template), its controller removes that specific finalizer from the template.
    4.  **Result:** Kubernetes will reject the deletion of a Template if any Cluster is still referencing it (because the finalizers won't be empty).

## End-User Examples

### 1\. The Ultra-Minimalist (Relying on Namespace/Webhook Defaults)

This creates Multigres cluster with one cell and one database, one tablegroup and one shard.

The defaults for this ultra-minimalistic example can be fetched in two ways:

1.  All components are defaulted by the operator's webhook.
2.  If a `CoreTemplate`, `CellTemplate`, and `ShardTemplate` named `default` exists within the same namespace it will take these as its default values.

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

### 2\. The Minimalist (Relying on Cluster Defaults)

This user relies on the `spec.templateDefaults` field to set cluster-wide defaults.

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: minimal-cluster-with-defaults
spec:
  # This cluster will use the "dev-defaults" CoreTemplate for its
  # global components. All cells and shards will use their respective
  # "dev-defaults" templates.
  templateDefaults:
    coreTemplate: "dev-defaults"
    cellTemplate: "dev-defaults-cell"
    shardTemplate: "dev-defaults-shard"

  # CoreComponents (globalTopoServer, multiadmin) are omitted,
  # so they will use "dev-defaults" (from the CoreTemplate)

  cells:
    - name: "us-east-1a"
      zone: "us-east-1a"
      # 'spec' and 'cellTemplate' are omitted, so "dev-defaults-cell" is used.
    - name: "us-west-2a"
      zone: "us-west-2a"
      # 'spec' and 'cellTemplate' are omitted, so "dev-defaults-cell" is used.

  databases:
    - name: "db1"
      tablegroups:
      - name: "tg1"
        shards:
          - name: "0"
            # 'spec' and 'shardTemplate' are omitted, so "dev-defaults-shard" is used.
            # We still need to override the 'cell' for the primary pool.
            overrides:
              pools:
                primary:
                  cell: "us-east-1a"
          - name: "1"
            # 'spec' and 'shardTemplate' are omitted, so "dev-defaults-shard" is used.
            overrides:
              pools:
                primary:
                  cell: "us-west-2a"
```

### 3\. The Power User (Explicit Overrides)

This user explicitly defines everything, mixing inline specs and templates, and bypassing all defaults.

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: power-cluster
spec:
  # This user sets cluster defaults, but overrides them everywhere.
  templateDefaults:
    coreTemplate: "cluster-default-core"
    cellTemplate: "cluster-default-cell"
    shardTemplate: "cluster-default-shard"
  
  globalTopoServer:
    external:
      endpoints: 
        - "https://my-etcd-1.infra:2379"
        - "https://my-etcd-2.infra:2379"
        - "https://my-etcd-3.infra:2379"
      caSecret: "etcd-ca"
      clientCertSecret: "etcd-client-cert"
  
  multiadmin:
    spec:
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
      cellTemplate: "high-throughput-gateway"
    - name: "us-west-2a"
      zone: "us-west-2a"
      cellTemplate: "standard-gateway"
      overrides:
        multiGateway:
          # Overriding the entire resources block
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
                    # OVERRIDE of Postgres compute for this specific shard
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
  * **2025-11-10:** Refactored `pools` to use Maps instead of Lists and introduced atomic grouping for `resources` and `storage` to ensure safer template overrides.
  * **2025-11-11:** Introduced a consistent 4-level override chain (inline/explicit-template -\> cluster-default -\> namespace-default -\> webhook) for all components. Added `CoreTemplate` CRD and `spec.templateDefaults` block to support this. Reverted `spec.coreComponents` nesting to top-level `globalTopoServer` and `multiadmin` fields.
  * **2025-11-14:** Explored a multi-CRD "claim" model (`MultigresDatabase` + `MultigresDatabaseResources`) to support DDL-driven workflows and strong RBAC separation.
  * **2025-11-18:** Reverted to the 2025-11-11 monolithic `MultigresCluster` design to align with client requirements for Postgres-native "System Catalog" management. The `MultigresDatabase` CRD was rejected. We will instead support the DDL workflow via a synchronization controller that patches the monolithic `MultigresCluster` CR based on the `SystemCatalog` state.
  * **2025-11-25:** Moved `multiorch` and `pool` placement from single `cell` fields to explicit `cells` lists. This supports multi-cell pool definitions (e.g., for uniform read replicas) and decouples MultiOrch placement from pool presence, while maintaining safety via a "defaults to all" logic.

## Drawbacks

We have reverted to this design but the requirement to create resources via DDL (`CREATE DATABASE`) hasn't been dropped, but postponed. What follows are some caveats when following the current design as it stands right now and also incorporating this Posgres compatibility requirement:

  * **Broken "Delete" UX (Zombie Resources):** To support the "DB is Truth" requirement, the Operator **cannot** delete a database simply because it is removed from the `spec.databases` list as it might still exist in the default database schema ("system catalog"). This breaks the standard Kubernetes expectation that "deleting config = deleting resource." Users must use imperative SQL (`DROP DATABASE`) to delete resources; removing them from Git will only orphan them, leaving them running and accruing costs ("Zombie Databases"). Including a bidirectional update process here may complicate things.
  * **Perpetual GitOps Drift:** Since users can create databases via DDL at any time, the `MultigresCluster` CR in Git will rarely match the actual cluster state. `kubectl diff` will be noisy, and the `status` field will become the only reliable view of the system, degrading the value of the declarative spec. This can be mitigated if we have a component that constantly writes these changes to either git and applies them declaratively, but it is not a common pattern.
  * **Status Object Bloat (Scalability):** Because the Operator must track "Discovered" (DDL-created) databases in the `status` field to make them visible to SREs, a cluster with thousands of databases risks hitting the Kubernetes object size limit (etcd limits). This limits the scalability of the reporting mechanism compared to fanning out to separate CRs.
  * **Resource "Adoption" Friction:** Databases created via DDL are assigned a default `ShardTemplate`. To "upgrade" or resize these databases, an SRE must manually "adopt" them by adding them to the `MultigresCluster` YAML with the correct name and new template. This introduces a manual step and a potential race condition where a new database might be under-provisioned before it can be adopted.
  * **Default DB Schema (System Catalog) Availability Dependency:** The Operator's reconciliation loop now strictly depends on the read availability of the "Default Database" (System Catalog). This acts as a single point of failure for the control plane: if the System Catalog is down, the Operator cannot manage any other part of the cluster, even if those other parts are healthy.
  * **API Hotspot and "Blast Radius" Risk:** By centralizing all database definitions into the monolithic `MultigresCluster` CR, this single object becomes a massive reconciliation hotspot. A single typo in this large resource (e.g., while "adopting" a database) could potentially break reconciliation for the entire cluster's control plane.
  * **Rename/Replace Ambiguity:** Postgres allows `ALTER DATABASE RENAME`, but Kubernetes relies on stable names. If a user renames a database in SQL, the Operator may perceive this as a "Delete" (of the old name) and "Create" (of the new name), potentially attempting to re-provision the old name if it still exists in the YAML.
  * **Lack of Namespace Isolation:** All databases must be defined in (or adopted into) the central `MultigresCluster` resource. This effectively forces all application teams to rely on platform admins to manage resource sizing, removing the ability to use Kubernetes RBAC for self-service resource management in separate namespaces. No clean DBA/Platform Engineer persona separation model.

## Alternatives

Several alternative designs were considered and rejected in favor of the current parent/child model.

### Alternative 1: Component CRDs Only (No Parent)

This model would provide individual, user-managed CRDs for `MultiGateway`, `MultiOrch`, `MultiPooler`, and `Etcd`. Users would be responsible for "composing" a cluster by creating these resources themselves.

  * **Pros:** Maximum flexibility and composability.
  * **Cons:** Extremely verbose and complex for a standard deployment. Users must manually create all components and wire them together correctly.
  * **Rejected Because:** Makes the common case (deploying a full cluster) unnecessarily complex and error-prone.

### Alternative 2: Hybrid Model with `managed: true/false` Flag

This model would feature a top-level `MultigresCluster` CR, but each component section would have a `managed: true/false` flag. If `true`, the operator manages the child resource. If `false`, the operator ignores it.

  * **Pros:** Offers a "best-of-both-worlds" approach.
  * **Cons:** Introduces significant complexity around resource ownership and lifecycle (e.g., handling transitions from managed to unmanaged). Creates a high risk of cluster misconfiguration.
  * **Rejected Because:** The lifecycle and ownership transitions were deemed too complex and risky for a production-grade operator.

### Alternative 3: The "Claim" Model (`MultigresDatabase`)

This design (explored on 2025-11-14) separated the cluster definition from database definitions. Users would create a `MultigresDatabase` CR in their own namespace, which would "claim" resources from the central cluster.

  * **Pros:** Solved the "API Hotspot" problem by fanning out DB definitions. Enabled true Kubernetes-native multi-tenancy via RBAC. provided a clean, declarative target for DDL translation.
  * **Cons:** Introduced additional CRDs.
  * **Rejected Because:** The client preferred a Postgres-native "System Catalog" approach where the database state is the primary source of truth, rejecting the separation of the "DBA persona" and the additional CRDs.

### Alternative 4: Helm Charts Instead of CRDs

This approach would use Helm charts to deploy Multigres components without an operator.

  * **Pros:** Familiar deployment model for many Kubernetes users.
  * **Cons:** No automatic reconciliation, no custom status reporting, and no active lifecycle management (failover, scaling, etc.).
  * **Rejected Because:** The operator pattern provides superior lifecycle management, observability, and automation, which are critical for a stateful database system.
# MultigresCluster API v1alpha1

## Summary

This proposal defines the `v1alpha1` API for the Multigres Operator. The design is centered on a root `MultigresCluster` resource, which defines the core infrastructure, and a `MultigresDatabase` resource, which defines the logical database topology.

This new design separates the **logical** definition of a database (its tablegroups and sharding) from the **physical** definition (the compute, memory, and storage resources for its shards). This provides a clear separation of concerns between Platform/SREs (managing the cluster) and DBAs/Developers (managing the databases). It also enables us to define an agnostic database and sharding spec that could potentially be utilized in other platforms that are not Kubernetes.

1.  **`MultigresCluster`**: The root resource defining the desired state of the entire cluster's core infrastructure (control plane, cells, and the implicit system catalog database).
2.  **`CoreTemplate`**: A reusable, namespaced resource for defining standard configurations for core components.
3.  **`CellTemplate`**: A reusable, namespaced resource for defining standard configurations for Cell-level components.
4.  **`MultigresDatabase`**: A namespaced, user-managed resource that defines the **logical** topology of a single database. It acts as a "claim" on a `MultigresCluster`, enabling namespace-scoped RBAC.
5.  **`MultigresDatabaseResources`**: A namespaced, user-managed resource that defines the **physical** resource allocation (compute, storage, templates) for the shards of a specific `MultigresDatabase`.
6.  **`ShardTemplate`**: A reusable, namespaced resource for defining standard configurations for Shard-level components (`MultiOrch` and `Pools`).

The `MultigresCluster` owns core components (`TopoServer`, `Cell`). The `MultigresDatabase` CR becomes the owner of `TableGroup` CRs, which in turn own the `Shard` CRs.

## Motivation

Managing a distributed, sharded database system is complex. This revised parent/child "claim" model enhances the separation of concerns, providing distinct APIs for different personas and enabling new workflows if desired.

  * **Separation of Concerns (by Persona):**
      * **Platform (SRE):** Manages the `MultigresCluster` (cells, templates, core components) in a central namespace.
      * **Application (DBA):** Manages the `MultigresDatabase` (logical sharding) in their own application namespace.
      * **Infrastructure (SRE/Platform):** Manages the `MultigresDatabaseResources` (assigning physical resources to logical databases), also in the application namespace.
  * **DDL-driven Workflow:** This model enables a new DB creation flow where a `CREATE DATABASE` DDL command can be intercepted to automatically generate the `MultigresDatabase` CR, with a default `MultigresDatabaseResources` CR being created automatically by the operator.
  * **GitOps vs. DDL:** By generating and applying the CRs based on the imperative DDL, we can safely allow DDL-driven creation while still allowing GitOps to manage the underlying resource templates and cluster configuration, solving potential state conflicts.
  * **Scoped Reusability:** By splitting templates into `CoreTemplate`, `CellTemplate`, and `ShardTemplate`, we provide clear, reusable configurations.

## Proposal: API Architecture and Resource Topology

### 1\. Cluster Infrastructure Topology

This diagram shows what the `MultigresCluster` **directly owns and manages** as its core infrastructure.

```ascii
[MultigresCluster] 🚀 (Root CR - Manages Core Infrastructure)
      │
      ├── 🌍 [GlobalTopoServer] (Child CR)
      │
      ├── 🤖 MultiAdmin Resources (Child)
      │
      ├── 📇 SystemCatalog (Child - "Default" DB's Shard)
      │
      └── 💠 [Cell] (Child CR)
           │
           ├── 🚪 MultiGateway Resources
           └── 📡 [LocalTopoServer] (Child CR, optional)
```

### 2\. Database "Claim" Topology

This diagram shows the **decoupled "claim" model** for a user-created database. It shows how the database objects live in their own namespace and relate to each other and the cluster.

```ascii
Kubernetes Namespace: "platform-system"
+---------------------------------------+
| [MultigresCluster] 🚀                 |
|   (Watches for claims)                |
+---------------------------------------+
                    ^
                    │
(Operator resolves this "clusterRef")
                    │
Kubernetes Namespace: "app-team-1"
+---------------------------------------+
| [MultigresDatabase] 🗃️ (Logical "Claim") |
|   spec:                               |
|     clusterRef: "example-cluster"     |
+---------------------------------------+
      │                         │
      │ (Owns)                  │ (Referenced by)
      │                         │
      │                         +-> [MultigresDatabaseResources] 🖥️
      │                               spec:
      │                                 databaseRef: "production-db"
      │
      │ (Controller merges logical + physical)
      │
      └-> 💠 [TableGroup] (Child CR)
           │
           └── 📦 [Shard] (Child CR)
                │
                ├── 🧠 MultiOrch Resources
                └── 🏊 Pools (Postgres+MultiPooler)
```

-----

## DDL-driven vs. GitOps-driven Workflow

This API structure is designed to support two primary methods of database creation, which can coexist.

### The DDL DB Creation Flow (Platform Agnostic)

By default, MultigresCluster accepts DDL queries to create databases and the required infrastructure. This enables other users (e.g., DBAs or application developers) to create a database using SQL commands without defining infrastructure. It works like this:

1.  A user defines the **logical** topology via a DDL command:
    ```sql
    CREATE DATABASE "events" WITH SHARDING (TableGroup 'tg1' (Shard '0' (keyRange '0-80'), Shard '1' (keyRange '80-inf')));
    ```
2.  `MultiGateway` intercepts this command and converts it into a platform agnostic YAML spec and hands it to the operator to convert and apply into a  `MultigresDatabase` to the user's default namespace or whichever namespace matches their role.
3.  The `multigres-database-controller` sees the new `MultigresDatabase` CR.
4.  The controller **automatically creates** a corresponding `MultigresDatabaseResources` CR (e.g., `events-resources`) in the same namespace.
5.  Crucially, the controller reads `MultigresCluster.spec.templateDefaults.shardTemplate` (e.g., `"cluster-wide-shard"`) and populates `MultigresDatabaseResources.spec.defaultShardTemplate` with this value.
6.  A separate `multigres-resource-controller` sees the `MultigresDatabaseResources` CR, merges it with the `MultigresDatabase` CR, and reconciles the `TableGroup` and `Shard` child resources, provisioning pods and storage.
7.  Once the physical resources are ready, the operator signals `MultiAdmin` (or whichever component we end up deciding on in the end) to run the final DDL to make the logical database available. This must also be state driven so we don't do a fire and forget and then the logical DB is not created.

### Resource Upgrades (Separation of Duties)

Removing resource definitions from the `MultigresDatabase` CR creates a clear separation of duties. A user (e.g., an SRE) with permission to edit `MultigresDatabaseResources` can apply a change to upgrade a database's resources without touching the `MultigresDatabase` CR that a DBA might own.

### Solving GitOps vs. DDL Chaos

This separation allows DDL-created and Git-created databases to live in the same cluster. SREs can still create databases declaratively by applying `MultigresDatabase` and `MultigresDatabaseResources` CRs via Git.

For DDL-driven databases, we can advise users to add an annotation to the `MultigresDatabase` CR. This tells a GitOps tool like Argo CD to ignore the resource, preventing it from being altered if it's not in Git. We could allow the defaulting these annotations in MultigresCluster too.

> ```yaml
> apiVersion: multigres.com/v1alpha1
> kind: MultigresDatabase
> metadata:
>   name: "prod-analytics-db"
>   annotations:
>     # Tells Argo CD to "hands off"
>     "argocd.argoproj.io/ignore-resource": "true" 
> spec:
>   ...
> ```

-----

## Design Details: API Specification

### User Managed CR: MultigresCluster

  * This CR and the three scoped templates (`CoreTemplate`, `CellTemplate`, `ShardTemplate`) are the primary editable entries for the **Platform Team**.
  * This CR no longer contains `spec.databases`. Instead, it defines the `spec.systemCatalog` which is the implicit "default database". This default database does not create a `MultigresDatabase` CR inside the cluster and cannot be deleted.

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: example-cluster
  # Typically lives in a central 'platform-system' namespace
  namespace: platform-system
spec:
  # ----------------------------------------------------------------
  # Global Images Cluster Configuration
  # ----------------------------------------------------------------
  images:
    imagePullPolicy: "IfNotPresent"
    imagePullSecrets:
      - name: "my-registry-secret"
    multigateway: "multigres/gateway:latest"
    multiorch: "multigres/orch:latest"
    multipooler: "multigres/pooler:latest"
    multiadmin: "multigres/admin:latest"
    postgres: "postgres:15.3"
    # NOTE: etcd image is NOT defined here. It is defined
    # in the 'managedSpec' of the toposerver components.

  # ----------------------------------------------------------------
  # Cluster-Wide Policies
  # ----------------------------------------------------------------
  policy:
    # Set to true to allow 'CREATE DATABASE' DDL
    # to automatically create MultigresDatabase CRs.
    # If false, DDL commands will be rejected.
    # This would need to trigger a configuration within MultiGateway to explicitly disable these DDLs
    # Default is true.
    enableDDLDatabaseCreation: true

    # Default annotations to add to DDL-created
    # MultigresDatabase CRs to protect them from GitOps tools.
    defaultDatabaseAnnotations:
      "argocd.argoproj.io/ignore-resource": "true"
      # "another-tool.com/hands-off": "true"

    # Policy specifying what namespaces should be used to deploy and watch for DBs
    # ONLY namespaces specified here are watched by the controller
    databaseNamespaces:
      # A list of mappings from a Postgres ROLE (group)
      # to a target Kubernetes namespace.
      # This tells the operator on what namespace the databases should be created depending on the role that creates them.
      # The first match for the user's role is used.
      roleToNamespace:
        - role: "analytics_team"
          namespace: "analytics-prod"
        - role: "payments_team"
          namespace: "payments-prod"
        - role: "engineering_team"
          namespace: "engineering-dev"
      
      # The default namespace to use if a user's role
      # does not match any of the mappings above.
      fallbackNamespace: "default-db-claims"

  # ----------------------------------------------------------------
  # Cluster-Level Template Defaults
  # ----------------------------------------------------------------
  # These are the default templates to use for any component that
  # does not have an inline 'spec' or an explicit 'templateRef'.
  templateDefaults:
    coreTemplate: "cluster-wide-core"
    cellTemplate: "cluster-wide-cell"
    # This is the default ShardTemplate used when a
    # MultigresDatabase is created via DDL. The operator
    # will copy this name into the auto-created
    # MultigresDatabaseResources.spec.defaultShardTemplate field.
    shardTemplate: "cluster-wide-shard"

  # ----------------------------------------------------------------
  # Global Components
  # ----------------------------------------------------------------
  globalTopoServer:
    # --- OPTION 1: Inline Managed Spec ---
    managedSpec:
      image: "quay.io/coreos/etcd:v3.5"
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

  multiadmin:
    # --- OPTION 1: Inline Managed Spec ---
    managedSpec:
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
  # System Catalog (Implicit "Default Database")
  # ----------------------------------------------------------------
  # This is the bootstrapped, hidden "default" database that stores
  # the list of all other databases and tablegroups.
  # It is NOT a user database and does NOT get a MultigresDatabase CR.
  systemCatalog:
    # This spec defines the physical resources for the
    # "default" database's single shard. It can use a template
    # or an inline spec.
    shardTemplate: "system-catalog-shard"
    overrides:
      pools:
        primary:
          cell: "us-east-1a" # Must be pinned to a cell

  # ----------------------------------------------------------------
  # Cells Configuration
  # ----------------------------------------------------------------
  cells:
    - name: "us-east-1a"
      zone: "us-east-1a" 
      cellTemplate: "standard-cell-ha"
    
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

  # ----------------------------------------------------------------
  # Database Topology (REMOVED)
  # ----------------------------------------------------------------
  # spec.databases has been REMOVED from this resource.
  # This is now defined in separate `MultigresDatabase`
  # and `MultigresDatabaseResources` CRs in user namespaces.

status:
  observedGeneration: 1
  conditions:
    - type: Available
      status: "True"
      message: "All core components are available."
  cells:
    us-east-1a: 
      ready: True
      gatewayReplicas: 3
    us-east-1b: 
      ready: True
      gatewayReplicas: 2
```

### User Managed CR: CoreTemplate

  * The `etcd` image is now defined within the `globalTopoServer.managedSpec`.

```yaml
apiVersion: multigres.com/v1alpha1
kind: CoreTemplate
metadata:
  name: "standard-core-ha"
  namespace: platform-system
spec:
  # Defines the Global TopoServer component
  globalTopoServer:
    # --- OPTION 1: Managed by Operator ---
    managedSpec:
      image: "quay.io/coreos/etcd:v3.5"
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
    managedSpec:
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

  * The `etcd` image is now defined within the `localTopoServer.managedSpec`.

```yaml
apiVersion: multigres.com/v1alpha1
kind: CellTemplate
metadata:
  name: "standard-cell-ha"
  namespace: platform-system
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
  localTopoServer:
    managedSpec:
      image: "quay.io/coreos/etcd:v3.5"
      replicas: 3
      storage:
        class: "standard-gp3"
        size: "5Gi"
      resources:
        requests:
          cpu: "500m"
          memory: "1Gi"
        limits:
          cpu: "1"
          memory: "2Gi"
```

### User Managed CR: MultigresDatabase

  * This new CR defines the **logical** structure of a database.
  * `keyRange` is defined at the `shards` level.

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresDatabase
metadata:
  name: "production-db"
  namespace: app-team-1
  annotations:
    # Allows DDL-driven DBs to coexist with GitOps
    "argocd.argoproj.io/ignore-resource": "true" 
spec:
  # Reference to the MultigresCluster (can be in another namespace)
  clusterRef:
    name: "example-cluster"
    namespace: "platform-system"
  
  # The logical database name
  databaseName: "production_db"

  # Defines the logical sharding layout
  tablegroups:
    - name: "orders_tg"
      # The tablegroup itself is just a logical container
      # Shards define the key ranges
      shards:
        - name: "0"
          keyRange:
            start: "0"
            end: "80"
        - name: "1"
          keyRange:
            start: "80"
            end: "inf"
    
    - name: "users_tg"
      # This tablegroup has a single shard, covering the whole range
      shards:
        - name: "0"
          keyRange:
            start: "0"
            end: "inf"
```

### User Managed CR: MultigresDatabaseResources

  * This new CR provides the **physical** specification for a `MultigresDatabase`.
  * Comments are updated to clarify the `defaultShardTemplate` workflow.
  * These live within the same namespace as `MultigresDatabase`. Platform teams that want a clean separation of responsibilities may want to deny editing this, or add resourcing limits at namespace level.

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresDatabaseResources
metadata:
  # Often named after the database it supports
  name: "production-db-resources"
  namespace: app-team-1
spec:
  # Reference to the logical database in the same namespace
  databaseRef: "production-db"
  
  # Default template for all shards in this DB that
  # do not have an explicit 'shardTemplate' defined below.
  #
  # NOTE: When this CR is auto-created by the DDL workflow,
  # this field will be populated from
  # 'MultigresCluster.spec.templateDefaults.shardTemplate'.
  #
  # An SRE/Infra user can override that default by
  # setting this field explicitly.
  defaultShardTemplate: "standard-shard-ha"
  
  # The physical specs, mirroring the logical structure.
  tablegroups:
    - name: "orders_tg" # MUST match name in MultigresDatabase
      shards:
        # --- SHARD 0: Using an Explicit Template ---
        - name: "0" # MUST match name in MultigresDatabase
          shardTemplate: "high-cpu-shard"
          overrides:
             pools:
               primary:
                 cell: "us-east-1a"
        
        # --- SHARD 1: Using Inline Spec ---
        - name: "1"
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
                  resources:
                    requests:
                      cpu: "2"
                      memory: "4Gi"
                    limits:
                      cpu: "4"
                      memory: "8Gi"
                multipooler:
                  resources:
                    requests:
                      cpu: "500m"
                      memory: "1Gi"
                    limits:
                      cpu: "1"
                      memory: "2Gi"
    
    - name: "users_tg"
      shards:
        # --- SHARD 0: Using Database Default Template ---
        - name: "0"
          # 'spec' and 'shardTemplate' are omitted.
          # This will use 'spec.defaultShardTemplate' ("standard-shard-ha")
          overrides:
             pools:
               primary:
                 cell: "us-east-1a"
```

### User Managed CR: ShardTemplate

  * This CR is **unchanged**. It defines the "shape" of a shard: its orchestration and its data pools.

```yaml
apiVersion: multigres.com/v1alpha1
kind: ShardTemplate
metadata:
  name: "standard-shard-ha"
  namespace: platform-system
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
      # 'cell' MUST be overridden in MultigresDatabaseResources
      # if left empty here.
      cell: "" 
      replicas: 2
      storage:
        class: "standard-gp3"
        size: "100Gi"
      postgres:
        resources:
          requests:
            cpu: "2"
            memory: "4Gi"
          limits:
            cpu: "4"
            memory: "8Gi"
      multipooler:
        resources:
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
         resources:
           requests:
             cpu: "1"
             memory: "2Gi"
           limits:
             cpu: "2"
             memory: "4Gi"
      multipooler:
         resources:
           requests:
             cpu: "500m"
             memory: "512Mi"
           limits:
             cpu: "1"
             memory: "1Gi"
```

-----

### Child Resources (Read-Only)


### Child CR: TopoServer

  * Applies to both Global (owned by `MultigresCluster`) and Local (owned by `Cell`) topology servers.
  * This CR does *not* exist if the user configures an `external` etcd connection in the parent.

```yaml
apiVersion: multigres.com/v1alpha1
kind: TopoServer
metadata:
  name: "example-cluster-global"
  namespace: platform-system
  ownerReferences:
    - apiVersion: multigres.com/v1alpha1
      kind: MultigresCluster
      name: "example-cluster"
      controller: true
spec:
  # Fully resolved 'managedSpec' from MultigresCluster inline definition
  image: "quay.io/coreos/etcd:v3.5"
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
status:
  conditions:
    - type: Available
      status: "True"
      lastTransitionTime: "2025-11-07T12:01:00Z"
  clientService: "example-cluster-global-client"
  peerService: "example-cluster-global-peer"
```

-----

### Child CR: Cell

  * Owned by `MultigresCluster`.
  * Strictly contains `MultiGateway` and optional `LocalTopoServer`.
  * The `allCells` field is used for discovery by gateways.

```yaml
apiVersion: multigres.com/v1alpha1
kind: Cell
metadata:
  name: "example-cluster-us-east-1a"
  namespace: platform-system
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

  * Owned by `MultigresDatabase` (no longer `MultigresCluster`).
  * Its `spec` is populated by a controller that **merges** `MultigresDatabase` (logical) and `MultigresDatabaseResources` (physical).
  * `keyRange` is no longer here; it's now part of the logical spec in the `shards` list.

```yaml
apiVersion: multigres.com/v1alpha1
kind: TableGroup
metadata:
  name: "production-db-orders-tg"
  namespace: app-team-1
  labels:
    multigres.com/database: "production_db"
    multigres.com/tablegroup: "orders_tg"
  ownerReferences:
    - apiVersion: multigres.com/v1alpha1
      kind: MultigresDatabase
      name: "production-db"
      controller: true
spec:
  databaseName: "production_db"
  tableGroupName: "orders_tg"

  # --- Logical & Physical Spec (Merged by Controller) ---
  # This is the list of FULLY RESOLVED shard specifications.
  shards:
    - name: "0"
      # --- Logical Spec (from MultigresDatabase) ---
      keyRange:
        start: "0"
        end: "80"
      # --- Physical Spec (from MultigresDatabaseResources) ---
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
              resources:
                requests:
                  cpu: "2"
                  memory: "4Gi"
                limits:
                  cpu: "4"
                  memory: "8Gi"
          multipooler:
              resources:
                requests:
                  cpu: "1"
                  memory: "512Mi"
                limits:
                  cpu: "2"
                  memory: "1Gi"
    
    - name: "1"
      # --- Logical Spec (from MultigresDatabase) ---
      keyRange:
        start: "80"
        end: "inf"
      # --- Physical Spec (from MultigresDatabaseResources) ---
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
              resources:
                requests:
                  cpu: "2"
                  memory: "4Gi"
                limits:
                  cpu: "4"
                  memory: "8Gi"
          multipooler:
              resources:
                requests:
                  cpu: "500m"
                  memory: "1Gi"
                limits:
                  cpu: "1"
                  memory: "2Gi"
        
status:
  readyShards: 2
  totalShards: 2
```

-----

### Child CR: Shard

  * Owned by `TableGroup`.
  * Now contains `MultiOrch` (Raft leader helper) AND `Pools` (actual data nodes).
  * Represents one entry from the `TableGroup` shards list.

```yaml
apiVersion: multigres.com/v1alpha1
kind: Shard
metadata:
  name: "production-db-orders-tg-0"
  namespace: app-team-1
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
  # 'shardName' and 'keyRange' are passed down
  # from the parent TableGroup spec.
  shardName: "0"
  keyRange:
    start: "0"
    end: "80"
  
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
           resources:
             requests:
               cpu: "2"
               memory: "4Gi"
             limits:
               cpu: "4"
               memory: "8Gi"
      multipooler:
           resources:
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
## Defaults via Webhooks

### Cluster Component Override Chain (4-Level)

This logic is applied consistently by the webhook for `GlobalTopoServer`, `MultiAdmin`, and `Cells`:

1.  **Component-Level Definition (Highest):** An inline `spec` or explicit `templateRef` on the component in the `MultigresCluster` CR.
2.  **Cluster-Level Default Template:** The corresponding template in `MultigresCluster.spec.templateDefaults` (e.g., `templateDefaults.coreTemplate`).
3.  **Namespace-Level Default Template:** A template of the correct kind (e.g., `CoreTemplate`) named `default` in the cluster's namespace.
4.  **Operator Hardcoded Defaults (Lowest):** A final fallback applied by the operator.

### Shard Override Chain (4-Level)

This logic is applied by the controller when resolving the `TableGroup` spec:

1.  **Shard-Level Definition (Highest):** An inline `spec` or explicit `shardTemplate` on the entry in `MultigresDatabaseResources.spec.tablegroups.shards`.
2.  **Database-Level Default Template:** The `spec.defaultShardTemplate` field in the `MultigresDatabaseResources` CR.
3.  **Namespace-Level Default Template:** A `ShardTemplate` named `default` in the same namespace as the `MultigresDatabaseResources` CR.
4.  **Operator Hardcoded Defaults (Lowest):** A final fallback applied by the operator.


## Webhook Validations and Controller Logic

This section outlines the separation of duties between synchronous webhooks and asynchronous controller logic (finalizers and status) to ensure a robust, non-blocking, and declarative API.

### 1. Synchronous Validating Webhooks

Webhooks are used *only* for fast, synchronous, and semantic validation.

#### `MultigresCluster`
* **On `CREATE` and `UPDATE`:**
    * **Internal Consistency:** Validates that `spec.systemCatalog.overrides.pools.primary.cell` matches a `name` in the `spec.cells` list.
    * **Template Existence:** Validates that templates named in `spec.templateDefaults` (e.g., `coreTemplate`) exist *at the time of creation/update*. This is an acceptable synchronous check as platform components are often bootstrapped together.
    * **Component Spec:** Enforces that `managedSpec`, `external`, and `templateRef` are mutually exclusive.
    * **Uniqueness:** Validates that all `name` entries in `spec.cells` are unique.
    * **Note:** Namespace existence checks are **not** performed by the webhook. This is handled by the controller's reconcile loop to support declarative bootstrapping (e.g., `helm install` or `kubectl apply -f .`).

#### `MultigresDatabase`
* **On `CREATE` and `UPDATE`:**
    * **Cluster Reference:** Verifies that the `MultigresCluster` specified in `spec.clusterRef` (e.g., `platform-system/example-cluster`) exists.
    * **Namespace Policy:** Verifies that this `MultigresDatabase` is being created in a namespace that is "allowed" by the target `MultigresCluster`'s `spec.policy.databaseNamespaces` list.
    * **Topology Validation:** Verifies that `tablegroups` and `shards` names are unique within the spec and that `keyRange` values are valid.

#### `MultigresDatabaseResources`
* **On `CREATE` and `UPDATE`:**
    * **Database Reference:** Verifies that the `MultigresDatabase` named in `spec.databaseRef` exists **in the same namespace**.
    * **Topology Consistency:** Performs a critical check by fetching the referenced `MultigresDatabase` and ensuring the `tablegroups` and `shards` defined here are a perfect subset of the logical topology. It will reject if this CR defines a physical resource for a logical shard that doesn't exist.
    * **Template/Cell Existence:** Verifies that the `ShardTemplate` in `spec.defaultShardTemplate` (and any per-shard `shardTemplate`) exists. It also verifies any `cell` name in a `pools` override exists on the target `MultigresCluster`.

---

### 2. Asynchronous Controller and Finalizer Logic

Asynchronous logic (deletion protection, external dependencies) is handled by controllers and finalizers.

#### `MultigresCluster`
* **Namespace Existence (Controller Status):** The `MultigresCluster` controller (not the webhook) reconciles `spec.policy.databaseNamespaces`. If a listed namespace is not found, the controller will update its *own status* with a Condition (e.g., `Type: "PolicyReady", Status: "False", Reason: "MissingNamespace"`) and requeue. It will not block.
* **Deletion Protection (Finalizer):**
    1.  The `MultigresCluster` controller adds a **finalizer** (e.g., `multigres.com/database-claims-check`) to its own CR.
    2.  When a user deletes the `MultigresCluster`, the `DeletionTimestamp` is set.
    3.  The controller's reconcile loop sees this and LISTs all `MultigresDatabase` CRs in the cluster.
    4.  If claims exist, the controller updates the `MultigresCluster.status` with a "Terminating" Condition and requeues. It **does not** remove the finalizer.
    5.  Only when all claims are gone does the controller remove its finalizer, allowing Kubernetes to complete the deletion.

#### `MultigresDatabaseResources`
* **Deletion Protection (Owner Reference):** This CR's deletion is handled by standard Kubernetes garbage collection. The controller that creates the `MultigresDatabaseResources` **must** set an `ownerReference` pointing to its corresponding `MultigresDatabase`. When the user deletes the `MultigresDatabase`, Kubernetes will automatically and correctly cascade-delete this `MultigresDatabaseResources` CR and its children (`TableGroup`, `Shard`).

#### `CoreTemplate`, `CellTemplate`, `ShardTemplate`
* **Deletion Protection (Finalizer):**
    1.  These templates are protected by a "fan-out" finalizer pattern.
    2.  When a `MultigresCluster` or `MultigresDatabaseResources` controller *uses* a template (e.g., "prod-cell"), its reconcile loop **adds a finalizer** (e.g., `multigres.com/in-use-by: example-cluster`) *to the `CellTemplate` object*.
    3.  When the `MultigresCluster` is deleted (or updated to no longer use "prod-cell"), its cleanup logic **removes its own finalizer** *from the `CellTemplate` object*.
    4.  Kubernetes will only be able to delete the `CellTemplate` after all controllers that were using it have removed their finalizers. This prevents deleting a template that is actively in use.

----

## End-User Examples

### 1\. The Ultra-Minimalist (Relying on Namespace/Webhook Defaults)

This user creates the smallest possible cluster and database. All values are defaulted by the operator's webhook or by templates named `default` in the namespace.

**SRE applies the cluster:**

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: minimal-cluster
  namespace: platform-system
spec:
  # All core components (globalTopoServer, multiadmin)
  # will use the 'CoreTemplate' named 'default' in this
  # namespace, or be defaulted by the webhook.
  
  # The system catalog will use the 'ShardTemplate'
  # named 'default' or be defaulted by the webhook.
  # It MUST be pinned to a cell.
  systemCatalog:
    overrides:
      pools:
        primary:
          cell: "us-east-1a"

  # At least one cell is required.
  cells:
    - name: "us-east-1a"
      zone: "us-east-1a"
      # This cell will use the 'CellTemplate' named
      # 'default' or be defaulted by the webhook.
```

**DBA applies the database (e.g., in `app-prod` namespace):**
This could also be auto-generated from a `CREATE DATABASE` DDL command.

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresDatabase
metadata:
  name: "analytics-db"
  namespace: app-prod
spec:
  clusterRef:
    name: "minimal-cluster"
    namespace: "platform-system"
  databaseName: "analytics"
  tablegroups:
    - name: "events"
      shards:
        - name: "0" # A single logical shard
          keyRange:
            start: "0"
            end: "inf"
```

**What happens:**

1.  The operator sees `analytics-db`.
2.  It auto-creates `MultigresDatabaseResources` named `analytics-db-resources`.
3.  It populates `defaultShardTemplate` by first looking for `minimal-cluster.templateDefaults.shardTemplate`. Since that's missing, it looks for a `ShardTemplate` named `default` in the `app-prod` namespace. If that's missing, it uses the operator's hardcoded default.
4.  The DBA must edit `analytics-db-resources` to add the required `overrides` (like `cell`), or the operator will report an error.

### 2\. The Minimalist (Relying on Cluster Defaults)

This user relies on the `spec.templateDefaults` field to set cluster-wide defaults.

**SRE applies the cluster:**

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: minimal-cluster-with-defaults
  namespace: platform-system
spec:
  images:
    imagePullPolicy: "IfNotPresent"
    multigateway: "multigres/gateway:latest"
    multiorch: "multigres/orch:latest"
    multipooler: "multigres/pooler:latest"
    multiadmin: "multigres/admin:latest"
    postgres: "postgres:15.3"
  
  templateDefaults:
    coreTemplate: "dev-defaults-core"
    cellTemplate: "dev-defaults-cell"
    # This is the cluster-wide default for DDL
    shardTemplate: "dev-defaults-shard"

  globalTopoServer:
    # Omitted, will use 'dev-defaults-core' template
  multiadmin:
    # Omitted, will use 'dev-defaults-core' template

  systemCatalog:
    shardTemplate: "dev-defaults-shard"
    overrides:
      pools:
        primary:
          cell: "us-east-1a"

  cells:
    - name: "us-east-1a"
      zone: "us-east-1a"
      # Omitted, will use 'dev-defaults-cell' template
```

**DBA applies the database (e.g., in `app-prod` namespace):**

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresDatabase
metadata:
  name: "analytics-db"
  namespace: app-prod
spec:
  clusterRef:
    name: "minimal-cluster-with-defaults"
    namespace: "platform-system"
  databaseName: "analytics"
  tablegroups:
    - name: "events"
      shards:
        - name: "0"
          keyRange:
            start: "0"
            end: "inf"
```

**What happens:**

1.  Operator auto-creates `analytics-db-resources`.
2.  It reads `minimal-cluster-with-defaults.templateDefaults.shardTemplate` and sets `analytics-db-resources.spec.defaultShardTemplate` to `"dev-defaults-shard"`.
3.  The DBA still needs to edit `analytics-db-resources` to add the `overrides` for the `cell`.

### 3\. The Power User (Explicit Overrides)

The SRE provides the cluster. The DBA defines their logical DB, and the SRE (or a platform-savvy DBA) defines the physical resources explicitly.

**DBA applies the logical database:**

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresDatabase
metadata:
  name: "users-db"
  namespace: app-prod
spec:
  clusterRef:
    name: "prod-cluster"
    namespace: "platform-system"
  databaseName: "users"
  tablegroups:
    - name: "users_tg"
      shards:
        - name: "0"
          keyRange:
            start: "0"
            end: "100"
        - name: "1"
          keyRange:
            start: "100"
            end: "inf"
```

**SRE/DBA applies the physical resources:**

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresDatabaseResources
metadata:
  name: "users-db-resources"
  namespace: app-prod
spec:
  databaseRef: "users-db"
  # This SRE explicitly sets the default for this DB,
  # overriding the cluster-wide default.
  defaultShardTemplate: "standard-shard"
  
  tablegroups:
    - name: "users_tg"
      shards:
        # Shard 0: Use an explicit template with overrides
        - name: "0"
          shardTemplate: "high-mem-shard"
          overrides:
            pools:
              primary:
                cell: "us-east-1a"
                storage:
                  size: "500Gi" # Atomic override for storage
                  class: "io1"
        
        # Shard 1: Use the 'defaultShardTemplate' from this CR
        - name: "1"
          # No 'shardTemplate', uses 'defaultShardTemplate' ("standard-shard")
          overrides:
            pools:
              primary:
                cell: "us-west-2a"
```

## Implementation History

* **2025-10-08:** Initial proposal to create individual, user-managed CRDs for each component (`MultiGateway`, `MultiOrch`, etc.).
* **2025-10-14:** A second proposal introduced a top-level `MultigresCluster` CR as the primary user-facing API.
* **2025-10-28:** The "parent/child" model was formalized, designating `MultigresCluster` as the single source of truth with read-only children.
* **2025-11-05:** Explored a simplified V1 API limited to a single shard. Rejected to ensure the API is ready for multi-shard from day one.
* **2025-11-06:** Explored a single "all-in-one" `DeploymentTemplate`. Rejected due to N:1 conflicts when trying to apply one template to both singular Cell components and multiplied Shard components.
* **2025-11-07:** Finalized the "Scoped Template" model (`CellTemplate` & `ShardTemplate`) and restored full explicit `database` -> `tablegroup` -> `shard` hierarchy.
* **2025-11-10:** Refactored `pools` to use Maps instead of Lists and introduced atomic grouping for `resources` and `storage` to ensure safer template overrides.
* **2025-11-11:** Introduced a consistent 4-level override chain (inline/explicit-template -> cluster-default -> namespace-default -> webhook) for all components. Added `CoreTemplate` CRD and `spec.templateDefaults` block to support this. Reverted `spec.coreComponents` nesting to top-level `globalTopoServer` and `multiadmin` fields.
* **2025-11-14:** Decoupled database definitions from the core cluster to support DDL-driven workflows and multi-tenancy. This involved removing `spec.databases` from `MultigresCluster` and introducing the new `MultigresDatabase` (logical "claim") and `MultigresDatabaseResources` (physical spec) CRDs. Added `spec.systemCatalog` to `MultigresCluster` to explicitly manage the implicit "default database" for metadata.

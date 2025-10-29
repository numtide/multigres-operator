---
title: MultigresCluster API with Child Resources based on Multigres Architecture
state: ready
---

## Summary

This proposal defines the `v1alpha1` API for the Multigres Operator. The design is centered on two user-editable Custom Resources (CRs):

1.  **`MultigresCluster`**: The root resource that defines the desired state (intent) of the entire cluster.
2.  **`DeploymentTemplate`**: A reusable, namespaced resource for defining common component configurations (e.g., "standard-ha", "analytics-workload").

All other resources (`TopoServer`, `Cell`, `TableGroup`, `Shard`) are implemented as read-only child CRs owned by the `MultigresCluster`. These child CRs reflect the *realized state* of the system and are managed by their own dedicated controllers. This enables a clean, hierarchical, and observable architecture.

## Motivation

Managing a distributed, sharded database system across multiple failure domains (cells) is inherently complex. A single, monolithic CRD for this task would be unmanageable, difficult to debug, and lead to a complex, monolithic controller.

This proposal advocates for a parent/child CRD model to address these challenges. The primary motivations for this design are:

  * **Separation of Concerns:** Splitting logic into child CRs (`Cell`, `TableGroup`, `Shard`) results in simple, specialized controllers that are easier to build, test, and maintain.
  * **Hierarchical Observability:** This model provides a clean, hierarchical tree for status aggregation. A failure in a specific `Shard` can be clearly observed on the `Shard` CR, its `TableGroup` parent, and finally aggregated up to the root `MultigresCluster` status.
  * **Efficient, Event-Driven Reconciliation:** This architecture enables cascading reconciliation. A change to the `MultigresCluster` only triggers its controller, which may then update a `Cell` child CR. This, in turn, triggers the `Cell` controller, and so on. Only the specific controller for a changed resource needs to run.
  * **Idempotency and Single Source of Intent:** By making the `MultigresCluster` the single editable source of truth, we prevent unstable conflicts. Any manual edits to read-only child CRs are automatically reverted by their controller, ensuring the cluster's *realized state* always converges with the parent CR's *desired state*.

## Goals

  * Provide a declarative, Kubernetes-native API for deploying a multi-cell, sharded Multigres cluster.
  * Separate user-facing *intent* (`MultigresCluster`) from operator-managed *realized state* (child CRs).
  * Enable configuration reuse across components and clusters via a `DeploymentTemplate` resource.
  * Establish a clear, hierarchical ownership model using `OwnerReferences` for clean garbage collection and status aggregation.
  * Define a clear API contract for managing both global and cell-local topology servers (etcd), including external, unmanaged instances.

## Non-Goals

  * This document does not define the specific implementation details of each controller's reconciliation logic.
  * This document does not cover Day 2 operations such as database backup, restore, monitoring, or alerting. These will be addressed in future proposals.
  * This document does not cover automated, in-place version upgrades, though the global `images` spec provides a foundation for this.

## Proposal: API Architecture and Resource Topology

  * The operator can create a managed global etcd topology server and/or a managed local topology server. The same `TopoServer` CRD is used for both. The global `TopoServer` will belong to the `MultigresCluster` CR directly, whereas the local `TopoServer` belongs to the `Cell` CR.
  * A user can choose to point the cluster to an external etcd topology server, in which case the `TopoServer` resource will not be provisioned.
  * If no local topology server is configured for a cell, it will use the global topology server by default.


```ascii
[MultigresCluster] 🚀 (The root CR - user-editable)
      │
      ├── 🌍 [GlobalTopoServer] (Child CR if managed)
      │    │
      │    └── 🏛️ etcd Resources (if managed)
      │
      ├── 🤖 MultiAdmin Resources - Deployment, Services, Etc
      │    
      │
      ├── 💠 [Cell] (Child CR)
      │    │
      │    ├── 🚪 MultiGateway Resources (Deployment, Service, etc.)
      │    │    
      │    │
      │    ├── 🧠 MultiOrch Resources (Deployment, etc.)
      │    │    
      │    │
      │    └── 📡 [LocalTopoServer] (Child CR if managed and not using global)
      │         │
      │         └── 🏛️ etcd Resources (if managed)
      │
      └── 🗃️ [TableGroup] (Child CR)
           │
           └── 📦 [Shard] (Child CR)
                │
                └── 🏊 MultiPooler and postgres resources (pods or statefulset)
                    


📋 [DeploymentTemplate] (Separate CR - user-editable, NOT a child)
   ├── Contains spec sections for:
   │   ├── multiadmin
   │   ├── multigateway
   │   ├── multiorch
   │   ├── shardPool
   │   └── managedTopoServer
   │
   └── Watched by MultigresCluster controller ONLY when referenced
       └── Resolved into child CRs (children are unaware of templates)
```

## Design Details: API Specification

This section provides the comprehensive definitions and examples for each Custom Resource.

### User Managed CR: MultigresCluster

  * This and the `DeploymentTemplate` are the only two editable entries for the end-user. All other child CRs will be owned by this top-level CR, and any manual changes to those child CRs will be reverted as the operator reconciles based on the top-level CR definition.
  * Every field that uses a `deploymentTemplate` comes with an `override` option.
  * Images are defined globally to avoid the danger of running multiple incongruent versions at once. This implies the operator will handle upgrades. Future iterations may allow defining versions across the cluster to provide more flexibility.
  * Users can configure this CR directly (inline) or by using deployment templates and overrides.
  * The `DeploymentTemplates` are only used in the `MultigresCluster` CR. When users view a child read-only CR, they will see a resolved version of the `deploymentTemplate`.
  * This CR is essentially a composition of its child CRs. The configuration blocks seen below will be split into their own children read-only CRs with their own controllers.
  * The `MultigresCluster` also manages its own resources (non-CRs), in particular `MultiAdmin`.
  * The `MultigresCluster` does not create its grandchildren; for example, the shard configuration is passed on to the `TableGroup` CR, which then creates its own children `Shard` CRs.


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

  # Optional
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

  # Optional
  globalTopoServer:
    rootPath: "/multigres/global"
    deploymentTemplate: "standard-ha"
    # --- ALTERNATIVE: Inline Definition ---
    # If 'deploymentTemplate' is omitted, the controller uses this spec directly.
    # managedSpec:
    #   image: quay.io/coreos/etcd:v3.5.17
    #   replicas: 3
    #   dataVolumeClaimTemplate:
    #     accessModes: ["ReadWriteOnce"]
    #     resources:
    #       requests:
    #         storage: "10Gi"
    # When external is defined, no topoServer CR is created
    # external:
    #   address: "my-external-etcd-client.etcd.svc:2379"

  # ----------------------------------------------------------------
  # MultiAdmin Configuration
  # ----------------------------------------------------------------

  # Optional
  multiadmin:
    # This tells the controller to fetch the 'multiadmin' section
    # from the 'standard-ha' DeploymentTemplate resource.
    deploymentTemplate: "standard-ha"
    # Optional overrides can be added here if needed
    # overrides:
    #   replicas: 2
    # --- ALTERNATIVE: Inline Definition ---
    # If 'deploymentTemplate' is omitted, the controller uses this spec directly.
    # replicas: 1
    # resources:
    #   requests:
    #     cpu: "100m"
    #     memory: "128Mi"
    #   limits:
    #     cpu: "200m"
    #     memory: "256Mi"

  # ----------------------------------------------------------------
  # Cells Configuration
  # ----------------------------------------------------------------
  
  # Optional 
  cells:
    - name: "us-east-1a"
      spec:
        # If no topoServer config is specified, it uses global by default
        multigateway:
          # This tells the controller to fetch the 'multiGateway' section
          # from the 'standard-ha' DeploymentTemplate resource.
          deploymentTemplate: "standard-ha"
          # Optional overrides can be added always to template
          overrides:
            resources:
              limits:
                cpu: "2"
        # --- ALTERNATIVE: Inline Definition ---
        # If 'deploymentTemplate' is omitted, the controller uses this spec directly.
        # replicas: 2
        # resources:
        #   requests:
        #     cpu: "500m"
        #     memory: "512Mi"
        #   limits:
        #     cpu: "1"
        #     memory: "1Gi"
        multiorch:
          # ---  Inline Definition ---
          # If 'deploymentTemplate' is omitted, the controller uses this spec directly.
          replicas: 1
          resources:
            requests:
              cpu: "100m"
              memory: "128Mi"
            limits:
              cpu: "200m"
              memory: "256Mi"

    - name: "us-east-1b"
      spec:
        multigateway:
          # Using the template for this cell as well
          deploymentTemplate: "standard-ha"
        multiorch:
          deploymentTemplate: "standard-ha"
        topoServer: # This cell uses a managed local topo server
          deploymentTemplate: "standard-ha"
          rootPath: "/multigres/us-east-1b"
          # --- ALTERNATIVE: Inline Definition ---
          # managedSpec:
          #   rootPath: "/multigres/us-east-1b"
          #   image: quay.io/coreos/etcd:v3.5.17
          #   replicas: 2
          #   dataVolumeClaimTemplate:
          #     accessModes: ["ReadWriteOnce"]
          #     resources:
          #       requests:
          #         storage: "5Gi"
        # You can specify external per cell as well or it takes global by default
        # external:
        #   address: "etcd-us-east-1a.my-domain.com:2379"
        #   rootPath: "/multigres/us-east-1a"

  # ----------------------------------------------------------------
  # TableGroup Configuration
  # ----------------------------------------------------------------

  # Optional
  databases:
    - name: "production_db"
      spec:
        tablegroups:
          # --- TABLEGROUP 1: Uses the 'shardPool' section from an external template ---
          - name: "default"
            partitioning:
              shards: 1
            # ----------------------------------------------------------------
            # Shard Configuration
            # ----------------------------------------------------------------
            shardTemplate:
              pools:
                - type: "replica"
                  cell: "us-east-1a"
                  # This name now refers to a DeploymentTemplate resource.
                  # The controller will fetch its 'shardPool' section.
                  deploymentTemplate: "default-ha"

          # --- TABLEGROUP 2: Uses other external templates ---
          - name: "orders_tg"
            partitioning:
              shards: 2
            # ----------------------------------------------------------------
            # Shard Configuration
            # ----------------------------------------------------------------
            shardTemplate:
              pools:
                - type: "replica"
                  cell: "us-east-1b"
                  deploymentTemplate: "orders-ha-replica" # Refers to another template CR

                - type: "readOnly"
                  cell: "us-east-1a"
                  deploymentTemplate: "orders-read-only" # Refers to another template CR

          # --- TABLEGROUP 3: Uses external template with overrides ---
          - name: "analytics_tg"
            partitioning:
              shards: 1
            shardTemplate:
            # ----------------------------------------------------------------
            # Shard Configuration
            # ----------------------------------------------------------------
              pools:
                - type: "replica"
                  cell: "us-east-1a"
                  deploymentTemplate: "default-ha" # Use template as base
                  # Overrides are applied *after* fetching the template spec
                  overrides:
                    dataVolumeClaimTemplate:
                      resources:
                        requests:
                          storage: "1000Gi"
                    postgres:
                      resources:
                        requests:
                          cpu: "4"
          
          # --- TABLEGROUP 4: Using inline (non-templated) definition ---
          # This demonstrates that inline definitions are still supported if
          # 'DeploymentTemplate' is omitted.
          - name: "custom_tg"
            partitioning:
              shards: 1
            shardTemplate:
            # ----------------------------------------------------------------
            # Shard Configuration
            # ----------------------------------------------------------------
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
                      limits:
                        cpu: "1"
                        memory: "2Gi"
                  multipooler:
                    resources:
                      requests:
                        cpu: "100m"
                        memory: "128Mi"
                      limits:
                        cpu: "200m"
                        memory: "256Mi"

# --- Status ---
status:
  observedGeneration: 1
  globalTopoServer:
    etcd:
      available: "True"
  conditions:
    - type: Available
      status: "True"
      lastTransitionTime: "2025-10-08T12:00:00Z"
      message: "All components are available."
    - type: Progressing
      status: "False"
      lastTransitionTime: "2025-10-08T12:00:00Z"
      message: "Cluster reconciliation complete."
  cells:
    us-east-1a:
      gatewayAvailable: "True"
      multiorchAvailable: "True" 
      topoServerAvailable: "True" # Assuming global is available (default)
    us-east-1b:
      gatewayAvailable: "True"
      multiorchAvailable: "True" 
      topoServerAvailable: "True" # Assuming managed becomes available
  databases:
    production_db:
      desiredInstances: 14 
      readyInstances: 14
      servingWrites: "True"
  multiadmin:
    available: "True"
    serviceName: "example-multigres-cluster-multiadmin"
```

### User Managed CR: DeploymentTemplate

  * This CR is not a child of any other resource. It's purely a configuration CR for `MultigresCluster`.
  * Incomplete config blocks will either error out or be completed with defaults or overrides if present.
  * All fields are optional, although at least one field is required for the creation of this resource.
  * When created, these templates are not watched or reconciled by any controller; they must first be referenced by at least one `MultigresCluster` CR to be reconciled.
  * They cannot be deleted if they are referenced by at least one `MultigresCluster` CR (enforced by a webhook).
  * The content of these templates is resolved by the `MultigresCluster` controller and used to configure its children CRs. A user can only see references to these templates on the `MultigresCluster`; they are not referenced by its children CRs. NOTE: A proposed idea here would be to add a status field in the children showing what template/s are using.
  * This resource is namespaced to support RBAC scoping for different teams (e.g., DBAs vs. application developers).
  * We initially had images as part of the DeploymentTemplate but we removed it to prevent users from thinking that multiple templates meant multiple image sets since this is not possible at the moment as images are a global resource (except for toposerver which is considered a separate resource)


```yaml
# This defines a reusable template named "standard-ha".
# It contains specifications for multiple component types.
apiVersion: multigres.com/v1alpha1
kind: DeploymentTemplate
metadata:
  name: "standard-ha"
  namespace: example
spec:
  # --- Template for Postgres/Multipooler Shard Pods ---
  shardPool:
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
        - weight: 50
          podAffinityTerm:
            labelSelector:
              matchLabels:
                app.kubernetes.io/component: shard-pool
            topologyKey: "topology.kubernetes.io/zone"
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
        limits:
          cpu: "4"
          memory: "8Gi"
    multipooler:
      resources:
        requests:
          cpu: "500m"
          memory: "256Mi"
        limits:
          cpu: "1"
          memory: "512Mi"

  # --- Template for MultiOrch ---
  multiorch:
    replicas: 1
    affinity:
      podAntiAffinity:
        preferredDuringSchedulingIgnoredDuringExecution:
        - weight: 100
          podAffinityTerm:
            labelSelector:
              matchLabels:
                app.kubernetes.io/component: multiorch
            topologyKey: "kubernetes.io/hostname"
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"
      limits:
        cpu: "200m"
        memory: "256Mi"

  # --- Template for MultiGateway ---
  multigateway:
    replicas: 2
    affinity:
      podAntiAffinity:
        preferredDuringSchedulingIgnoredDuringExecution:
        - weight: 100
          podAffinityTerm:
            labelSelector:
              matchLabels:
                app.kubernetes.io/component: multigateway
            topologyKey: "kubernetes.io/hostname"
        - weight: 50
          podAffinityTerm:
            labelSelector:
              matchLabels:
                app.kubernetes.io/component: multigateway
            topologyKey: "topology.kubernetes.io/zone"
    resources:
      requests:
        cpu: "500m"
        memory: "512Mi"
      limits:
        cpu: "1"
        memory: "1Gi"

  # --- Template for MultiAdmin ---
  multiadmin:
    replicas: 1
    affinity:
      podAntiAffinity:
        preferredDuringSchedulingIgnoredDuringExecution:
        - weight: 100
          podAffinityTerm:
            labelSelector:
              matchLabels:
                app.kubernetes.io/component: multiadmin
            topologyKey: "kubernetes.io/hostname"
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"
      limits:
        cpu: "200m"
        memory: "256Mi"

  managedTopoServer:
    image: quay.io/coreos/etcd:v3.5.17
    imagePullPolicy: "IfNotPresent"
    imagePullSecrets:
      - name: "my-registry-secret"
    replicas: 3
    affinity:
      podAntiAffinity:
        preferredDuringSchedulingIgnoredDuringExecution:
        - weight: 100
          podAffinityTerm:
            labelSelector:
              matchLabels:
                app.kubernetes.io/component: topo-server
            topologyKey: "kubernetes.io/hostname"
        - weight: 50
          podAffinityTerm:
            labelSelector:
              matchLabels:
                app.kubernetes.io/component: topo-server
            topologyKey: "topology.kubernetes.io/zone"
    dataVolumeClaimTemplate:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: "10Gi"
status:
  # This list is populated by the MultigresCluster controllers using it.
  # It shows which clusters are actively referencing this template.
  consumers:
    - name: "example-multigres-cluster"
      namespace: "example"
    - name: "other-cluster"
      namespace: "default"
```


### Child CRs of MultigresCluster

There are number of child CRs created from MultigresCluster.

#### Child CR of MultigresCluster OR Cell: TopoServer

 * This CR applies to both the global topology and the local topology server. It uses the same CRD for both.
      * If global, it's directly owned by the `MultigresCluster`.
      * If local, it's owned by the `Cell`.
  * This CR does not exist if the user configures an external etcd server.
  * The `Cell` uses the global `TopoServer` by default if a local one is not configured.
  * Because this is considered a separate resource, the image can be declared per template or per cell, every CR can have its own separate image declaration.
  * This CR owns its own etcd resources (e.g., StatefulSet, Services).


```yaml
apiVersion: multigres.com/v1alpha1
kind: TopoServer
metadata:
  name: "example-multigres-cluster-global"
  namespace: example
  creationTimestamp: "2025-10-21T10:30:00Z"
  generation: 1
  resourceVersion: "12345"
  uid: "b2c3d4e5-1234-5678-90ab-f0e1d2c3b4a5"
  labels:
    multigres.com/cluster: "example-multigres-cluster"
  ownerReferences:
  - apiVersion: multigres.com/v1alpha1
    kind: MultigresCluster
    name: "example-multigres-cluster"
    uid: "a1b2c3d4-1234-5678-90ab-f0e1d2c3b4a5"
    controller: true
    blockOwnerDeletion: true
spec:
  rootPath: "/multigres/global"
  image: "quay.io/coreos/etcd:v3.5.17"
  replicas: 3
  dataVolumeClaimTemplate:
    accessModes: ["ReadWriteOnce"]
    resources:
      requests:
        storage: "10Gi"
  # --- ALTERNATIVE CONFIG (for a Local TopoServer) ---
  # If this CR were created for a cell (like 'us-east-1b' from our
  # example), its metadata and spec would look like this:
  #
  # metadata:
  #   name: "example-multigres-cluster-us-east-1b"
  #   labels:
  #     multigres.com/cluster: "example-multigres-cluster"
  #     multigres.com/topo-scope: "cell"
  #     multigres.com/cell: "us-east-1b"
  # spec:
  #   rootPath: "/multigres/us-east-1b"
  #   image: "quay.io/coreos/etcd:v3.5.17"
  #   replicas: 3
  #   dataVolumeClaimTemplate:
  #     resources:
  #       requests:
  #         storage: "5Gi"
status:
  conditions:
  - type: Available
    status: "True"
    lastTransitionTime: "2025-10-21T10:35:00Z"
    message: "Etcd cluster is healthy"
  replicas: 3
  readyReplicas: 3
  clientServiceName: "example-multigres-cluster-global-client"
  peerServiceName: "example-multigres-cluster-global-peer"
```



#### Child CR of MultigresCluster: Cell

* The `Cell` CR is owned by the `MultigresCluster`.
* The `Cell` CR owns the `MultiOrch` and `MultiGateway` resources (Deployments, Services, etc.) and the `LocalTopoServer` CR if configured.
* The `allCells` field is here to manage the creation of `multiGateway` and `multiOrch` resources. We are assuming that this information is needed for now so it is passed down by the `MultigresCluster` CR

```yaml
# This specific example is for 'us-east-1a', which had no 'topoServer'
# block, so it defaults to using the 'global' topoServer.
#
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
    controller: true
spec:
  name: "us-east-1a"
  images:
    multigateway: "multigres/multigres:latest"
    multiorch: "multigres/multigres:latest"

  multiGateway:
    replicas: 2
    resources:
      requests:
        cpu: "500m"
        memory: "512Mi"
      limits:
        cpu: "2"
        memory: "1Gi"

  multiOrch:
    replicas: 1
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"
       limits:
        cpu: "200m"
        memory: "256Mi"
  
  # A reference to the GLOBAL TopoServer.
  # This is always populated by the parent controller.
  globalTopoServer:
    rootPath: "/multigres/global"
    clientServiceName: "example-multigres-cluster-global-client"

  # ALTERNATIVE CONFIG: (Using the Global TopoServer)
  #
  # Because the 'us-east-1a' cell in the parent CR had no 'topoServer'
  # block, the MultigresCluster controller sets this to empty.
  # topoServer: {}

  # ALTERNATIVE CONFIG: Inline Definition (External)
  # If this were 'us-east-1a', the MultigresCluster controller would
  # have copied the 'external' block directly, like this:
  #
  # topoServer:
  #   external:
  #     address: "etcd-us-east-1a.my-domain.com:2379"
  #     rootPath: "/multigres/us-east-1a"

  # ALTERNATIVE CONFIG: (Managed Local) ---
  # If this were 'us-east-1b', the MultigresCluster controller would
  # have copied the 'managedSpec' block directly, like this.
  # The Cell controller would then be responsible for creating
  # a NEW 'TopoServer' CR from this spec, with this Cell
  # as its owner.
  #
  # topoServer:
  #   managedSpec:
  #     rootPath: "/multigres/us-east-1b"
  #     image: "quay.io/coreos/etcd:v3.5.17"
  #     replicas: 1
  #     dataVolumeClaimTemplate:
  #       accessModes: ["ReadWriteOnce"]
  #       resources:
  #         requests:
  #           storage: "5Gi"

  # List of all cells in the cluster for discovery.
  allCells:
  - "us-east-1a"
  - "us-east-1b"
  - "us-east-1c"

  # Topology flags for the Cell controller to act on.
  topologyReconciliation:
    registerCell: true
    pruneTablets: true
status:
  conditions:
  - type: Available
    status: "True"
    lastTransitionTime: "2025-10-21T10:36:00Z"
    message: "MultiGateway is healthy"
  gatewayReplicas: 2
  topoServer: {} # This would get populated with rootPath and address if configuring the local topoServer
  gatewayReadyReplicas: 2
  gatewayServiceName: "example-multigres-cluster-us-east-1a-gateway"
  multiorchAvailable: "True"
  ```

#### Child CR of MultigresCluster: TableGroup
  * This CR is owned by the `MultigresCluster`.
  * This CR defines and manages the pools where shards reside. It owns the child `Shard` CRs.

```yaml
apiVersion: multigres.com/v1alpha1
kind: TableGroup
metadata:
  name: "production-db-orders-tg"
  namespace: example
  creationTimestamp: "2025-10-21T10:30:02Z"
  generation: 1
  resourceVersion: "12347"
  uid: "d4e5f6a7-1234-5678-90ab-f0e1d2c3b4a7"
  labels:
    multigres.com/cluster: "example-multigres-cluster"
    multigres.com/database: "production_db"
    multigres.com/tablegroup: "orders_tg"
  ownerReferences:
  - apiVersion: multigres.com/v1alpha1
    kind: MultigresCluster
    name: "example-multigres-cluster"
    uid: "a1b2c3d4-1234-5678-90ab-f0e1d2c3b4a5"
    controller: true
    blockOwnerDeletion: true
spec:
  images:
    multipooler: "multigres/multigres:latest"
    postgres: "postgres:15.3"

  partitioning:
    shards: 2

  shardTemplate:
    pools:
      - type: "replica"
        cell: "us-east-1b"
        replicas: 2
        dataVolumeClaimTemplate:
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: "500Gi"
        postgres:
          resources:
            requests:
              cpu: "4"
              memory: "8Gi"
        multipooler:
          resources:
            requests:
              cpu: "1"
              memory: "512Mi"

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
              cpu: "2"
              memory: "4Gi"
        multipooler:
          resources:
            requests:
              cpu: "500m"
              memory: "256Mi"
            limits:
              cpu: "1"
              memory: "512Mi"
status:
  conditions:
  - type: Available
    status: "True"
    lastTransitionTime: "2025-10-21T10:37:00Z"
    message: "All shards are healthy"
  shards: 2
  readyShards: 2
  ```

#### Child of MultigresCluster: Shard

* This CR is owned by the `TableGroup`.
* The `Shard` CR owns the resources for the shard (e.g., `MultiPooler` `StatefulSet` and `Postgres` resources).

```yaml
apiVersion: multigres.com/v1alpha1
kind: Shard
metadata:
  name: "production-db-orders-tg-0"
  namespace: example
  creationTimestamp: "2025-10-21T10:35:00Z"
  generation: 1
  resourceVersion: "12399"
  uid: "e5f6a7b8-1234-5678-90ab-f0e1d2c3b4a8"
  labels:
    multigres.com/cluster: "example-multigres-cluster"
    multigres.com/database: "production_db"
    multigres.com/tablegroup: "orders_tg"
    multigres.com/shard: "0"
  ownerReferences:
  - apiVersion: multigres.com/v1alpha1
    kind: TableGroup
    name: "production-db-orders-tg"
    uid: "d4e5f6a7-1234-5678-90ab-f0e1d2c3b4a7"
    controller: true
    blockOwnerDeletion: true
spec:
  images:
    multipooler: "multigres/multigres:latest"
    postgres: "postgres:15.3"
  pools:
    - type: "replica"
      cell: "us-east-1b"
      replicas: 2
      dataVolumeClaimTemplate:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: "500Gi"
      postgres:
        resources:
          requests:
            cpu: "4"
            memory: "8Gi"
      multipooler:
        resources:
          requests:
            cpu: "1"
            memory: "512Mi"
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
            cpu: "2"
            memory: "4Gi"
      multipooler:
        resources:
          requests:
            cpu: "500m"
            memory: "256Mi"
          limits:
            cpu: "1"
            memory: "512Mi"
status:
  conditions:
  - type: Available
    status: "True"
    lastTransitionTime: "2025-10-21T10:38:00Z"
    message: "Shard is healthy and serving"
  primaryCell: "us-east-1b"
  totalPods: 3
  readyPods: 3
  ```

## Open Issues / Design Questions

This section captures outstanding questions from the initial design phase.

  * Verify that configuration structure of the manifest is true to the way users would use and understand Multigres.
  * What fields should be defaulted if the user was not providing templates or inline configuration?
  * Is the proposed `shard`/`cell`/`tablegroup` structure sufficient for a first iteration, or is it overly complex?


## Implementation History

* **2025-10-08:** Initial proposal to create individual, user-managed CRDs for each component (`MultiGateway`, `MultiOrch`, etc.).
* **2025-10-14:** A second proposal introduced a top-level `MultigresCluster` CR as the primary user-facing API. This design explored a `managed: true/false` flag to allow detaching child resources.
* **2025-10-15:** Discussions from the initial designs led to concerns about the complexity of the `managed: true/false` flag and the potential for cluster misconfiguration. The "Operator as a Platform-Agnostic Delegator" model was also proposed as a long-term architectural alternative.
* **2025-10-28:** The current "parent/child" model was formalized. This model designates `MultigresCluster` as the single source of truth and makes all child CRs (`Cell`, `TableGroup`, etc.) read-only, resolving the ambiguity of the `managed` flag by explicitly disallowing independent child management. The `DeploymentTemplate` CR was introduced to add configuration reusability.

## Drawbacks

While the proposed parent/child model with read-only children provides significant benefits for simplicity and stability, it has several trade-offs:

* **Difficult Cluster Management with Child CRs:** Users can freely provision each of Multigres component independently, and make up the Multigres cluster by providing relevant references to other components. However, this management is error prone and we do not intend to add validation controls at this level, as the child CRs are mainly used as a part of the `MultigresCluster` resource.
* **Potential User Confusion:** Users accustomed to editing any Kubernetes resource might be confused when their manual edits to a child `Cell` or `Shard` CR are immediately reverted by the operator.
* **Abstraction of `DeploymentTemplate`:** The `DeploymentTemplate` CR adds a layer of indirection. A user must look in two places (`MultigresCluster` and `DeploymentTemplate`) to fully understand the configuration of a component. This might add a slight learning curve compared to a fully inline model.
* **Increased Number of CRDs:** This model introduces more CRDs (`Cell`, `TableGroup`, `Shard`, etc.) than a single monolithic approach. While this enables separation of concerns, it increases the total number of API resource types managed by the operator.

## Alternatives

Several alternative designs were considered and rejected in favor of the current parent/child model.

### Alternative 1: Single Monolithic CRD

This approach would involve creating only a single `MultigresCluster` CRD that has all component specs embedded within it.

* **Pros:** Simpler API surface (only one CRD) and easier for basic use cases.
* **Cons:** No component reuse between clusters. Cannot deploy components independently. Leads to a massive, complex, and unmanageable monolithic controller. Debugging becomes very difficult as a failure in one small component could put the entire `MultigresCluster` CR into a failed state, with poor observability.
* **Rejected Because:** Lacks the flexibility, observability, and separation of concerns required for a complex, distributed system.

### Alternative 2: Component CRDs Only (No Parent)

This model, proposed in `create-child-resource-crds.md`, would provide individual, user-managed CRDs for `MultiGateway`, `MultiOrch`, `MultiPooler`, and `Etcd`. Users would be responsible for "composing" a cluster by creating these resources themselves.

* **Pros:** Maximum flexibility and composability. Allows users to share a single `Etcd` component across multiple clusters or deploy only `MultiPooler`.
* **Cons:** Extremely verbose and complex for a standard deployment. Users must manually create all components and wire them together correctly. There is no convenience wrapper or single source of truth for an entire "cluster".
* **Rejected Because:** Makes the common case (deploying a full cluster) unnecessarily complex and error-prone.

### Alternative 3: Hybrid Model with `managed: true/false` Flag

This model would feature a top-level `MultigresCluster` CR, but each component section would have a `managed: true/false` flag. If `true`, the operator manages the child resource. If `false`, the operator ignores it, and the user can manage it themselves.

* **Pros:** Offers a "best-of-both-worlds" approach, combining the convenience of a top-level CR with the flexibility of independent management.
* **Cons:** Introduces significant complexity around resource ownership and lifecycle. What happens when a user switches from `true` to `false`? Does the operator orphan the resource? What if they switch back? This creates a high risk of cluster misconfiguration and an unstable, "split-brain" source of truth.
* **Rejected Because:** The lifecycle and ownership transitions were deemed too complex and risky for a production-grade operator.

### Alternative 4: Operator as a Platform-Agnostic Delegator

This architectural model suggests that the operator should not contain the core Multigres provisioning logic. Instead, the operator would act as a "thin adapter" that provisions a central `MultiAdmin`-like service. This central service would expose its own API (e.g., gRPC) for all cluster operations, and the operator would simply delegate to this API.

* **Pros:** Core Multigres logic remains platform-agnostic and can be managed by tools other than Kubernetes (Terraform, Ansible, etc.). The operator itself becomes simpler, focusing only on Kubernetes resource lifecycles.
* **Cons:** Represents a significant increase in architectural complexity. It requires building and maintaining a highly-available management service *in addition* to the operator.
* **Rejected Because:** This is a strategic, long-term architectural decision that can be adopted later. It is not mutually exclusive with the proposed CRD structure but adds too much scope for the initial v1alpha1.

### Alternative 5: Helm Charts Instead of CRDs

This approach would use Helm charts to deploy Multigres components without an operator.

* **Pros:** Familiar deployment model for many Kubernetes users.
* **Cons:** Provides no automatic reconciliation, no custom status reporting, and no active lifecycle management. Helm cannot react to cluster changes, manage failovers, or handle complex Day 2 operations. Helm templating logic for a system this complex is difficult to maintain and test. Finally, it is another tool and spec that would need to be managed by the end-user.
* **Rejected Because:** The operator pattern provides superior lifecycle management, observability, and automation, which are critical for a stateful database system.
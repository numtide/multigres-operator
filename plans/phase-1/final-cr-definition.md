# Final Multigres Operator CR Definition

This document provides comprehensive examples of what the custom resources (CRs) will look like for Multigres Operator. This document will be use as a guideline to design the API and write the operator code with all its controllers and resources. This document does not cover how the YAML for the dependent resources (pods, deployments, etc.) will look like.

The CRs contain a working definition with alternatives presented in comments to show the various configuration options.

## CR Topology

* The operator can create a managed global etcd topology server and/or a managed local topology server. The CRD to used to create these CRs will be the same, but the global topology server will belong to the MultiGresCluster CR directly whereas the local topology server belongs to the cell. A user can choose to point the multigres cluster to an external etcd topology server, in which case this resource will not be provisioned. If no local topology server is configured, it will use global by default.
* The reason why we chose the following model of parent MultigresCluster are as follows:
    * Splitting logic into child CRs creates simple, specialized controllers that are easier to build and maintain
    * This enables efficient, cascading reconciliation, as only the specific controller for a changed resource needs to run.
    * It provides a clean, hierarchical tree for status, making it much easier to debug a specific component's status
    * The design allows for granular API abstractions and role-based access control (RBAC) for different user types.
    * Enforcing edits at the top-level parent CR creates a single source of truth, preventing unstable conflicts where user edits and the parent controller's logic would "duel" and overwrite each other.



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
      │    ├── 🚪 MultiGate Resources (Deployment, Service, etc.)
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



## MultigresCluster CR

* This and the DeploymentTemplate are the only two editable entries for the end-user. All other child CRs will be owned by this top-level CR, and any manual changes to those child CRs will be reverted as the operator reconciles based on the top-level CR definition. 
* Every field that uses a `deploymentTemplate` comes with an `override` option
* Images are defined globally to avoid the danger of running multiple incongruent versions at once. This would mean the operator would handle the upgrades. However we may allow defining versions across the cluster to provide more flexibility in upgrades in future iterations of the provider.
* Users can configure this CR directly (inline) or by using deployment templates and overrides. 
* The `DeploymentTemplates` are only used in the MultigresCluster CR, when users view a child read-only CR they will see a resolved version of the `deploymentTemplate`.
* This CR is essentially a combination of its child CRs. So the configuration blocks you see below will be split into its own children read-only CRs with their own controllers managing them and each with their own resources. The MultigresCluster also manages its own resources (non-CRs), in particular `MultiAdmin` but for the most part all resources are managed by the children CRs. We will separate the config sections with comment headings to make it easy for the reader to differentiate the various child resources and other config blocks. It's worth pointing out here that the MultigresCluster does not created its granchildren, so the shard configuration below is passed on to the tablegroup CR and it creates its own children shards from it.


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
    #   replicas: 1
    #   resources:
    #     requests:
    #       cpu: "100m"
    #       memory: "128Mi"
    #     limits:
    #       cpu: "200m"
    #       memory: "256Mi"

  # ----------------------------------------------------------------
  # Cells Configuration
  # ----------------------------------------------------------------
  
  # Optional 
  cells:
    - name: "us-east-1"
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
        # spec:
        #   replicas: 2
        #   resources:
        #     requests:
        #       cpu: "500m"
        #       memory: "512Mi"
        #     limits:
        #       cpu: "1"
        #       memory: "1Gi"
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
      # No topology config means it uses global by default

    - name: "us-west-2"
      spec:
        multigateway:
          # Using the template for this cell as well
          deploymentTemplate: "standard-ha"
        multiorch:
          deploymentTemplate: "standard-ha"
        topoServer: # This cell uses a managed local topo server
          deploymentTemplate: "standard-ha"
          rootPath: "/multigres/us-west-2"
          # --- ALTERNATIVE: Inline Definition ---
          # managedSpec:
          #   rootPath: "/multigres/us-west-2"
          #   image: quay.io/coreos/etcd:v3.5.17
          #   replicas: 2
          #   dataVolumeClaimTemplate:
          #     accessModes: ["ReadWriteOnce"]
          #     resources:
          #       requests:
          #         storage: "5Gi"
        # You can specify external per cell as well or it takes global by default
        # external:
        #   address: "etcd-us-east-1.my-domain.com:2379"
        #   rootPath: "/multigres/us-east-1"

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
                  cell: "us-east-1"
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
                  cell: "us-west-2"
                  deploymentTemplate: "orders-ha-replica" # Refers to another template CR

                - type: "readOnly"
                  cell: "us-east-1"
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
                  cell: "us-east-1"
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
                  cell: "us-west-2"
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
    us-east-1:
      gatewayAvailable: "True"
      multiorchAvailable: "True" 
      topoServerAvailable: "True" # Assuming global is available (default)
    us-west-2:
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


### Outstanding questions

* Verify that configuration structure of the manifest is true to the way would users use and understand multigres.
* Would the shard/cell/tablegroup structure above work for a first iteration?
* What fields should be defaulted if the user was not providing templates or inline configuration?

## DeploymentTemplate CR

* This CR is not a child of any other resource. It's purely a configuration CR for MultigresCluster
* All fields are optional, although at least one field is required for the creation of this resource.
* When created, these templates are not watched or reconciled by any controller, they must first be referenced by at least one `MultigresCluster` CR, then they will be watched and reconciled by that controller.
* They cannot be deleted if they are referenced by at least one MultigresCluster CR.
* The content of these templates is resolved by the MultigresCluster and used to configure its children CRs. A user can only see references to these templates on the `MultigresCluster`, they are not referenced by its children CRs. This means if a user views a child resource, they will be able to see the configuration complete.
* Incomplete config blocks will either error out or be completed with defaults or overrides if present.
* We initially had images as part of the DeploymentTemplate but we removed it to prevent users from thinking that multiple templates meant multiple image sets since this is not possible at the moment as images are a global resource (except for toposerver which is considered a separate resource)
* This resource is namespaced scope to prevent granting too many permissions to DBAs or maintainers of these templates.


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



## TopoServer CR - Read-Only Child of MultigresCluster (or cell if localtopology) 

* This CR is applies to both the global topology and the local topology server. It uses the same CRD for both.
    * If global, it's directly owned by the `MultigresCluster`
    * If local, it's owned by the cell.
* This CR does not exist at all if the user configures an external etcd server in the `MultigresCluster` and does not configure a managed etcd for the local cell.
* The cell uses global topoServer by default if not configured locally.
* Because this is considered a separate resource, the image is declared locally within it, not globally.
* This CR owns its own etcd resources (i.e. pods, deployments, services etc.) 


```yaml
# This child CR is created from the `spec.globalTopoServer` block 
# in the parent `MultigresCluster`.
apiVersion: multigres.com/v1alpha1
kind: TopoServer
metadata:
  # The name is derived from the parent cluster + its role
  name: "example-multigres-cluster-global"
  namespace: example
  creationTimestamp: "2025-10-21T10:30:00Z"
  generation: 1
  resourceVersion: "12345"
  uid: "b2c3d4e5-...-..."
  # Labels link it to the parent cluster
  labels:
    multigres.com/cluster: "example-multigres-cluster"
  # OwnerReference makes it a child of MultigresCluster for garbage collection
  ownerReferences:
  - apiVersion: multigres.com/v1alpha1
    kind: MultigresCluster
    name: "example-multigres-cluster"
    uid: "a1b2c3d4-1234-5678-90ab-f0e1d2c3b4a5"
    controller: true
    blockOwnerDeletion: true
spec:
  # The spec is inherited from MultigresCluster
  # in the parent CR.
  rootPath: "/multigres/global"
  image: "quay.io/coreos/etcd:v3.5.17"
  replicas: 3
  dataVolumeClaimTemplate:
    accessModes: ["ReadWriteOnce"]
    resources:
      requests:
        storage: "10Gi"
  # --- ALTERNATIVE CONFIG (for a Local TopoServer) ---
  # If this CR were created for a cell (like 'us-west-2' from our
  # example), its metadata and spec would look like this:
  #
  # metadata:
  #   name: "example-multigres-cluster-us-west-2"
  #   labels:
  #     multigres.com/cluster: "example-multigres-cluster"
  #     multigres.com/topo-scope: "cell"
  #     multigres.com/cell: "us-west-2"
  # spec:
  #   rootPath: "/multigres/us-west-2"
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



## Cell CR - Read-Only Child of MultigresCluster

* The `Cell` CR owns the `multiorch` and `multigateway` resources and the `localTopoServer` CR if configured.
* The cells are currently referenced on the `TableGroup` and `Shard` CRs.
* The `allcells` field is here to manage the creation of `multigate` and `multiorch` resources. We are assuming that this information is needed for now so it is passed down by the `MultigresCluster` CR

```yaml
# This 'Cell' CR is created from an item in the `spec.cells.templates` list
# in the parent `MultigresCluster`.
#
# This specific example is for 'us-east-1', which had no 'topoServer'
# block, so it defaults to using the 'global' topoServer.
#
apiVersion: multigres.com/v1alpha1
kind: Cell
metadata:
  name: "example-multigres-cluster-us-east-1"
  namespace: example
  labels:
    multigres.com/cluster: "example-multigres-cluster"
    multigres.com/cell: "us-east-1"
  ownerReferences:
  # The Cell CR is owned by the MultigresCluster
  - apiVersion: multigres.com/v1alpha1
    kind: MultigresCluster
    name: "example-multigres-cluster"
    uid: "a1b2c3d4-1234-5678-90ab-f0e1d2c3b4a5"
    controller: true
spec:
  # The logical name of the cell, copied from the template.
  name: "us-east-1"

  # The parent MultigresCluster passes down the relevant
  # images for this controller to use.
  images:
    multigateway: "multigres/multigres:latest"
    multiorch: "multigres/multigres:latest"


  # The multigateway spec is copied from multigrescluster
  multigateway:
    replicas: 2
    resources:
      requests:
        cpu: "500m"
        memory: "512Mi"
      limits:
        cpu: "1"
        memory: "1Gi"

 # The multiorch spec is copied from multigrescluster.
  multiorch:
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
  # Because the 'us-east-1' cell in the parent CR had no 'topoServer'
  # block, the MultigresCluster controller sets this to empty.
  # topoServer: {}

  # ALTERNATIVE CONFIG: Inline Definition (External)
  # If this were 'us-east-1', the MultigresCluster controller would
  # have copied the 'external' block directly, like this:
  #
  # topoServer:
  #   external:
  #     address: "etcd-us-east-1.my-domain.com:2379"
  #     rootPath: "/multigres/us-east-1"

  # ALTERNATIVE CONFIG: (Managed Local) ---
  # If this were 'us-west-2', the MultigresCluster controller would
  # have copied the 'managedSpec' block directly, like this.
  # The Cell controller would then be responsible for creating
  # a NEW 'TopoServer' CR from this spec, with this Cell
  # as its owner.
  #
  # topoServer:
  #   managedSpec:
  #     rootPath: "/multigres/us-west-2"
  #     image: "quay.io/coreos/etcd:v3.5.17"
  #     replicas: 1
  #     dataVolumeClaimTemplate:
  #       accessModes: ["ReadWriteOnce"]
  #       resources:
  #         requests:
  #           storage: "5Gi"

  # List of all cells in the cluster for discovery.
  allCells:
  - "us-east-1"
  - "us-west-2"
  - "eu-central-1"

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
  gatewayServiceName: "example-multigres-cluster-us-east-1-gateway"
  multiorchAvailable: "True"
  ```

  ## TableGroup CR - Read-Only Child of MultigresCluster
  * This CR defines and manages the pools where shards reside. It owns the child `Shard` CRs

```yaml
apiVersion: multigres.com/v1alpha1
kind: TableGroup
metadata:
  # Name is derived from database + table group name
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
  # The parent MultigresCluster controller passes down the
  # images relevant to this CR's children (Shard).
  images:
    multipooler: "multigres/multigres:latest"
    postgres: "postgres:15.3"

  partitioning:
    shards: 2

  shardTemplate:
    pools:
      - type: "replica"
        cell: "us-west-2"
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
        cell: "us-east-1"
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

  ## Shard CR - Read-Only Child of TableGroup

  * The Shard CR owns the resources for the shard. At this point most likely a multipooler replicateSet and dependent resources. 

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
  # This images struct is copied from the parent 'TableGroup'.
  # The Shard controller will read these values to build pods.
  images:
    multipooler: "multigres/multigres:latest"
    postgres: "postgres:15.3"
  pools:
    - type: "replica"
      cell: "us-west-2"
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
      cell: "us-east-1"
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
  primaryCell: "us-west-2"
  totalPods: 3
  readyPods: 3
  ```

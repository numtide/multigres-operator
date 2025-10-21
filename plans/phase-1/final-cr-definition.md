# Final Multigres Operator CR Definition

```ascii
[MultiCluster] 🚀 (The root CR)
      │
      ├── 🌍 [GlobalTopoServer] (Child CR if managed)
      │    │
      │    └── 🏛️ etcd Resources (if managed)
      │
      ├── 🤖 MultiAdmin Resources - Deployment, Services, Etc
      │
      ├── 💠 [MultiCell] (Child CR)
      │    │
      │    ├── 🚪 MultiGate Resources (Deployment, Service, etc.)
      │    │
      │    ├── 🧠 MultiOrch Resources (Deployment, etc.)
      │    │
      │    └── 📡 [LocalTopoServer] (Child CR if managed and not using global)
      │         │
      │         └── 🏛️ etcd Resources (if managed)
      │
      └── 🗃️ [MultiTableGroup] (Child CR)
           │
           └── 📦 [MultiShard] (Child CR)
                │
                └── 🏊 MultiPooler and postgres resources (pods or statefulset)
```

## MultiGres Cluster

The only editable entry for the end-user. All other child CRs are read-only.

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: example-multigres-cluster
  namespace: multigres
spec:
  # Images are defined globally to avoid the danger of running multiple incongruent versions at once.
  # The operator will eventually take care of rolling updates in a safe way.
  images:
    multigateway: "multigres/multigres:latest"
    multiorch: "multigres/multigres:latest"
    multipooler: "multigres/multigres:latest"
    multiadmin: "multigres/multigres:latest"
    postgres: "postgres:15.3"

  # ----------------------------------------------------------------
  # Base Cluster Configuration
  # ----------------------------------------------------------------
  globalTopoServer:
    rootPath: "/multigres/global"
    managedSpec:
      image: quay.io/coreos/etcd:v3.5.17
      replicas: 3
      dataVolumeClaimTemplate:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: "10Gi"
    # When external is defined, no toposerver CR is created        
    # external:
    #   address: "my-external-etcd-client.etcd.svc:2379"

  admin:
    spec:
      replicas: 1
      resources:
        requests:
          cpu: "100m"
          memory: "128Mi"
        limits:
          cpu: "200m"
          memory: "256Mi"

  cells:
    templates:
      - name: "us-east-1"
        spec:
          multiGateway:
            replicas: 2
            resources:
              requests:
                cpu: "500m"
                memory: "512Mi"
              limits:
                cpu: "1"
                memory: "1Gi"
        # You can specify external per cell as well or it takes global by default        
        #   topoServer:
        #     external:
        #       address: "etcd-us-east-1.my-domain.com:2379"
        #       rootPath: "/multigres/us-east-1"
      - name: "us-west-2"
        spec:
          multiGateway:
            replicas: 2
            resources:
              requests:
                cpu: "500m"
                memory: "512Mi"
              limits:
                cpu: "1"
                memory: "1Gi"
          topoServer:
            managedSpec:
              rootPath: "/multigres/us-west-2"
              image: quay.io/coreos/etcd:v3.5.17
              replicas: 2
              dataVolumeClaimTemplate:
                accessModes: ["ReadWriteOnce"]
                resources:
                  requests:
                    storage: "5Gi"

  # ----------------------------------------------------------------
  # Reusable Instance Type Definitions
  # ----------------------------------------------------------------
  deploymentSpecs:
    - name: "default-ha"
      replicas: 3
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

    - name: "orders-ha-replica"
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

    - name: "orders-read-only"
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

  # ----------------------------------------------------------------
  # Database Definitions (using Deployment Specs)
  # ----------------------------------------------------------------
  databases:
    templates:
      - name: "production_db"
        spec:
          multiorch:
            spec:
              replicas: 1
              resources:
                requests:
                  cpu: "100m"
                  memory: "128Mi"
                limits:
                  cpu: "200m"
                  memory: "256Mi"

          tableGroups:
            # --- TABLEGROUP 1: An unsharded (single shard) default group ---
            - name: "default"
              partitioning:
                shards: 1
              shardTemplate:
                pools:
                  - type: "replica"
                    cell: "us-east-1"
                    deploymentSpecName: "default-ha"

            # --- TABLEGROUP 2: A sharded group for high-traffic tables ---
            - name: "orders_tg"
              partitioning:
                shards: 2
              shardTemplate:
                pools:
                  - type: "replica"
                    cell: "us-west-2"
                    deploymentSpecName: "orders-ha-replica"

                  - type: "readOnly"
                    cell: "us-east-1"
                    deploymentSpecName: "orders-read-only"

            # --- TABLEGROUP 3: Example of using an override ---
            - name: "analytics_tg"
              partitioning:
                shards: 1
              shardTemplate:
                pools:
                  - type: "replica"
                    cell: "us-east-1"
                    deploymentSpecName: "default-ha"
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
            - name: "custom_tg"
              partitioning:
                shards: 1
              shardTemplate:
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
                          cpu:s "200m"
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
      topoServerAvailable: "True"
    us-west-2:
      gatewayAvailable: "True"
  databases:
    production_db:
      desiredInstances: 14 
      readyInstances: 14
      servingWrites: "True"
      multiorchAvailable: "True" 
  admin:
    available: "True"
    serviceName: "example-multigres-cluster-admin"
```

## toposerver - Read-Only Child of MultigresCluster (or cell if localtopology) 

```yaml
# This child CR is created from the `spec.globalTopoServer` block 
# in the parent `MultigresCluster`.
apiVersion: multigres.com/v1alpha1
kind: TopoServer
metadata:
  # The name is derived from the parent cluster + its role
  name: "example-multigres-cluster-global"
  namespace: multigres
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
  # The spec is inherited directly from the 'managedSpec' block
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
  #   replicas: 1
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

## MultiCell CR - Read-Only Child of MultigresCluster


```yaml
# FILE: multicell-sample.yaml
#
# This 'MultiCell' CR is created from an item in the `spec.cells.templates` list
# in the parent `MultigresCluster`.
#
# This specific example is for 'eu-central-1', which had no 'topoServer'
# block, so it defaults to using the 'global' toposerver.
#
apiVersion: multigres.com/v1alpha1
kind: MultiCell
metadata:
  name: "example-multigres-cluster-eu-central-1"
  namespace: multigres
  labels:
    multigres.com/cluster: "example-multigres-cluster"
    multigres.com/cell: "eu-central-1"
  ownerReferences:
  # The MultiCell CR is owned by the MultigresCluster
  - apiVersion: multigres.com/v1alpha1
    kind: MultigresCluster
    name: "example-multigres-cluster"
    uid: "a1b2c3d4-1234-5678-90ab-f0e1d2c3b4a5"
    controller: true
spec:
  # The logical name of the cell, copied from the template.
  name: "eu-central-1"

  # The parent MultigresCluster passes down the relevant
  # images for this controller to use.
  images:
    multigateway: "multigres/multigres:latest"

  # The resolved spec for the MultiGateway
  multiGateway:
    replicas: 2
    resources:
      requests:
        cpu: "500m"
        memory: "512Mi"
      limits:
        cpu: "1"
        memory: "1Gi"
  
  # A reference to the GLOBAL TopoServer.
  # This is always populated by the parent controller.
  globalTopoServer:
    rootPath: "/multigres/global"
    clientServiceName: "example-multigres-cluster-global-client"

  # --- CONFIG 1 (DEFAULT): Using the Global TopoServer ---
  #
  # Because the 'eu-central-1' cell in the parent CR had no 'topoServer'
  # block, the MultigresCluster controller sets this to 'global'.
  # The MultiCell controller will use this to configure its MultiGateway
  # to talk to the global topo server.
  topoServer:
    global:
      rootPath: "/multigres/global"

  # --- ALTERNATIVE CONFIG 2 (External) ---
  # If this were 'us-east-1', the MultigresCluster controller would
  # have copied the 'external' block directly, like this:
  #
  # topoServer:
  #   external:
  #     address: "etcd-us-east-1.my-domain.com:2379"
  #     rootPath: "/multigres/us-east-1"

  # --- ALTERNATIVE CONFIG 3 (Managed Local) ---
  # If this were 'us-west-2', the MultigresCluster controller would
  # have copied the 'managedSpec' block directly, like this.
  # The MultiCell controller would then be responsible for creating
  # a NEW 'TopoServer' CR from this spec, with this MultiCell
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

  # Topology flags for the MultiCell controller to act on.
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
  gatewayReadyReplicas: 2
  gatewayServiceName: "example-multigres-cluster-us-east-1-gateway"
  ```

  ## MultiTablegroup CR - Read-Only Child of MultigresCluster

```yaml
# This child CR is created from the 'orders_tg' entry
# under 'production_db' in the MultigresCluster CR.
#
# The MultigresCluster controller does the following:
# 1. Copies the *relevant* images (postgres, multipooler) into `spec.images`.
# 2. Resolves and stamps the 'multiorch' spec.
# 3. Resolves the `deploymentSpecName` entries into the 'shardTemplate'.
#
apiVersion: multigres.com/v1alpha1
kind: MultiTableGroup
metadata:
  # Name is derived from database + table group name
  name: "production-db-orders-tg"
  namespace: multigres
  creationTimestamp: "2025-10-21T10:30:02Z"
  generation: 1
  resourceVersion: "12347"
  uid: "d4e5f6a7-1234-5678-90ab-f0e1d2c3b4a7"
  labels:
    multigres.com/cluster: "example-multigres-cluster"
    multigres.com/database: "production_db"
    multigres.com/table-group: "orders_tg"
  ownerReferences:
  - apiVersion: multigres.com/v1alpha1
    kind: MultigresCluster
    name: "example-multigres-cluster"
    uid: "a1b2c3d4-1234-5678-90ab-f0e1d2c3b4a5"
    controller: true
    blockOwnerDeletion: true
spec:
  # The parent MultigresCluster controller passes down the
  # images relevant to this CR's children (MultiShard).
  images:
    multipooler: "multigres/multigres:latest"
    postgres: "postgres:15.3"
    multiorch: "multigres/multigres:latest"
  
# The multiorch spec is copied from the parent database definition.
  # The image here *is* resolved and stamped by the parent.
  multiorch:
    spec:
      replicas: 1
      resources:
        requests:
          cpu: "100m"
          memory: "128Mi"
        limits:
          cpu: "200m"
          memory: "256Mi"

  partitioning:
    shards: 2

  # This template is fully resolved from `deploymentSpecName`s.
  # The 'image' fields are NOT embedded in the sub-components.
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
  multiorchAvailable: "True"
  ```

  ## MultiShard CR - Read-Only Child of MultiTableGroup

```yaml
# FILE: multishard-sample.yaml
#
# This child CR is created by the 'MultiTableGroup' controller
# for 'production-db-orders-tg'. This is shard "0" of 2.
#
# The MultiTableGroup controller copies its own 'spec.images'
# and its 'spec.shardTemplate.pools' into this CR's spec.
#
apiVersion: multigres.com/v1alpha1
kind: MultiShard
metadata:
  name: "production-db-orders-tg-0"
  namespace: multigres
  creationTimestamp: "2025-10-21T10:35:00Z"
  generation: 1
  resourceVersion: "12399"
  uid: "e5f6a7b8-1234-5678-90ab-f0e1d2c3b4a8"
  labels:
    multigres.com/cluster: "example-multigres-cluster"
    multigres.com/database: "production_db"
    multigres.com/table-group: "orders_tg"
    multigres.com/shard: "0"
  ownerReferences:
  - apiVersion: multigres.com/v1alpha1
    kind: MultiTableGroup
    name: "production-db-orders-tg"
    uid: "d4e5f6a7-1234-5678-90ab-f0e1d2c3b4a7"
    controller: true
    blockOwnerDeletion: true
spec:
  # This images struct is copied from the parent 'MultiTableGroup'.
  # The MultiShard controller will read these values to build pods.
  images:
    multipooler: "multigres/multigres:latest"
    postgres: "postgres:15.3"
    multiorch: "multigres/multigres:latest"

    
  # The 'pools' block is a direct copy of the
  # 'shardTemplate.pools' from the parent MultiTableGroup.
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

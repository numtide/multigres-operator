# MultigresCluster CR Initial Design Proposal

  - **Authors**: Fernando Villalba
  - **Status**: Provisional
  - **Creation Date**: 2025-10-14
  - **Last Updated**: 2025-10-15

-----

## Summary

This document presents a design proposal for the Kubernetes Custom Resources (CRs) that will define the Multigres cluster architecture. The primary focus is the top-level `MultigresCluster` CR, which serves as the user-facing API for provisioning and managing a complete Multigres deployment.

-----

## Motivation

A well-defined API is critical for the success of the Multigres Operator. The motivations for formalizing this design are:

  * **Provide a clear blueprint**: This document will establish a definitive contract for the operator's behavior and the configuration options available to end-users.
  * **Guide implementation**: The proposed CR structure will inform the development process and the hierarchical organization of the operator's codebase.
  * **Define resource ownership**: It will clarify the relationship between custom resources and the underlying Kubernetes objects, establishing a clear model for how they are managed by the operator.

-----

## Goals

  * Finalize the API specification for all Custom Resources managed by the Multigres Operator. Beginning with the top CR, MultigresCluster.
  * Illustrate the relationship between the CRs and the Kubernetes resources they generate to ensure a robust management strategy.
  * Codify the operator's scope of responsibilities and behaviors within the `MultigresCluster` Custom Resource Definition.

-----

## Non-Goals

  * To provide an exhaustive definition of every subordinate Kubernetes resource created by the operator.
  * To include the final, formal Custom Resource Definition (CRD) schemas in this document.
  * The feature set in this document will be implicit in the design of the CRs but not the main point of discussion.

-----

## Proposal

The core of the operator's API is the `MultigresCluster` Custom Resource. This resource provides a single, unified interface for configuring an entire Multigres cluster. The operator will reconcile this top-level CR by creating and managing a series of more granular child resources for components like cells, shards, and topology servers.

While the primary workflow involves defining the entire topology within the `MultigresCluster` CR, the design also accommodates advanced use cases. Users will have the option to provision and manage child resources independently by setting a `managed: false` flag, allowing for greater flexibility and integration with existing infrastructure.


### User Stories

As a platform operator, I want to define my entire multi-cell, sharded Multigres topology in a single YAML file and apply it with `kubectl`. The system should then automatically provision all the necessary components, connect them, and bring the database cluster online without further manual intervention.

### Operator Design and Architecture

The operator's core responsibility is to translate the high-level state defined in the `MultigresCluster` CR into a set of child CRs. Each child CR represents a distinct component of the cluster, such as a cell or a database shard. The controllers for these child CRs are then responsible for creating and managing the low-level Kubernetes resources (like `StatefulSets`, `Deployments`, and `Services`) required to run the system.

### API Definition (`MultigresCluster` CRD)

The user interacts with the operator primarily through the `MultigresCluster` custom resource. This single manifest serves as the source of truth for the entire cluster. 

> Please note that this sample does not contain the tablegroups or shard definitions, which are discussed later in this document.

```yaml
apiVersion: [multigres.com/v1alpha1](https://multigres.com/v1alpha1)
kind: MultigresCluster
metadata:
  name: example-multigres-cluster
  namespace: multigres
spec:
  # globalTopoServer defines the topology server for the entire cluster.
  # Required.
  globalTopoServer:
    # If 'managed' is true, the operator will create and manage the global topology server CR.
    # Optional. Defaults to 'true'.
    managed: true
    # The root path in etcd where this cluster's topology data will be stored.
    # Required.
    rootPath: "/multigres/global"
    # Defines the spec for the etcd cluster that will be managed by this operator.
    # This is used as a template if 'managed' is true.
    managedSpec:
      image: quay.io/coreos/etcd:v3.5.17
      replicas: 3
      dataVolumeClaimTemplate:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: "10Gi"
    # Optional. Use this if 'managed' is false or to override the managed spec.
    # external:
    #   address: "my-external-etcd-client.etcd.svc:2379"

  # admin provides a template for the MultiAdmin component.
  # Optional.
  admin:
    # If 'managed' is true, the operator will create and manage the MultiAdmin CR.
    # Optional. Defaults to 'true'.
    managed: true
    spec:
      image: multigres/multigres:latest
      replicas: 1
      resources:
        requests:
          cpu: "100m"
          memory: "128Mi"
        limits:
          cpu: "200m"
          memory: "256Mi"

  # orchestrator provides a template for the MultiOrch component.
  # Optional.
  orchestrator:
    # If 'managed' is true, the operator will create and manage the MultiOrch CR.
    # Optional. Defaults to 'true'.
    managed: true
    spec:
      image: multigres/multigres:latest
      replicas: 1
      resources:
        requests:
          cpu: "100m"
          memory: "128Mi"
        limits:
          cpu: "200m"
          memory: "256Mi"

  # cells contains the templates for the MultiCell CRs.
  # Required.
  cells:
    # If 'managed' is true, the operator will create and manage MultiCell CRs from this list.
    # Optional. Defaults to 'true'.
    managed: true
    # Defines the default image for all multiGateway instances created from these templates.
    # This can be overridden within a specific template's spec if needed.
    multiGatewayImage: multigres/multigres:latest
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
          # topoServer defines a cell-local topology server.
          # Optional. Default is to use global. In the future this can use the topology CR to create local topology
          topoServer:
            external:
              address: "etcd-us-east-1.my-domain.com:2379"
              rootPath: "/multigres/us-east-1"

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

  # databases contains the templates for the MultiShard CRs.
  # Optional.
  databases:
    # If 'managed' is true, the operator will create and manage MultiShard CRs from this list.
    # Optional. Defaults to 'true'.
    managed: true
    templates:
      - name: "users_db"
        spec:
          images:
            postgres: postgres:15.3
            multiPooler: multigres/multigres:latest
          pools:
            - cell: "us-east-1"
              primary:
                replicas: 1
                dataVolumeClaimTemplate:
                  accessModes: ["ReadWriteOnce"]
                  resources:
                    requests:
                      storage: "100Gi"
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
                      cpu: "200m"
                      memory: "128Mi"
                    limits:
                      cpu: "400m"
                      memory: "256Mi"
              replica:
                replicas: 2
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
                      cpu: "200m"
                      memory: "128Mi"
                    limits:
                      cpu: "400m"
                      memory: "256Mi"
            - cell: "us-west-2"
              replica:
                replicas: 3
                dataVolumeClaimTemplate:
                  accessModes: ["ReadWriteOnce"]
                  resources:
                    requests:
                      storage: "100Gi"
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
                      cpu: "200m"
                      memory: "128Mi"
                    limits:
                      cpu: "400m"
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
    users_db:
      desiredInstances: 6
      readyInstances: 6
      servingWrites: "True"
  admin:
    available: "True"
    serviceName: "example-multigres-cluster-admin"
  orchestrator:
    available: "True"
```

### Key Design Decisions for Discussion

This section outlines critical design questions where we seek collaboration with the Supabase team to ensure the final API meets both immediate and long-term requirements.

#### 1\. Defining a Declarative Sharding Strategy

A core feature of Multigres is its ability to scale horizontally through sharding via `TableGroups`. The operator must provide a powerful yet intuitive way for users to define this sharding strategy declaratively. The central question is how to map the logical concepts of `TableGroups` and shards to the physical deployment of pods across different cells.

We propose a model which separates the logical partitioning strategy from the physical pod configuration. This involves defining `tableGroups` within the database spec, each with a `partitioning` strategy and a `shardTemplate`.

Below is our proposed API structure for this section. It establishes a clear pattern for defining sharded databases and serves as a foundation for more advanced partitioning strategies in the future.

```yaml
# Proposal for a shards snippet for a MultigresCluster CRD
spec:
  databases:
    managed: true
    templates:
      - name: "production_db"
        spec:
          tableGroups:
            # --- TABLEGROUP 1: An unsharded (single shard) default group ---
            - name: "default"
              partitioning:
                # This table group will have only one shard.
                shards: 1
              shardTemplate:
                pools:
                  # This is the high-availability, primary-eligible pool.
                  # MultiOrch will elect one of these 3 pods to be the primary.
                  - type: "replica"
                    cell: "us-east-1"
                    replicas: 3
                    dataVolumeClaimTemplate:
                      accessModes:
                        - ReadWriteOnce
                      resources:
                        requests:
                          storage: "250Gi"
                    postgres:
                      image: "postgres:15.3"
                      resources:
                        requests:
                          cpu: "2"
                          memory: "4Gi"
                        limits:
                          cpu: "4"
                          memory: "8Gi"
                    multiPooler:
                      image: "multigres/multigres:latest"
                      resources:
                        requests:
                          cpu: "500m"
                          memory: "256Mi"
                        limits:
                          cpu: "1"
                          memory: "512Mi"

            # --- TABLEGROUP 2: A sharded group for high-traffic tables ---
            - name: "orders_tg"
              partitioning:
                # This table group will be split into 2 shards.
                shards: 2
              shardTemplate:
                pools:
                  # This template defines the HA pool for EACH of the 2 shards.
                  - type: "replica"
                    cell: "us-west-2"
                    replicas: 2 # Total pods for EACH shard. 1 will be primary, 1 will be replica.
                    dataVolumeClaimTemplate:
                      accessModes:
                        - ReadWriteOnce
                      resources:
                        requests:
                          storage: "500Gi"
                    postgres:
                      image: "postgres:15.3"
                      resources:
                        requests:
                          cpu: "4"
                          memory: "8Gi"
                    multiPooler:
                      image: "multigres/multigres:latest"
                      resources:
                        requests:
                          cpu: "1"
                          memory: "512Mi"
                  # This template defines a read-only pool for EACH of the 2 shards.
                  - type: "readOnly"
                    cell: "us-east-1"
                    replicas: 1
                    dataVolumeClaimTemplate:
                      accessModes:
                        - ReadWriteOnce
                      resources:
                        requests:
                          storage: "500Gi"
                    postgres:
                      image: "postgres:15.3"
                      resources:
                        requests:
                          cpu: "2"
                          memory: "4Gi"
```

#### 2\. Ensuring High Availability During Upgrades

To achieve true high availability, the operator must be able to perform rolling upgrades of database pods without causing downtime. The default rolling update strategy of a `StatefulSet` is unaware of primary/replica roles and can inadvertently terminate a primary before a replica is ready to be promoted, leading to a service interruption.

We propose that `MultiOrch` will fulfill this role in the Multigres ecosystem. For the initial implementation of the operator, we will utilize `StatefulSets` for their stable storage and networking guarantees, while deferring the implementation of advanced, zero-downtime upgrade logic. However, it is critical to acknowledge this requirement now to ensure the architecture can support this managed upgrade process in a future iteration.

This strategy introduces two technical requirements:

1.  **Shard-to-Resource Mapping:** The operator must establish a predictable mapping from a logical shard to its generated `StatefulSet`.
2.  **Resource Discovery:** `MultiOrch` needs a mechanism to discover which pods belong to the specific table group and shard it manages.

We will solve both requirements by applying a comprehensive set of Kubernetes **labels** to all generated resources. The operator will assign labels indicating the cluster, database, table group, and shard name, allowing `MultiOrch` to accurately identify and manage its target pods via the Kubernetes API.

This approach—using `StatefulSets` for their stable identity and storage guarantees while delegating failover logic to an internal agent—is a proven pattern seen in other production-grade Postgres operators.ts but other Postgres operators like Zalando do with the aid of Patroni, which may do something similar to multiorch.

#### 3\. Managing CRs separately

Looking at the way Vitess Operator was designed for reference, we find that the way it has a top-level approach to the way it is configured. A [VitessCluster CR](https://github.com/planetscale/vitess-operator/blob/6282e16648cc6976277412f7db123aa2c2a02640/docs/api.md#vitesscluster) configures the entire Vitess Cluster and every other child CR of it is read only, if a user tries to change it, it will inmediately revert to VitessCluster source of truth. The only other CRs that can be configured by the user separately are backups.

The `managed: true/false` flag is a powerful feature, but it introduces some challenges:

* What happens when a user switches a component from managed: true to managed: false? Does the operator orphan the resources it created? What if they switch it back? The lifecycle and ownership transitions are not fully defined.
* Does the flexibility of managing child resources individually outweigh the risk of cluster misconfiguration? For example, a user might create a `MultiCell` CR without its necessary dependencies, leading to an incomplete or non-functional state.
* Is it worth starting with a read-only child resources mode and then later design further flexibility?


---

## Appendix

### Operator-Generated Resources

The following examples illustrate the types of Kubernetes resources the operator will generate from the `MultigresCluster` spec. These are provided for context and are subject to change.

>These are here mostly for illustration purposes, they do not need to be discussed within the context of this document.

#### 1\. Topology Service (Etcd Cluster)

Created if `spec.globalTopoServer.managed` is true.

```yaml
# OPERATOR-CREATED: Headless service for etcd peer discovery.
apiVersion: v1
kind: Service
metadata:
  name: example-cluster-etcd-peer
  namespace: multigres
spec:
  clusterIP: None
  ports:
  - port: 2380
    name: peer
  selector:
    [multigres.com/cluster](https://multigres.com/cluster): example-cluster
    [multigres.com/component](https://multigres.com/component): etcd
---
# OPERATOR-CREATED: Client service for etcd.
apiVersion: v1
kind: Service
metadata:
  name: example-cluster-etcd-client
  namespace: multigres
spec:
  ports:
  - port: 2379
    name: client
  selector:
    [multigres.com/cluster](https://multigres.com/cluster): example-cluster
    [multigres.com/component](https://multigres.com/component): etcd
---
# OPERATOR-CREATED: StatefulSet to manage the etcd cluster pods.
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: example-cluster-etcd
  namespace: multigres
spec:
  serviceName: "example-cluster-etcd-peer"
  replicas: 3
  selector:
    matchLabels:
      [multigres.com/cluster](https://multigres.com/cluster): example-cluster
      [multigres.com/component](https://multigres.com/component): etcd
  template:
    metadata:
      labels:
        [multigres.com/cluster](https://multigres.com/cluster): example-cluster
        [multigres.com/component](https://multigres.com/component): etcd
    spec:
      containers:
      - name: etcd
        image: "k8s.gcr.io/etcd:3.5.9-0"
        command:
        - "etcd"
        - "--name=$(HOSTNAME)"
        - "--listen-client-urls=[http://0.0.0.0:2379](http://0.0.0.0:2379)"
        - "--advertise-client-urls=http://$(HOSTNAME).example-cluster-etcd-peer:2379"
        - "--listen-peer-urls=[http://0.0.0.0:2380](http://0.0.0.0:2380)"
        - "--initial-advertise-peer-urls=http://$(HOSTNAME).example-cluster-etcd-peer:2380"
        - "--initial-cluster=example-cluster-etcd-0=[http://example-cluster-etcd-0.example-cluster-etcd-peer:2380](http://example-cluster-etcd-0.example-cluster-etcd-peer:2380),example-cluster-etcd-1=[http://example-cluster-etcd-1.example-cluster-etcd-peer:2380](http://example-cluster-etcd-1.example-cluster-etcd-peer:2380),example-cluster-etcd-2=[http://example-cluster-etcd-2.example-cluster-etcd-peer:2380](http://example-cluster-etcd-2.example-cluster-etcd-peer:2380)"
        - "--initial-cluster-token=multigres-etcd-cluster"
        - "--initial-cluster-state=new"
        - "--data-dir=/var/run/etcd/default.etcd"
        volumeMounts:
        - name: etcd-data
          mountPath: /var/run/etcd
  volumeClaimTemplates:
  - metadata:
      name: etcd-data
    spec:
      accessModes: ["ReadWriteOnce"]
      storageClassName: "standard-rwo"
      resources:
        requests:
          storage: "10Gi"
```




```yaml
# OPERATOR-CREATED: Manages orchestration within a cell.
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-cluster-multiorch-us-east-1
  namespace: multigres
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: multiorch
        image: "multigres/multigres:latest"
        command: ["multiorch"]
        args:
        # --- Topology Flags ---
        - "--topo_implementation=etcd"
        - "--topo_global_server_address=example-cluster-etcd-client:2379"
        - "--topo_global_root=/multigres/global"
        # --- Identity & Network Flags ---
        - "--cell=us-east-1"
        - "--http_port=15300"
        - "--grpc_port=16000"
---
# OPERATOR-CREATED: The query routing gateway for a cell.
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-cluster-multigateway-us-east-1
  namespace: multigres
spec:
  replicas: 2
  template:
    spec:
      containers:
      - name: multigateway
        image: "multigres/multigres:latest"
        command: ["multigateway"]
        args:
        # --- Topology Flags ---
        - "--topo_implementation=etcd"
        - "--topo_global_server_address=example-cluster-etcd-client:2379"
        - "--topo_global_root=/multigres/global"
        # --- Identity & Network Flags ---
        - "--cell=us-east-1"
        - "--port=15433"      # Port for PostgreSQL clients
        - "--grpc-port=15991" # Port for internal gRPC
---
# OPERATOR-CREATED: Service to expose the multigateway.
apiVersion: v1
kind: Service
metadata:
  name: example-cluster-multigateway-us-east-1
  namespace: multigres
spec:
  type: LoadBalancer
  ports:
  - port: 15433
    targetPort: 15433
    name: pg-client
  selector:
    [multigres.com/component](https://multigres.com/component): multigateway
    [multigres.com/cell](https://multigres.com/cell): us-east-1
---
# OPERATOR-CREATED: Central admin UI/API.
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-cluster-multiadmin
  namespace: multigres
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: multiadmin
        image: "multigres/multigres:latest"
        command: ["multiadmin"]
        args:
        # --- Topology Flags ---
        - "--topo_implementation=etcd"
        - "--topo_global_server_address=example-cluster-etcd-client:2379"
        - "--topo_global_root=/multigres/global"
        # --- Network Flags ---
        - "--http_port=15000"
        - "--grpc_port=15990"
```

#### 3\. Database Instance Group (PostgreSQL Shard)

One `StatefulSet` is created per shard to manage the stateful database pods.

```yaml
# OPERATOR-CREATED: A StatefulSet for the 'commerce' database, shard '0'.
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: example-cluster-commerce-0-pg
  namespace: multigres
spec:
  serviceName: "example-cluster-commerce-0-pg-headless"
  replicas: 2
  persistentVolumeClaimRetentionPolicy:
    whenDeleted: Retain
    whenScaled: Retain
  selector:
    matchLabels:
      [multigres.com/cluster](https://multigres.com/cluster): example-cluster
      [multigres.com/database](https://multigres.com/database): commerce
      [multigres.com/shard](https://multigres.com/shard): "0"
  template:
    metadata:
      labels:
        [multigres.com/cluster](https://multigres.com/cluster): example-cluster
        [multigres.com/database](https://multigres.com/database): commerce
        [multigres.com/shard](https://multigres.com/shard): "0"
    spec:
      securityContext:
        runAsUser: 1000
        fsGroup: 1000
      # The init container is needed if we want to give the user the option to use their own postgresql image.
      # Other option may be sharing container namespace and disk in the pod.
      initContainers:
      - name: bootstrap-multigres
        image: "multigres/multigres:latest"
        command: ["sh", "-c", "set -ex; cp /usr/local/bin/pgctld /multigres/bin/; cp /usr/local/bin/multipooler /multigres/bin/"]
        volumeMounts:
        - name: multigres-bin
          mountPath: /multigres/bin
      containers:
      # 1. pgctld, which manages the postgres process.
      - name: postgres
        image: "postgres:15"
        command: ["/multigres/bin/pgctld"]
        args:
        - "server"
        # --- Topology Flags (inherited by servenv) ---
        - "--topo_implementation=etcd"
        - "--topo_global_server_address=example-cluster-etcd-client:2379"
        - "--topo_global_root=/multigres/global"
        # --- Identity & Path Flags ---
        - "--cell=us-east-1"
        - "--database=commerce"
        - "--shard=0"
        - "--pg-data=/var/lib/postgresql/data/pgdata"
        # --pooler-dir is set to the same as pg-data for simplicity
        - "--pooler-dir=/var/lib/postgresql/data"
        - "--pg-version=15"
        # --- Network Flags ---
        - "--grpc_port=17000"
        - "--pg-port=5432" # The actual PostgreSQL port
        # --- Config Flags ---
        - "--pg-user=postgres" # Default user
        - "--pg-database=postgres" # Default database
        - "--timeout=30"
        volumeMounts:
        - name: pg-data
          mountPath: /var/lib/postgresql/data
        - name: multigres-bin
          mountPath: /multigres/bin

      # 2. multipooler sidecar for connection pooling.
      - name: multipooler
        image: "multigres/multigres:latest"
        command: ["/multigres/bin/multipooler"]
        args:
        # --- Topology Flags ---
        - "--topo_implementation=etcd"
        - "--topo_global_server_address=example-cluster-etcd-client:2379"
        - "--topo_global_root=/multigres/global"
        # --- Identity & Path Flags ---
        - "--cell=us-east-1"
        - "--database=commerce"
        - "--table-group=0" # Corresponds to the shard
        - "--pooler-dir=/var/lib/postgresql/data"
        # --- Network Flags ---
        - "--pgctld-addr=localhost:17000" # Connects to its local pgctld
        - "--pg-port=15432"               # Port for proxied PG connections
        - "--grpc_port=16001"             # Port for multigateway discovery
        - "--http_port=15100"
        volumeMounts:
        - name: multigres-bin
          mountPath: /multigres/bin
        - name: pg-data
          mountPath: /var/lib/postgresql/data

      # 3. postgres-exporter sidecar for metrics. This can be an optional feature
      - name: postgres-exporter
        image: "prometheus-community/postgres-exporter:v0.15.0"
        env:
        - name: DATA_SOURCE_NAME
          value: "postgresql://postgres@localhost:5432/postgres?sslmode=disable"
        ports:
        - containerPort: 9187
          name: metrics

      volumes:
      - name: multigres-bin
        emptyDir: {}
  volumeClaimTemplates:
  - metadata:
      name: pg-data
    spec:
      accessModes: ["ReadWriteOnce"]
      storageClassName: "your-local-storage-class"
      resources:
        requests:
          storage: "50Gi"
```
---
title: MultigresCluster API All Config in Shard Alternative 
state: ready
---

# This is a WIP

## Summary

This proposal defines the `v1alpha1` API for the Multigres Operator. This design is centered on two user-editable Custom Resources (CRs):

1.  **`MultigresCluster`**: The root resource that defines the desired state (intent) of the entire cluster, including the full `database.tablegroup.shard` hierarchy.
2.  **`ShardTemplate`**: A reusable, namespaced resource for defining the complete configuration for a shard, including its pools (`MultiPooler`/`Postgres`), `MultiOrch`, `MultiGateway`, and `LocalTopoServer` components.

This design relies on the top-level `MultigresCluster` controller to resolve all shard-level configurations and populate the specs of the read-only child CRs (`Cell`, `TableGroup`, `Shard`). This creates a significant implementation challenge, as the controller must resolve N:1 conflicts (e.g., multiple shards defining different `MultiGateway` configs for the same cell).

## Motivation

Managing a distributed, sharded database system is inherently complex. This API attempts to provide a declarative, Kubernetes-native interface for this task. The primary motivations for this specific parent/child design are:

  * **Hierarchical Observability:** A parent/child model provides a clean tree for status aggregation. A failure in a `Shard` can be observed on the `Shard` CR, its `TableGroup` parent, and aggregated up to the `MultigresCluster`.
  * **Separation of Concerns (Child CRs):** Splitting logic into child CRs (`Cell`, `TableGroup`, `Shard`) allows for specialized controllers, though the logic for populating their specs is now heavily concentrated in the `MultigresCluster` controller.
  * **Single Source of Intent:** The `MultigresCluster` is the single editable source of truth. Any manual edits to read-only child CRs are automatically reverted by their controller.

## Goals

  * Provide a declarative, Kubernetes-native API for deploying a multi-cell, sharded Multigres cluster.
  * Allow users to define the full sharding hierarchy, including `TableGroups` and `Shards`.
  * Co-locate all component configurations (`MultiGateway`, `MultiOrch`, `LocalTopoServer`, `Pools`) within a single, reusable `ShardTemplate`.
  * Separate user-facing *intent* (`MultigresCluster`) from operator-managed *realized state* (child CRs).

## Non-Goals

  * This document does not define the conflict-resolution strategy (e.g., "first-one-wins," "merge," etc.) that the `MultigresCluster` controller must use to resolve contradictory configs for cell-level components. This is a major open issue.
  * This document does not cover Day 2 operations (backup, restore, monitoring, alerting).

## Proposal: API Architecture and Resource Topology

  * The user defines the full `databases.tablegroups.shards` hierarchy in the `MultigresCluster` CR.
  * All component configurations (`MultiGateway`, `MultiOrch`, `LocalTopoServer`, `Pools`) are defined at the shard level, either inline or via a `ShardTemplate`.
  * **Controller Logic:**
      * The `MultigresCluster` controller reads this spec.
      * It creates the `Cell` child CRs. It must then **resolve** the N:1 `MultiGateway` and `LocalTopoServer` configs from all shards in that cell and populate the `Cell.spec`.
      * It creates the `TableGroup` child CRs, populating their `spec.shards` list.
      * The `TableGroup` controller creates the `Shard` child CRs.
      * The `Shard.spec` is populated with the `MultiOrch` and `Pools` config.
  * **Component Ownership:**
      * `MultiGateway` & `LocalTopoServer` are **owned by the `Cell` CR** (which gets its config from the `MultigresCluster` controller).
      * `MultiOrch` is **owned by the `Shard` CR**.
      * `MultiPooler`/`Postgres` are **owned by the `Shard` CR** (as part of its `Pools`).

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
      │
      ├── 💠 [Cell] (Child CR)
      │    │ 
      │    │  (Spec is populated by MultigresCluster controller
      │    │   after resolving conflicts from shard definitions)
      │    │
      │    ├── 🚪 MultiGateway Resources (Deployment, Service, etc.)
      │    │    
      │    │
      │    └── 📡 [LocalTopoServer] (Child CR if managed)
      │         │
      │         └── 🏛️ etcd Resources (if managed)
      │
      └── 🗃️ [TableGroup] (Child CR)
           │
           └── 📦 [Shard] (Child CR)
                │
                ├── 🧠 MultiOrch Resources (Deployment, etc.)
                │
                └── 🏊 MultiPooler and postgres resources (pods or statefulset)
                    


📋 [ShardTemplate] (Separate CR - user-editable)
   ├── Contains spec sections for:
   │   ├── pools: [...]
   │   ├── multiOrch: {...}
   │   ├── multiGateway: {...}
   │   └── localTopoServer: {...}
   │
   └── Watched by MultigresCluster controller when referenced
```

## Design Details: API Specification

### User Managed CR: MultigresCluster

  * The `databases.tablegroups.shards` structure is now fully exposed to the user.
  * `MultiOrch`, `MultiGateway`, and `LocalTopoServer` configs are **removed** from the cell/cluster level and now live *inside* the `shards[]` definition (or the `ShardTemplate` it references).

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
    etcd: "quay.io/coreos/etcd:v3.5.17"

  # ----------------------------------------------------------------
  # globalTopoServer Configuration
  # ----------------------------------------------------------------
  globalTopoServer:
    rootPath: "/multigres/global"
    managedSpec:
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
    replicas: 1
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"

  # ----------------------------------------------------------------
  # Cells Configuration
  # ----------------------------------------------------------------
  cells:
    - name: "us-east-1a"
      spec:
        zone: "us-east-1a"
        # Note: All 'multiGateway' and 'topoServer' configs
        # are REMOVED from the cell spec. The controller must
        # resolve these from this cell's shard definitions.

    - name: "us-east-1b"
      spec:
        zone: "us-east-1b"

  # ----------------------------------------------------------------
  # Database Configuration (Full Sharding)
  # ----------------------------------------------------------------
  databases:
    - name: "production_db"
      tablegroups:
        - name: "default"
          shards:
            # --- SHARD 1: Uses a Template ---
            - name: "0" # Future: name could be key range
              shardTemplateRef: "standard-ha-shard"

        - name: "orders_tg"
          shards:
            # --- SHARD 2: Uses Template + Overrides ---
            - name: "0"
              shardTemplateRef: "standard-ha-shard"
              overrides:
                # Override for a pool
                pools:
                  - type: "replica"
                    cell: "us-east-1a"
                    dataVolumeClaimTemplate:
                      resources:
                        requests:
                          storage: "1000Gi"
                # Override for MultiOrch
                multiOrch:
                  resources:
                    requests:
                      cpu: "500m"

            # --- SHARD 3: Uses Inline Definition ---
            - name: "1"
              # --- Inline Definition (mutually exclusive with template) ---
              spec:
                # 1. Shard-Level Component: MultiOrch
                multiOrch:
                  replicas: 1
                  resources:
                    requests:
                      cpu: "100m"
                      memory: "128Mi"
                
                # 2. Cell-Level Component: MultiGateway
                # This config will be used by the controller to
                # configure the 'us-east-1a' and 'us-east-1b' Cell CRs.
                # This will conflict with other shards in the same cell.
                multiGateway:
                  static:
                    replicas: 2
                    resources:
                      requests:
                        cpu: "500m"
                        memory: "512Mi"

                # 3. Cell-Level Component: LocalTopoServer
                # This will be used to configure the 'us-east-1a' Cell CR.
                localTopoServer:
                  cell: "us-east-1a"
                  rootPath: "/multigres/us-east-1a"
                  managedSpec:
                    replicas: 1
                    dataVolumeClaimTemplate:
                      resources:
                        requests:
                          storage: "5Gi"
                
                # 4. Pool-Level Components: Pools
                pools:
                  - type: "replica"
                    cell: "us-east-1a"
                    replicas: 2 
                    dataVolumeClaimTemplate:
                      resources:
                        requests:
                          storage: "75Gi"
                    postgres:
                      resources:
                        requests:
                          cpu: "1"
                    multipooler:
                      resources:
                        requests:
                          cpu: "100m"

# --- Status ---
status:
  observedGeneration: 1
  globalTopoServer:
    etcd:
      available: "True"
  conditions:
    - type: Available
      status: "True"
  cells:
    us-east-1a:
      gatewayAvailable: "True"
      topoServerAvailable: "True" 
    us-east-1b:
      gatewayAvailable: "True"
      topoServerAvailable: "True" # Using global
  multiadmin:
    available: "True"
  # Note: MultiOrch status is no longer global
```

### User Managed CR: ShardTemplate

  * This CR is now the template for a *complete* shard, including components that are logically cell-level.
  * The controller must read this template and "split" the configs:
      * `multiOrch` & `pools` -\> Go to the `Shard` child CR.
      * `multiGateway` & `localTopoServer` -\> Go to the `Cell` child CRs (after conflict resolution).

<!-- end list -->

```yaml
apiVersion: multigres.com/v1alpha1
kind: ShardTemplate
metadata:
  name: "standard-ha-shard"
  namespace: example
spec:
  # ----------------------------------------------------------------
  # 1. Shard-Level Component: MultiOrch
  # ----------------------------------------------------------------
  multiOrch:
    replicas: 1 # Deployed per cell, but configured per shard
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"
      limits:
        cpu: "200m"
        memory: "256Mi"

  # ----------------------------------------------------------------
  # 2. Cell-Level Component: MultiGateway
  # ----------------------------------------------------------------
  multiGateway:
    # This config will be used for ANY cell a shard pool
    # is deployed to, unless overridden.
    static:
      replicas: 2
      resources:
        requests:
          cpu: "500m"
          memory: "512Mi"
        limits:
          cpu: "1"
          memory: "1Gi"
    # dynamic:
    #   replicasPerCell: 1
    #   resourceMultiplier: 1.0

  # ----------------------------------------------------------------
  # 3. Cell-Level Component: LocalTopoServer
  # ----------------------------------------------------------------
  # Defines a template for a local topo server.
  # This config is applied to a cell IF a shard's pool
  # is deployed to that cell. Conflicts are not defined.
  localTopoServer:
    # rootPath will be set by controller (e.g., /multigres/<cell-name>)
    managedSpec:
      replicas: 1
      dataVolumeClaimTemplate:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: "5Gi"
  
  # ----------------------------------------------------------------
  # 4. Pool-Level Components: Pools
  # ----------------------------------------------------------------
  pools:
    - type: "replica"
      cell: "us-east-1a"
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
      multipooler:
        resources:
          requests:
            cpu: "500m"
            memory: "256Mi"

    - type: "replica"
      cell: "us-east-1b"
      replicas: 3
      # ... (similar config)
```

### Child CRs (Read-Only)

These specs are now populated by the `MultigresCluster` controller based on the complex, shard-level user configs.

#### Child CR: Cell

  * This CR's `spec` is no longer defined by the user. It is **fully resolved** by the `MultigresCluster` controller, which must pick a winning config from all shards deploying to this cell.
  * This CR is still the owner of the `MultiGateway` and `LocalTopoServer` deployments.

<!-- end list -->

```yaml
apiVersion: multigres.com/v1alpha1
kind: Cell
metadata:
  name: "example-multigres-cluster-us-east-1a"
  ownerReferences:
  - apiVersion: multigres.com/v1alpha1
    kind: MultigresCluster
    name: "example-multigres-cluster"
spec:
  name: "us-east-1a"
  zone: "us-east-1a" 
  images:
    multigateway: "multigres/multigres:latest"

  # This spec is RESOLVED by the parent controller
  # from a ShardTemplate or inline shard definition.
  multiGateway:
    static:
      replicas: 2
      resources:
        requests:
          cpu: "500m"
          memory: "512Mi"

  # This spec is also RESOLVED by the parent.
  topoServer:
    local:
      rootPath: "/multigres/us-east-1a"
      managedSpec:
        replicas: 1
        dataVolumeClaimTemplate:
          resources:
            requests:
              storage: "5Gi"
    global:
      rootPath: "/multigres/global"
      clientServiceName: "example-multigres-cluster-global-client"
  
  allCells:
  - "us-east-1a"
  - "us-east-1b"
status:
  gatewayAvailable: "True"
  topoServerAvailable: "True"
```

#### Child CR: TableGroup

  * The spec for this CR is now just a list of `shards`, copied/resolved from the `MultigresCluster` spec.
  * Its controller is responsible for creating the child `Shard` CRs.

<!-- end list -->

```yaml
apiVersion: multigres.com/v1alpha1
kind: TableGroup
metadata:
  name: "production-db-orders-tg"
  ownerReferences:
  - apiVersion: multigres.com/v1alpha1
    kind: MultigresCluster
    name: "example-multigres-cluster"
spec:
  images:
    multipooler: "multigres/multigres:latest"
    postgres: "postgres:15.3"
    multiorch: "multigres/multigres:latest"

  # This list is populated by the MultigresCluster controller
  # from the user's 'tablegroups.shards' spec.
  shards:
    - name: "0"
      multiOrch:
        replicas: 1
        resources: { ... }
      pools:
        - type: "replica"
          cell: "us-east-1a"
          replicas: 3
          # ...
    - name: "1"
      multiOrch:
        replicas: 1
        resources: { ... }
      pools:
        - type: "replica"
          cell: "us-east-1a"
          replicas: 2
          # ...
status:
  shards: 2
  readyShards: 2
```

#### Child CR: Shard

  * This CR is owned by `TableGroup`.
  * Its spec is populated by the `TableGroup` controller.
  * It is now the owner of the `MultiOrch` deployments for this shard (which it must deploy into each cell specified in `pools`).
  * It is also the owner of the `MultiPooler`/`Postgres` resources.

<!-- end list -->

```yaml
apiVersion: multigres.com/v1alpha1
kind: Shard
metadata:
  name: "production-db-orders-tg-0"
  ownerReferences:
  - apiVersion: multigres.com/v1alpha1
    kind: TableGroup
    name: "production-db-orders-tg"
spec:
  images:
    multipooler: "multigres/multigres:latest"
    postgres: "postgres:15.3"
    multiorch: "multigres/multigres:latest"

  # Spec copied from parent TableGroup.shards[]
  multiOrch:
    replicas: 1
    resources: { ... }
  
  pools:
    - type: "replica"
      cell: "us-east-1a"
      replicas: 3
      # ...
status:
  multiorchAvailable: "True"
  primaryCell: "us-east-1a"
  totalPods: 3
  readyPods: 3
```

## Open Issues / Design Questions

  * **N:1 Conflict Resolution:** This is the most significant new issue. The design **must** define how the `MultigresCluster` controller resolves conflicts when multiple shards in the same cell provide different configs for `MultiGateway` or `LocalTopoServer`. (e.g., "first shard wins," "merge," "error," "default shard only").
  * **API Complexity:** The API is now significantly more complex. Placing cell-level component configs inside a shard-level template creates a "leaky abstraction" that is likely to confuse users, as it does not match the realized architecture.
  * **Component Lifecycle:** The lifecycle of a `MultiGateway` is now tied to the shards within its cell. What happens to the `MultiGateway` when the *last* shard is removed from a cell?

## Implementation History

  * **2025-10-08:** Initial proposal (individual, user-managed CRDs).
  * **2025-10-14:** Second proposal (top-level `MultigresCluster` CR).
  * **2025-10-28:** "Parent/child" model formalized with `DeploymentTemplate`.
  * **2025-11-05 (Consolidated):** Simplified v1 API (one-shard-per-db), `ShardTemplate`, `MultiOrch` global.
  * **2025-11-05 (Revision 2):** `TableGroup` CR updated to use a `shards:[]` list.
  * **2025-11-06 (Revision 3):** Reverted to full `databases.tablegroups.shards` user-facing API. Moved `MultiOrch`, `MultiGateway`, and `LocalTopoServer` configs into the `ShardTemplate` / inline shard definition per client feedback. This introduces a major N:1 conflict resolution problem.

## WARNING

We consider this design to be flawed, please read below why before committing to go forward with this.


## Core Design & Architectural Flaws

* **1. The "N:1" Conflict Problem:** This is the most significant issue. A single Cell (like `us-east-1a`) will contain **many** Shards (N) but only **one** `MultiGateway` and **one** `LocalTopoServer` (1). This design allows every shard to provide a *different* configuration for those single components. The operator has no way to logically resolve this:
    * If `Shard-A`'s template specifies 2 replicas for `MultiGateway` and `Shard-B`'s template specifies 4, which one wins?
    * What if `Shard-A` defines a `LocalTopoServer` but `Shard-B` in the same cell does not?
    * This forces you to implement a complex and arbitrary conflict-resolution strategy (e.g., "first shard to be created in the cell wins"), which is brittle and non-obvious.

* **2. Violation of Separation of Concerns:** The `ShardTemplate` is now a "god object." It's being forced to define the configuration for components that have completely different lifecycles and scopes:
    * **Pools** (Shard x Cell scope)
    * **`MultiOrch`** (Shard scope)
    * **`MultiGateway`** (Cell scope)
    * **`LocalTopoServer`** (Cell scope)
    This creates a "leaky abstraction" and is a classic API design anti-pattern.

* **3. Mismatched Lifecycles & Brittleness:** This design dangerously ties the lifecycle of a **Cell-level** component to a **Shard-level** component.
    * **Example:** Imagine `Shard-A` is the "winner" whose `MultiGateway` config is used for `us-east-1a`. What happens when a user **deletes `Shard-A`**?
    * The controller must now re-evaluate all other shards in that cell and "promote" a different shard's config (e.g., `Shard-B`'s) to be the new source of truth for the `MultiGateway`.
    * This means **deleting a database shard could unexpectedly reconfigure the entire cell's gateway**, causing an outage or performance change. This is a massive, non-obvious side effect.

---

## Implementation & Controller Complexity

* **1. Complex "At-a-Distance" Reconciliation:** The `MultigresCluster` controller (the parent) becomes incredibly complex. Instead of just copying a user's `cell.multiGateway` spec into the `Cell` CR, it must now:
    1.  Discover all shards being deployed to a given cell.
    2.  Read all of their (potentially different) `ShardTemplate`s or inline specs.
    3.  Implement the conflict-resolution logic ("pick a winner").
    4.  Inject the "winning" config into the `Cell` CR's spec.
    This is a huge, stateful, and difficult-to-test piece of logic.

* **2. Ambiguous Source of Truth:** The realized `Cell` child CR is no longer a simple reflection of the user's intent. Its spec is the *result* of a complex, hidden calculation.
    * This makes debugging impossible. A developer running `kubectl get cell us-east-1a -o yaml` will see a `multiGateway` spec but have **no idea where that configuration came from**.
    * They can't trace it back to the `MultigresCluster` CR because the config isn't there; it's hidden in one of many `ShardTemplate`s.

---

## User Experience (UX) & Operational Downsides

* **1. Extreme User Confusion:** This design is not discoverable.
    * **Question:** "I want to change the `MultiGateway` replicas in `us-east-1a`."
    * **Answer:** "You have to find which of the 10 shards in that cell was the 'first' one created, find its `ShardTemplate`, and edit the `multiGateway` block in that one file."
    * This is an unusable and confusing user experience.

* **2. Misleading and "Lying" API:** The `ShardTemplate` *implies* it is configuring a `MultiGateway` **for that shard**, which is false. The `MultiGateway` is a shared, cell-level component. This "lie" makes the API's behavior difficult to predict.

* **3. Useless Reusability:** The entire point of a `ShardTemplate` is to be reusable. This design makes reuse a minefield.
    * A user can't safely re-use a "standard-ha-shard" template because it *also* carries cell-level configs that may conflict with another shard's template.
    * Users will be forced to create highly specific, non-reusable templates just to avoid config conflicts, defeating the purpose of having templates at all.


## What if we disallow the creation of more than one shard per database with the controller?

This approach, while seeming like a solution, **fails to solve the core architectural problems** and only introduces new, arbitrary limitations.

* **It Does Not Solve the "N:1 Conflict":** The fundamental conflict still exists. A cell (e.g., `us-east-1a`) can (and will) still contain multiple shards, but they will be from *different databases* (`Database-A` and `Database-B`). The controller would still face the same impossible choice: *which shard's template* should define the `MultiGateway` config for the shared cell? This rule only moves the conflict, it does not resolve it.
* **It Makes the Operator Less Useful:** It places an artificial constraint on the user that isn't present in Multigres itself. The user is now prevented from using a core sharding feature for no logical reason, but the API *still* forces them to use the confusing, architecturally-incorrect "god object" `ShardTemplate`.
* **It Fails to Fix the "Lying API":** The API remains confusing. The user must still edit a shard-level object to configure a cell-level component, violating the "separation of concerns" principle.

---

## What if we disallow the creation of more than one shard per MultigresCluster?

This is a much more extreme limitation that technically solves the "N:1 Conflict," but aside from the obvious limitation of having a single database multigres cluster we will still have the following issue: 

* **It Still Doesn't Fix the Confusing API:** The user must *still* use the flawed, "lying" API. To configure the cell's gateway, they have to find and edit the *one and only shard*. This is still non-intuitive for users who get familiar with Multigres architecture. It fixes the conflict by sacrificing features and flexibility, while still leaving the user with a confusing and architecturally-incorrect API.

If we were to commit to this spec, this would be the only way we would recommend using it.

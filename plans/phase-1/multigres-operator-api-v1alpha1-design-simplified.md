# MultigresCluster API (v1alpha1) Proposal

**State:** Proposed

## Summary

This proposal defines the `v1alpha1` API for the Multigres Operator. The design is centered on three user-editable Custom Resources (CRs):

1.  **`MultigresCluster`**: The root resource that defines the desired state (intent) of the entire cluster. It *composes* its configuration by referencing templates or defining components inline.
2.  **`DeploymentTemplate`**: A flexible, reusable, namespaced "library" for defining defaults for **one or more** cluster-level components (e.g., just `multiadmin`, or `cells` and `globalTopoServer` together).
3.  **`ShardTemplate`**: A reusable, namespaced resource for defining **shard-level** configurations (orchestrator and resource pools).

All other resources (`TopoServer`, `Cell`, `TableGroup`, `Shard`) are implemented as read-only child CRs owned by the `MultigresCluster`. These child CRs reflect the *realized* state of the system and are managed by their own dedicated controllers.

This "layered" model, which separates cluster configuration from shard configuration, provides a simple user experience for the common case while remaining architecturally sound, extensible, and secure.

## Motivation

Managing a distributed, sharded database system across multiple failure domains (cells) is inherently complex. This proposal advocates for a parent/child CRD model with layered templating to address these challenges:

  * **Separation of Concerns:** The API is structured to match the Multigres architecture. Cell-level components (like `MultiGateway`) are configured at the cell level. Shard-level components (like `MultiOrch`) are configured at the shard level.
  * **Hierarchical Observability:** This model provides a clean, hierarchical tree for status aggregation. A failure in a `Shard` can be clearly observed on the `Shard` CR, its `TableGroup` parent, and finally aggregated up to the root `MultigresCluster`.
  * **Ultimate Flexibility:** Users can choose their preferred configuration style:
      * **Simple (Global Template):** Reference a single "master" `DeploymentTemplate` for all cluster settings.
      * **Simple (Per-Component Template):** Reference a different, smaller `DeploymentTemplate` for each component (e.g., `multiadmin`, `cells`).
      * **Hybrid:** Define some components inline (like `multiadmin`) while referencing templates for others.
      * **Advanced (Full Inline):** Define all components inline with no templates.
  * **Powerful Overrides:** The "layered" configuration provides a clear precedence waterfall, allowing users to override any default inline for advanced use cases.
  * **RBAC & Security:** Separating cluster infrastructure (`DeploymentTemplate`) from database workloads (`ShardTemplate`) allows for clean, granular RBAC for different teams (e.g., Platform Admins vs. DBAs).

## API Architecture: Layered Configuration

This design solves configuration conflicts and lifecycle issues by separating templates based on their logical scope.

1.  **`DeploymentTemplate` (Flexible Library):** A template for the *cluster itself*. It can define defaults for **one component** (like just `multiadmin`) or **many components** (`cells`, `globalTopoServer`, `multiadmin`, etc.).
2.  **`ShardTemplate` (Pure Shard Config):** A *pure* template for a shard. It *only* defines **Shard-level** components: `MultiOrch` and `Pools`.

Configuration for each cluster component (e.g., `multiadmin`) is resolved with the following precedence, from lowest to highest:

1.  **Global `deploymentTemplateRef`:** A single template referenced at the `spec` level, providing defaults for *all* components.
2.  **Per-Component `templateRef`:** A template referenced by a specific component (e.g., `spec.multiadmin.templateRef`). This *overrides* any `multiadmin` config from the global template.
3.  **Per-Component `overrides`:** An inline block (e.g., `spec.multiadmin.overrides`) that is deep-merged on top of the loaded template (from step 1 or 2).
4.  **Full Inline Definition:** A user can *instead* provide a full inline spec (e.g., `spec.multiadmin.replicas: 1`). This is mutually exclusive with the `templateRef` and `overrides` keys for that component.

-----

### Example Component: `DeploymentTemplate`

This CRD is a flexible "library." A single template can contain one or many component definitions.

```yaml
apiVersion: multigres.com/v1alpha1
kind: DeploymentTemplate
metadata:
  name: "standard-ha-cluster"
  namespace: example
spec:
  # This template defines MULTIPLE cluster components
  images:
    imagePullPolicy: "IfNotPresent"
    multigateway: "multigres/multigres:latest"
    multiadmin: "multigres/multigres:latest"
    etcd: "quay.io/coreos/etcd:v3.5.17"

  globalTopoServer:
    rootPath: "/multigres/global"
    managedSpec: { replicas: 3 }
  
  multiadmin:
    replicas: 1
    resources: { }

  cells:
    - name: "us-east-1a"
      spec:
        zone: "us-east-1a"
        multiGateway: { static: { replicas: 2 } }
    - name: "us-east-1b"
      spec:
        zone: "us-east-1b"
        multiGateway: { static: { replicas: 2 } }
---
apiVersion: multigres.com/v1alpha1
kind: DeploymentTemplate
metadata:
  name: "small-multiadmin-only"
  namespace: example
spec:
  # This template defines ONLY ONE component
  multiadmin:
    replicas: 1
    resources:
      requests:
        cpu: "50m"
        memory: "64Mi"
```

### Example Component: `ShardTemplate` (Pure & Reusable)

This template is simple and defines *only* shard components. It is 100% reusable.

```yaml
apiVersion: multigres.com/v1alpha1
kind: ShardTemplate
metadata:
  name: "standard-ha-shard"
  namespace: example
spec:
  # 1. Shard-Level Component: MultiOrch
  multiOrch:
    replicas: 1
    resources: { }

  # 2. Pool-Level Components: Pools
  pools:
    - type: "replica"
      cell: "us-east-1a"
      replicas: 3
      dataVolumeClaimTemplate: { }
      postgres: { }
      multipooler: { }
    
    - type: "replica"
      cell: "us-east-1b"
      replicas: 3
      # ... (similar config)
```

-----

### Example `MultigresCluster` Use Cases

This model supports multiple user styles, from simple to advanced.

#### Example 1: The "Simple Global Template" Path (Simplest Case)

The user points to one "master" template for all cluster defaults. This is the cleanest possible user experience.

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: "my-simple-cluster"
spec:
  # 1. Point to ONE global CLUSTER template.
  # This provides defaults for images, globalTopoServer,
  # multiadmin, and cells.
  deploymentTemplateRef: "standard-ha-cluster"

  # 2. Define databases and point to SHARD templates
  databases:
    - name: "production_db"
      tablegroups:
        - name: "default"
          shards:
            - name: "0"
              shardTemplateRef: "standard-ha-shard"
```

#### Example 2: The "Per-Component Template" Path (Flexible)

The user does *not* use a global template, but points each component to a specific, smaller template.

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: "my-component-cluster"
spec:
  # No global deploymentTemplateRef

  # 1. Point each cluster component to its own template.
  images:
    templateRef: "standard-ha-cluster" # Uses 'images' from this template
  
  globalTopoServer:
    templateRef: "standard-ha-cluster" # Uses 'globalTopoServer'

  multiadmin:
    templateRef: "small-multiadmin-only" # Uses the smaller, specific template

  cells:
    templateRef: "standard-ha-cluster" # Uses 'cells'

  # 2. Define databases and point to SHARD templates
  databases:
    - name: "production_db"
      tablegroups:
        - name: "default"
          shards:
            - name: "0"
              shardTemplateRef: "standard-ha-shard"
```

#### Example 3: The "Hybrid / Override" Path (Mix and Match)

The user uses a global template as a base, but overrides specific parts inline or with different component templates.

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: "my-hybrid-cluster"
spec:
  # 1. Start with the global template as a base
  deploymentTemplateRef: "standard-ha-cluster"
  
  # 2. globalTopoServer is INLINE (overrides global template)
  globalTopoServer:
    rootPath: "/my/custom/path"
    managedSpec: { replicas: 5 }

  # 3. multiadmin uses a DIFFERENT template (overrides global template)
  multiadmin:
    templateRef: "small-multiadmin-only"

  # 4. cells uses the global template, but with OVERRIDES
  cells:
    # 'templateRef' is implicitly "standard-ha-cluster"
    overrides:
      # Overrides 'us-east-1a' from the template
      - name: "us-east-1a"
        spec:
          multiGateway:
            static:
              replicas: 4 # Override template's default of 2
      # Adds a NEW cell not in the template
      - name: "us-west-2"
        spec:
          zone: "us-west-2"
          multiGateway: { static: { replicas: 2 } }

  # 5. Databases use ShardTemplates
  databases:
    - name: "analytics_db"
      tablegroups:
        - name: "default"
          shards:
            - name: "0"
              shardTemplateRef: "standard-ha-shard"
```

#### Example 4: The "Full Inline Path" (Power-User)

The user defines everything inline, using no templates at all.

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: "my-full-inline-cluster"
spec:
  # No templates referenced
  images:
    imagePullPolicy: "IfNotPresent"
    # ... all images
  
  globalTopoServer:
    managedSpec: { replicas: 3 }

  multiadmin:
    replicas: 1
    resources: { }

  cells:
    - name: "us-east-1a"
      spec:
        zone: "us-east-1a"
        multiGateway: { static: { replicas: 2 } }

  databases:
    - name: "production_db"
      tablegroups:
        - name: "default"
          shards:
            - name: "0"
              # Shard is also defined inline
              spec:
                multiOrch:
                  replicas: 1
                pools:
                  - type: "replica"
                    cell: "us-east-1a"
                    replicas: 2
                    # ...
```

-----

## Design Rationale: Why Not Just Use One Template to Make it Even Simpler?

> Despite the downsides to this approach below, this could still be considered for v1 as it would make it even simpler.

Another suggestion is to reduce the number of CRDs by using a single, flexible `DeploymentTemplate` CRD for *both* cluster and shard configurations. In this model, a user could create one `DeploymentTemplate` instance that *only* contains `spec.cells` (for the cluster) and another instance that *only* contains `spec.pools` (for the shard).

While this is technically possible, there are two caveats:

### 1\. It Lacks Semantic Clarity

By using one `kind` for two *semantically different* purposes, the API becomes confusing. A user running `kubectl get deploymenttemplates` would see a mixed list of resources:

  * `standard-cluster-layout` (Defines cells, gateways, etc.)
  * `small-shard-config` (Defines pools and an orchestrator)
  * `large-shard-config` (Defines different pools and an orchestrator)

The `kind: DeploymentTemplate` no longer tells the user what the resource *is for*. Having two separate, purpose-built CRDs (`DeploymentTemplate` and `ShardTemplate`) is far clearer.

### 2\. It Prevents Granular RBAC

This is the most significant downside. In a production environment, different teams have different responsibilities:

  * **Platform Admins:** Manage the infrastructure (cells, gateways).
  * **DBAs/Developers:** Manage the database workloads (shards, pools).

With a single `DeploymentTemplate` CRD, it is **impossible** to enforce this separation with Kubernetes RBAC. If you give a DBA permission to create `deploymenttemplates`, they can *also* create one that defines a new `MultiGateway` or `Cell`, which is a security and governance risk.

The two-CRD model solves this cleanly:

  * **Platform Admin Role:** Can create/edit `deploymenttemplates`.
  * **DBA/Developer Role:** Can create/edit `shardtemplates`.


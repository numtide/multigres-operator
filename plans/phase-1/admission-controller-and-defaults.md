# Integrate Hybrid Admission Control for Cluster Validation and Defaulting

**State:** Draft
**Tags:** [validation, mutation, webhook, cel, security, data-handler]


## Summary

This proposal outlines a hybrid, four-tiered admission control strategy for the Multigres operator. It combines:

1. **Basic CRD Schema** (OpenAPI v3)
2. **CRD-Embedded CEL** (`XValidation`)
3. **`ValidatingAdmissionPolicy` (CEL)** resources
4. A **Validating and Mutating Webhook Server**

This layered approach uses the most efficient tool for each task, from basic field validation to complex, stateful checks and mutation, while solving the "invisible defaults" problem.

---

## Motivation

### Motivation 1: The "Invisible Defaults" Problem

A review of classic operators like `vitess-operator` highlights a major user-facing weakness: applying complex defaults *in-memory* during reconciliation.

* **The "Classic" Problem:** The `vitess-operator` avoids webhooks entirely. It uses simple OpenAPI v3 CRD defaults (Level 1) for static values but applies all complex, dynamic defaults (like images and resource requests) via Go code *after* fetching the object (Level 2).
* **The User-Facing Confusion:** Because these Level 2 defaults are not persisted back to `etcd`, a user running `kubectl get` on a CR sees a spec that does **not** match the functional state the operator is enforcing. This is confirmed by a 6-year-old `TODO` in their code explicitly mentioning the lack of mutating webhook support in Operator SDK as a blocker.
* **Our Motivation:** This "invisible" default pattern is unacceptable for `multigres-operator`. Our API relies on a sophisticated **4-Level Override Chain** (Inline -> Cluster Default -> Namespace Default -> Operator Hardcoded). We *must* make sure our manifests are fully declarative and the user sees the complete, resolved CR with `kubectl get`. A **mutating webhook** is the correct solution, as it applies all defaults and persists them to `etcd` *before* the reconciler ever sees the object.

### Motivation 2: The Need for Powerful, Multi-Layered Validation

Our API design (Design 6) requires validation logic that is too complex for a single tool.

* **Simple Cross-Field Logic:** We need to enforce rules like mutual exclusion (e.g., "you can't set both `etcd` and `templateRef` on `GlobalTopoServer`"). This is best handled *inside* the CRD.
* **Context-Aware Logic:** We need to enforce the **Read-Only Child Resource** pattern. Users must be prevented from manually editing `Cell`, `TableGroup`, `Shard`, or `TopoServer` resources, as these are owned by the `MultigresCluster`. This requires checking `request.userInfo` (Level 3).
* **Stateful Logic:** We need to enforce rules that query *other objects*. This includes protecting a `CoreTemplate`, `CellTemplate`, or `ShardTemplate` from deletion if a `MultigresCluster` is using it, or checking if these templates exist on `MultigresCluster` creation. This logic can *only* be performed by a webhook.

---

## Goals

* Implement a **mutating webhook** to execute the **4-Level Override Chain**, resolving defaults for `GlobalTopoServer`, `MultiAdmin`, `Cells`, and `Shards` into the `MultigresCluster` spec, solving the "invisible defaults" problem.
* Implement a **stateful validating webhook** for rules that require cluster-awareness (querying other objects), such as:
* Preventing the deletion of a `CoreTemplate`, `CellTemplate`, or `ShardTemplate` that is in use by any `MultigresCluster`.
* Validating that referenced templates exist upon `MultigresCluster` creation or update.


* Utilize **CRD-Embedded CEL (`XValidation`)** for all advanced *stateless, object-only* validation (e.g., mutual exclusion, name length limits).
* Utilize **`ValidatingAdmissionPolicy` (CEL)** for *stateless, context-aware* validation that needs access to `request.userInfo` (specifically to enforce read-only child resources).
* Provide clear, actionable error messages at admission time.
* Support multiple certificate management strategies (cert-manager and self-contained).
* Keep webhook logic in the `data-handler` module.

---

## Non-Goals

* Using the webhook server for *any* validation that can be handled by CRD-Embedded CEL or a `ValidatingAdmissionPolicy`. The webhook is the most expensive layer and must be reserved for logic that truly requires it.
* **Creating Child Resources:** The webhook resolves configuration (defaults) but does *not* create the child `Cell` or `Shard` CRs. That remains the responsibility of the Reconciler.

---

## Proposal: A Hybrid 4-Tier Admission Control Strategy

We will implement a four-tiered admission control system, where each request passes through these gates in order.

### Level 1: CRD OpenAPI Schema (Stateless, Basic)

This is the baseline, built into all our CRDs. It is the fastest check, enforced by the API server before any other logic.

* **Use Case:** Enforcing data types (string, int, object), `required` fields, and simple constraints.
* **Implementation:** `kubebuilder` validation markers (e.g., `// +kubebuilder:validation:Minimum=1`, `// +kubebuilder:validation:MaxLength=63`, `// +kubebuilder:validation:Enum=zone;region`).

### Level 2: CRD-Embedded CEL (Stateless, Advanced)

This logic is embedded directly into the CRD's OpenAPI v3.1 schema and is enforced by the API server. It can perform any validation that *only* requires access to the `object` (the new object state, as `self`).

* **Use Case:** Complex cross-field validation, mutual exclusion, and business logic.
* **Implementation:** `// +kubebuilder:validation:XValidation` markers in the Go type definitions.
* **Real Examples (Updated for Design 6):**
* **Mutual Exclusion (`GlobalTopoServer`):** Ensure a user provides only one configuration source.
`rule="!((has(self.etcd) && has(self.external)) || (has(self.etcd) && has(self.templateRef)) || (has(self.external) && has(self.templateRef)))", message="You can specify only one of: etcd, external, or templateRef."`
* **Conditional Requirement (`MultigresCluster`):** Ensure `overrides` can only be set if `templateRef` (or specific component template field) is also set.
* **Business Logic (`MultigresCluster`):** Ensure name lengths allow for generated child names.
`rule="size(self.metadata.name) + size(self.spec.databases[].name) < 50", message="Combined length of Cluster and Database names must be less than 50 characters."`
* **Required Sub-Field (`ShardTemplate`):** Ensure pools include storage requests.
`rule="!has(self.pools) || self.pools.all(k, p, has(p.storage))", message="All pools must define storage configuration."`



### Level 3: `ValidatingAdmissionPolicy` (CEL) (Stateless, Context-Aware)

This uses separate, cluster-level resources to apply validation. These policies are also executed by the API server but can access additional request context that CRD-Embedded CEL (Level 2) cannot.

* **Use Case 1: Accessing `request.userInfo`:** **Enforcing Read-Only Child Resources.**
The `MultigresCluster` is the single source of truth. The child resources (`Cell`, `TableGroup`, `Shard`, `TopoServer`) reflect realized state and should not be edited by users.
* **Use Case 2: Accessing `oldObject`:** Enforcing safe updates, like preventing storage shrinking.
* **Implementation:** `ValidatingAdmissionPolicy` and `ValidatingAdmissionPolicyBinding` manifests shipped with the operator.
* **Trade-off:** This introduces a dependency on Kubernetes **v1.30+** (where `ValidatingAdmissionPolicy` is GA).

### Level 4: Webhook Server (Stateful & Mutating)

This is the final and most powerful layer: an HTTP server run by our operator. It is the only layer that can **mutate objects** and **query other objects** during admission.

* **Use Case 1: Mutation (The 4-Level Override Chain).**
* **Goal:** Solve the "invisible defaults" problem and resolve the complex override logic defined in Design (6).
* **Logic:** On `MultigresCluster` `CREATE` or `UPDATE`, the webhook will apply defaults for `GlobalTopoServer`, `MultiAdmin`, `Cells`, and `Shards` following this priority:
1. **Inline Spec:** Use if present.
2. **Explicit Template:** If `templateRef` (or `cellTemplate`/`shardTemplate`) is set, load that template.
3. **Cluster Default:** If `spec.templateDefaults.core/cell/shardTemplate` is set, load that template.
4. **Namespace Default:** Look for a template named "default" in the namespace.
5. **Operator Default:** Apply hardcoded values.




* **Use Case 2: Stateful Validation (Referential Integrity).**
* **Goal:** Enforce rules that require querying the API server.
* **Logic:**
1. On `CoreTemplate`/`CellTemplate`/`ShardTemplate` `DELETE`: **`List`** all `MultigresCluster`s. If any cluster references this template, **reject** the deletion.
2. On `MultigresCluster` `CREATE` or `UPDATE`: **`Get`** every referenced `CoreTemplate`, `CellTemplate`, and `ShardTemplate` to ensure they exist.





---

## Design Details

### 1. Admission Control Layer Manifests

We will ship manifests for Layers 3 and 4. Layers 1 and 2 are built into the CRD itself (generated from the `+kubebuilder` comments in the Go types).

#### `ValidatingAdmissionPolicy` Manifests (Layer 3)

**Example 1: Read-Only Child Resources (Uses `request.userInfo`)**

```yaml
apiVersion: admission.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: "multigres-readonly-child-resources"
spec:
  failurePolicy: Fail
  matchConstraints:
    resourceRules:
    - apiGroups:   ["multigres.com"]
      apiVersions: ["v1alpha1"]
      operations:  ["CREATE", "UPDATE", "DELETE"]
      resources:   ["cells", "tablegroups", "shards", "toposervers"]
  validations:
    - expression: "request.userInfo.username == 'system:serviceaccount:multigres-system:multigres-operator'"
      message: "This resource is managed by the Multigres operator and cannot be modified manually. Edit the MultigresCluster CR instead"

```

**Example 2: Safe Updates on Parent Resource (Uses `oldObject.status`)**

```yaml
apiVersion: admission.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: "multigres-cluster-safe-updates"
spec:
  failurePolicy: Fail
  matchConstraints:
    resourceRules:
    - apiGroups:   ["multigres.com"]
      apiVersions: ["v1alpha1"]
      operations:  ["UPDATE"]
      resources:   ["multigresclusters"]
  validations:
    # Example: Prevent Storage Shrinking
    - expression: "!has(object.spec.globalTopoServer.etcd) || object.spec.globalTopoServer.etcd.storage.size >= oldObject.spec.globalTopoServer.etcd.storage.size"
      message: "storage size cannot be decreased."

```

#### 1.1. Manual Creation of `ValidatingAdmissionPolicy` Manifests

It is critical to understand that **`ValidatingAdmissionPolicy` (Layer 3) manifests cannot be generated from Go code comments.**

* **Kubebuilder markers** (e.g., `+kubebuilder:validation:XValidation`) *only* generate **CRD-Embedded CEL (Level 2)**. They are part of the API's contract.
* **`ValidatingAdmissionPolicy`** resources are designed to be separate, cluster-level policies. They are decoupled from the operator's code and can be managed by cluster administrators.

Therefore, the "usual process" for including these policies is manual:

1. **Hand-write YAML:** The policies (like the examples above) are written as static YAML files.
2. **Store in `/config`:** These YAMLs will be stored in a new directory, e.g., `config/policies/`.
3. **Bundle with Kustomize:** We will "tag on" these manifests by editing the `config/default/kustomization.yaml` to include this new directory.

```yaml
# config/default/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../crd
  - ../rbac
  - ../webhook
  - ../policies  # <-- This new line bundles the hand-written policy manifests
  - manager.yaml

```

This process treats our default policies as configuration we ship, not as code we generate.

#### Webhook Configuration Manifests (Layer 4)

These *are* generated from the `+kubebuilder:webhook` comments in our Go code. The `${CA_BUNDLE}` placeholder would be populated by the certificate management strategy (e.g., cert-manager or the in-process generator).

**Mutating Webhook (For Defaulting)**

This configuration intercepts `CREATE` and `UPDATE` events for `multigresclusters` to apply the **4-Level Override Chain**.

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: multigres-mutating-webhook
webhooks:
  - name: mmultigrescluster.kb.io
    rules:
      - apiGroups: ["multigres.com"]
        apiVersions: ["v1alpha1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["multigresclusters"]
    clientConfig:
      service:
        name: multigres-operator-webhook
        namespace: multigres-system
        path: /mutate-multigres-com-v1alpha1-multigrescluster
      caBundle: ${CA_BUNDLE}
    admissionReviewVersions: ["v1"]
    sideEffects: None
    timeoutSeconds: 10
    failurePolicy: Fail

```

**Validating Webhook (For Stateful Checks)**

This configuration intercepts events for `MultigresCluster` (existence check) and all template types (in-use check).

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: multigres-validating-webhook
webhooks:
  # Validate Cluster Config (Template Existence)
  - name: vmultigrescluster.kb.io
    rules:
      - apiGroups: ["multigres.com"]
        apiVersions: ["v1alpha1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["multigresclusters"]
    clientConfig:
      service:
        name: multigres-operator-webhook
        namespace: multigres-system
        path: /validate-multigres-com-v1alpha1-multigrescluster
      caBundle: ${CA_BUNDLE}
    admissionReviewVersions: ["v1"]
    sideEffects: None
    timeoutSeconds: 10
    failurePolicy: Fail
    
  # Protect CoreTemplates
  - name: vcoretemplate.kb.io
    rules:
      - apiGroups: ["multigres.com"]
        apiVersions: ["v1alpha1"]
        operations: ["DELETE"]
        resources: ["coretemplates"]
    clientConfig:
      service:
        name: multigres-operator-webhook
        namespace: multigres-system
        path: /validate-multigres-com-v1alpha1-coretemplate
      caBundle: ${CA_BUNDLE}
    admissionReviewVersions: ["v1"]
    sideEffects: None
    timeoutSeconds: 10
    failurePolicy: Fail

  # Protect CellTemplates
  - name: vcelltemplate.kb.io
    rules:
      - apiGroups: ["multigres.com"]
        apiVersions: ["v1alpha1"]
        operations: ["DELETE"]
        resources: ["celltemplates"]
    clientConfig:
      service:
        name: multigres-operator-webhook
        namespace: multigres-system
        path: /validate-multigres-com-v1alpha1-celltemplate
      caBundle: ${CA_BUNDLE}
    admissionReviewVersions: ["v1"]
    sideEffects: None
    timeoutSeconds: 10
    failurePolicy: Fail

  # Protect ShardTemplates
  - name: vshardtemplate.kb.io
    rules:
      - apiGroups: ["multigres.com"]
        apiVersions: ["v1alpha1"]
        operations: ["DELETE"]
        resources: ["shardtemplates"]
    clientConfig:
      service:
        name: multigres-operator-webhook
        namespace: multigres-system
        path: /validate-multigres-com-v1alpha1-shardtemplate
      caBundle: ${CA_BUNDLE}
    admissionReviewVersions: ["v1"]
    sideEffects: None
    timeoutSeconds: 10
    failurePolicy: Fail

```

### 2. Implementation Components

Located in `pkg/data-handler/webhook/`:

```go
// +kubebuilder:webhook:path=/mutate-multigres-com-v1alpha1-multigrescluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=multigres.com,resources=multigresclusters,verbs=create;update,versions=v1alpha1,name=mmultigrescluster.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-multigres-com-v1alpha1-multigrescluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=multigres.com,resources=multigresclusters,verbs=create;update,versions=v1alpha1,name=vmultigrescluster.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-multigres-com-v1alpha1-coretemplate,mutating=false,failurePolicy=fail,sideEffects=None,groups=multigres.com,resources=coretemplates,verbs=delete,versions=v1alpha1,name=vcoretemplate.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-multigres-com-v1alpha1-celltemplate,mutating=false,failurePolicy=fail,sideEffects=None,groups=multigres.com,resources=celltemplates,verbs=delete,versions=v1alpha1,name=vcelltemplate.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-multigres-com-v1alpha1-shardtemplate,mutating=false,failurePolicy=fail,sideEffects=None,groups=multigres.com,resources=shardtemplates,verbs=delete,versions=v1alpha1,name=vshardtemplate.kb.io,admissionReviewVersions=v1

// AdmissionHandler implements admission.Handler
type AdmissionHandler struct {
    Client client.Client
    // decoder, etc.
}

// Implement the admission.Handler interface
func (h *AdmissionHandler) Handle(ctx context.Context, req admission.Request) admission.Response

// Logic for Mutating MultigresCluster (Override Chain)
func (h *AdmissionHandler) mutateMultigresCluster(ctx context.Context, cluster *v1alpha1.MultigresCluster) admission.Response {
    // 1. Resolve GlobalTopoServer & MultiAdmin (Inline -> TemplateRef -> ClusterDefault -> NSDefault -> OpDefault)
    // 2. Iterate Cells -> Resolve (Inline -> CellTemplate -> ClusterDefault -> NSDefault -> OpDefault)
    // 3. Iterate Databases -> TableGroups -> Shards -> Resolve (Inline -> ShardTemplate -> ClusterDefault -> NSDefault -> OpDefault)
    // 4. Return Patched Object
}

// Logic for Validating MultigresCluster (Template Existence)
func (h *AdmissionHandler) validateMultigresCluster(ctx context.Context, req admission.Request) admission.Response {
     // 1. Identify all referenced templates in spec.templateDefaults and specific component refs.
     // 2. Perform Client.Get() for each.
     // 3. If any missing, return Denied.
}

// Logic for Validating Template Deletion (In-Use Check)
func (h *AdmissionHandler) validateCoreTemplateDelete(ctx context.Context, req admission.Request) admission.Response {
    // 1. Client.List() all MultigresClusters.
    // 2. Iterate and check if cluster.spec references req.Name.
    // 3. If found, return Denied.
}
// (Repeat logic for CellTemplate and ShardTemplate)

```

The `main.go` will be modified to register the webhook server with the `controller-runtime` manager.

### 3. Certificate Management

Since we **must** run a webhook server (Layer 4), we must manage TLS certificates. We will support three strategies.

#### Strategy A: cert-manager (Recommended for Production)

* **Pros:** Automatic certificate generation, renewal, and CA bundle injection. Industry standard.
* **Cons:** Adds a runtime dependency on `cert-manager`.
* **Implementation:** Ship `Certificate` and `Issuer` manifests in `config/webhook/cert-manager`.

#### Strategy B: Init Container (Recommended for Simplicity)

* **Pros:** No external dependencies; self-contained installation.
* **Cons:** Certificates are typically long-lived and require a manual rotation process (e.g., re-running a Job).
* **Implementation:** Use a `batch/v1.Job` with an image like `kube-webhook-certgen` to create the secret, and a patching job to inject the `caBundle`.

#### Strategy C: In-Process Operator Generation (Self-Bootstrapping)

* **Pros:** **Zero dependencies.** The operator is fully self-contained and manages its own certificate lifecycle, including rotation. Solves startup race conditions.
* **Cons:** **High implementation complexity.** The operator must contain all the logic for crypto generation, `Secret` management, and `ValidatingWebhookConfiguration` patching. It also requires **higher RBAC permissions**.
* **Implementation:** The operator's `main.go` would include logic on startup to manage the certificate lifecycle.

#### Recommendation

**Strategy A (cert-manager)** is the recommended approach for production environments.

**Strategy C (In-Process)** is a strong runner-up and superior to Strategy B. We should support **both A and C** to allow users to choose between a "zero-dependency" installation (C) and an "industry-standard" installation (A).

---

## Test Plan

### Unit Tests

* Test all pure validation functions in `validation.go` in isolation.
* **Override Chain Logic:** Feed the mutator partial configs (e.g., only `templateDefaults` set) and verify the output spec contains the fully populated values from mock templates.

### Integration Tests (envtest)

* **Mutation (Layer 4):**
1. Create a `ShardTemplate` named "default" in the namespace.
2. Create a `MultigresCluster` with a shard defined only as `name: "0"`.
3. `Get` the `MultigresCluster` and assert that the shard spec is fully populated with values from the "default" template (Namespace Defaulting).


* **Stateful Validation (Layer 4):**
1. Create a `CellTemplate` named "production-cell".
2. Create a `MultigresCluster` referencing "production-cell".
3. Attempt to `DELETE` the "production-cell" template.
4. Assert that the deletion is **rejected**.
5. Attempt to `CREATE` a `MultigresCluster` that references a non-existent `CoreTemplate`.
6. Assert that the creation is **rejected**.


* **CRD-Embedded CEL (Layer 2):**
1. Attempt to `CREATE` a `GlobalTopoServer` with *both* `etcd` and `templateRef` defined.
2. Assert that the request is **rejected** by the CRD's schema.


* **`ValidatingAdmissionPolicy` (Layer 3):**
1. Start `envtest` with the `ValidatingAdmissionPolicy` CRD installed.
2. As a non-operator user, attempt to `CREATE` or `UPDATE` a `Cell` CR.
3. Assert that the request is **rejected** by the read-only CEL policy.
4. Create a `MultigresCluster` and then attempt an unsafe `UPDATE` (e.g., shrinking storage).
5. Assert that the update is **rejected**.



---

## Drawbacks

1. **Webhook Complexity**: We accept the complexity of running a webhook server (certificates, networking) because our stateful validation requirements (template protection) and complex defaulting (override chain) make it unavoidable.
2. **Version Lock-in**: Using `ValidatingAdmissionPolicy` (Layer 3) requires Kubernetes v1.30+ for GA stability, limiting the cluster versions our operator can support.
3. **Performance Impact**: Every `MultigresCluster` create/update now takes a small network hop to the webhook server. The "In-Use" check (Listing all clusters) scales linearly with the number of clusters, but template deletion is a rare event, making this acceptable.

---

## Alternatives Considered

### Alternative 1: No Webhooks (Vitess-Style)

* **Approach:** Use only Layers 1, 2, 3 and in-memory Go-based defaulting.
* **Pros:** Operational simplicity, no certificate management.
* **Cons:**
* Cannot perform stateful validation (a hard requirement for us).
* Suffers from "invisible defaults," which is a poor user experience.


* **Decision:** **Rejected.** Fails to meet core design goals.

### Alternative 2: Webhook-Only

* **Approach:** Use the webhook server for *all* validation (Layers 2, 3, and 4) and mutation.
* **Pros:** Single implementation pattern; works on older Kubernetes versions.
* **Cons:** Less efficient. Puts unnecessary load on the webhook server for simple checks (like name length or `request.userInfo`) that CEL can handle in-process.
* **Decision:** **Rejected.** The hybrid approach is more modern and efficient.

### Alternative 3: Use a Policy Engine (e.g., Kyverno) for all Validation

* **Approach:** Do not ship *any* validating webhooks or `ValidatingAdmissionPolicy` resources. Instead, ship the operator with a library of Kyverno/OPA policies that users must install and manage themselves.
* **Pros:**
* Reduces the operator's footprint; no need to manage webhook HA or certificates.
* Appeals to users who *already* have a policy engine.


* **Cons:**
* **Fails to solve Mutation:** Policy engines cannot handle the complex Go-logic required for the **4-Level Override Chain** (conditional merging of deeply nested structs).
* **Fails to solve Stateful Validation:** A generic policy engine cannot easily perform the custom logic of "list all `MultigresCluster`s to see if this `CellTemplate` is in use."
* **Poor User Experience:** "Batteries not included" approach.


* **Decision:** **Rejected.** This approach fails to meet our core goals for mutation and stateful validation.

### Alternative 4: Use In-Process `MutatingAdmissionPolicy` (CEL)

* **Approach:** Instead of a mutating webhook server (Layer 4), use the in-process, CEL-based `MutatingAdmissionPolicy` for all defaulting.
* **Pros:** Performance (runs in-process) and Simplicity (no webhook server).
* **Cons:**
* **Wrong Tool for the Job:** CEL is an expression language. It is not powerful enough to handle the complex, conditional logic our operator's **4-Level Override Chain** requires (e.g., looking up a template by name from the API server and merging it).
* **Feature Immaturity:** The `MutatingAdmissionPolicy` feature is still in its early stages.


* **Decision:** **Rejected.** It fails to meet our primary motivation.

---

## Appendix: Vitess Defaulting Analysis

This is for information, showing Vitess defaults.

### Dynamic Defaults (The "Invisible" Go Defaults)

These defaults are applied **in-memory** by the operator *after* it reads the CR from `etcd`. They couldn't be static for three primary reasons:

1. **Dynamic Value:** The default value is set at compile time (like the operator's version).
2. **Conditional Block Logic:** The defaults are only applied if a user provides an empty block (e.g., `vtadmin: {}`).
3. **Conditional Field Logic:** One default depends on the value of another field.

| Defaulted Field(s) | Default Value | Why It Couldn't Be Static |
| --- | --- | --- |
| `spec.images.*` (all images) | The `DefaultImages` struct | **(1) Dynamic Value.** Image tags are set at compile time. |
| `spec.globalLockserver.etcd` | An empty `&EtcdLockserverTemplate{}` object. | **(3) Conditional Field Logic.** Only applied if `spec.globalLockserver.external` is `nil`. |
| `spec.vitessDashboard.replicas` | `defaultVtctldReplicas` (a Go constant). | **(2) Conditional Block Logic.** Only defaulted if the user provides `spec.vitessDashboard: {}`. |
| `spec.vitessDashboard.resources` | `requests` (CPU/mem) and `limits` (mem). | **(2) Conditional Block Logic.** Same reason. |
| `spec.vtadmin.replicas` | `defaultVtadminReplicas` (a Go constant). | **(2) Conditional Block Logic.** Skipped if `spec.vtadmin` is `nil`. |
| `spec.vtadmin.webResources` | `requests` (CPU/mem) and `limits` (mem). | **(2) Conditional Block Logic.** Same reason. |
| `spec.vtadmin.apiResources` | `requests` (CPU/mem) and `limits` (mem). | **(2) Conditional Block Logic.** Same reason. |
| `spec.vtadmin.fetchCredentials` | `defaultVtadminFetchCredentials` (a Go constant). | **(2) Conditional Block Logic.** Same reason. |
| `spec.keyspaces[].turndownPolicy` | `VitessKeyspaceTurndownPolicyRequireIdle`. | **Maintainability.** Kept in Go to have all default logic in one place. |
| `spec.backup.engine` | `defaultBackupEngine` (a Go constant). | **Maintainability.** Same reason. |
| `spec.topologyReconciliation` | The *entire object* is initialized. | **(2) Conditional Block Logic.** Populates a struct if the user provides `nil`. |
| `spec.updateStrategy.type` | `ExternalVitessClusterUpdateStrategyType`. | **(3) Conditional Field Logic.** Part of a larger conditional block. |
| `spec.gatewayService` / `spec.tabletService` | An empty `&ServiceOverrides{}` object. | **Code Stability.** Nil-guards to prevent reconciler crashes. |

### Static Defaults (The OpenAPI v3 CRD Defaults)

These are the simple, "Level 1" defaults that *are* visible with `kubectl get` because they are defined in the CRD file. This list is tiny, proving the method was insufficient for their main needs.

| Defaulted Field | Default Value |
| --- | --- |
| `spec.backup.schedules.items.concurrencyPolicy` | `"Forbid"` |
| `spec.backup.schedules.items.jobTimeoutMinute` | `10` |
| `spec.keyspaces.partitionings.equal.hexWidth` | `0` |
| `spec.keyspaces.partitionings.*.shardTemplate.tabletPools.name` | `""` (empty string) |
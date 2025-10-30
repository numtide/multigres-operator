
## Recommendations for Admission Control and Defaults in Multigres Operator

Here's an analysis of how the `vitess-operator` handles resource validation and defaulting, and how that compares to the design goals for the `multigres-operator`.

### How the Vitess Operator Currently Does It

Our analysis of the `vitess-operator` code shows it does **not** use any webhook servers for the admission (validation or mutation) of its resources. It uses a "classic" operator pattern that predates the new, easier-to-use admission tools.

---

### Validation of Resources in Vitess

Vitess relies entirely on **OpenAPI v3 schema** validation, which is defined directly within its `CustomResourceDefinition` (CRD) files. It does **not** use the newer `ValidatingAdmissionPolicy` (CEL).

When you `kubectl apply` a `VitessCluster`, the `kube-apiserver` validates the object against the rules in the CRD *before* it's ever written to `etcd` or seen by the operator.

**Examples of validation rules from the `VitessCluster` CRD:**
* **Enums:** `spec.backup.engine` is restricted to `[builtin, xtrabackup, mysqlshell]`.
* **String Patterns:** `spec.backup.locations.name` must match the regex `^[A-Za-z0-9]([A-Za-z0-9-_.]*[A-Za-z0-9])?$` and have a `maxLength: 63`.
* **Required Fields:** A backup schedule (`spec.backup.schedules.items`) *must* have `name`, `resources`, `schedule`, and `strategies`.
* **Numeric Ranges:** `spec.keyspaces.partitionings.equal.parts` must have a `minimum: 1` and `maximum: 65536`.

---

### Defaulting in Vitess Operator

The operator uses a robust **two-pronged defaulting strategy** that avoids the complexity of a mutating webhook.

1.  **Level 1: CRD-Based Defaults (Simple & Static)**
    The first layer of defaulting is done by the `kube-apiserver` using the `default:` keyword in the OpenAPI schema. These are for simple, static values.

    * **Example:** For a backup schedule, `spec.backup.schedules.items.concurrencyPolicy` is set to `default: Forbid`.

2.  **Level 2: Go-Based "Scheme" Defaults (Complex & Dynamic)**
    The operator applies more complex defaults (like setting default Docker images or resource requests) in its own Go code. This logic lives in files like `pkg/apis/planetscale/v2/vitesscluster_defaults.go`.

    * **Mechanism:** This is the clever part. A generated file (`zz_generated.defaults.go`) contains an `init()` function that calls `runtime.Scheme.AddTypeDefaultingFunc`. This registers the Go-based defaulting functions with the `controller-runtime`'s client.
    * **Result:** When the operator's controller fetches a `VitessCluster` object (e.g., `client.Get(...)`), `controller-runtime` automatically runs these registered defaulting functions on the object *in memory*. This ensures the reconciliation logic *always* sees a fully-defaulted object, simplifying the code.

---

### Strengths of the Vitess Approach

* **Operational Simplicity:** The biggest strength is **no webhook server**. This completely sidesteps the single greatest pain point of admission control: **TLS certificate management**. There's no need to provision, rotate, or inject certificates for the webhook, which makes installing and managing the operator *vastly* simpler and more reliable.
* **No "Deadlock" Risk:** A failing admission webhook (especially one with `failurePolicy: Fail`) can block all creates, updates, and deletes for that resource, effectively "deadlocking" your ability to manage the cluster. This design has zero risk of that.
* **Fast Feedback (for Schema):** Using the OpenAPI schema for validation gives users immediate, client-side feedback from `kubectl apply` if their manifest is syntactically invalid.
* **Clean Code:** The Go-based defaulting pattern keeps complex default-setting logic in testable, maintainable Go code and ensures the reconciler always works with a complete, defaulted object.

### Weaknesses (The Conscious Trade-offs)

* **Validation is Less Powerful:** The operator **cannot** perform complex, cross-field, or stateful validation.
    * It **cannot** use CEL for rules like "if `spec.backup.engine` is `xtrabackup`, then `spec.images.vtbackup` must be set."
    * It **cannot** validate that a referenced `Secret` for backup credentials actually exists when the `VitessCluster` is applied.
* **Defaults are "Invisible" to Users:** This is the most significant user-facing "weakness." Because the complex Go-based defaults (Level 2) are applied *inside the operator's process*, they are **not** persisted back to `etcd`.
    * If a user runs `kubectl get vitesscluster my-cluster -o yaml`, they will **not** see the default `resources` or `image` values that the operator is using in memory.
    * This can be confusing, as the "source of truth" in `etcd` doesn't match the "functional state" the operator is enforcing. A mutating webhook would have solved this.
* **Confusing Child Resource Edits:** As we noted in our own design, if a user patches or updates a "read-only" child resource, the operator's controller just silently overwrites the change. The user is not told that the update failed or will be reverted. A validating webhook could prevent this.

---

## Implications for Multigres Operator

### 1. The New "In-Process" Option: CEL

The new **in-process admission plugins** (`ValidatingAdmissionPolicy` and `MutatingAdmissionPolicy`) use the Common Expression Language (CEL) to validate and mutate fields more extensively.

This approach would allow us to get powerful validation **without the need for a webhook server**.

* **Pros:** This would make the operational cost of running and maintaining the Multigres operator much lighter. We wouldn't need to manage certificates or write complex webhook server logic.
* **Cons (Major):**
    * **Limited Power:** These policies are **stateless**. They cannot query the API server to check the state of *other* objects.
    * **Version Lock-in:** `ValidatingAdmissionPolicy` only became stable (GA) in **Kubernetes v1.30**. The `MutatingAdmissionPolicy` is still in **Beta** (v1.34+). This would significantly limit the cluster versions our operator could support.

### 2. Key Differences in Our Design

Our `multigres-operator` design has two key requirements that Vitess does not, which forces our hand.

* **Requirement 1: Protect `DeploymentTemplate` Deletion**
    * **The Problem:** We must prevent a user from deleting a `DeploymentTemplate` if it's being used by any `MultigresCluster`.
    * **The Solution:** This is a **stateful** check (it must list other objects). We **cannot** use CEL for this. The *only* way to implement this is with a traditional **`ValidatingWebhookConfiguration`** that calls a webhook server.

* **Requirement 2: Enforce "Read-Only" Child CRs**
    * **The Problem:** We want to "outright deny" users who try to manually `create` or `update` our child CRs (`Cell`, `Shard`, etc.).
    * **The Solution:** We have two options here:
        1.  **Webhook Server:** Use the same webhook server from Requirement 1.
        2.  **CEL:** This is a **perfect use case for `ValidatingAdmissionPolicy` (CEL)**. A CEL expression can easily inspect the `request.userInfo.username` and reject the request if it's *not* from our operator's ServiceAccount.

### Recommendation: A Hybrid Approach

Based on this, the Vitess model (no webhooks at all) is **not** an option for us because we have a firm requirement for stateful validation (Requirement 1).

The best path forward is a **hybrid model**:

1.  **We MUST run a Webhook Server:** We need a (small, focused) webhook server to handle the **stateful** validation for `DeploymentTemplate` deletion.
2.  **Use the Webhook Server for Mutation:** Since we're already paying the "cost" of a webhook server (certificates, etc.), we should also use it to implement a **mutating webhook** for our defaults. This will solve the "invisible defaults" problem that Vitess has.
3.  **Use CEL for Everything Else:** We should use **`ValidatingAdmissionPolicy` (CEL)** for all *stateless* validation, such as:
    * Enforcing our "read-only" child CRs (by checking the user).
    * Validating fields in our `MultigresCluster` (e.g., `shards > 0`).

This hybrid approach gives us the best of all worlds, but it *does* mean we must accept the `cert-manager` dependency and the version-lock-in of `ValidatingAdmissionPolicy` (v1.30+).
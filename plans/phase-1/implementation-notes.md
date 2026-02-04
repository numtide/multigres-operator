# Implementation Notes & Architecture

## Controller Caching Strategy

### The Problem: The Informer Memory Bomb

By default, the `controller-runtime` cache (Informer) watches *every* instance of a resource kind across *every* namespace.
In large multi-tenant clusters (e.g. Supabase production), a single cluster may contain:
- 5,000+ Secrets (Helm releases, other operators, user apps)
- 10,000+ Services
- 2,000+ StatefulSets

If the Multigres Operator watches `Secrets` globally without a filter, it will attempt to:
1.  List all 5,000 secrets on startup.
2.  Maintain a local copy of all 5,000 secrets in memory.
3.  Receive watch events for every change to any secret in the cluster.

This causes:
- **OOM (Out of Memory):** The operator pod crashes as it tries to cache GBs of unrelated data.
- **Slow Startup:** The initial List call times out or takes forever.
- **Network Load:** Massive unnecessary traffic on the API Server.

### The Solution: Hybrid Global/Local Caching

We implement a **Split-Brain Caching Strategy** using `cache.ByObject` options. This allows us to apply different rules based on the namespace.

#### Global Rule (The "Noise Cancelling")
For high-volume resources in regular user namespaces, we strictly filter the cache to ONLY store objects managed by this operator.

**Filtered Resources:**
- `Secret`
- `Service`
- `StatefulSet`

**Mechanism:**
We use `cache.AllNamespaces` with a `LabelSelector`:
```go
app.kubernetes.io/managed-by = multigres-operator
```
This effectively ignores any Secret/Service/StatefulSet that does not belong to us.

#### Local Exception (The "Safe Zone")
Some critical resources *must* be seen even if they don't have our label.
- **Cert-Manager Secrets:** Created by `cert-manager` for webhook TLS.
- **Leader Election Leases:** Created by `client-go`.
- **Own Deployment:** For `ownerRef` resolution.

**Mechanism:**
We configure a specific override for the operator's own namespace (e.g. `multigres-system`) with an **empty selector** (cache everything).
```go
defaultNS: {} // Unfiltered in our own namespace
```

#### ConfigMap Policy (The "Flexibility")
**Decision:** ConfigMaps are **NOT** filtered globally.

**Reasoning:**
Users frequently provide their own "unmanaged" ConfigMaps for Postgres configuration (e.g. `postgresql.conf` overrides).
If we filtered ConfigMaps, the operator would be unable to:
1.  Read the user's `my-postgres-config`.
2.  Calculate its hash.
3.  Trigger a rolling update when the user changes it.

Since ConfigMaps are generally lower volume and lower security risk than Secrets, we trade scalability for usability here.

#### Summary Table

| Resource | Scope | Filter | Why? |
| :--- | :--- | :--- | :--- |
| **Secret** | All Namespaces | `managed-by=multigres` | **OOM Prevention.** |
| **Secret** | Operator NS | **NONE (All)** | Cert-Manager compatibility. |
| **Service** | All Namespaces | `managed-by=multigres` | Noise reduction. |
| **Service** | Operator NS | **NONE (All)** | Self-discovery. |
| **StatefulSet** | All Namespaces | `managed-by=multigres` | Noise reduction. |
| **StatefulSet** | Operator NS | **NONE (All)** | Self-discovery. |
| **ConfigMap** | All Namespaces | **NONE (All)** | User Configs (postgresql.conf). |

### Developer Guide: Reading Secrets

Because Secrets are filtered globally, you cannot simply `r.Get()` an arbitrary user secret (e.g. `spec.passwordSecretRef`) unless it is labeled.

**If you need to read a User Secret:**

**Option A (Preferred):** Require the user to label it.
Tell the user: *"Please add `app.kubernetes.io/managed-by: multigres-operator` to your secret if you want us to use it."*

**Option B (Bypass Cache):** Direct API Reader.
If Option A is impossible, use the API Reader directly. This makes a live call to K8s, bypassing the cache.

```go
// Direct API call (slower, but sees everything)
err := mgr.GetAPIReader().Get(ctx, key, &secret)
```

## Operator Performance Tuning

### High Concurrency & Throughput
To handle large-scale clusters with thousands of shards, we have tuned the operator for high concurrency.

**1. Increased API Client Limits**
The default client-go rate limits (QPS: 5, Burst: 10) are insufficient for an operator managing thousands of resources. We have increased these limits globally:
- **QPS:** 50
- **Burst:** 100

This prevents the operator from self-throttling when multiple workers are requesting resources simultaneously.

**2. Parallel Reconciliation (20 Workers)**
By default, the controller runtime uses a single worker per controller. This means if one Shard is slow to reconcile (e.g. waiting for a slow API call), all other Shards are blocked.

We have enabled **20 Parallel Workers** (`MaxConcurrentReconciles: 20`) for all major controllers:
- `MultigresCluster`
- `Shard`
- `Cell`
- `TableGroup`
- `TopoServer`

**Advantages:**
- **Throughput:** Can process 20 resources simultaneously.
- **Resilience:** A stalled reconciliation does not block the entire queue.
- **Speed:** Faster convergence during initial startup or massive updates.

**Downsides:**
- **Load:** Increases load on the Kubernetes API Server (mitigated by our Caching Strategy).
- **Complexity:** Requires careful handling of shared resources (though our controllers are designed to be stateless/independent).

## Event Filtering & Idempotency

### GenerationChangedPredicate

We apply the `GenerationChangedPredicate` to the **Primary Resource** (the `For` object) in all controllers. This ensures the controller does **not** reconcile when only the `Status` subresource changes, preventing infinite loops where a controller updates status, triggers a new reconcile, updates status again, and so on.

**When to use it:**
Use this on the *primary* resource being reconciled to break self-recursion loops.

**Code Example:**
```go
func (r *MultigresClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        // FILTER: Only reconcile if Spec or Metadata changes. Ignore Status changes.
        For(&multigresv1alpha1.MultigresCluster{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
        // ...
        Complete(r)
}
```

**Potential Issues:**
If the controller logic *depended* on reading its own status to make decisions (e.g. "If status is 'Initializing', do X"), using this predicate would break that logic because the controller wouldn't wake up after setting 'Initializing'.
*   *Mitigation:* Our controllers are designed to be level-triggered based on `Spec` and child resources. We do not use the primary resource's `Status` as a state machine driver; `Status` is purely an output observation.

**Child Resources (Why we don't use it there):**
We do *not* apply this predicate to child resources (`Owns`). We must react to child resource status transitions (e.g., a Deployment becoming Ready, a Pod changing state) to update the parent's status. Each parent only monitors the status of its *immediate* children (e.g. `MultigresCluster` watches `Cell`, but not `StatefulSet` directly if `Cell` owns it).

### Server-Side Apply (SSA) & Idempotency

We deliberately omit client-side `reflect.DeepEqual` checks before patching status. We rely on the Kubernetes API Server's **Server-Side Apply (SSA)** logic to detect no-ops.

**No-Op Logic:**
If the status patch matches the existing state, the API Server will treat it as a no-op, generating no events and no resource version updates.

**Code Example:**
```go
// SSA Pattern: blindly patch the status.
// The API Server calculates the diff. If no change, it returns 200 OK with no-op.
if err := r.Status().Patch(
    ctx,
    &multigresv1alpha1.MultigresCluster{
        ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
        Status:     newStatus,
    },
    client.Apply,
    client.FieldOwner("multigres-operator"),
    client.ForceOwnership,
); err != nil {
    return err
}
```

**Alternative (Client-Side Check):**
If network bandwidth becomes a bottleneck, we *could* implement a client-side check, but it requires deep-copying and careful handling of pointers:
```go
// NOT RECOMMENDED unless profiling shows network IO is a bottleneck.
// Requires deep equality check which is expensive on CPU and hard to maintain.
if !reflect.DeepEqual(cluster.Status, newStatus) {
    r.Status().Patch(...)
}
```
**Decision:** We prefer the cleaner code of SSA rely on the API Server's optimized diffing engine until proven otherwise.


## Resource Naming Strategy

### Hierarchical Naming with Safety Hashes

We use a **hierarchical naming scheme** where child resource names are built by concatenating the logical path from the cluster root to the resource, separated by hyphens.

**Naming Pattern:**
```
{cluster-name}-{database-name}-{tablegroup-name}-{shard-name}-{pool-name}-{cell-name}-{hash}
```

**Examples:**
- **Cell:** `inline-z1-8a4f2b1c` (cluster: `inline`, cell: `z1`, hash for uniqueness)
- **Shard:** `inline-postgres-default-0-inf-a7c3d9e2` (cluster: `inline`, database: `postgres`, tablegroup: `default`, shard: `0-inf`, hash)
- **Pool StatefulSet:** `inline-postgres-default-0-inf---d8f1a` (truncated with `---` separator, then hash)
- **Pod (from StatefulSet):** `inline-postgres-default-0-inf---d8f1a-0` (StatefulSet name + ordinal suffix)

### The `name` Package

All resource naming goes through the `pkg/util/name` package, which provides:

**1. Hash-Based Collision Prevention**

The `Hash()` function generates an 8-character hex suffix (4 bytes, FNV-1a 32-bit) from the input parts:
```go
Hash([]string{"cluster", "db", "tg", "shard"}) → "a7c3d9e2"
```

This hash ensures uniqueness even when names are truncated to fit Kubernetes limits.

**2. Smart Truncation with Constraints**

`JoinWithConstraints()` concatenates name parts with hyphens, applies transformations (lowercase, alphanumeric only), and truncates if necessary while preserving the hash:
```go
// Full name fits
JoinWithConstraints(ServiceConstraints, "cluster", "cell")
→ "cluster-cell-8a4f2b1c"

// Name too long, truncated with special marker
JoinWithConstraints(ServiceConstraints, "very-long-cluster-name", "postgres", "default", "0-inf", "pool", "main", "z1")
→ "very-long-cluster-name-postgres-default-0-inf---d8f1a"
```

The `---` (triple-hyphen) marker indicates truncation occurred.

**3. Predefined Constraints**

Different Kubernetes resources have different name length limits:

| Constraint | MaxLength | ValidFirstChar | Used For |
|:---|:---:|:---|:---|
| `DefaultConstraints` | 253 | alphanumeric | CRDs, most resources |
| `ServiceConstraints` | 63 | lowercase letter | Services, PVCs |
| `StatefulSetConstraints` | 52 | lowercase letter | StatefulSets (reserves 11 chars for pod suffix + controller hash) |

### Character Limits on User-Provided Names

To ensure generated resource names stay within Kubernetes limits even after adding hashes and parent prefixes, we enforce strict character limits on user-provided names:

| Field | MaxLength | Rationale |
|:---|:---:|:---|
| **Cluster Name** | 30 | Root of all names; must leave room for 5-6 more levels |
| **Database Name** | 30 | Typically short; allows deep nesting |
| **TableGroup Name** | 25 | Reduces risk of truncation in shard/pool names |
| **Shard Name** | 25 | Often simple (e.g., `0-inf`, `shard1`) |
| **Pool Name** | 25 | Conservative to prevent StatefulSet truncation |
| **Cell Name** | 30 | Typically az names (e.g., `us-east-1a`, `z1`) |

These limits are enforced via **CRD validation** (`+kubebuilder:validation:MaxLength=X`) in `api/v1alpha1/common_types.go`.

### Resources Without Hashes (No Collision Risk)

Some resources use **simple string concatenation** without hashes:

**Singleton Global Resources:**
- `{cluster-name}-global-topo` (e.g., `inline-global-topo`)
- `{cluster-name}-multiadmin` (e.g., `inline-multiadmin`)
- `{cluster-name}-multiadmin-web` (e.g., `inline-multiadmin-web`)

**Why no hash?**
1. **1:1 Relationship:** Each cluster has exactly one GlobalTopoServer and one MultiAdmin.
2. **Predictable and Short:** The cluster name is already validated to be ≤30 chars, and we only append a fixed suffix.
3. **No User-Defined Nesting:** Unlike cells/shards/pools where users can define arbitrary names, these components are static.
4. **No Collision Possible:** Since there's only ever one instance per cluster, there's no scenario where two different logical paths could produce the same name after truncation.

**Collision Example (why others need hashes):**
- `very-long-cluster-name-postgres-tablegroup1-shard1-pool1-z1` (63 chars, truncated to 63)
- `very-long-cluster-name-postgres-tablegroup1-shard1-pool2-z1` (different pool, but if truncated at the same point, would collide)
- **Solution:** The hash distinguishes them: `...tablegroup1-shard1---a7c3d9e2` vs `...tablegroup1-shard1---b8e4f1a3`

### Naming Scheme Drawbacks

**1. Deeply Nested Names Become Abbreviated**

For resources like pools with long hierarchical paths, the generated names can exceed 63 (Service) or 52 (StatefulSet) characters, triggering truncation:
```
Original intent:  inline-postgres-default-0-inf-pool-main-z1
After truncation: inline-postgres-default-0-inf---d8f1a
```

**Impact:**
- **Pod Names:** become even longer with ordinal suffixes (e.g., `inline-postgres-default-0-inf---d8f1a-0`, `...-1`)
- **Readability:** Users see truncated names in `kubectl get pods`, making it harder to identify which pool/shard a pod belongs to without checking labels
- **Debugging:** Must rely on labels (`multigres.com/pool`, `multigres.com/cell`, etc.) to understand the logical hierarchy

**2. User Names Constrained**

Users cannot use very descriptive names for databases, tablegroups, shards, or pools without triggering truncation early.

**Mitigation:**
- Enforce strict character limits (25-30 chars) on user-provided names
- Encourage short, meaningful names (e.g., `main`, `z1`, `0-inf` instead of `primary-read-write-pool`, `us-east-1a-availability-zone`)
- Provide rich labeling to compensate for abbreviated resource names

### Why Different Maximum Lengths?

**Services and PVCs: 63 Characters**
- Kubernetes DNS label limit (RFC 1123)
- Service names become DNS records: `{service-name}.{namespace}.svc.cluster.local`

**StatefulSets: 52 Characters**
- Reserve 11 characters for Pod ordinal suffix and controller hash
  - Pod name format: `{statefulset-name}-{ordinal}` (e.g., `-0`, `-1`, ..., `-999`)
  - Controller-revision-hash label: appended by Kubernetes, adds ~10 chars
- Without this reservation, long StatefulSet names would produce invalid Pod names (>63 chars)

**Pods: 63 Characters (Derived)**
- Pods don't have a separate constraint in our naming package
- Their names are auto-generated by the owning controller (StatefulSet, Deployment)
- StatefulSet pods: `{sts-name}-{ordinal}`
- Deployment pods: `{deployment-name}-{replicaset-hash}-{pod-hash}`

**CRDs and most other resources: 253 Characters**
- Kubernetes metadata.name limit for most resources
- We use `DefaultConstraints` for custom resources (Cell, Shard, TableGroup, etc.)
- These names are rarely used in DNS, so the 63-char limit doesn't apply

## Known Behaviors & Quirks

### Infinite "Configured" Loop (Client-Side Apply)

**The Symptom:**
When running `kubectl apply -f ...` on a manifest (like `no-templates.yaml`) repeatedly, `kubectl` reports `configured` every time, even though nothing changes in the cluster.

**The Cause:**
This is a conflict between **Client-Side Apply (CSA)** and **Mutating Webhooks**.
1.  **The Diff:** Legacy `kubectl apply` compares your local file (which omits defaults like `replicas`) against the live server object (where the webhook has injected `replicas: 1`).
2.  **The Patch:** `kubectl` sees a discrepancy and sends a PATCH request to **remove** the field (setting it to `null`).
3.  **The Webhook:** The Webhook intercepts this PATCH and immediately puts `replicas: 1` back.
4.  **The No-Op:** The API Server sees that the final state matches the initial state and performs a **No-Op** (no `Generation` or `ResourceVersion` bump).
5.  **The Report:** Despite the server-side no-op, `kubectl` reports `configured` because it successfully sent a non-empty patch.

**The Verdict:**
This is **standard Kubernetes behavior** for Operators with defaulting webhooks (common in Istio, Cert-Manager, etc.). It is a limitation of the legacy `kubectl apply` logic, not a bug in the operator. **Critically, this is purely cosmetic** - no actual changes occur on the server (confirmed by no `Generation` increment), and controllers do not unnecessarily reconcile.

**The Solutions:**

If seeing repeated `configured` messages is disturbing, users can:

* **Recommended:** Use Server-Side Apply (`kubectl apply --server-side`). This moves the merge logic to the API server, which correctly handles ownership and defaults without fighting.
* **Alternative:** Explicitly set all default values in your local YAML to match the server state (e.g., manually add all defaulted fields).

**Rejected Alternative Solutions:**

We considered several approaches to eliminate this behavior entirely, but each has significant drawbacks:

**Option 1: Remove the Defaulting Webhook** ❌
* **Why it would work:** Without the webhook adding defaults, there would be no discrepancy for `kubectl` to detect.
* **Why we rejected it:** Our defaulting logic includes complex computed defaults (e.g., deriving resource requirements, setting up template references) that are essential for operator functionality and good UX. Removing the webhook would break the API design and force users to specify every field manually.

**Option 2: Make All Fields Required** ❌
* **Why it would work:** If every field must be specified in the YAML, the webhook wouldn't add anything new.
* **Why we rejected it:** This defeats the entire purpose of defaults and creates terrible user experience. Users would need to write massive YAML files with hundreds of fields just to create a simple cluster.

**Option 3: Use CRD-Level Defaults Instead of Webhooks** ⚠️
* **Why it would work:** CRD OpenAPIv3 schemas support `default:` values that are applied by the API server before storage. `kubectl apply` sees these in the OpenAPI schema and includes them in comparison.
* **Why we rejected it:** CRD defaults are extremely limited - they only support simple scalar values (e.g., `replicas: 1`), not complex computed logic like "derive this field from these other fields" or "populate template references." Our defaulting logic is too sophisticated for CRD-level defaults.

**Option 4: Document and Accept** ✅
* **What we did:** Documented this as a known cosmetic quirk with zero operational impact.
* **Why this is correct:** The behavior is harmless, standard across the ecosystem, and users have easy workarounds. The alternatives would compromise our API design or user experience for purely cosmetic gain.

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

## PVC Deletion Policy

### Overview

The operator supports fine-grained control over PVC lifecycle through the `PVCDeletionPolicy` type, which is embedded at multiple levels of the resource hierarchy. This feature maps directly to Kubernetes StatefulSet's `persistentVolumeClaimRetentionPolicy` (available since Kubernetes 1.23).

### API Design

**Type Definition** (`api/v1alpha1/common_types.go`):
```go
type PVCDeletionPolicy struct {
    WhenDeleted PVCRetentionPolicyType `json:"whenDeleted,omitempty"`
    WhenScaled  PVCRetentionPolicyType `json:"whenScaled,omitempty"`
}

type PVCRetentionPolicyType string
const (
    RetainPVCRetentionPolicy PVCRetentionPolicyType = "Retain"
    DeletePVCRetentionPolicy PVCRetentionPolicyType = "Delete"
)
```

**Fields**:
- `WhenDeleted`: Controls PVC deletion when the parent resource (Cluster, TopoServer, Shard) is deleted
- `WhenScaled`: Controls PVC deletion when StatefulSets are scaled down (replicas reduced)

**Default Values**: Both fields default to `Retain` (safest option). This is enforced via:
1. CRD-level defaults: `+kubebuilder:default=Retain`
2. Webhook defaulting: Ensures nil policies are populated
3. Operator-level defaults in `pkg/resolver`

### Hierarchical Merging

The policy propagates through the resource hierarchy with child values taking precedence:

**Hierarchy Path**:
```
MultigresCluster.Spec.PVCDeletionPolicy
  ↓ (merged with)
TableGroup.Spec.PVCDeletionPolicy
  ↓ (merged with)
Shard.Spec.PVCDeletionPolicy  (inline spec)
  → Resolved and stored in TableGroup.Spec.Shards[].PVCDeletionPolicy
```

**Template Overrides**:
- `CoreTemplate.Spec.GlobalTopoServer.PVCDeletionPolicy` → applied to cluster's GlobalTopoServer
- `ShardTemplate.Spec.PVCDeletionPolicy` → applied to shards using that template

**Merge Function** (`api/v1alpha1/common_types.go:MergePVCDeletionPolicy`):
- Field-level inheritance: If child specifies only `whenDeleted`, it inherits parent's `whenScaled`
- Nil handling: If both child and parent are nil, returns nil (caller falls back to operator defaults)
- Empty struct handling: If merged result has both fields empty, returns nil

### Implementation Points

**1. Webhook Defaulter** (`pkg/webhook/handlers/defaulter.go`):
- Resolves shard templates and captures the `PVCDeletionPolicy` from resolver
- Previously ignored the third return value from `ResolveShard()`, causing "invisible defaults"
- Fix: Now properly assigns `resolvedPvcPolicy` to `shard.Spec.PVCDeletionPolicy`

**2. Resolver** (`pkg/resolver/shard.go`):
- `ResolveShard()` returns `(multiOrch, pools, pvcPolicy, error)`
- Merges policies in precedence order: inline spec → shard template → cluster defaults
- Returns nil if no policy specified at any level (signals use of operator defaults)

**3. Resource Handlers**:

**TopoServer StatefulSets** (`pkg/resource-handler/controller/toposerver/statefulset.go`):
```go
sts.Spec.PersistentVolumeClaimRetentionPolicy = pvc.BuildRetentionPolicy(
    topo.Spec.PVCDeletionPolicy,
)
```

**Shard Pool StatefulSets** (`pkg/resource-handler/controller/shard/pool_statefulset.go`):
```go
sts.Spec.PersistentVolumeClaimRetentionPolicy = pvc.BuildRetentionPolicy(
    shard.Spec.PVCDeletionPolicy,
)
```

**Utility Function** (`pkg/util/pvc/retention.go:BuildRetentionPolicy`):
- Converts operator's `PVCDeletionPolicy` to Kubernetes `StatefulSetPersistentVolumeClaimRetentionPolicy`
- Handles nil input by returning safe `Retain/Retain` default
- Maps enum values: `Delete` → `Delete`, anything else → `Retain`

### Critical Caveats for Future Developers

#### 1. StatefulSet Spec Changes Trigger Recreation

**Issue**: Modifying `spec.persistentVolumeClaimRetentionPolicy` on an existing StatefulSet requires recreation (not supported by in-place updates).

**Current Behavior**: The operator uses **Server-Side Apply (SSA)**, which will:
- Detect the conflict
- Return an error if the field is immutable
- Require manual intervention (delete + recreate StatefulSet)

**Mitigation**:
- Document this as a known limitation in README
- Consider implementing a controller that detects policy changes and:
  1. Gracefully drains pods
  2. Deletes the StatefulSet (keeping pods via `orphan` deletion)
  3. Recreates StatefulSet with new policy
  4. Lets Kubernetes re-adopt pods

**Decision**: Deferred to future implementation. Current behavior (requiring manual intervention) is acceptable for v1alpha1.

#### 2. No Validation of Policy vs. Cluster Intent

**Issue**: The operator does **not** prevent users from setting `whenDeleted: Delete` on production clusters.

**Rationale**: We cannot reliably determine user intent (dev vs. prod). Cluster labels or annotations are not standardized.

**Mitigation**:
- Clear documentation in README with warnings
- Recommend using CI/CD policy enforcement (e.g., OPA, Kyverno) to prevent `Delete` in production namespaces
- Consider adding a validating webhook warning (not error) in future versions

#### 3. Immediate Effect on Scale-Down

**Behavior**: When `whenScaled: Delete` is set, scaling down **immediately deletes PVCs** (within seconds of pod termination).

**Risk**: Users may expect a grace period or confirmation step.

**Mitigation**:
- Document this behavior prominently
- Recommend backups before scale-down operations
- Future enhancement: Add a `gracePeriodSeconds` field to delay PVC deletion

#### 4. Template Changes Don't Auto-Update Existing Shards

**Issue**: If a user changes `ShardTemplate.Spec.PVCDeletionPolicy` after shards are created, existing shards do **not** automatically update.

**Why**: The resolved policy is "frozen" in `TableGroup.Spec.Shards[].PVCDeletionPolicy` at creation time.

**Workaround**: Users must:
1. Edit the TableGroup spec directly (not recommended - it's a child resource), OR
2. Delete and recreate the cluster

**Future Enhancement**: Implement a "template sync" reconciliation loop that detects template changes and propagates them to resolved specs.

#### 5. Nil vs. Empty Struct Semantics

**Critical**: The `MergePVCDeletionPolicy` function returns:
- `nil`: No policy specified at this level or above → use operator defaults
- `&PVCDeletionPolicy{WhenDeleted: "", WhenScaled: ""}`: **Also treated as nil**
- `&PVCDeletionPolicy{WhenDeleted: "Retain", WhenScaled: ""}`: Use `Retain` for deletion, inherit scaled from parent

**Implication**: Controllers must check `if policy == nil` and apply defaults, not just check `if policy.WhenDeleted == ""`.

**Code Pattern**:
```go
finalPolicy := resolvedPolicy
if finalPolicy == nil {
    finalPolicy = &PVCDeletionPolicy{
        WhenDeleted: RetainPVCRetentionPolicy,
        WhenScaled:  RetainPVCRetentionPolicy,
    }
}
```

This is already handled correctly in `pkg/util/pvc/retention.go`.

### Testing Coverage

**Integration Tests**:
- `pkg/cluster-handler/controller/multigrescluster/integration_test.go`: Verifies policy propagation through hierarchy
- `pkg/cluster-handler/controller/multigrescluster/integration_resolution_enforcement_test.go`: Verifies template overrides work correctly
- `pkg/webhook/handlers/defaulter_test.go`: (Should be added) Unit tests for webhook defaulting logic

**Unit Tests**:
- `api/v1alpha1/common_types_test.go`: (Should be added) Tests for `MergePVCDeletionPolicy` function
- `pkg/util/pvc/retention_test.go`: (Should be added) Tests for `BuildRetentionPolicy` conversion

**Notable Test Case**: The "invisible defaults" bug was caught because tests expected `nil` but got `Retain/Retain` after the fix. This validates that the fix is working correctly.

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

## Metrics Collection: Pull vs Push

### The Two Models

The operator ecosystem uses **two different metric collection models** simultaneously:

| Component | Model | Transport | Why |
|:---|:---|:---|:---|
| **Operator** | **Pull** (Prometheus scrape) | HTTP `/metrics` on `:8443` | controller-runtime uses `prometheus/client_golang` natively |
| **Data plane** (multiorch, multipooler, multigateway) | **Push** (OTLP) | gRPC/HTTP to OTLP endpoint | Multigres binaries use the OpenTelemetry SDK with `autoexport` |

### Why the Operator Uses Pull

controller-runtime's metrics infrastructure is built on `prometheus/client_golang`. Every metric registered via `sigs.k8s.io/controller-runtime/pkg/metrics.Registry` is automatically exposed on the HTTP handler. The framework provides no built-in OTLP metrics exporter.

We **could** add one by programmatically creating an `otelsdkmetric.MeterProvider` with an OTLP exporter and bridging the Prometheus registry into it. However, this would:

1. **Fight the framework** — controller-runtime assumes pull-based Prometheus metrics. All built-in metrics (`controller_runtime_reconcile_total`, `workqueue_depth`, etc.) go through the Prometheus registry. Bridging them to OTLP adds complexity for no functional gain.
2. **Duplicate signals** — Prometheus would still scrape `/metrics`, so every metric would exist in two places unless we disabled scraping entirely, which breaks standard monitoring patterns.
3. **Be unnecessary** — the Prometheus pull model works well for a single long-lived operator pod. Push-based metrics exist to solve problems the operator doesn't have (short-lived processes, scale-to-zero, high cardinality per-request metrics).

### Why the Data Plane Uses Push

Multigres binaries (multiorch, multipooler, multigateway) are built with the OpenTelemetry SDK and the `autoexport` library, which reads `OTEL_*` environment variables to configure exporters automatically. They have no `/metrics` HTTP endpoint — all telemetry (traces, metrics, logs) is pushed to a single OTLP endpoint.

This design is intentional: multigres components are data-plane workloads that may run at high scale across many pods. Push-based telemetry avoids the complexity of service discovery and scrape configuration for hundreds of pool replicas.

### The OTel Collector Bridge

Because multigres sends **all signals** (traces + metrics) to a single OTLP endpoint, the local observability stack uses an **OTel Collector** to split them:

```
multigres pods ──OTLP──▶ OTel Collector ──▶ Tempo      (traces)
                                          ──▶ Prometheus (metrics, via OTLP receiver)

operator pod   ◀── Prometheus scrapes /metrics (pull, unchanged)
```

Without the collector, metrics would be sent to Tempo (which only handles traces) and silently dropped. The collector's pipeline configuration routes each signal type to the appropriate backend.

In production, organizations typically already have an OTel Collector or a managed observability backend that accepts OTLP natively, making this split transparent.

## Observability Architecture

### Package Layout

All observability code lives in `pkg/monitoring/`:

| File | Purpose |
|:---|:---|
| `metrics.go` | Prometheus metric declarations and registration |
| `recorder.go` | Type-safe recorder functions that controllers call |
| `tracing.go` | OTel tracer init, span helpers, traceparent bridge, log-trace correlation |
| `tracing_test.go` | Tests for all tracing functions |
| `recorder_test.go` | Tests for metric recording |

External artifacts:

| Path | Purpose |
|:---|:---|
| `config/monitoring/prometheus-rules.yaml` | PrometheusRule alerts (7 rules) |
| `config/monitoring/grafana-dashboard-operator.json` | Operator health dashboard |
| `config/monitoring/grafana-dashboard-cluster.json` | Per-cluster topology dashboard |
| `config/monitoring/grafana-dashboards.yaml` | ConfigMap for Grafana sidecar auto-provisioning |
| `docs/monitoring/runbooks/*.md` | Alert runbooks (7 files) |

---

### Metrics

#### Registration Pattern

Metrics are declared as package-level `var` blocks in `metrics.go` and registered in an `init()` function using `sigs.k8s.io/controller-runtime/pkg/metrics.Registry.MustRegister(...)`. This ensures they are available as soon as the monitoring package is imported.

Controllers do **not** interact with Prometheus types directly. Instead, they call type-safe recorder functions in `recorder.go`:

```go
// In a controller:
monitoring.SetClusterInfo(cluster.Name, cluster.Namespace, string(cluster.Status.Phase))
monitoring.SetShardPoolReplicas(shard.Name, pool.Name, shard.Namespace, desired, ready)
monitoring.RecordWebhookRequest("DEFAULT", "MultigresCluster", err, duration)
```

This indirection keeps controller code free of Prometheus imports and makes it easy to add/change metric dimensions without touching every controller.

#### Metric Naming Convention

All operator-specific metrics use the `multigres_operator_` prefix. Labels follow Kubernetes conventions (`name`, `namespace`, `cluster`, `cell`, `shard`, `pool`).

The `cluster_info` gauge uses the **info-style pattern**: it is always set to `1` and uses labels (`phase`) to expose the cluster's current state. This allows PromQL joins:

```promql
multigres_operator_cluster_info{phase!="Healthy"} == 1
```

When the phase changes, `SetClusterInfo` calls `DeletePartialMatch` first to clean up the old phase label, preventing stale series with the old phase from lingering.

#### Adding New Metrics

1. Declare the metric variable in `metrics.go`
2. Register it in `init()`
3. Add it to the `Collectors()` function (used by tests)
4. Create a recorder function in `recorder.go`
5. Call the recorder from the appropriate controller

---

### Tracing

#### Lifecycle

Tracing is initialised in `main.go` via `monitoring.InitTracing()`:

```go
shutdown, err := monitoring.InitTracing(ctx, "multigres-operator", version)
if err != nil {
    setupLog.Error(err, "failed to initialise tracing")
    os.Exit(1)
}
defer shutdown(ctx)
```

If `OTEL_EXPORTER_OTLP_ENDPOINT` is unset, `InitTracing` returns a noop shutdown — the global `Tracer` stays as the default noop tracer from `otel.Tracer()`, so all `StartReconcileSpan`/`StartChildSpan` calls are zero-cost.

When the env var **is** set, `InitTracing`:
1. Creates an OTLP gRPC exporter (auto-configures from standard OTel env vars)
2. Builds a `Resource` with `service.name` and `service.version` semantic conventions
3. Registers a `TracerProvider` with batched export
4. Sets the W3C `TraceContext` propagator
5. Re-acquires the package-level `Tracer` from the new provider

#### Span Hierarchy

```
MultigresCluster.Reconcile (root span — or child if traceparent bridge is active)
├── MultigresCluster.PopulateDefaults
├── MultigresCluster.ReconcileTableGroups
├── MultigresCluster.ReconcileCells
├── MultigresCluster.ReconcileGlobalComponents
└── MultigresCluster.UpdateStatus
```

Each controller's `Reconcile` creates a top-level span via `StartReconcileSpan`, and sub-operations use `StartChildSpan`. The span carries `k8s.resource.name`, `k8s.namespace`, and `k8s.resource.kind` attributes.

#### Traceparent Annotation Bridge

The Kubernetes webhook and reconcile loop are asynchronous: the API Server calls the webhook, persists the object, and then the informer wakes the controller at an unpredictable time. To bridge this gap:

1. **Webhook side** (`defaulter.go`): After applying defaults, `InjectTraceContext(ctx, cluster.Annotations)` writes:
   - `multigres.com/traceparent` — W3C traceparent header
   - `multigres.com/traceparent-ts` — Unix timestamp of injection

2. **Controller side** (`multigrescluster_controller.go`): After fetching the cluster, `ExtractTraceContext(annotations)` reads the annotation:
   - **Fresh** (< 10 min): Ends the initial orphan span and restarts it as a child of the webhook trace
   - **Stale** (> 10 min): Creates a new root span with an OTel **Link** to the old trace, preserving causal history without creating misleading parent-child relationships

**Why only MultigresCluster?** Child resources (Cell, Shard, etc.) are created within the MultigresCluster reconcile loop, so they naturally inherit the trace context via the `ctx` parameter. Only the top-level resource needs the annotation bridge.

**The stale threshold (10 minutes)** prevents requeues, periodic reconciles, or operator restarts from creating misleading child spans under a trace from hours ago. A Link preserves the relationship without implying the old webhook is "still running."

#### Adding Tracing to New Code

For a new controller:
```go
func (r *MyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    ctx, span := monitoring.StartReconcileSpan(ctx, "MyKind.Reconcile", req.Name, req.Namespace, "MyKind")
    defer span.End()
    ctx = monitoring.EnrichLoggerWithTrace(ctx)
    // ...
}
```

For a new sub-operation:
```go
ctx, span := monitoring.StartChildSpan(ctx, "MyKind.DoSomething")
defer span.End()
```

To record errors:
```go
if err != nil {
    monitoring.RecordSpanError(span, err)
    return ctrl.Result{}, err
}
```

---

### Log-Trace Correlation

`EnrichLoggerWithTrace(ctx)` extracts `trace_id` and `span_id` from the current OTel span and injects them into the `logr` logger in the context. All subsequent `log.FromContext(ctx).Info(...)` calls will include these fields automatically.

This enables "click log → view trace" in Grafana when Loki and Tempo are connected via a derived field on `trace_id`.

**Placement rule:** Call `EnrichLoggerWithTrace(ctx)` immediately after `StartReconcileSpan`, before acquiring the logger. This ensures the enriched logger is used throughout the entire reconcile, including by sub-operations that call `log.FromContext(ctx)`.

---

### Alerts & Runbooks

The 7 PrometheusRule alerts in `config/monitoring/prometheus-rules.yaml` are grouped by signal type:

| Category | Alerts |
|:---|:---|
| **Errors** | `MultigresClusterReconcileErrors`, `MultigresClusterDegraded`, `MultigresCellGatewayUnavailable`, `MultigresShardPoolDegraded`, `MultigresWebhookErrors` |
| **Latency** | `MultigresReconcileSlow` |
| **Saturation** | `MultigresControllerSaturated` |

Each alert's `annotations.runbook_url` points to a markdown file in `docs/monitoring/runbooks/` with:
- **Meaning** — what the alert indicates
- **Impact** — what happens if ignored
- **Investigation Steps** — PromQL queries and `kubectl` commands to diagnose
- **Remediation** — specific actions to resolve

**Adding a new alert:**
1. Add the `PrometheusRule` entry in `prometheus-rules.yaml`
2. Create a runbook in `docs/monitoring/runbooks/{AlertName}.md`
3. Link the runbook URL in the alert's annotations

---

### Grafana Dashboards

Two JSON dashboards are provisioned via a `ConfigMap` (`grafana-dashboards.yaml`) that uses the standard Grafana sidecar label (`grafana_dashboard: "1"`):

**Operator Dashboard** — focuses on operator health:
- Reconcile rate and error rate per controller
- p50/p99 reconcile latency
- Work queue depth and saturation
- Webhook request rate and latency

**Cluster Dashboard** — focuses on cluster topology:
- Cluster phase status
- Cell and shard counts
- Gateway and pool replica health (desired vs. ready)
- TopoServer replica status

**Editing dashboards:** Export the updated dashboard JSON from Grafana, save it to `config/monitoring/grafana-dashboard-*.json`, and update the ConfigMap checksum annotation if needed.

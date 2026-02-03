# Architecture: Controller Caching Strategy

## The Problem: The Informer Memory Bomb

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

## The Solution: Hybrid Global/Local Caching

We implement a **Split-Brain Caching Strategy** using `cache.ByObject` options. This allows us to apply different rules based on the namespace.

### Global Rule (The "Noise Cancelling")
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

### Local Exception (The "Safe Zone")
Some critical resources *must* be seen even if they don't have our label.
- **Cert-Manager Secrets:** Created by `cert-manager` for webhook TLS.
- **Leader Election Leases:** Created by `client-go`.
- **Own Deployment:** For `ownerRef` resolution.

**Mechanism:**
We configure a specific override for the operator's own namespace (e.g. `multigres-system`) with an **empty selector** (cache everything).
```go
defaultNS: {} // Unfiltered in our own namespace
```

### ConfigMap Policy (The "Flexibility")
**Decision:** ConfigMaps are **NOT** filtered globally.

**Reasoning:**
Users frequently provide their own "unmanaged" ConfigMaps for Postgres configuration (e.g. `postgresql.conf` overrides).
If we filtered ConfigMaps, the operator would be unable to:
1.  Read the user's `my-postgres-config`.
2.  Calculate its hash.
3.  Trigger a rolling update when the user changes it.

Since ConfigMaps are generally lower volume and lower security risk than Secrets, we trade scalability for usability here.

### Summary Table

| Resource | Scope | Filter | Why? |
| :--- | :--- | :--- | :--- |
| **Secret** | All Namespaces | `managed-by=multigres` | **OOM Prevention.** |
| **Secret** | Operator NS | **NONE (All)** | Cert-Manager compatibility. |
| **Service** | All Namespaces | `managed-by=multigres` | Noise reduction. |
| **Service** | Operator NS | **NONE (All)** | Self-discovery. |
| **StatefulSet** | All Namespaces | `managed-by=multigres` | Noise reduction. |
| **StatefulSet** | Operator NS | **NONE (All)** | Self-discovery. |
| **ConfigMap** | All Namespaces | **NONE (All)** | User Configs (postgresql.conf). |

## Developer Guide: Reading Secrets

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

# Operator Performance Tuning

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

# Event Filtering & Idempotency

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

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

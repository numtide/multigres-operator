# Resource Naming Strategy

## Hierarchical Naming with Safety Hashes

We use a **hierarchical naming scheme** where child resource names are built by concatenating the logical path from the cluster root to the resource, separated by hyphens.

**Naming Pattern:**
```
{cluster-name}-{database-name}-{tablegroup-name}-{shard-name}-{pool-name}-{cell-name}-{hash}
```

**Examples:**
- **Cell:** `inline-z1-8a4f2b1c` (cluster: `inline`, cell: `z1`, hash for uniqueness)
- **Shard:** `inline-postgres-default-0-inf-a7c3d9e2` (cluster: `inline`, database: `postgres`, tablegroup: `default`, shard: `0-inf`, hash)
- **Pool Pod:** `inline-postgres-default-0-inf-pool-main-z1-a7c3d9e2-0` (base name with PodConstraints + index suffix)
- **Data PVC:** `data-inline-postgres-default-0-inf-pool-main-z1-a7c3d9e2-0` (same as pod, prefixed with `data-`)

## The `name` Package

All resource naming goes through the `pkg/util/name` package, which provides:

### 1. Hash-Based Collision Prevention

The `Hash()` function generates an 8-character hex suffix (4 bytes, FNV-1a 32-bit) from the input parts:
```go
Hash([]string{"cluster", "db", "tg", "shard"}) → "a7c3d9e2"
```

This hash ensures uniqueness even when names are truncated to fit Kubernetes limits.

### 2. Smart Truncation with Constraints

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

### 3. Predefined Constraints

Different Kubernetes resources have different name length limits:

| Constraint | MaxLength | ValidFirstChar | Used For |
|:---|:---:|:---|:---|
| `DefaultConstraints` | 253 | alphanumeric | CRDs, most resources |
| `ServiceConstraints` | 63 | lowercase letter | Services, PVCs, PDBs |
| `PodConstraints` | 60 | lowercase letter | Pool pods (reserves 3 chars for `-{index}` suffix) |
| `StatefulSetConstraints` | 52 | lowercase letter | TopoServer StatefulSets (reserves 11 chars for pod suffix + controller hash) |

## Character Limits on User-Provided Names

To ensure generated resource names stay within Kubernetes limits even after adding hashes and parent prefixes, we enforce strict character limits on user-provided names:

| Field | MaxLength | Rationale |
|:---|:---:|:---|
| **Cluster Name** | 30 | Root of all names; must leave room for 5-6 more levels |
| **Database Name** | 30 | Typically short; allows deep nesting |
| **TableGroup Name** | 25 | Reduces risk of truncation in shard/pool names |
| **Shard Name** | 25 | Often simple (e.g., `0-inf`, `shard1`) |
| **Pool Name** | 25 | Conservative to prevent pod name truncation |
| **Cell Name** | 30 | Typically az names (e.g., `us-east-1a`, `z1`) |

These limits are enforced via **CRD validation** (`+kubebuilder:validation:MaxLength=X`) in `api/v1alpha1/common_types.go`.

## Resources Without Hashes (No Collision Risk)

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

## Naming Scheme Drawbacks

### 1. Deeply Nested Names Become Abbreviated

For resources like pools with long hierarchical paths, the generated names can exceed 63 (Service) or 60 (Pod base) characters, triggering truncation:
```
Original intent:  inline-postgres-default-0-inf-pool-main-z1
After truncation: inline-postgres-default-0-inf---d8f1a
```

**Impact:**
- **Pod Names:** become even longer with ordinal suffixes (e.g., `inline-postgres-default-0-inf---d8f1a-0`, `...-1`)
- **Readability:** Users see truncated names in `kubectl get pods`, making it harder to identify which pool/shard a pod belongs to without checking labels
- **Debugging:** Must rely on labels (`multigres.com/pool`, `multigres.com/cell`, etc.) to understand the logical hierarchy

### 2. User Names Constrained

Users cannot use very descriptive names for databases, tablegroups, shards, or pools without triggering truncation early.

**Mitigation:**
- Enforce strict character limits (25-30 chars) on user-provided names
- Encourage short, meaningful names (e.g., `main`, `z1`, `0-inf` instead of `primary-read-write-pool`, `us-east-1a-availability-zone`)
- Provide rich labeling to compensate for abbreviated resource names

## Why Different Maximum Lengths?

**Services and PVCs: 63 Characters**
- Kubernetes DNS label limit (RFC 1123)
- Service names become DNS records: `{service-name}.{namespace}.svc.cluster.local`

**Pool Pods: 60 Characters (Base) + Index Suffix**
- Reserve 3 characters for the `-{index}` suffix (e.g., `-0`, `-1`)
- Pool pods are created directly by the operator with `PodConstraints` (60 char base)
- Final pod name: `{base-name}-{index}` (max 63 chars)

**StatefulSets: 52 Characters (TopoServer only)**
- Reserve 11 characters for Pod ordinal suffix and controller hash
- TopoServer still uses StatefulSets; pool pods no longer do

**Deployment Pods: 63 Characters (Derived)**
- Their names are auto-generated by the Deployment controller
- Deployment pods: `{deployment-name}-{replicaset-hash}-{pod-hash}`

**CRDs and most other resources: 253 Characters**
- Kubernetes metadata.name limit for most resources
- We use `DefaultConstraints` for custom resources (Cell, Shard, TableGroup, etc.)
- These names are rarely used in DNS, so the 63-char limit doesn't apply

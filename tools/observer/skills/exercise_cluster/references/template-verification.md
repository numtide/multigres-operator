# Template Verification Protocol

Run this at baseline for **any fixture that uses templates or overrides** (`templated-full`, `overrides-complex`, or any fixture with `prerequisites.yaml` containing template CRs). This catches template resolution bugs where the cluster is "healthy" but configured with wrong values.

## When to Run

- **After Phase 1 baseline deploy** (after stability verification passes)
- **After template mutation scenarios** (`update-core-template-cr`, `update-cell-template-cr`, etc.)

## Step 1 — Build Expected Values Map

Read the template CRs and cluster CR. For each resource, determine the expected value by overlaying in precedence order:

```
Hardcoded Default → Template Base → Override → Inline Spec
```

Produce a table: `(resource, field, expected_value, source)` where source is one of: `template:<name>`, `override`, `inline`, `default`.

**Hardcoded defaults** (from `pkg/resolver/defaults.go`):
- `DefaultAdminReplicas = 1`
- `DefaultEtcdReplicas = 3`
- Multigateway default replicas = 1
- Multiorch default replicas = 1
- Pool default `replicasPerCell = 3`

## Step 2 — Query Actual Values

For each Kubernetes resource, extract:

| Resource Type | How to Query | Fields |
|---|---|---|
| etcd StatefulSet | `kubectl get sts -l app.kubernetes.io/component=topo-server` | `spec.replicas` |
| multiadmin Deployment | `kubectl get deploy -l app.kubernetes.io/component=multiadmin` | `spec.replicas`, container resources, pod template labels/annotations |
| multigateway Deployment | `kubectl get deploy -l app.kubernetes.io/component=multigateway` | `spec.replicas`, container resources, pod template labels/annotations |
| multiorch Deployment | `kubectl get deploy -l app.kubernetes.io/component=multiorch` | `spec.replicas`, container resources, pod template labels/annotations |
| Pool pods | `kubectl get pods -l app.kubernetes.io/component=shard-pool` | count (= replicasPerCell), container resources |
| Pool PVCs | `kubectl get pvc -l app.kubernetes.io/component=shard-pool` | `spec.resources.requests.storage` |
| Shard CRs | `kubectl get shard -o yaml` | `spec.pvcDeletionPolicy` |

## Step 3 — Compare and Classify

For each `(resource, field)`:

| Classification | Meaning |
|---|---|
| **PASS** | Actual matches expected AND expected differs from hardcoded default |
| **COINCIDENCE** | Actual matches expected BUT expected equals the hardcoded default (correct by luck, not by template resolution) |
| **FAIL** | Actual does not match expected |

> **COINCIDENCE is a yellow flag.** It means you cannot confirm the template was actually read. When designing fixtures, ensure template values differ from hardcoded defaults to avoid this.

## Step 4 — Report

Include the full comparison table in the exercise run report:

```markdown
| Resource | Field | Expected | Source | Actual | Result |
|---|---|---|---|---|---|
| etcd STS | replicas | 5 | template:full-core-template | 5 | PASS |
| multiadmin | replicas | 2 | template:full-core-template | 1 | FAIL |
| multigateway | replicas | 2 | override (cell) | 2 | PASS |
```

Any FAIL is a bug finding. Any COINCIDENCE should be noted and the fixture should be updated to use a non-default value in future runs.

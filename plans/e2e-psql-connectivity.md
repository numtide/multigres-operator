# E2E Tests: Full psql Connectivity Verification

## Architecture

Each test case gets its **own kind cluster** for full isolation:
- `e2e-minimal` — minimal cluster with defaults
- `e2e-inline` — inline configuration (no templates)  
- `e2e-templated` — template-based multi-cell cluster

Each test:
1. Creates kind cluster + loads images + installs CRDs
2. Starts operator in-process (via `setUpOperator`)
3. Creates MultigresCluster CR
4. Waits for all pods to be Ready (etcd, multipooler, multiorch, multigateway)
5. Port-forwards to multigateway service on port 15432
6. Runs `SELECT 1` via psql to verify query serving
7. Cleans up (kind cluster deleted in test cleanup)

## Key Changes Needed

### 1. `pkg/testutil/kind.go` — Add cluster creation
Currently `SetUpKind` expects a pre-existing cluster. Add `WithKindCreateCluster()` option that:
- Creates a new kind cluster with a random name
- Loads specified images into it
- Registers cleanup to delete the cluster

### 2. `test/e2e/helpers_test.go` — Enhanced helpers
- `waitForAllPodsReady(t, ctx, c, ns)` — wait for ALL pods in namespace to be Ready
- `psqlViaKubectl(t, ns, podName, query)` — run psql via kubectl exec (no local psql needed)
- `waitForQueryServing(t, ns, gatewayServiceName)` — poll SELECT 1 through port-forward

### 3. Per-test kind cluster
Each test file creates its own cluster via `WithKindCreateCluster()`:
```go
func TestMinimalCluster(t *testing.T) {
    mgr, c, ns := setUpOperator(t, 
        withKindCluster("e2e-minimal-"+randomSuffix()),
        withImages(multigresImages...),
    )
    // ... create CR, wait for pods, verify psql
}
```

### 4. No local psql needed
Use `kubectl exec` into a postgres pod to run psql queries against multigateway:
```bash
kubectl exec -n <ns> <multipooler-pod> -c postgres -- \
    psql -h <multigateway-svc> -p 15432 -U postgres -d postgres -tA -c "SELECT 1"
```

Or use port-forward + Go's `database/sql` with `lib/pq` driver.

## Timeouts (from upstream research)
- etcd ready: 120s
- multipooler pods ready: 180s (includes postgres init)
- multiorch ready: 120s  
- multigateway ready: 120s
- Shard bootstrap: 60s
- Query serving after bootstrap: 30s
- **Total per test: ~8-10 minutes**

## Agent Split

| Agent | Kind Cluster | Test File | Scope |
|-------|-------------|-----------|-------|
| `e2e-helpers` | n/a | helpers_test.go, kind.go | Shared infrastructure |
| `e2e-minimal` | e2e-minimal-xxx | cluster_minimal_test.go | Minimal cluster + psql |
| `e2e-inline` | e2e-inline-xxx | cluster_inline_test.go | Inline cluster + psql |
| `e2e-templated` | e2e-templated-xxx | cluster_templated_test.go | Templated cluster + psql |

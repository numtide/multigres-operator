# Concurrent Mutation Protocol

Use this protocol to test race conditions by firing two mutations in rapid succession.

## When to Use

Only in **full** execution mode. Concurrent mutations require thorough observation because the combined effect of two operations may reveal race conditions invisible to single-mutation testing.

## Protocol

1. **Record baseline state**: pod counts, replica counts, resource versions of affected CRs.
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get multigrescluster <name> -n <ns> -o jsonpath='{.metadata.resourceVersion}'
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get pods -n <ns> -l app.kubernetes.io/part-of=multigres --no-headers | wc -l
   ```

2. **Fire both mutations within 2-3 seconds**: Run two `kubectl patch` commands back-to-back (NOT in parallel — the second should start within seconds of the first to create overlapping reconciliation).

3. **Use FULL Stability Verification Protocol** (no fast-path). Concurrent operations need thorough observation with the `standard` or `lifecycle` tier depending on the mutations involved.

4. **Verify BOTH mutations took effect**: After stability, check that both mutations were applied. A common race condition bug is one mutation being overwritten by the other's reconciliation.

5. **Check observer history for transient findings**:
   ```bash
   observer /api/history | jq '{
     transient: [.transient[]? | {check, component, message, count}],
     flapping: [.flapping[]? | {check, component, message, count}]
   }'
   ```
   Transient findings during the concurrent window are expected but should be reported — they may reveal ordering dependencies.

## Applicable Fixtures

`minimal-retain`, `minimal-delete`, `templated-full` — these have enough resources for concurrent operations. NOT applicable to `observability-custom` or `s3-backup` (too specialized).

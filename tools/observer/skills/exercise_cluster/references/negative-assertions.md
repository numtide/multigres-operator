# Negative Assertion Protocol

Use these assertion types to verify that deletions, rejections, and cleanups behave correctly.

## Assertion Types

### 1. Resource Removal

After scale-down or pod deletion, verify the correct number of resources remain:
```bash
# Poll every 5s for up to 120s
KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get pods -n <ns> -l <selector> --no-headers | wc -l
# Assert: count == expected (fewer than before)
```
Applicable scenarios: `scale-down-pool-replicas`, `delete-pool-pod` (replacement count matches original).

### 2. Webhook Rejection

For negative test scenarios, verify the webhook rejects invalid mutations:
```bash
KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl patch multigrescluster <name> -n <ns> \
  --type=json -p '<invalid-patch>' 2>&1
# Assert: exit code != 0 AND stderr contains expected error substring
```
Applicable scenarios: `remove-cell`, `remove-pool`.

**Expected error substrings:**
| Scenario | Expected Error |
|---|---|
| `remove-cell` | `cells are append-only` or `cannot remove cell` |
| `remove-pool` | `pools are append-only` or `cannot remove pool` |

If the patch **succeeds** (exit code 0), that is a critical bug — the webhook is not enforcing invariants.

### 3. Cleanup Verification

After cluster deletion, verify all managed resources are gone before recreating:
```bash
# Poll every 5s for up to 180s
KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get pods -n <ns> -l app.kubernetes.io/part-of=multigres --no-headers 2>&1
# Assert: "No resources found" or empty output
```
Also verify child CRs are cleaned up:
```bash
KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get cells,shards,toposervers -n <ns> --no-headers 2>&1
# Assert: "No resources found" or empty output
```
Applicable scenarios: `delete-and-recreate-cluster`.

### 4. History-Based Assertion

After any mutation, check the observer's finding history to detect patterns:
```bash
observer /api/history | jq '{
  persistent: [.persistent[]? | {check, component, message, count}],
  flapping: [.flapping[]? | {check, component, message, count}]
}'
```
- **Persistent findings** after stabilization indicate unresolved bugs.
- **Flapping findings** indicate intermittent issues worth investigating.
- **Transient findings** that resolved are expected during mutations but should be reported.

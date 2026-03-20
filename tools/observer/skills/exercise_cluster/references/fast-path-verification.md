# Fast-Path Verification Protocol

Use this instead of the full Stability Verification Protocol for **simple mutations** where the expected outcome is clear and verifiable. This cuts verification time from ~5 minutes to ~2 minutes.

## Eligibility

A scenario is fast-path eligible when:
- It is a single, non-destructive mutation (no drain, no delete-recreate)
- The success criteria can be verified with kubectl alone
- No concurrent mutations are happening

**Eligible scenarios:**

| Scenario | Success Criteria |
|---|---|
| `scale-multigateway-replicas` | Deployment `.status.readyReplicas` == target |
| `update-resource-limits` | Pod template hash changed, all pods Running+Ready |
| `add-pod-annotations` | Pod annotations contain the new key |
| `scale-up-pool-replicas` | Pool pod count == target per cell, all Running+Ready |
| `delete-pool-pod` | Replacement pod Running+Ready |
| `delete-operator-pod` | Operator pod Running+Ready, data plane unaffected |

**NOT eligible** (use full Stability Verification Protocol): `scale-down-pool-replicas` (drain machine), `delete-and-recreate-cluster`, all concurrent scenarios, lifecycle scenarios (`add-cell`, `add-pool`, `switch-template`).

## Protocol

### Step 1 — Poll Success Criteria

Poll the scenario's success criteria every 5 seconds for up to 2 minutes:
```bash
# Example for scale-multigateway-replicas:
KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get deploy -n <ns> -l app.kubernetes.io/component=multigateway \
  -o jsonpath='{.items[0].status.readyReplicas}'
```
If success criteria do NOT pass within 2 minutes, **fall back to full Stability Verification Protocol**.

### Step 2 — Targeted Observer Check

Once success criteria pass, trigger an immediate targeted observer check:
```bash
observer '/api/check?categories=pod-health,connectivity,crd-status' | jq '{
  summary: .summary,
  errors: [(.findings // [])[] | select(.level == "error" or .level == "fatal")]
}'
```
If any errors/fatals are returned, **fall back to full Stability Verification Protocol**.

### Step 3 — Shortened Grace Wait

Wait 60 seconds (instead of 150s), then poll `/api/status` twice with 15 seconds between polls:
```bash
observer /api/status | jq '{
  summary: .summary,
  errors: [(.findings // [])[] | select(.level == "error" or .level == "fatal")]
}'
```
If BOTH polls are clean (0 errors, 0 fatals): classify as **STABLE (fast-path)**.
If either poll has errors: **fall back to full Stability Verification Protocol**.

### Step 4 — Result Classification

Report as:
- **STABLE (fast-path, clean)**: Success criteria met, observer clean across all fast-path checks.
- **STABLE (fast-path, transients observed)**: Success criteria met but transient findings appeared. List them.
- **FALLBACK**: Fast-path failed, fell back to full protocol. Report why.

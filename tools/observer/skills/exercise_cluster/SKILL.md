---
name: exercise_cluster
description: Deploy MultigresCluster fixtures, run mutation scenarios, and validate health using the observer. Finds bugs in the operator and upstream multigres by exercising real cluster operations and verifying end-to-end health beyond CRD phase status.
---

# Exercise Cluster Skill

**Goal:** Find bugs in the multigres operator and upstream multigres by deploying real MultigresCluster configurations, mutating them through operator-driven workflows, and using the observer to verify true end-to-end cluster health.

**Core principles:**
- The **observer is the single source of truth** for cluster health. CRD phase `Healthy` is a necessary gate but NOT sufficient — it misses broken replication, connection failures, and multi-primary states.
- **You drive kubectl directly.** Read the live CR, understand its structure, construct correct patches. Do not use hardcoded JSON paths — fixtures have different structures (some use `.spec.pools`, others `.overrides.pools`).
- **Every error/fatal from the observer after the grace period is potentially a real bug** until investigated and proven otherwise.
- **Report ALL findings to the user**, even transient ones that resolved — they may indicate intermittent bugs or race conditions.
- **NEVER dismiss or gloss over ANY error.** This includes operator log errors, RBAC warnings, webhook warnings, kubectl output warnings — everything. If you see an error and think "that's probably fine", you are WRONG. Investigate it, explain what it means, and report it. An RBAC error like "cannot watch nodes" is a real missing permission, not background noise. A webhook warning like "no nodes match region=X" means pods will never schedule. Read every line of output critically.

## Phase 0: Cluster Setup

1. Verify the kind cluster is reachable and running the latest version of the operator code:
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl cluster-info
   ```
   If not reachable, run `make kind-deploy` (deploys operator + observer).

2. Ensure observer is running and port-forwarded:
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get pods -l app.kubernetes.io/name=multigres-observer -n multigres-operator
   ```
   If missing: `make kind-deploy-observer`.
   Port-forward (check if already running first):
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl port-forward -n multigres-operator svc/multigres-observer 9090:9090 &
   ```
   Verify: `curl -s http://localhost:9090/api/status | jq '.summary'`

## Phase 1: Deploy Fixture & Baseline Verification

Pick a fixture from `fixtures/` (see **Fixture Selection** below).

1. Deploy prerequisites if `prerequisites.yaml` exists:
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl apply -f fixtures/<fixture>/prerequisites.yaml
   ```

2. Deploy the cluster:
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl apply -f fixtures/<fixture>/cluster.yaml
   ```

   > **READ THE KUBECTL OUTPUT.** The webhook emits warnings for real problems — topology mismatches (`no nodes currently match`), storage incompatibilities, name length issues. If you see a warning, **stop and fix the issue** before waiting for pods. A warning like "no nodes match region=X" means pods will be Pending forever. Do not ignore warnings and blindly wait for Healthy.

3. **Run the Stability Verification Protocol** (next section). Use tier `lifecycle` (extended timeout).
   **Baseline MUST be fully clean.** Any error at baseline is a bug — investigate immediately using the `diagnose_with_observer` skill.

4. **For template/override fixtures**: Run the **Template Verification Protocol** after stability is confirmed. This verifies that template values, overrides, and defaults were resolved correctly by comparing expected vs. actual field values on deployed resources. Any FAIL is a bug finding.

## Phase 2: Mutation Testing

For each scenario (see `references/scenarios.md`):

1. **Read the live CR** to understand its current structure:
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get multigrescluster <name> -n <ns> -o yaml
   ```

2. **Save pre-mutation state** of whatever field you will change (for teardown).

3. **Construct and apply the correct patch** based on the actual CR, not assumptions. Use `kubectl patch` with the appropriate patch type (`json` for precise operations, `merge` for additive changes).

4. **Run the Stability Verification Protocol**. Use the tier matching the scenario type.

5. **Log the result** in your running report (see **Reporting** below).

6. **Teardown** if applicable: reverse the mutation and run stability verification again.

7. Only proceed to the next scenario after stability is confirmed.

## Stability Verification Protocol

Run this after EVERY cluster change: initial deploy, mutation, and teardown.

### Tiers

| Tier | When | CRD Phase Timeout | Min Observation |
|------|------|-------------------|-----------------|
| `quick` | Config-only changes (annotations, PVC policy) | 3 minutes | 60 seconds |
| `standard` | Scale, resource updates, image changes | 5 minutes | 60 seconds |
| `lifecycle` | Initial deploy, delete-recreate, template switches | 10 minutes | 90 seconds |

### Step 1 — CRD Phase Gate

Poll `.status.phase` every 5 seconds until `Healthy`:
```bash
KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get multigrescluster <name> -n <ns> -o jsonpath='{.status.phase}'
```
- If `Degraded` or `Failed` persists for 2 minutes → STOP, investigate.
- If timeout reached → STOP, investigate.

### Step 2 — Grace Period Wait

The observer suppresses pool pod errors for 2 minutes after creation. We must wait past this before trusting observer output.

Check the youngest pool pod age:
```bash
KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get pods -n <ns> -l app.kubernetes.io/component=shard-pool \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.creationTimestamp}{"\n"}{end}'
```
Wait until ALL pool pods are at least **150 seconds old** (120s grace period + 30s margin).

### Step 3 — Stability Observation

Poll the observer every 10 seconds. Track state across all polls.

```bash
curl -s http://localhost:9090/api/status | jq '{
  summary: .summary,
  errors: [(.findings // [])[] | select(.level == "error" or .level == "fatal")],
  warns: [(.findings // [])[] | select(.level == "warn")]
}'
```

**State tracking** (maintain across polls):
- `consecutive_clean`: count of consecutive polls with 0 errors AND 0 fatals
- `all_findings`: list of every error/fatal finding seen during observation (even if resolved later)
- `elapsed`: time since observation started

**Exit conditions:**
| Condition | Action |
|-----------|--------|
| `consecutive_clean >= 3` AND `elapsed >= min_observation` | **STABLE** — proceed |
| Any error/fatal persists for > 3 minutes | **UNSTABLE** — STOP, investigate |
| `elapsed >= 5 minutes` without stability | **TIMEOUT** — STOP, investigate |

**Important:** A finding "persisting" means the same check+component combination appears in consecutive polls. Use the observer's `/api/history` endpoint to detect persistent vs transient vs flapping findings automatically.

### Step 4 — Result Classification

Report one of:
- **STABLE (clean)**: No findings during entire observation.
- **STABLE (transients observed)**: Errors appeared but resolved. List every transient finding with its check, component, message, and how long it lasted. *These must be reported to the user — they may indicate intermittent bugs.*
- **UNSTABLE**: Persistent errors. List all findings. Trigger investigation.

### Step 5 — Warn Review

After stability is confirmed, review warn-level findings:
```bash
curl -s http://localhost:9090/api/status | jq '[(.findings // [])[] | select(.level == "warn")]'
```
Warnings don't block progress but note them. Persistent warnings like replication lag >10s or WAL replay paused may indicate latent issues.

### Step 6 — Primary Verification

After stability is confirmed, verify that CRD podRoles matches actual PostgreSQL state. This catches stale podRoles bugs where the operator's reconciliation settles before multiorch elects a primary.

1. Check CRD podRoles has exactly 1 PRIMARY per pool per cell:
```bash
kubectl get shard -n <ns> -o json | jq '[.items[].status.podRoles | to_entries[] | select(.value == "PRIMARY")] | length'
```
Assert the count equals the expected number of primaries (1 per pool per cell).

2. Verify via SQL that exactly 1 pod per pool per cell reports as primary:
```bash
for pod in $(kubectl get pods -n <ns> -l app.kubernetes.io/component=shard-pool -o name); do
  echo -n "$pod: "
  kubectl exec -n <ns> $pod -c postgres -- psql -h 127.0.0.1 -p 5432 -U postgres -tAc "SELECT CASE WHEN pg_is_in_recovery() THEN 'REPLICA' ELSE 'PRIMARY' END"
done
```
Assert exactly 1 pod per pool per cell reports PRIMARY.

3. Cross-reference: the pod marked PRIMARY in CRD must be the same pod that reports `pg_is_in_recovery() = false` via SQL.

4. If mismatch: trigger a reconcile by labeling any pool pod (`kubectl label pod <pod> trigger=reconcile --overwrite`), wait 30s, then re-check. Report the mismatch as an exerciser finding with severity "error" — this indicates the stale podRoles bug.

This step runs after EVERY scenario (both full and fast-path verification).

### Step 7 — Observer History Assertion

After primary verification passes, check the observer's finding history for the mutation window:

```bash
curl -s http://localhost:9090/api/history | jq '{persistent: .persistent, flapping: .flapping, transientCount: (.transient | length)}'
```

- Assert: `persistent == []` (no persistent findings remaining)
- Assert: `flapping == []` (no flapping findings)
- Log the transient finding count for the exercise report

If persistent or flapping findings exist, investigate before proceeding to the next scenario.

## Port-Forward Management

The exerciser uses `kubectl port-forward` to reach the observer's HTTP API. Port-forwards can go stale during cluster mutations. Follow this protocol:

### Setup (once per exercise run)
```bash
# Kill any existing port-forwards to port 9090
pkill -f "port-forward.*9090" 2>/dev/null || true
sleep 1

# Find the observer pod and establish port-forward
OBSERVER_POD=$(kubectl get pods -n multigres-operator -l app.kubernetes.io/name=multigres-observer -o name | head -1)
kubectl port-forward -n multigres-operator $OBSERVER_POD 9090:9090 &
PF_PID=$!
sleep 2
```

### Health Check (before each observer API call)
```bash
curl -sf http://localhost:9090/healthz > /dev/null 2>&1 || {
  # Port-forward is stale — re-establish
  kill $PF_PID 2>/dev/null
  pkill -f "port-forward.*9090" 2>/dev/null || true
  sleep 1
  OBSERVER_POD=$(kubectl get pods -n multigres-operator -l app.kubernetes.io/name=multigres-observer -o name | head -1)
  kubectl port-forward -n multigres-operator $OBSERVER_POD 9090:9090 &
  PF_PID=$!
  sleep 2
}
```

### Cleanup (end of exercise run)
```bash
kill $PF_PID 2>/dev/null
pkill -f "port-forward.*9090" 2>/dev/null || true
```

## Fast-Path Verification Protocol

Use this instead of the full Stability Verification Protocol for **simple mutations** where the expected outcome is clear and verifiable. This cuts verification time from ~5 minutes to ~2 minutes for eligible scenarios.

### Eligibility

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

### Protocol

#### Step 1 — Poll Success Criteria

Poll the scenario's success criteria every 5 seconds for up to 2 minutes:
```bash
# Example for scale-multigateway-replicas:
KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get deploy -n <ns> -l app.kubernetes.io/component=multigateway \
  -o jsonpath='{.items[0].status.readyReplicas}'
```
If success criteria do NOT pass within 2 minutes, **fall back to full Stability Verification Protocol**.

#### Step 2 — Targeted Observer Check

Once success criteria pass, trigger an immediate targeted observer check:
```bash
curl -s 'http://localhost:9090/api/check?categories=pod-health,connectivity,crd-status' | jq '{
  summary: .summary,
  errors: [(.findings // [])[] | select(.level == "error" or .level == "fatal")]
}'
```
If any errors/fatals are returned, **fall back to full Stability Verification Protocol**.

#### Step 3 — Shortened Grace Wait

Wait 60 seconds (instead of 150s), then poll `/api/status` twice with 15 seconds between polls:
```bash
curl -s http://localhost:9090/api/status | jq '{
  summary: .summary,
  errors: [(.findings // [])[] | select(.level == "error" or .level == "fatal")]
}'
```
If BOTH polls are clean (0 errors, 0 fatals): classify as **STABLE (fast-path)**.
If either poll has errors: **fall back to full Stability Verification Protocol**.

#### Step 4 — Result Classification

Report as:
- **STABLE (fast-path, clean)**: Success criteria met, observer clean across all fast-path checks.
- **STABLE (fast-path, transients observed)**: Success criteria met but transient findings appeared. List them.
- **FALLBACK**: Fast-path failed, fell back to full protocol. Report why.

## Template Verification Protocol

Run this at baseline for **any fixture that uses templates or overrides** (`templated-full`, `overrides-complex`, or any fixture with `prerequisites.yaml` containing template CRs). This catches template resolution bugs where the cluster is "healthy" but configured with wrong values.

### When to Run

- **After Phase 1 baseline deploy** (after stability verification passes)
- **After template mutation scenarios** (`update-core-template-cr`, `update-cell-template-cr`, etc.)

### Step 1 — Build Expected Values Map

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

### Step 2 — Query Actual Values

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

### Step 3 — Compare and Classify

For each `(resource, field)`:

| Classification | Meaning |
|---|---|
| **PASS** | Actual matches expected AND expected differs from hardcoded default |
| **COINCIDENCE** | Actual matches expected BUT expected equals the hardcoded default (correct by luck, not by template resolution) |
| **FAIL** | Actual does not match expected |

> **COINCIDENCE is a yellow flag.** It means you cannot confirm the template was actually read. When designing fixtures, ensure template values differ from hardcoded defaults to avoid this.

### Step 4 — Report

Include the full comparison table in the exercise run report:

```markdown
| Resource | Field | Expected | Source | Actual | Result |
|---|---|---|---|---|---|
| etcd STS | replicas | 5 | template:full-core-template | 5 | PASS |
| multiadmin | replicas | 2 | template:full-core-template | 1 | FAIL |
| multigateway | replicas | 2 | override (cell) | 2 | PASS |
```

Any FAIL is a bug finding. Any COINCIDENCE should be noted and the fixture should be updated to use a non-default value in future runs.

## Negative Assertion Protocol

Use these assertion types to verify that deletions, rejections, and cleanups behave correctly.

### Assertion Types

#### 1. Resource Removal

After scale-down or pod deletion, verify the correct number of resources remain:
```bash
# Poll every 5s for up to 120s
KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get pods -n <ns> -l <selector> --no-headers | wc -l
# Assert: count == expected (fewer than before)
```
Applicable scenarios: `scale-down-pool-replicas`, `delete-pool-pod` (replacement count matches original).

#### 2. Webhook Rejection

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

#### 3. Cleanup Verification

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

#### 4. History-Based Assertion

After any mutation, check the observer's finding history to detect patterns:
```bash
curl -s http://localhost:9090/api/history | jq '{
  persistent: [.persistent[]? | {check, component, message, count}],
  flapping: [.flapping[]? | {check, component, message, count}]
}'
```
- **Persistent findings** after stabilization indicate unresolved bugs.
- **Flapping findings** indicate intermittent issues worth investigating.
- **Transient findings** that resolved are expected during mutations but should be reported.

## Deep Investigation Protocol

When a scenario results in UNSTABLE or persistent observer findings, follow this protocol to trace root cause. This protocol delegates detailed investigation to the **`diagnose_with_observer` skill** (located at `tools/observer/skills/diagnose_with_observer/SKILL.md`), which has comprehensive triage procedures, log tracing commands, and code investigation guidance.

### Step 1 — Capture State

Before anything changes, capture the current state:
```bash
# Full observer snapshot
curl -s http://localhost:9090/api/status | jq . > /tmp/exercise-snapshot.json

# Finding history with pattern classification
curl -s http://localhost:9090/api/history | jq . > /tmp/exercise-history.json

# Targeted on-demand check for specific categories
curl -s 'http://localhost:9090/api/check?categories=replication,connectivity' | jq .
```

### Step 2 — Triage with diagnose_with_observer

Switch to the `diagnose_with_observer` skill and follow its investigation protocol:
1. **Severity triage** (Section 4) — process fatals first, then errors, then warns
2. **Diagnostic patterns** (Section 5) — match findings to known patterns (silent data plane failure, replication chains, topology issues)
3. **Call chain tracing** (Section 7) — trace through gateway → multiorch → pooler → postgres logs
4. **Code investigation** (Section 8) — determine if the bug is in the operator or upstream multigres

### Step 3 — Classify and Report

After investigation, classify the finding:
- **Operator bug**: File location, code path, and suggested fix
- **Upstream bug**: Multigres component, version affected, and whether it's fixed on main
- **Expected transient**: Document why it's benign (e.g., rolling restart noise)

### Rules

1. **Do NOT auto-dismiss.** Every post-grace-period error is a potential real bug.
2. **Check operator code** — see `references/operator-knowledge.md` for where bugs hide, organized by observer check category.
3. **Check upstream multigres** — clone/pull `/tmp/multigres`:
   ```bash
   if [ -d /tmp/multigres ]; then cd /tmp/multigres && git pull origin main; else git clone https://github.com/multigres/multigres /tmp/multigres; fi
   ```
   Check which version is running: `cat api/v1alpha1/image_defaults.go` and compare with upstream HEAD.
4. **Report to user** with: what the observer detected, whether it's operator or upstream, the specific code path, and a suggested fix.
5. **Wait for user approval** before proceeding to the next scenario.

## Concurrent Mutation Protocol

Use this protocol to test race conditions by firing two mutations in rapid succession.

### When to Use

Only in **full** execution mode. Concurrent mutations require thorough observation because the combined effect of two operations may reveal race conditions invisible to single-mutation testing.

### Protocol

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
   curl -s http://localhost:9090/api/history | jq '{
     transient: [.transient[]? | {check, component, message, count}],
     flapping: [.flapping[]? | {check, component, message, count}]
   }'
   ```
   Transient findings during the concurrent window are expected but should be reported — they may reveal ordering dependencies.

### Applicable Fixtures

`minimal-retain`, `minimal-delete`, `templated-full` — these have enough resources for concurrent operations. NOT applicable to `observability-custom` or `s3-backup` (too specialized).

## Execution Modes

| Mode | What it does | When to use |
|------|-------------|-------------|
| **smoke** | Deploy fixture → baseline verification only. For template fixtures, includes Template Verification Protocol. | Quick sanity check, finds bootstrap/template bugs |
| **core** | smoke + `scale-up`, `scale-down`, `update-resource-limits`, `delete-pool-pod`. Uses fast-path verification for eligible scenarios. | Standard operator path coverage |
| **full** | All scenarios applicable to the fixture, including template & override scenarios, concurrent mutations, and negative assertions. Uses fast-path where eligible. | Thorough testing, finds edge cases and race conditions |

When the user says "run tests" or "exercise the cluster" without specifying, default to **core** mode.

## Fixture Selection

| Fixture | Kind-Ready | Template Verification | Tests |
|---------|-----------|----------------------|-------|
| `minimal-retain` | Yes | N/A (no templates) | Core operator logic, PVC retention |
| `minimal-delete` | Yes | N/A (no templates) | PVC deletion paths |
| `templated-full` | Yes (needs prereqs) | Full TVP (all 3 template types) | Template resolution, pure template propagation |
| `overrides-complex` | Yes (needs prereqs) | Override precedence TVP | Override merging, template+override interaction |
| `external-etcd-mixed` | Yes (needs prereqs) | N/A | External topology server, mixed-mode cells |
| `s3-backup` | Partial (needs real S3 bucket) | N/A | Backup configuration with S3 |
| `multi-cell-quorum` | Yes (resource heavy) | N/A | Multi-cell, quorum |
| `observability-custom` | Yes (needs prereqs) | N/A | Custom observability config |

**Recommended order for bug-finding:**
1. `minimal-retain` — simplest, fastest feedback, validates core logic
2. `minimal-delete` — tests PVC deletion paths (historically buggy)
3. `templated-full` — tests template resolution (complex, high bug surface)
4. `overrides-complex` — tests override merging (known edge cases)

## Negative Tests

These scenarios MUST be rejected by the webhook:
- `remove-cell` — cells are append-only
- `remove-pool` — pools are append-only

Apply the patch and verify rejection. **If it succeeds, that is a bug** — the webhook isn't enforcing invariants.

## Reporting

Create a running report at `agent-docs/exerciser/exercise-run-<YYYY-MM-DD-HHMMSS>.md` tracking:
- Environment (cluster, observer version, operator image, multigres images)
- Per-fixture: baseline result, each scenario result (mutation applied, stability classification, all findings including transients, teardown result)
- Summary: fixtures tested, scenarios executed, bugs found, transient findings observed

This report is the deliverable. The user uses it to understand what was tested and what was found.

## Reference Documents

- **Scenario Catalog**: `references/scenarios.md` — all mutation scenarios with instructions
- **Reusable Patches**: `patches/` — parameterized shell scripts for common mutations (scale, resources, annotations, pod deletion)
- **Operator Knowledge**: `references/operator-knowledge.md` — architecture, code paths, investigation guide
- **Observer Diagnosis**: Use the `diagnose_with_observer` skill for structured investigation

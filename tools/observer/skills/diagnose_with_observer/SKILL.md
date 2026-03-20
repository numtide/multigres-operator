---
name: diagnose_with_observer
description: Use the multigres observer to diagnose cluster health issues. Fetch structured diagnostics from the /api/status endpoint, triage findings by severity, correlate root causes, and produce actionable bug reports. Use this whenever the user reports cluster problems, wants to investigate observer findings, needs to debug multigres issues, asks about cluster health, or sees errors in operator or data plane logs.
---

# Diagnose with Observer Skill

**Goal:** Fetch a complete diagnostic snapshot from the observer's API, triage findings, investigate root causes in both operator and upstream multigres code, and produce an actionable bug report.

> **For systematic bug hunting** (deploying fixtures, running mutations, validating health), use the `exercise_cluster` skill instead. This skill is for **reactive investigation** — when something is already broken and you need to find out why.

## 1. Ensure the Observer is Running

```bash
KUBECONFIG=kubeconfig.yaml kubectl get pods -l app.kubernetes.io/name=multigres-observer -n multigres-operator
```

If not running: `make kind-deploy-observer`. The observer deploys automatically with `make kind-deploy`.

## 2. Fetch the Diagnostic Snapshot

Define the observer helper (stateless `kubectl exec` — no stale port-forwards to manage):
```bash
observer() {
  KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl exec -n multigres-operator deploy/multigres-observer -- curl -sf "http://localhost:9090$1"
}
```

Fetch the full snapshot:
```bash
observer /api/status | jq .
```

The response is a single JSON object:

| Key | Contents |
|-----|----------|
| `summary` | Cycle timing, finding counts by severity |
| `healthy` | Per-check health status (`true`/`false`) |
| `findings` | Array of all findings from the latest cycle |
| `probes` | Raw probe data per check (pods, connectivity, replication, drain, topology, crdStatus) |
| `coverage` | What was checked: SQL probes enabled, checks run, namespace |

### Quick Triage

```bash
# Just errors and fatals
observer /api/status | jq '[.findings[] | select(.level == "fatal" or .level == "error")]'

# Finding history with classification
observer /api/history | jq '{
  persistent: [.persistent[]? | {check, component, message, severity, count}],
  flapping: [.flapping[]? | {check, component, message, severity, count}],
  transientCount: (.transient | length)
}'

# Run specific checks on demand (without waiting for next cycle)
observer '/api/check?categories=replication,connectivity' | jq .
```

Valid categories: `pod-health`, `resource-validation`, `crd-status`, `drain-state`, `connectivity`, `logs`, `events`, `topology`, `replication`

For the full query catalog, see `references/observer-queries.md`.

### Finding History Classification

| Category | Meaning | Action |
|----------|---------|--------|
| `persistent` | Present in 75%+ of cycles (or <3 cycles) | Real issue — investigate |
| `flapping` | Active with gaps (3+ appearances, <75%) | Intermittent — may be a race condition |
| `transient` | Appeared then resolved | Expected during operations — report but don't block |

### Findings Structure

Each finding has:
- `level`: `info`, `warn`, `error`, `fatal`
- `check`: Category — `pod-health`, `resource-validation`, `crd-status`, `drain-state`, `connectivity`, `operator-logs`, `dataplane-logs`, `events`, `topology`, `replication`
- `component`: Affected resource, e.g., `shard/default/my-shard`
- `message`: Human-readable description
- `details`: Structured data for deeper investigation

### Probes

Raw data collected during the cycle, independent of findings:
- `probes.pods`: All managed pods with phase, readiness, component labels
- `probes.connectivity`: Every TCP/HTTP/gRPC/SQL probe result with latency and error
- `probes.replication`: Per-shard primary/replica counts and podRoles
- `probes.drain`: All pods currently in a drain state
- `probes.topology`: Per-cluster etcd reachability and rootPath
- `probes.crdStatus`: All CRDs with phase and readiness

## CRITICAL: Investigation Rules

**NEVER blame infrastructure (kind, Docker, CPU, memory, network) without concrete proof.** If a probe times out, investigate the actual call chain — check logs of every component in the path (gateway → multiorch → pooler → postgres). A 5-second timeout on `SELECT 1` is not "expected in kind" — it means something is broken.

1. **Trace the full call chain.** If the gateway SQL probe fails, check gateway logs → multiorch logs → pooler logs. Find where the request gets stuck.
2. **Check upstream component logs.** The observer only sees symptoms. Use `kubectl logs` on the actual components to find root causes.
3. **Never speculate about performance.** If something is slow, find the bottleneck with evidence.
4. **Report what you actually found.** State the specific failure chain with log evidence, not theories.

## 3. Severity Triage

Process findings in this order:

### Fatal (Critical — address immediately)
- **SPLIT-BRAIN**: Multiple pods report as primary. Data integrity emergency.
- **Writes blocked**: Write probe timed out.
- **Backward drain transition**: Drain state went backwards — controller bug.
- **Invalid phase transition**: Phase went `Healthy` → `Initializing` — should never happen.
- **Readiness cross-check**: Pod reports Ready but gRPC/readiness probe failed — silent data plane failure.
- **All poolers unreachable**: `multiorch-pooler-health` shows all poolers down — total outage.
- **Missing PVC**: Running pool pod references a PVC that doesn't exist.

### Error (Investigate)
- **Missing replicas**: Primary sees 0 replication connections.
- **Stuck drain**: Drain state hasn't progressed past its timeout.
- **Generation divergence**: Controller isn't reconciling (observed generation stale >60s).
- **WAL receiver down**: Replica disconnected from primary.
- **Connectivity failure**: Can't reach a multigres service.
- **PVC not Bound**: Pool pod's PVC exists but is not in `Bound` phase.
- **Backup very stale**: Last backup >49h old.
- **Cell ready > total**: `GatewayReadyReplicas` exceeds `GatewayReplicas` — impossible state.

### Warn (Monitor)
- **Replication lag >10s**: Standby falling behind.
- **WAL replay paused**: Someone paused replay on a replica.
- **Backup stale**: Last backup >25h old.
- **Backup never completed**: Backup configured but `LastBackupTime` is nil.
- **Stuck Progressing**: Phase stuck in `Progressing` >10 minutes.
- **Empty status message**: `Degraded`/`Unknown` phase with no status message.
- **Service missing endpoints**: Managed Service has no ready addresses.
- **Cell deployment mismatch**: Gateway readyReplicas doesn't match Cell status.
- **Operator metrics unreachable**: Can't probe operator `/metrics` on port 8443.
- **Topology unreachable**: etcd checks skipped (may be expected during startup).

### Info (Normal)
- Synced events, successful probes, orphaned PVCs — no action needed.

## 4. Common Diagnostic Patterns

### Multiple fatals from the same check
They often share a single root cause. Read the `details` fields — look for a common pod, application_name, or component. Fix the root cause rather than addressing each fatal individually.

### Silent data plane failure (all components "healthy" but queries fail)
The highest-signal finding is a **readiness cross-check fatal**: "Pod X reports Ready but multipooler-grpc-health probe failed". The pod passes Kubernetes readiness but the observer's gRPC probe detects the component is broken.

Check probes in order:
1. `multipooler-grpc-health`: If failing on all pool pods, the gRPC server (port 15270) is hanging
2. `multiorch-pooler-health`: If showing "all poolers unreachable", multiorch knows the data plane is down
3. `sql-probe`: If timing out while the above are failing, confirms total data plane outage

### Connectivity errors — always investigate
`context deadline exceeded` on SQL probes does NOT mean "kind is slow". Trace the call chain: gateway logs → multiorch logs → pooler logs. If multiorch can't reach poolers via gRPC, that's the root cause. TCP/HTTP probes passing while SQL fails means the problem is in the application layer, not the transport layer.

### System pod FailedScheduling in kind
`FailedScheduling` on `coredns`, `local-path-provisioner`, or `kube-scheduler` are kind artifacts during node startup. Resolve automatically. Ignore unless they persist >5 minutes.

### Topology validation skipped
`topology validation skipped: etcd unreachable` — observer can't connect to TopoServer etcd. Typically a label mismatch or TopoServer pods haven't started yet.

### Replication errors correlate
Replication findings often chain — broken sync config causes async standbys, which causes blocked writes. Look at `details` to find the upstream cause.

## 5. Investigation and Bug Report

After triaging findings, investigate root causes using:
- **Component log tracing**: `references/log-tracing.md` — trace failures through gateway → multiorch → pooler → postgres logs, plus kubectl cross-reference commands
- **Code investigation**: `references/code-investigation.md` — determine if the bug is in the operator or upstream multigres, with directory maps and version checking

Then produce a bug report documenting:
1. **Finding summary**: What the observer detected (severity, check, message)
2. **Affected component**: Operator, upstream multigres, or both
3. **Root cause**: The specific code path causing the issue
4. **Version info**: Which multigres SHA is running vs latest
5. **Fix location**: Whether it needs a fix in the operator, upstream, or both
6. **Suggested fix**: Concrete code change recommendation

If the bug exists on an older upstream SHA but is already fixed on `main`, report this — do not update image tags yourself.

## 6. Reference Documents

| Reference | When to Read |
|-----------|-------------|
| `references/observer-queries.md` | Full catalog of jq queries for probing specific aspects of observer output |
| `references/log-tracing.md` | When tracing failures through component logs and cross-referencing with kubectl |
| `references/code-investigation.md` | When investigating root causes in operator or upstream multigres code |
| `tools/observer/docs/observer.md` | Full check reference with all sub-checks and SQL queries |
| `tools/observer/docs/configuration.md` | Threshold values and constants |
| `tools/observer/docs/architecture.md` | How the observer works internally |

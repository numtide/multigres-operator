---
name: diagnose_with_observer
description: Use the multigres observer to diagnose cluster health issues. Fetch structured diagnostics from the /api/status endpoint, triage findings by severity, correlate root causes, and produce actionable bug reports.
---

# Diagnose with Observer Skill

**Goal:** Fetch a complete diagnostic snapshot from the observer's API, triage findings, investigate root causes in both operator and upstream multigres code, and produce an actionable bug report.

> **For systematic bug hunting** (deploying fixtures, running mutations, validating health), use the `exercise_cluster` skill instead. This skill is for **reactive investigation** — when something is already broken and you need to find out why.

## 1. Ensure the Observer is Running

Check if the observer pod is running:
```bash
KUBECONFIG=kubeconfig.yaml kubectl get pods -l app.kubernetes.io/name=multigres-observer -n multigres-operator
```

If not running, deploy it:
```bash
make kind-deploy-observer
```

The observer deploys automatically with `make kind-deploy`. Check the manage_local_cluster skill if you need to set up a cluster first.

## 2. Fetch the Diagnostic Snapshot

Port-forward the observer and fetch `/api/status`:
```bash
# Start port-forward in background (kill when done)
KUBECONFIG=kubeconfig.yaml kubectl port-forward -n multigres-operator svc/multigres-observer 9090:9090 &
PF_PID=$!
sleep 1

# Fetch the full diagnostic snapshot
curl -s http://localhost:9090/api/status | jq .

# Clean up
kill $PF_PID 2>/dev/null
```

The response is a single JSON object with these top-level keys:

| Key | Contents |
|-----|----------|
| `summary` | Cycle timing, finding counts by severity |
| `healthy` | Per-check health status (`true`/`false`) |
| `findings` | Array of all findings from the latest cycle |
| `probes` | Raw probe data per check (pods, connectivity, replication, drain, topology, crdStatus) |
| `coverage` | What was checked: SQL probes enabled, checks run, namespace |

### Quick Triage Queries

```bash
# Just the summary
curl -s http://localhost:9090/api/status | jq '.summary'

# Unhealthy checks only
curl -s http://localhost:9090/api/status | jq '.healthy | to_entries | map(select(.value == false))'

# Fatal and error findings only
curl -s http://localhost:9090/api/status | jq '[.findings[] | select(.level == "fatal" or .level == "error")]'

# Findings for a specific check
curl -s http://localhost:9090/api/status | jq '[.findings[] | select(.check == "replication")]'

# Connectivity probe results (all failed probes)
curl -s http://localhost:9090/api/status | jq '[.probes.connectivity[] | select(.ok == false)]'

# gRPC health probes (detect pool starvation / hanging multipooler)
curl -s http://localhost:9090/api/status | jq '[.probes.connectivity[] | select(.check == "multipooler-grpc-health")]'

# Multiorch pooler health (detect when orchestrator sees all poolers down)
curl -s http://localhost:9090/api/status | jq '[.probes.connectivity[] | select(.check == "multiorch-pooler-health")]'

# Readiness cross-check failures (Kubernetes says Ready but observer probes disagree)
curl -s http://localhost:9090/api/status | jq '[.findings[] | select(.message | test("reports Ready but"))]'

# Replication topology (per-shard primary/replica counts)
curl -s http://localhost:9090/api/status | jq '.probes.replication'

# Pod inventory
curl -s http://localhost:9090/api/status | jq '.probes.pods'

# Drain state overview
curl -s http://localhost:9090/api/status | jq '.probes.drain'

# CRD status overview
curl -s http://localhost:9090/api/status | jq '.probes.crdStatus'
```

### On-Demand Targeted Checks

Use `/api/check` to run specific checks immediately without waiting for the next cycle:
```bash
# Run only replication and connectivity checks
curl -s 'http://localhost:9090/api/check?categories=replication,connectivity' | jq .

# Run all checks on demand
curl -s http://localhost:9090/api/check | jq .
```

Valid categories: `pod-health`, `resource-validation`, `crd-status`, `drain-state`, `connectivity`, `logs`, `events`, `topology`, `replication`

Use this when you've made a change and want to verify immediately rather than waiting 10 seconds for the next cycle.

### Finding History and Pattern Detection

Use `/api/history` to see how findings behave over time:
```bash
# Full history with classification
curl -s http://localhost:9090/api/history | jq .

# Just persistent and flapping findings (the ones that matter)
curl -s http://localhost:9090/api/history | jq '{
  persistent: [.persistent[]? | {check, component, message, severity, count}],
  flapping: [.flapping[]? | {check, component, message, severity, count}],
  transientCount: (.transient | length),
  totalCycles: .totalCycles,
  observerStartedAt: .observerStartedAt
}'
```

**Classification:**

| Category | Meaning | Action |
|----------|---------|--------|
| `persistent` | Present in 75%+ of cycles (or <3 cycles) | Real issue — investigate |
| `flapping` | Active with gaps (3+ appearances, <75%) | Intermittent — may be a race condition |
| `transient` | Appeared then resolved | Expected during operations — report but don't block |

**`observerStartedAt`** tells you when the observer process started. If `totalCycles` is low and `observerStartedAt` is recent, findings may not have stabilized yet — wait for more cycles before classifying.

### Fallback: Read Logs Directly

If port-forwarding isn't available, fall back to logs. Observer logs use double-nested JSON:
```bash
KUBECONFIG=kubeconfig.yaml kubectl logs -l app.kubernetes.io/name=multigres-observer -n multigres-operator --tail=200 | grep '\"fatal\"'
KUBECONFIG=kubeconfig.yaml kubectl logs -l app.kubernetes.io/name=multigres-observer -n multigres-operator --tail=200 | grep '\"error\"'
```

## 3. Understanding the Response Structure

### Findings

Each finding has:
- `ts`: Timestamp
- `level`: Severity — `info`, `warn`, `error`, `fatal`
- `check`: Check category — `pod-health`, `resource-validation`, `crd-status`, `drain-state`, `connectivity`, `operator-logs`, `dataplane-logs`, `events`, `topology`, `replication`
- `component`: Affected resource, e.g., `shard/default/my-shard`
- `message`: Human-readable description
- `details`: Structured data for deeper investigation

### Probes

Raw data collected during the cycle, independent of findings. Use probes to understand the full state even when there are no errors:
- `probes.pods`: All managed pods with phase, readiness, component labels
- `probes.connectivity`: Every TCP/HTTP/gRPC/SQL probe result with latency and error
- `probes.replication`: Per-shard primary/replica counts and podRoles
- `probes.drain`: All pods currently in a drain state
- `probes.topology`: Per-cluster etcd reachability and rootPath
- `probes.crdStatus`: All CRDs with phase and readiness

## CRITICAL: Investigation Rules

**NEVER blame infrastructure (kind, Docker, CPU, memory, network) without concrete proof.** If a probe times out, investigate the actual call chain — check logs of every component in the path (gateway → multiorch → pooler → postgres). A 5-second timeout on `SELECT 1` is not "expected in kind" — it means something is broken.

When investigating errors:
1. **Trace the full call chain.** If the gateway SQL probe fails, check gateway logs for the connection lifecycle, then check multiorch logs, then pooler logs. Find where the request gets stuck.
2. **Check upstream component logs.** The observer only sees symptoms. Use `kubectl logs` on the actual components (multiorch, multipooler, pgctld) to find root causes. grep for `error`, `warn`, `failed`, `timeout`, `deadline`.
3. **Never speculate about performance.** If something is slow, find the bottleneck with evidence. Check if gRPC calls are failing, if DNS resolution works, if ports are actually reachable.
4. **Report what you actually found.** State the specific failure chain with log evidence, not theories about resource constraints.

## 4. Severity Triage

Process findings in this order:

### Fatal (Critical — address immediately)
- **SPLIT-BRAIN**: Multiple pods report as primary. Data integrity emergency.
- **Writes blocked**: The write probe timed out, `INSERT`/`UPDATE` will hang.
- **Backward drain transition**: Drain state went backwards, indicating a bug.
- **Invalid phase transition**: Phase went `Healthy` → `Initializing` — controller bug, should never happen.
- **Readiness cross-check**: Pod reports Ready but gRPC/readiness probe failed — silent data plane failure.
- **All poolers unreachable**: `multiorch-pooler-health` shows all poolers down — total data plane outage.
- **Missing PVC**: Running pool pod references a PVC that doesn't exist — pod cannot function.

### Error (Investigate)
- **Missing replicas**: Primary sees 0 replication connections.
- **Stuck drain**: Drain state hasn't progressed past its timeout.
- **Generation divergence**: Controller isn't reconciling (observed generation stale >60s).
- **WAL receiver down**: Replica disconnected from primary.
- **Connectivity failure**: Can't reach a multigres service.
- **PVC not Bound**: Pool pod's PVC exists but is not in `Bound` phase.
- **Backup very stale**: Last backup >49h old.
- **Cell ready > total**: `GatewayReadyReplicas` exceeds `GatewayReplicas` — impossible state, status update bug.

### Warn (Monitor)
- **Replication lag >10s**: Standby falling behind.
- **WAL replay paused**: Someone paused replay on a replica.
- **Backup stale**: Last backup >25h old.
- **Backup never completed**: Backup is configured but `LastBackupTime` is nil.
- **Stuck Progressing**: Phase stuck in `Progressing` for >10 minutes without advancing.
- **Empty status message**: `Degraded` or `Unknown` phase with no status message.
- **Service missing endpoints**: Multigres-managed Service has no Endpoints object or zero ready addresses.
- **Cell deployment mismatch**: Actual gateway Deployment readyReplicas doesn't match Cell status.
- **Operator metrics unreachable**: Can't probe operator `/metrics` endpoint on port 8443.
- **Topology unreachable**: etcd checks skipped (may be expected during startup).

### Info (Normal)
- Synced events, successful probes, orphaned PVCs — no action needed.

## 5. Common Diagnostic Patterns

### Multiple fatals from the same check
When you see multiple `fatal` findings from the same `check` category, they often share a single root cause. Read the `details` fields — look for a common pod, application_name, or component. Fix the root cause rather than addressing each fatal individually.

### Silent data plane failure (all components "healthy" but queries fail)
The highest-signal finding is a **readiness cross-check fatal**: "Pod X reports Ready but multipooler-grpc-health probe failed". This means the pod's Kubernetes readiness probe returns 200 but the observer's own gRPC probe (3s timeout) detected the component is broken. This pattern occurs when the multipooler's connection pool is exhausted or its gRPC server is hanging.

Check these probes in order:
1. `multipooler-grpc-health`: If failing on all pool pods, the multipooler gRPC server (port 15270) is hanging
2. `multiorch-pooler-health`: If showing "all poolers unreachable", multiorch knows the data plane is down
3. `sql-probe`: If timing out while the above are failing, confirms total data plane outage
4. Cross-check findings: Any "reports Ready but... probe failed" finding means Kubernetes readiness probes are not detecting the failure

```bash
# Quick check for silent failures
curl -s http://localhost:9090/api/status | jq '[.findings[] | select(.message | test("reports Ready but"))]'

# All gRPC health probes
curl -s http://localhost:9090/api/status | jq '[.probes.connectivity[] | select(.check == "multipooler-grpc-health")]'

# Multiorch pooler health
curl -s http://localhost:9090/api/status | jq '[.probes.connectivity[] | select(.check == "multiorch-pooler-health")]'
```

### Connectivity errors — always investigate
`context deadline exceeded` on SQL probes does NOT mean "kind is slow". Trace the call chain: check gateway logs for the connection ID lifecycle, check multiorch logs for pooler poll failures, check pooler logs for gRPC errors. If multiorch can't reach poolers via gRPC, that's the root cause — not network performance. TCP/HTTP probes passing while SQL fails means the problem is in the application layer (routing, gRPC, upstream bugs), not the transport layer.

### System pod FailedScheduling in kind
`FailedScheduling` errors on `coredns`, `local-path-provisioner`, or `kube-scheduler` pods are kind cluster artifacts during node startup. These resolve automatically. Ignore unless they persist >5 minutes.

### Topology validation skipped
`topology validation skipped: etcd unreachable` means the observer can't connect to the TopoServer etcd (which uses plain HTTP, not TLS). This typically indicates a label mismatch preventing service discovery, or that the TopoServer pods haven't started yet. If it persists after all pods are Running, check that the TopoServer service has the expected `app.kubernetes.io/component=toposerver` label.

### Replication errors correlate with each other
Replication findings often chain — for example, a broken sync config causes async standbys, which causes blocked writes. Look at the `details` to find the upstream cause rather than treating each finding independently.

## 6. Cross-Referencing with kubectl

After identifying findings, verify with direct kubectl:
```bash
# Check pod status
KUBECONFIG=kubeconfig.yaml kubectl get pods -A | grep -v kube-system

# Check shard status
KUBECONFIG=kubeconfig.yaml kubectl get shards -A -o wide

# Check CRD conditions
KUBECONFIG=kubeconfig.yaml kubectl get multigresclusters -A -o yaml | grep -A5 conditions

# Check drain annotations
KUBECONFIG=kubeconfig.yaml kubectl get pods -l app.kubernetes.io/component=shard-pool -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.drain\.multigres\.com/state}{"\n"}{end}'

# Cross-reference CRD podRoles with actual SQL state
# (catches stale podRoles where CRD says REPLICA but pod is actually PRIMARY)
KUBECONFIG=kubeconfig.yaml kubectl get shard -A -o json | jq '[.items[] | {name: .metadata.name, podRoles: .status.podRoles}]'
for pod in $(KUBECONFIG=kubeconfig.yaml kubectl get pods -l app.kubernetes.io/component=shard-pool -o name); do
  echo -n "$pod: "
  KUBECONFIG=kubeconfig.yaml kubectl exec $pod -c postgres -- psql -h 127.0.0.1 -p 5432 -U postgres -tAc "SELECT CASE WHEN pg_is_in_recovery() THEN 'REPLICA' ELSE 'PRIMARY' END"
done

# Check finding history for patterns
curl -s http://localhost:9090/api/history | jq '{persistent: (.persistent // []) | length, flapping: (.flapping // []) | length, transient: (.transient // []) | length}'
```

## 7. Trace the Failure Chain in Component Logs

Before jumping to code, trace the actual failure through component logs. The observer detects symptoms — the root cause is in the component logs.

```bash
# Gateway: trace a specific connection by ID, check routing decisions
KUBECONFIG=kubeconfig.yaml kubectl logs -l app.kubernetes.io/component=multigateway --tail=200 | grep -E 'error|failed|timeout|cancel'

# MultiOrch: check if it can reach poolers (gRPC Status RPCs)
KUBECONFIG=kubeconfig.yaml kubectl logs -l app.kubernetes.io/component=multiorch --tail=200 | grep -v 'capacity' | grep -E 'error|warn|failed|poll|dead|quorum' -i

# Pooler: check gRPC server health, backend connections
KUBECONFIG=kubeconfig.yaml kubectl logs <pod-name> -c multipooler --tail=200 | grep -E 'error|failed|grpc|backend' -i

# Postgres: check for replication issues, connection limits
KUBECONFIG=kubeconfig.yaml kubectl logs <pod-name> -c postgres --tail=200 | grep -E 'ERROR|FATAL|LOG.*replication'
```

**Key patterns:**
- Gateway SQL timeout + multiorch "pooler poll failed" = multiorch can't reach poolers, so gateway can't route queries
- "DeadlineExceeded" on gRPC Status RPCs = pooler gRPC server is hanging or unreachable (check if port 15270 is listening, check for version mismatches)
- "PrimaryIsDead" + "insufficient poolers for quorum" = multiorch thinks primary is dead because it can't poll it, and can't failover without quorum
- "method not implemented" on gRPC calls = version mismatch between multiorch and multipooler binaries

## 8. Investigate Root Causes in Code

Once you have findings, investigate whether the bug is in the **operator** or **upstream multigres**:

### Determine which multigres version is running

Check what image tags the running pods are using:
```bash
KUBECONFIG=kubeconfig.yaml kubectl get pods -A -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{range .spec.containers[*]}{.image}{" "}{end}{"\n"}{end}' | grep multigres
```

Or check the defaults compiled into the operator:
```bash
cat api/v1alpha1/image_defaults.go
```

Image tags use the format `sha-XXXXXXX` which corresponds to a commit SHA in the multigres repo.

### Investigate operator code

Search the operator repo for code related to the finding:
- **Replication findings**: Check `pkg/resource-handler/controller/shard/` — especially `containers.go` (how pod names are set), `pool_pod.go` (pod spec construction)
- **Drain findings**: Check `pkg/data-handler/drain/drain.go` — the drain state machine
- **Status findings**: Check `pkg/resource-handler/controller/shard/shard_controller.go` — status reconciliation
- **Connectivity/resource findings**: Check `pkg/resource-handler/controller/` for the relevant component
- **Topology findings**: Check `pkg/data-handler/topo/` — etcd topology operations

### Investigate upstream multigres code

Clone or pull the latest upstream code:
```bash
# Clone if not present, pull if already cloned
if [ -d /tmp/multigres ]; then
  cd /tmp/multigres && git fetch origin && git pull origin main
else
  git clone https://github.com/multigres/multigres /tmp/multigres
fi
```

**Check the version actually running, not just latest.** If the bug exists on `main` but not on the SHA the operator is using (or vice versa), note that in the report:
```bash
# Checkout the exact version running in the cluster
cd /tmp/multigres
git checkout <sha-from-image-tag>
```

Key upstream directories:
- `go/cmd/multigateway/` — gateway behavior, routing, ports
- `go/cmd/multipooler/` — connection pooling, health endpoints
- `go/cmd/multiorch/` — orchestration, failover, replication management
- `go/cmd/pgctld/` — PostgreSQL lifecycle, startup, shutdown
- `go/proto/` — gRPC service definitions

## 9. Produce a Bug Report

After investigation, document:

1. **Finding summary**: What the observer detected (severity, check, message)
2. **Affected component**: Operator, upstream multigres, or both
3. **Root cause**: The specific code path causing the issue
4. **Version info**: Which multigres SHA is running vs latest
5. **Fix location**: Whether it needs a fix in the operator, upstream, or both
6. **Suggested fix**: Concrete code change recommendation

If the bug exists on an older upstream SHA but is already fixed on `main`, **report this to the user** — do not update image tags yourself.

## 10. Reference Documentation

- `tools/observer/docs/observer.md` — Full check reference with all sub-checks and SQL queries
- `tools/observer/docs/configuration.md` — Threshold values and constants
- `tools/observer/docs/architecture.md` — How the observer works internally

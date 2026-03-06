# Architecture

Internal design of the observer: how the binary is structured, how reports flow, how metrics work, and how deployment is organized.

---

## Binary Design

The `multigres-observer` binary is a single Go process that runs continuous health checks against a Multigres cluster.

```
main.go
  ├── Flag/env parsing
  ├── Kubernetes client setup
  ├── Prometheus registry + metrics server (:9090)
  ├── Reporter (JSON log emitter)
  └── Observer.Run(ctx)
```

Environment variables override corresponding flags for Kubernetes-native configuration:
- `NAMESPACE` → `--namespace`
- `OPERATOR_NAMESPACE` → `--operator-namespace`

---

## Observer Cycle

The observer runs a ticker loop. Each tick executes all 9 check categories sequentially, tracks each check's duration, then emits a summary.

```
┌─────────────────────────────────────────────────────────────┐
│                     Observer Cycle                           │
│                                                             │
│  track("pod-health",          checkPodHealth)                │
│  track("resource-validation", checkResources)                │
│  track("crd-status",          checkCRDStatus)                │
│  track("drain-state",         checkDrainState)               │
│  track("connectivity",        checkConnectivity)             │
│  track("logs",                checkLogs)                     │
│  track("events",              checkEvents)                   │
│  track("topology",            checkTopology)                 │
│  track("replication",         checkReplication)               │
│                                                             │
│  → Summary: {findings: N, errors: M, fatals: K}            │
│  → Metric: multigres_observer_observer_cycle_duration_seconds│
│  → Metric: multigres_observer_check_healthy{check=X} = 0|1 │
└─────────────────────────────────────────────────────────────┘
```

The `track()` wrapper measures each check's execution time and records it in `multigres_observer_check_duration_seconds{check=X}`.

---

## Report Pipeline

Findings flow through a single pipeline:

```
Check function
    ↓
reporter.Report(Finding{...})
    ↓
┌─────────────────────────────┐
│ Reporter                     │
│  1. Set timestamp (UTC)     │
│  2. Append to buffer        │
│  3. Record Prometheus metric │
│  4. Marshal to JSON          │
│  5. Emit via slog at        │
│     appropriate level:      │
│     info → logger.Info()    │
│     warn → logger.Warn()   │
│     error/fatal → Error()  │
└─────────────────────────────┘
```

### Finding Format

Every finding is a single JSON line:

```json
{
  "ts": "2026-03-05T20:04:53.470Z",
  "level": "fatal",
  "check": "replication",
  "component": "shard/default/my-cluster-shard-0",
  "message": "Standby \"zone-a_my-pod\" is async but synchronous replication is configured",
  "details": {
    "pod": "my-pod-0",
    "applicationName": "zone-a_my-pod",
    "syncState": "async",
    "synchronousStandbyNames": "ANY 1 (\"zone-a_my-pod-1\")"
  }
}
```

### Severity Levels

| Level | Meaning | When to use |
|-------|---------|-------------|
| `info` | Normal operation confirmed | Synced events, successful probes |
| `warn` | Potential issue, may self-resolve | Pod readiness flap, etcd unreachable, backup stale |
| `error` | Bug or failure requiring attention | Stuck drain, missing resources, connectivity failure |
| `fatal` | Critical invariant violation | Split-brain, blocked writes, truncated names |

### Cycle Summary

At the end of each cycle, the observer emits a summary line:

```json
{
  "msg": "observer cycle complete",
  "duration": 10687000000,
  "findings": 21,
  "errors": 11,
  "fatals": 4
}
```

---

## Prometheus Metrics

All metrics use the `multigres_observer` namespace. Exposed at `:9090/metrics`.

### Observer Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `multigres_observer_observer_findings_total` | Counter | `check`, `severity` | Findings count by check and severity |
| `multigres_observer_observer_cycle_duration_seconds` | Histogram | — | Duration of each full cycle |
| `multigres_observer_observer_cycles_total` | Counter | — | Total cycles completed |
| `multigres_observer_check_healthy` | Gauge | `check` | 1=healthy, 0=had errors/fatals |
| `multigres_observer_check_duration_seconds` | Histogram | `check` | Per-check duration |

### Pod Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `multigres_observer_pod_restarts_total` | Counter | `pod`, `container` | Observed container restarts |
| `multigres_observer_pod_ready` | Gauge | `pod`, `namespace` | 1=ready, 0=not ready |

### Connectivity Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `multigres_observer_probe_latency_seconds` | Histogram | `check`, `target` | Probe latency with buckets from 10ms to 5s |

---

## Deployment Model

### Kustomize Base

```
deploy/
└── base/
    ├── kustomization.yaml
    ├── serviceaccount.yaml
    ├── clusterrole.yaml         # Read-only RBAC
    ├── clusterrolebinding.yaml
    └── deployment.yaml
```

### RBAC (Read-Only)

```yaml
rules:
  - apiGroups: ["multigres.com"]
    resources: ["*"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["pods", "pods/log", "services", "persistentvolumeclaims",
                "events", "configmaps", "secrets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apps"]
    resources: ["deployments", "statefulsets"]
    verbs: ["get", "list", "watch"]
```

### Scoping

All operations target only Multigres-managed resources using label selectors:

```
app.kubernetes.io/managed-by: multigres-operator
```

---

## In-Memory State

The observer maintains several tracking maps that persist across cycles but reset on pod restart:

| Map | Key Format | Purpose |
|-----|-----------|---------|
| `prevRestarts` | `ns/pod/container` | Detect new restarts (delta from last cycle) |
| `podPhaseSince` | `ns/pod` | Track how long a pod has been in a bad phase |
| `drainStateSince` | `ns/pod` | Track how long a drain state has been stuck |
| `prevDrainState` | `ns/pod` | Detect backward state transitions |
| `generationDivergeSince` | `kind/ns/name` | Track how long generation hasn't caught up |
| `primaryViolationSince` | `pool-cell` | Grace period before flagging wrong primary count |
| `lastLogCheck` | single timestamp | Avoid re-tailing already-checked logs |
| `lastEventResourceVersion` | single string | Only process new events each cycle |

This state is purely observational — losing it on restart is safe. The observer re-converges within 1-2 cycles.

---

## Go Module

The observer is a **separate Go module** (`tools/observer/go.mod`). This keeps the operator's dependency tree clean of:
- `pgx` (PostgreSQL driver for SQL probes)
- `prometheus/client_golang` (metrics instrumentation)

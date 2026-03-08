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

The observer runs a ticker loop. Each tick executes all 10 check categories sequentially, tracks each check's duration, then emits a summary.

```
┌──────────────────────────────────────────────────────────────┐
│                     Observer Cycle                           │
│                                                              │
│  track("pod-health",          checkPodHealth)                │
│  track("resource-validation", checkResources)                │
│  track("crd-status",          checkCRDStatus)                │
│  track("drain-state",         checkDrainState)               │
│  track("connectivity",        checkConnectivity)             │
│  track("logs",                checkLogs)                     │
│    → findings emit as "operator-logs" or "dataplane-logs"    │
│  track("events",              checkEvents)                   │
│  track("topology",            checkTopology)                 │
│  track("replication",         checkReplication)              │
│                                                              │
│  → Summary: {findings: N, errors: M, fatals: K}              │
│  → Metric: multigres_observer_observer_cycle_duration_seconds│
│  → Metric: multigres_observer_check_healthy{check=X} = 0|1   │
└──────────────────────────────────────────────────────────────┘
```

> **Note:** The `track()` wrapper records duration under the name `"logs"`, but the
> findings emitted by `checkLogs` use two distinct check names: `"operator-logs"`
> (operator manager container) and `"dataplane-logs"` (multipooler, postgres,
> multigateway, multiorch, toposerver containers). The `healthy` map and
> `check_healthy` gauge use these two names, not `"logs"`.

The `track()` wrapper measures each check's execution time and records it in `multigres_observer_check_duration_seconds{check=X}`.

---

## Report Pipeline

Findings flow through a single pipeline:

```
Check function
    ↓
reporter.Report(Finding{...})
    ↓
┌──────────────────────────────┐
│ Reporter                     │
│  1. Set timestamp (UTC)      │
│  2. Append to buffer         │
│  3. Record Prometheus metric │
│  4. Marshal to JSON          │
│  5. Emit via slog at         │
│     appropriate level:       │
│     info → logger.Info()     │
│     warn → logger.Warn()     │
│     error/fatal → Error()    │
└──────────────────────────────┘
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

## HTTP API

The observer exposes a structured HTTP API alongside Prometheus metrics on the same port (`:9090`).

### `GET /api/status`

Returns the latest cycle's complete diagnostic snapshot as JSON. Returns `503 Service Unavailable` if no cycle has completed yet.

```json
{
  "summary": {
    "cycleStart": "2026-03-06T10:00:00Z",
    "cycleEnd": "2026-03-06T10:00:02Z",
    "counts": { "info": 3, "warn": 1, "error": 2, "fatal": 0 },
    "totalFindings": 6
  },
  "healthy": {
    "pod-health": true,
    "connectivity": false,
    "replication": true
  },
  "findings": [
    {
      "ts": "2026-03-06T10:00:01Z",
      "level": "error",
      "check": "connectivity",
      "component": "multigateway-svc",
      "message": "multigateway-pg: TCP probe failed for ...",
      "details": {}
    }
  ],
  "probes": {
    "pods": { "total": 8, "pods": [...] },
    "connectivity": [ { "check": "...", "ok": true, "latency": "12ms" } ],
    "replication": { "shards": [...], "sqlProbeEnabled": true },
    "drain": { "drainingPods": [] },
    "topology": { "clusters": [...] },
    "crdStatus": { "clusters": [...], "shards": [...], "cells": [...] }
  },
  "coverage": {
    "sqlProbeEnabled": true,
    "checksRun": ["pod-health", "resource-validation", ...],
    "namespace": ""
  }
}
```

### Data Flow

Each `runCycle()` creates a fresh `ProbeCollector`. Check functions append raw probe data to the collector alongside reporting findings. At the end of the cycle, the reporter returns findings via `SummaryWithFindings()`, and the observer atomically stores a `StatusResponse` snapshot. The HTTP handler reads the latest snapshot under a `sync.RWMutex`.

```
runCycle()
  ├── probes = newProbeCollector()
  ├── track("pod-health", ...) → findings + probes.Set("pods", ...)
  ├── track("connectivity", ...) → findings + probes.RecordProbe(...)
  ├── ...
  ├── reporter.SummaryWithFindings() → []Finding, Summary, healthy
  └── snap.Store(&StatusResponse{...})

StatusHandler()
  └── snap.Load() → JSON response
```

### Other Endpoints

| Path | Description |
|------|-------------|
| `GET /metrics` | Prometheus metrics |
| `GET /healthz` | Liveness probe (always 200) |

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
| `podStartup` | `pod-name` | Pool pod creation time + readiness for startup grace period |
| `lastLogCheck` | single timestamp | Avoid re-tailing already-checked logs |
| `lastEventResourceVersion` | single string | Only process new events each cycle |

This state is purely observational — losing it on restart is safe. The observer re-converges within 1-2 cycles.

### Startup Grace Period

The `podStartup` map is populated each cycle by `checkPodHealth` for pool pods and consumed by downstream checks (`connectivity`, `replication`, `logs`). Pods younger than 60 seconds have all findings suppressed; pods older than 60 seconds but not yet Ready have `error`/`fatal` findings downgraded to `warn`. See the [README](../README.md#startup-grace-period) for the full rationale.

---

## Go Module

The observer is a **separate Go module** (`tools/observer/go.mod`). This keeps the operator's dependency tree clean of:
- `pgx` (PostgreSQL driver for SQL probes)
- `prometheus/client_golang` (metrics instrumentation)

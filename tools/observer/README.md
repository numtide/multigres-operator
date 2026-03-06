# Multigres Observer

A Kubernetes-native health validation tool for the Multigres Operator. It runs inside the cluster alongside the operator and provides continuous health monitoring across 9 check categories.

## Quick Start

### Deploy the Observer

The observer deploys automatically alongside the operator:

```bash
make kind-deploy
```

This chains `kind-deploy-observer` at the end. To deploy the observer alone:

```bash
make kind-deploy-observer
```

### Watch the Logs

```bash
KUBECONFIG=kubeconfig.yaml kubectl logs -f -l app.kubernetes.io/name=multigres-observer -n multigres-operator
```

Findings are structured JSON — one line per finding with severity, check name, component, and details. At the end of each 10-second cycle, a summary line reports total findings and error/fatal counts.

### Diagnostic API

The observer exposes a structured JSON endpoint with the complete diagnostic snapshot from the latest cycle:

```bash
KUBECONFIG=kubeconfig.yaml kubectl port-forward svc/multigres-observer -n multigres-operator 9090:9090
curl -s http://localhost:9090/api/status | jq .
```

The response includes findings (what's wrong), raw probe data (full picture), per-check health, and coverage info. See [Architecture](docs/architecture.md) for the full JSON schema.

### Prometheus Metrics

Metrics are exposed at `:9090/metrics` inside the pod, integrating with the existing observability stack:

```bash
curl http://localhost:9090/metrics
```

## Architecture

```
┌──────────────────────────────────────────────────┐
│                multigres-observer                 │
│                                                   │
│  ┌─────────┐  ┌──────────┐  ┌─────────────────┐ │
│  │ Observer │→ │ Reporter │→ │ JSON stdout     │ │
│  │ (checks)│  │          │  │ Prometheus :9090 │ │
│  └─────────┘  └──────────┘  └─────────────────┘ │
│       │                                           │
│  ┌────┴────────────────────────────────────────┐ │
│  │ Checks:                                      │ │
│  │  pod-health · resource-validation · crd-status│ │
│  │  drain-state · connectivity · replication     │ │
│  │  logs · events · topology                     │ │
│  └──────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────┘
         ↕ Kubernetes API              ↕ Direct SQL
   CRDs, Pods, Events, Logs      PostgreSQL on pool pods
```

The observer is **stateless** — no PVC, no persistent storage. Every cycle re-evaluates from scratch. In-memory trackers (restart counts, phase timestamps, drain state) reset when the pod restarts.

## What It Detects

The observer catches issues across the full stack:

**Control plane:**
- Pod crashes, OOMKills, restart storms
- Orphaned resources, broken ownership chains
- CRD phase regressions, stale ObservedGeneration
- Stuck drains, invalid drain state transitions
- Operator health failures

**Data plane (SQL probes):**
- Replication lag (warn >10s, error >60s)
- Truncated `application_name` breaking sync replication matching
- Async standbys when synchronous replication is configured
- Blocked writes on the primary
- WAL receiver disconnected on replicas
- WAL replay paused on replicas
- Split-brain (multiple pods reporting as primary)

**Infrastructure:**
- TCP/HTTP/gRPC connectivity to all components
- Probe latency exceeding thresholds
- etcd topology drift vs CRD state
- Kubernetes Warning events (FailedScheduling, OOM, Unhealthy)

## Documentation

| Document | Description |
|----------|-------------|
| [Observer Reference](docs/observer.md) | Full reference for all 9 check categories with thresholds |
| [Configuration Reference](docs/configuration.md) | Flags, environment variables, thresholds, RBAC |
| [Architecture](docs/architecture.md) | Internal design, report format, metrics, deployment model |

## Project Structure

```
tools/observer/
├── cmd/multigres-observer/
│   └── main.go                    # Entrypoint, flags, metrics server
├── pkg/
│   ├── observer/                  # 9 check categories + API
│   │   ├── observer.go            # Main loop, cycle orchestration, StatusHandler
│   │   ├── probes.go              # Per-cycle probe data collector
│   │   ├── snapshot.go            # Thread-safe latest-cycle snapshot store
│   │   ├── pods.go                # Pod health, restarts, OOM, counts
│   │   ├── resources.go           # Resource existence, ownership, labels
│   │   ├── status.go              # CRD phases, conditions, podRoles
│   │   ├── drain.go               # Drain state machine monitoring
│   │   ├── connectivity.go        # TCP/HTTP/gRPC/SQL endpoint probing
│   │   ├── replication.go         # SQL replication health, split-brain
│   │   ├── logs.go                # Log streaming, error patterns
│   │   ├── events.go              # Kubernetes event monitoring
│   │   └── topology.go            # etcd topology validation
│   ├── report/                    # Structured reporting + Prometheus
│   │   ├── reporter.go            # JSON log emission, SummaryWithFindings
│   │   ├── types.go               # Finding, Summary, StatusResponse, CoverageInfo
│   │   └── metrics.go             # Prometheus gauges/counters/histograms
│   └── common/                    # Shared constants, client setup
│       ├── client.go              # Kubernetes client factory
│       └── constants.go           # Labels, ports, thresholds, states
├── deploy/
│   └── base/                      # Kustomize manifests (read-only RBAC)
├── Dockerfile
└── go.mod                         # Separate Go module
```

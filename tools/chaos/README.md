# Multigres Chaos Tool

A Kubernetes-native testing and validation tool for the Multigres Operator. It runs inside the cluster alongside the operator and provides continuous health validation, operational scenario testing, fault injection, and adversarial mutation testing across four progressive levels.

| Level | Name | Description | Dependencies |
|-------|------|-------------|--------------|
| 0 | **Observer** | Continuous validation of cluster health | None (read-only) |
| 1 | **Exerciser** | Normal operations (scale, config, restart) | Level 0 |
| 2 | **Fault Injector** | Infrastructure faults via Chaos Mesh | Level 0 + Chaos Mesh |
| 3 | **Adversarial** | Hostile concurrent mutations | Level 0 + Chaos Mesh |

Each level is independently deployable. Level 0 ships with every `kind-deploy` target. Levels 1–3 are activated explicitly.

## Quick Start

### Deploy the Observer (Level 0)

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
KUBECONFIG=kubeconfig.yaml kubectl logs -f -l app.kubernetes.io/name=multigres-chaos -n multigres-operator
```

Findings are structured JSON — one line per finding with severity, check name, component, and details. At the end of each 10-second cycle, a summary line reports total findings and error/fatal counts.

### Deploy Higher Levels

```bash
CHAOS_LEVEL=1 make kind-deploy-chaos    # Exerciser
CHAOS_LEVEL=2 make kind-deploy-chaos    # Fault Injector (requires Chaos Mesh)
CHAOS_LEVEL=3 make kind-deploy-chaos    # Adversarial (requires Chaos Mesh)
```

### Prometheus Metrics

Metrics are exposed at `:9090/metrics` inside the pod, integrating with the existing observability stack:

```bash
# Port-forward and scrape
KUBECONFIG=kubeconfig.yaml kubectl port-forward svc/multigres-chaos -n multigres-operator 9090:9090
curl http://localhost:9090/metrics
```

## Architecture

```
┌──────────────────────────────────────────────────┐
│                 multigres-chaos                   │
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
| [Exerciser Reference](docs/exerciser.md) | Level 1 scenario catalog: scale, config, restart, lifecycle |
| [Fault Injection Reference](docs/fault-injection.md) | Level 2 Chaos Mesh scenarios: pod, network, IO, stress |
| [Adversarial Reference](docs/adversarial.md) | Level 3 concurrent mutations, cascade failures, corruption |
| [Configuration Reference](docs/configuration.md) | Flags, environment variables, thresholds, RBAC |
| [Architecture](docs/architecture.md) | Internal design, report format, metrics, deployment model |

## Project Structure

```
tools/chaos/
├── cmd/multigres-chaos/
│   └── main.go                    # Entrypoint, flags, metrics server
├── pkg/
│   ├── observer/                  # Level 0 — 9 check categories
│   │   ├── observer.go            # Main loop, cycle orchestration
│   │   ├── pods.go                # Pod health, restarts, OOM, counts
│   │   ├── resources.go           # Resource existence, ownership, labels
│   │   ├── status.go              # CRD phases, conditions, podRoles
│   │   ├── drain.go               # Drain state machine monitoring
│   │   ├── connectivity.go        # TCP/HTTP/gRPC/SQL endpoint probing
│   │   ├── replication.go         # SQL replication health, split-brain
│   │   ├── logs.go                # Log streaming, error patterns
│   │   ├── events.go              # Kubernetes event monitoring
│   │   └── topology.go            # etcd topology validation
│   ├── exerciser/                 # Level 1 — operational scenarios
│   ├── fault/                     # Level 2 — Chaos Mesh fault injection
│   ├── adversarial/               # Level 3 — hostile mutations
│   ├── report/                    # Structured reporting + Prometheus
│   │   ├── reporter.go            # JSON log emission
│   │   ├── types.go               # Finding, Summary, Severity
│   │   └── metrics.go             # Prometheus gauges/counters/histograms
│   └── common/                    # Shared constants, client setup
│       ├── client.go              # Kubernetes client factory
│       └── constants.go           # Labels, ports, thresholds, states
├── deploy/
│   ├── base/                      # Level 0 kustomize (read-only RBAC)
│   └── chaos/                     # Level 1–3 overlay (write RBAC)
├── Dockerfile
└── go.mod                         # Separate Go module
```

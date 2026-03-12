# Multigres Observer

A read-only health validation tool for Multigres clusters. It runs inside the cluster alongside the operator and provides continuous monitoring across 10 check categories. The observer's core purpose is catching issues that are **invisible to both `kubectl` and the operator itself** — silent data-plane failures, replication anomalies, and state mismatches that no single component can detect on its own.

## Use Cases

- Aggregate the health of every cluster component into a single diagnostic snapshot
- Validate operator behavior during failovers, scale-ups, drains, and upgrades
- Monitor cluster state during chaos experiments and fault injection
- Detect silent data plane failures where components report healthy but are broken

The observer exposes a structured `/api/status` endpoint designed for both human inspection and AI-assisted troubleshooting. An agent with the [diagnose skill](skills/diagnose_with_observer/SKILL.md) can fetch the snapshot, triage findings, and trace root causes across operator and upstream multigres code.

## Why This Exists

Multigres has many moving parts (gateway, orchestrator, pooler, postgres, etcd) and getting a full picture of cluster health requires checking each component individually. The observer consolidates all of these checks into a single loop and cross-references the results — for example, detecting when Kubernetes thinks a pod is Ready but the observer's own probes show it is broken, or when the operator reports Healthy but replication is actually degraded.

Neither `kubectl` nor the operator can reliably surface these issues:

- **`kubectl`** only shows pod-level status (Running, Ready, CrashLoopBackOff) — it cannot probe SQL replication, detect async standbys, or verify that gRPC health endpoints are actually serving.
- **The operator** focuses on driving the desired state and reporting its own phase. It trusts component health endpoints at face value. When those endpoints lie (e.g., multipooler returning `/ready=200` while its gRPC server hangs), the operator cannot detect the failure.

The observer independently verifies what each component self-reports. It is the tool of last resort for finding bugs that hide in the gaps between `kubectl`, the operator, and upstream multigres.

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

The observer exposes structured JSON endpoints for diagnostics:

```bash
KUBECONFIG=kubeconfig.yaml kubectl port-forward svc/multigres-observer -n multigres-operator 9090:9090
```

**Latest cycle snapshot:**
```bash
curl -s http://localhost:9090/api/status | jq .
```

**Finding history** (persistent, transient, and flapping classification across cycles):
```bash
curl -s http://localhost:9090/api/history | jq .
```

**On-demand targeted check** (immediate, does not wait for next cycle):
```bash
curl -s 'http://localhost:9090/api/check?categories=pod-health,connectivity' | jq .
```

See [Architecture](docs/architecture.md) for the full JSON schemas and endpoint details.

### Prometheus Metrics

Metrics are exposed at `:9090/metrics` inside the pod, integrating with the existing observability stack:

```bash
curl http://localhost:9090/metrics
```

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                 multigres-observer                  │
│                                                     │
│  ┌──────────┐   ┌──────────┐   ┌──────────────────┐ │
│  │ Observer │ → │ Reporter │ → │ JSON stdout      │ │
│  │ (checks) │   │          │   │ Prometheus :9090 │ │
│  └──────────┘   └──────────┘   └──────────────────┘ │
│       │                                             │
│  ┌────┴───────────────────────────────────────────┐ │
│  │ Checks:                                        │ │
│  │  pod-health · resource-validation · crd-status │ │
│  │  drain-state · connectivity · replication      │ │
│  │  operator-logs · dataplane-logs · events       │ │
│  │  topology                                      │ │
│  └────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────┘
         ↕ Kubernetes API              ↕ Direct SQL
   CRDs, Pods, Events, Logs      PostgreSQL on pool pods
```

The observer is **mostly stateless** — no PVC, no persistent storage. Every cycle re-evaluates from scratch. In-memory trackers (restart counts, phase timestamps, drain state, finding history) reset when the pod restarts.

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

## Startup Grace Period

Newly created pool pods go through a brief startup phase where they are not yet serving (Postgres is starting, WAL receiver hasn't connected, standby is initially async). The observer suppresses transient noise from these pods to avoid cluttering findings with expected startup behavior:

| Pod age | Ready? | Behavior |
|---------|--------|----------|
| < 60s | any | **Suppressed** — all findings for this pod are skipped |
| ≥ 60s | no | **Downgraded** — `error`/`fatal` findings become `warn` |
| ≥ 60s | yes | **Full severity** — this is the high-value signal |

The grace period only applies to pool pods (`component=shard-pool`). Operator, multiorch, multigateway, and toposerver findings are always reported at full severity.

When a pod that Kubernetes marks as Ready is showing errors, that is exactly the kind of issue the observer exists to catch — what `kubectl` and the operator cannot see.

## Documentation

| Document | Description |
|----------|-------------|
| [Observer Reference](docs/observer.md) | Full reference for all 10 check categories with thresholds |
| [Configuration Reference](docs/configuration.md) | Flags, environment variables, thresholds, RBAC |
| [Architecture](docs/architecture.md) | Internal design, report format, metrics, deployment model |
| [Skills](skills/README.md) | AI agent skills for observer-assisted diagnostics |

## Project Structure

```
tools/observer/
├── cmd/multigres-observer/
│   └── main.go                    # Entrypoint, flags, metrics server
├── pkg/
│   ├── observer/                  # 10 check categories + API
│   │   ├── observer.go            # Main loop, cycle orchestration, HTTP handlers
│   │   ├── history.go             # Finding history ring buffer + classification
│   │   ├── history_test.go        # Unit tests for history
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
├── skills/                          # AI agent skills
│   └── diagnose_with_observer/    # Diagnostic triage and root cause analysis
├── deploy/
│   └── base/                      # Kustomize manifests (read-only RBAC)
├── Dockerfile
└── go.mod                         # Separate Go module
```

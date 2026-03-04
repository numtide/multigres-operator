# Observability

The operator ships with built-in support for **metrics**, **alerting**, **distributed tracing**, and **structured logging**.

## Metrics

Metrics are served via the standard controller-runtime Prometheus endpoint on `:8443` over HTTPS by default (with authentication and authorization via `--metrics-secure`). To expose an insecure HTTP endpoint for local development, set `--metrics-bind-address=:8080 --metrics-secure=false`.

The operator exposes two classes of metrics:

**Framework Metrics** (provided automatically by controller-runtime):
- `controller_runtime_reconcile_total` — total reconcile count per controller
- `controller_runtime_reconcile_errors_total` — reconcile error rate
- `controller_runtime_reconcile_time_seconds` — reconcile latency histogram
- `workqueue_depth` — work queue backlog

**Operator-Specific Metrics**:

| Metric | Type | Labels | Description |
|:---|:---|:---|:---|
| `multigres_operator_cluster_info` | Gauge | `name`, `namespace`, `phase` | Cluster phase tracking (always 1) |
| `multigres_operator_cluster_cells_total` | Gauge | `cluster`, `namespace` | Cell count |
| `multigres_operator_cluster_shards_total` | Gauge | `cluster`, `namespace` | Shard count |
| `multigres_operator_cell_gateway_replicas` | Gauge | `cell`, `namespace`, `state` | Gateway ready/desired replicas |
| `multigres_operator_shard_pool_replicas` | Gauge | `shard`, `pool`, `namespace`, `state` | Pool ready/desired replicas |
| `multigres_operator_toposerver_replicas` | Gauge | `name`, `namespace`, `state` | TopoServer ready/desired replicas |
| `multigres_operator_webhook_request_total` | Counter | `operation`, `resource`, `result` | Webhook admission request count |
| `multigres_operator_webhook_request_duration_seconds` | Histogram | `operation`, `resource` | Webhook latency |

## Alerts

Pre-configured PrometheusRule alerts are provided in `config/monitoring/prometheus-rules.yaml`. Apply them to a Prometheus Operator installation:

```bash
kubectl apply -f config/monitoring/prometheus-rules.yaml
```

| Alert | Severity | Fires When |
|:---|:---:|:---|
| [`MultigresClusterReconcileErrors`](monitoring/runbooks/MultigresClusterReconcileErrors.md) | warning | Sustained non-zero reconcile error rate (5m) |
| [`MultigresClusterDegraded`](monitoring/runbooks/MultigresClusterDegraded.md) | warning | Cluster phase ≠ "Healthy" for >10m |
| [`MultigresCellGatewayUnavailable`](monitoring/runbooks/MultigresCellGatewayUnavailable.md) | critical | Zero ready gateway replicas in a cell (5m) |
| [`MultigresShardPoolDegraded`](monitoring/runbooks/MultigresShardPoolDegraded.md) | warning | Ready < desired replicas for >10m |
| [`MultigresWebhookErrors`](monitoring/runbooks/MultigresWebhookErrors.md) | warning | Webhook returning errors (5m) |
| [`MultigresReconcileSlow`](monitoring/runbooks/MultigresReconcileSlow.md) | warning | p99 reconcile latency >30s (5m) |
| [`MultigresControllerSaturated`](monitoring/runbooks/MultigresControllerSaturated.md) | warning | Work queue depth >50 for >10m |

Each alert links to a dedicated runbook with investigation steps, PromQL queries, and remediation actions.

## Grafana Dashboards

Two Grafana dashboards are included in `config/monitoring/`:

- **Operator Dashboard** (`grafana-dashboard-operator.json`) — reconcile rates, error rates, latencies, queue depth, and webhook performance.
- **Cluster Dashboard** (`grafana-dashboard-cluster.json`) — per-cluster topology (cells, shards), replica health, and phase tracking.

Import via the Grafana dashboards ConfigMap:
```bash
kubectl apply -f config/monitoring/grafana-dashboards.yaml
```

## Local Development

For local development, the observability overlay in `config/deploy-observability/` deploys the OTel Collector, Prometheus (via the Prometheus Operator), Tempo, and Grafana as separate pods. Both dashboards and datasources are pre-provisioned.

```bash
make kind-deploy-observability
make kind-portforward
```

This deploys the operator with tracing enabled and opens port-forwards to:

| Service | URL |
| :--- | :--- |
| Grafana | [http://localhost:3000](http://localhost:3000) |
| Prometheus | [http://localhost:9090](http://localhost:9090) |
| Tempo | [http://localhost:3200](http://localhost:3200) |

**Metrics collection:** The operator and data-plane components use different metric collection models:

| Component | Metric Model | How it works |
| :--- | :--- | :--- |
| **Operator** | **Pull** (Prometheus scrape) | Prometheus scrapes the operator's `/metrics` endpoint via controller-runtime's built-in Prometheus integration |
| **Data plane** (multiorch, multipooler, etc.) | **Push** (OTLP) | Multigres binaries push metrics via OpenTelemetry to the configured OTLP endpoint |

The OTel Collector receives all pushed OTLP signals from the data plane and routes them: **traces → Tempo**, **metrics → Prometheus** (via its OTLP receiver). This is necessary because multigres components send all signals to a single OTLP endpoint and cannot split them by signal type.

## Distributed Tracing

The operator supports **OpenTelemetry distributed tracing** via OTLP. Tracing is **disabled by default** and incurs zero overhead when off.

**Enabling tracing:** Set a single environment variable on the operator Deployment:

```yaml
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "http://otel-collector.monitoring.svc:4318"  # OTel Collector or Tempo
```

The endpoint must speak **OTLP** (HTTP or gRPC) — this can be an OpenTelemetry Collector, Grafana Tempo, Jaeger, or any compatible backend.

**What gets traced:**
- Every controller reconciliation (MultigresCluster, Cell, Shard, TableGroup, TopoServer)
- Sub-operations within a reconcile (ReconcileCells, UpdateStatus, PopulateDefaults, etc.)
- Webhook admission handling (defaulting and validation)
- Webhook-to-reconcile trace propagation: the defaulter webhook injects a trace context into cluster annotations so the first reconciliation appears as a child span of the webhook trace

**Additional OTel configuration:** The operator respects all standard [OTel environment variables](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/) including `OTEL_TRACES_SAMPLER`, `OTEL_EXPORTER_OTLP_INSECURE`, and `OTEL_SERVICE_NAME`.

## Structured Logging

The operator uses structured JSON logging (`zap` via controller-runtime). When tracing is enabled, every log line within a traced operation automatically includes `trace_id` and `span_id` fields, enabling **log-trace correlation** — click a log line in Grafana Loki to jump directly to the associated trace.

**Log level configuration:** The operator accepts standard controller-runtime zap flags on its command line:

| Flag | Default | Description |
| :--- | :--- | :--- |
| `--zap-devel` | `true` | Development mode preset (see table below) |
| `--zap-log-level` | depends on mode | Log verbosity: `debug`, `info`, `error`, or an integer (0=debug, 1=info, 2=error) |
| `--zap-encoder` | depends on mode | Log format: `console` or `json` |
| `--zap-stacktrace-level` | depends on mode | Minimum level that triggers stacktraces |

`--zap-devel` is a **mode** that sets multiple defaults at once. `--zap-log-level` overrides the mode's default level when specified explicitly:

| Setting | `--zap-devel=true` (default) | `--zap-devel=false` (production) |
| :--- | :--- | :--- |
| Default log level | `debug` | `info` |
| Encoder | `console` (human-readable) | `json` |
| Stacktraces from | `warn` | `error` |

To change the log level in a deployed operator, add `args` to the manager container:

```yaml
spec:
  template:
    spec:
      containers:
        - name: manager
          args:
            - --zap-devel=false       # Production mode (JSON, info level default)
            - --zap-log-level=info    # Explicit level (overrides mode default)
```

> [!NOTE]
> The default build ships with `Development: true`, which sets the default level to `debug` and uses the human-readable console encoder. For production deployments, set `--zap-devel=false` to switch to JSON encoding and `info`-level logging.

For a hands-on tutorial of the full observability stack, see the [Observability Demo](../demo/observability/demo.md).

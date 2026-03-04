# Observability Stack Demo

This demo deploys the Multigres Operator alongside a full observability stack for metrics, distributed tracing, and dashboards.

## What Gets Deployed

| Component | Purpose |
|:---|:---|
| **Prometheus** (via Prometheus Operator) | Scrapes operator metrics, stores time-series data |
| **OpenTelemetry Collector** | Receives OTLP signals from multigres data-plane, routes traces → Tempo and metrics → Prometheus |
| **Tempo** | Distributed trace storage and querying |
| **Grafana** | Pre-provisioned dashboards and datasources (Prometheus + Tempo) |

> [!NOTE]
> These are single-replica deployments with sane defaults intended for **evaluation and development**. They do not include HA, persistent storage, or authentication. For production observability, integrate the operator's metrics and traces with your existing monitoring infrastructure.

## Prerequisites

- A running Kubernetes cluster (or Kind for local testing)
- `kubectl` installed

## Steps

### 1. Install the Prometheus Operator

```bash
kubectl apply --server-side -f \
  https://github.com/prometheus-operator/prometheus-operator/releases/download/v0.80.0/bundle.yaml
kubectl wait --for=condition=Available deployment/prometheus-operator -n default --timeout=120s
```

### 2. Install the operator with the observability overlay

```bash
kubectl apply --server-side -f \
  https://github.com/numtide/multigres-operator/releases/latest/download/install-observability.yaml
```

### 3. Access the dashboards

Port-forward the observability services:

| Service | URL |
| :--- | :--- |
| Grafana | [http://localhost:3000](http://localhost:3000) |
| Prometheus | [http://localhost:9090](http://localhost:9090) |
| Tempo | [http://localhost:3200](http://localhost:3200) |

### Local Development (Kind)

For local testing with Kind, use the convenience targets:

```bash
make kind-deploy-observability
make kind-portforward
```

This deploys the operator with tracing enabled and opens port-forwards to all services above.

## Hands-on Tutorial

For a detailed walkthrough including Grafana exploration, trace analysis, and alert verification, see the [full observability demo](demo.md).

## Further Reading

- [Observability Guide](../../docs/observability.md) — metrics, tracing, alerts, and logging reference
- [Configuration Reference](../../docs/configuration.md) — operator flags including `OTEL_EXPORTER_OTLP_ENDPOINT`

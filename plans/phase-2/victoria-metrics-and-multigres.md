---
title: VictoriaMetrics and OpenTelemetry Integration for Operator and Component Observability
state: draft
tags: [observability, metrics, opentelemetry, production-readiness]
---

# Summary

Integrate VictoriaMetrics Operator (v0.48.3) and OpenTelemetry to provide comprehensive observability for both the multigres-operator itself and all deployed Multigres components (etcd, MultiGateway, MultiOrch, MultiPooler). This enables production-grade metrics collection with automated service discovery, OpenTelemetry-native instrumentation, centralized metrics storage, and standardized observability.

# Motivation

Production deployments require comprehensive observability to:
- Monitor operator health and reconciliation performance
- Track component availability and resource utilization
- Detect and diagnose issues before they impact users
- Enable capacity planning and performance optimization
- Provide SLA compliance evidence
- Standardize on OpenTelemetry for vendor-neutral observability

Currently, the operator exposes controller-runtime metrics but lacks automated collection, long-term storage, and OpenTelemetry integration. Deployed components (etcd, Multigres services) may expose metrics but are not automatically discovered or scraped in a standardized way.

## Goals

- **OpenTelemetry Integration**: Instrument operator and components with OTel SDK for metrics export
- **Operator Metrics**: Collect controller-runtime and custom OTel metrics from the operator itself (reconciliation duration, error rates, queue depth)
- **Component Metrics**: Automatically discover and scrape metrics from all deployed components
- **Zero Configuration**: VMServiceScrape resources created automatically when components are deployed
- **Production Ready**: Metrics retention, high availability, and efficient storage with VictoriaMetrics
- **Standardized Format**: OpenTelemetry Protocol (OTLP) as primary export format with Prometheus fallback
- **Alerts Foundation**: Expose metrics that enable alerting on operator and component issues

## Non-Goals

- **Distributed Tracing**: Focus on metrics only; traces are separate future work (though OTel SDK enables future trace addition)
- **Structured Logging via OTel**: Traditional structured logging is sufficient; OTel logs are future enhancement
- **Custom Application Metrics**: Only expose existing metrics from controller-runtime and components; application-specific business metrics are out of scope
- **Multi-Cluster**: Single cluster metrics only; cross-cluster aggregation is future work
- **Visualization Dashboards**: Metrics storage and collection only; users bring their own visualization tools

# Proposal

Deploy VictoriaMetrics Operator alongside the multigres-operator with OpenTelemetry integration. The operator will be instrumented with OTel SDK to export metrics in OTLP format, while also exposing Prometheus-format endpoints for compatibility. VMServiceScrape custom resources will be created automatically for each deployed component, enabling zero-configuration observability.

## Architecture Overview

```
┌──────────────────────────────────────────────────────────┐
│              VictoriaMetrics Stack                       │
│  ┌──────────────┐  ┌──────────────┐                     │
│  │   VMAgent    │→ │ VMCluster/   │                     │
│  │  (scraper)   │  │ VMSingle     │                     │
│  └──────────────┘  └──────────────┘                     │
│         ↑                                                 │
│         │ scrapes Prometheus /metrics                    │
│         │ OR receives OTLP push                          │
└─────────┼─────────────────────────────────────────────────┘
          │
          │ discovers via VMServiceScrape CRDs
          │
    ┌─────┴──────────────────────────────────────────────┐
    │              Multigres Resources                    │
    │                                                      │
    │  ┌────────────────────────────────────┐            │
    │  │  Operator (with OTel SDK)          │            │
    │  │  - OTLP exporter → VMAgent         │            │
    │  │  - Prometheus /metrics :8443       │            │
    │  │  - controller-runtime metrics      │            │
    │  │  - custom OTel metrics             │            │
    │  └────────────────────────────────────┘            │
    │                                                      │
    │  ┌────────┐  ┌──────────┐  ┌─────────┐            │
    │  │  etcd  │  │ Gateway  │  │  Orch   │            │
    │  │ :2379  │  │  :8080   │  │  :8080  │            │
    │  │/metrics│  │ /metrics │  │/metrics │            │
    │  └────────┘  └──────────┘  └─────────┘            │
    │                                                      │
    │  ┌─────────────┐                                    │
    │  │ MultiPooler │                                    │
    │  │   :9187     │                                    │
    │  │  /metrics   │                                    │
    │  └─────────────┘                                    │
    └──────────────────────────────────────────────────────┘
```

## OpenTelemetry Integration Strategy

### Operator Instrumentation

Instrument the multigres-operator with OpenTelemetry SDK:

1. **Metrics Export**: Use OTel Metrics SDK to export both controller-runtime metrics and custom metrics
2. **Export Format**: Primary OTLP (gRPC or HTTP), secondary Prometheus endpoint for compatibility
3. **Metrics Bridge**: Bridge controller-runtime Prometheus metrics to OTel format
4. **Resource Attributes**: Include operator version, cluster ID, namespace in all metrics

### Component Metrics Strategy

Components expose metrics in their native format:
- **etcd**: Native Prometheus endpoint at `:2379/metrics`
- **MultiGateway/Orch/Pooler**: Prometheus or OTLP endpoints (depending on Multigres implementation)
- **Discovery**: VMServiceScrape automatically discovers all endpoints
- **Future**: Migrate components to OTel SDK as Multigres upstream adopts it

## Metrics Endpoints

### Multigres Operator (with OpenTelemetry)

**Primary: OTLP Export**
- **Endpoint**: VMAgent with OTLP receiver enabled
- **Protocol**: OTLP/gRPC or OTLP/HTTP
- **Configuration**:
```go
import (
    "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
    "go.opentelemetry.io/otel/sdk/metric"
)

exporter, _ := otlpmetricgrpc.New(ctx,
    otlpmetricgrpc.WithEndpoint("vmagent.monitoring.svc:8428"),
    otlpmetricgrpc.WithInsecure(),
)
meterProvider := metric.NewMeterProvider(
    metric.WithReader(metric.NewPeriodicReader(exporter)),
)
```

**Secondary: Prometheus Endpoint** (for compatibility)
- **Endpoint**: `https://<pod-ip>:8443/metrics`
- **Metrics Provided**:
  - `controller_runtime_reconcile_total`: Total reconciliations per controller
  - `controller_runtime_reconcile_errors_total`: Reconciliation errors
  - `controller_runtime_reconcile_time_seconds`: Reconciliation duration histogram
  - `workqueue_depth`: Current queue depth per controller
  - `workqueue_adds_total`: Total items added to work queue
  - `rest_client_requests_total`: Kubernetes API client requests
  - Standard Go runtime metrics (goroutines, memory, GC)

### etcd Component
- **Endpoint**: `http://<pod-ip>:2379/metrics`
- **Format**: Prometheus
- **Metrics Provided**:
  - `etcd_server_has_leader`: Leader existence (0 or 1)
  - `etcd_server_leader_changes_seen_total`: Leadership changes
  - `etcd_server_proposals_committed_total`: Consensus proposals
  - `etcd_disk_wal_fsync_duration_seconds`: WAL fsync latency
  - `etcd_network_peer_sent_bytes_total`: Network traffic
  - `process_resident_memory_bytes`: Memory usage
  - `process_cpu_seconds_total`: CPU usage

### MultiGateway Component
- **Endpoint**: `http://<pod-ip>:8080/metrics` (assumed standard port)
- **Format**: Prometheus or OTLP (TBD - verify with Multigres upstream)
- **Metrics Provided**: (TBD - need to verify with Multigres upstream)
  - PostgreSQL protocol metrics (connections, queries)
  - Request latency histograms
  - Error rates and types

### MultiOrch Component
- **Endpoint**: `http://<pod-ip>:8080/metrics` (assumed standard port)
- **Format**: Prometheus or OTLP (TBD - verify with Multigres upstream)
- **Metrics Provided**: (TBD - need to verify with Multigres upstream)
  - Orchestration decisions
  - Cluster topology state
  - Coordination metrics

### MultiPooler Component
- **Endpoint**: `http://<pod-ip>:9187/metrics` (pgbouncer_exporter standard port)
- **Format**: Prometheus
- **Metrics Provided**: (assuming pgbouncer-style pooler)
  - `pgbouncer_pools_server_active_connections`: Active connections per pool
  - `pgbouncer_pools_server_idle_connections`: Idle connections
  - `pgbouncer_pools_client_active_connections`: Client connections
  - `pgbouncer_stats_queries_total`: Total queries processed
  - `pgbouncer_stats_bytes_received_total`: Network traffic

# Design Details

## OpenTelemetry SDK Integration

### Dependencies

Add OpenTelemetry SDK to operator:

```bash
cd cmd/multigres-operator
go get go.opentelemetry.io/otel@latest
go get go.opentelemetry.io/otel/sdk/metric@latest
go get go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc@latest
go get go.opentelemetry.io/contrib/instrumentation/runtime@latest
```

### Operator Initialization

Update `cmd/multigres-operator/main.go`:

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
    sdkmetric "go.opentelemetry.io/otel/sdk/metric"
    "go.opentelemetry.io/otel/sdk/resource"
    semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
    "go.opentelemetry.io/contrib/instrumentation/runtime"
)

func initOTel(ctx context.Context) (*sdkmetric.MeterProvider, error) {
    // Resource with operator identity
    res, err := resource.New(ctx,
        resource.WithAttributes(
            semconv.ServiceName("multigres-operator"),
            semconv.ServiceVersion(version), // from build flags
        ),
    )
    if err != nil {
        return nil, err
    }

    // OTLP exporter (VictoriaMetrics supports OTLP ingestion)
    otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
    if otlpEndpoint == "" {
        otlpEndpoint = "vmagent.monitoring.svc:4317" // default
    }

    exporter, err := otlpmetricgrpc.New(ctx,
        otlpmetricgrpc.WithEndpoint(otlpEndpoint),
        otlpmetricgrpc.WithInsecure(), // use TLS in production
    )
    if err != nil {
        return nil, err
    }

    // Meter provider with periodic export
    meterProvider := sdkmetric.NewMeterProvider(
        sdkmetric.WithResource(res),
        sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter,
            sdkmetric.WithInterval(30*time.Second),
        )),
    )

    otel.SetMeterProvider(meterProvider)

    // Start runtime metrics collection
    if err := runtime.Start(runtime.WithMinimumReadMemStatsInterval(time.Second)); err != nil {
        return nil, err
    }

    return meterProvider, nil
}

func main() {
    // ... existing setup ...

    // Initialize OpenTelemetry
    meterProvider, err := initOTel(context.Background())
    if err != nil {
        setupLog.Error(err, "failed to initialize OpenTelemetry")
        os.Exit(1)
    }
    defer func() {
        if err := meterProvider.Shutdown(context.Background()); err != nil {
            setupLog.Error(err, "failed to shutdown meter provider")
        }
    }()

    // ... rest of main ...
}
```

### Custom Metrics Example

Add custom OTel metrics in reconcilers:

```go
package etcd

import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/metric"
)

type EtcdReconciler struct {
    // ... existing fields ...

    // OTel metrics
    reconcileCounter metric.Int64Counter
    reconcileDuration metric.Float64Histogram
}

func (r *EtcdReconciler) SetupWithManager(mgr ctrl.Manager) error {
    meter := otel.Meter("multigres.io/etcd-controller")

    var err error
    r.reconcileCounter, err = meter.Int64Counter(
        "etcd.reconcile.count",
        metric.WithDescription("Number of etcd reconciliations"),
    )
    if err != nil {
        return err
    }

    r.reconcileDuration, err = meter.Float64Histogram(
        "etcd.reconcile.duration",
        metric.WithDescription("Duration of etcd reconciliations in seconds"),
        metric.WithUnit("s"),
    )
    if err != nil {
        return err
    }

    return ctrl.NewControllerManagedBy(mgr).
        For(&multigresv1alpha1.Etcd{}).
        Complete(r)
}

func (r *EtcdReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    start := time.Now()
    defer func() {
        duration := time.Since(start).Seconds()
        r.reconcileDuration.Record(ctx, duration)
    }()

    // ... reconciliation logic ...

    r.reconcileCounter.Add(ctx, 1)
    return ctrl.Result{}, nil
}
```

## VMServiceScrape Resource Creation

The multigres-operator will create VMServiceScrape resources as part of reconciliation, similar to how it creates Services and Deployments.

### Implementation Approach

Add VMServiceScrape creation to each component reconciler in `pkg/resource-handler/controller/{component}/`:

```go
// In each reconciler, after creating Service:

import victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/victoriametrics/v1beta1"

func (r *EtcdReconciler) reconcileVMServiceScrape(ctx context.Context, etcd *multigresv1alpha1.Etcd) error {
    vmServiceScrape := &victoriametricsv1beta1.VMServiceScrape{
        ObjectMeta: metav1.ObjectMeta{
            Name:      etcd.Name + "-metrics",
            Namespace: etcd.Namespace,
        },
        Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
            Selector: metav1.LabelSelector{
                MatchLabels: map[string]string{
                    "app.kubernetes.io/name":      "etcd",
                    "app.kubernetes.io/instance":  etcd.Name,
                    "app.kubernetes.io/component": "etcd",
                },
            },
            Endpoints: []victoriametricsv1beta1.Endpoint{
                {
                    Port:     "client", // Port 2379
                    Path:     "/metrics",
                    Interval: "30s",
                },
            },
        },
    }

    // Set owner reference for automatic cleanup
    if err := ctrl.SetControllerReference(etcd, vmServiceScrape, r.Scheme); err != nil {
        return err
    }

    // Create or update
    return r.createOrUpdate(ctx, vmServiceScrape)
}
```

### Operator Self-Monitoring

Create a VMServiceScrape for the operator itself in the deployment manifests:

```yaml
apiVersion: victoriametrics.com/v1beta1
kind: VMServiceScrape
metadata:
  name: multigres-operator-metrics
  namespace: multigres-system
spec:
  selector:
    matchLabels:
      control-plane: controller-manager
  endpoints:
  - port: https
    scheme: https
    path: /metrics
    interval: 30s
    tlsConfig:
      insecureSkipVerify: true  # Using self-signed certs by default
      # In production, use cert-manager:
      # ca:
      #   secret:
      #     name: multigres-operator-metrics-cert
      #     key: ca.crt
```

### Component VMServiceScrape Templates

**etcd:**
```yaml
apiVersion: victoriametrics.com/v1beta1
kind: VMServiceScrape
metadata:
  name: {{ .Name }}-etcd-metrics
  namespace: {{ .Namespace }}
  ownerReferences:
  - apiVersion: multigres.io/v1alpha1
    kind: Etcd
    name: {{ .Name }}
    uid: {{ .UID }}
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: etcd
      app.kubernetes.io/instance: {{ .Name }}
      app.kubernetes.io/component: etcd
  endpoints:
  - port: client
    path: /metrics
    interval: 30s
```

**MultiGateway:**
```yaml
apiVersion: victoriametrics.com/v1beta1
kind: VMServiceScrape
metadata:
  name: {{ .Name }}-gateway-metrics
  namespace: {{ .Namespace }}
  ownerReferences:
  - apiVersion: multigres.io/v1alpha1
    kind: MultiGateway
    name: {{ .Name }}
    uid: {{ .UID }}
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: multigateway
      app.kubernetes.io/instance: {{ .Name }}
      app.kubernetes.io/component: gateway
  endpoints:
  - port: http  # Assuming 8080
    path: /metrics
    interval: 30s
```

**MultiOrch:**
```yaml
apiVersion: victoriametrics.com/v1beta1
kind: VMServiceScrape
metadata:
  name: {{ .Name }}-orch-metrics
  namespace: {{ .Namespace }}
  ownerReferences:
  - apiVersion: multigres.io/v1alpha1
    kind: MultiOrch
    name: {{ .Name }}
    uid: {{ .UID }}
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: multiorch
      app.kubernetes.io/instance: {{ .Name }}
      app.kubernetes.io/component: orch
  endpoints:
  - port: http
    path: /metrics
    interval: 30s
```

**MultiPooler:**
```yaml
apiVersion: victoriametrics.com/v1beta1
kind: VMServiceScrape
metadata:
  name: {{ .Name }}-pooler-metrics
  namespace: {{ .Namespace }}
  ownerReferences:
  - apiVersion: multigres.io/v1alpha1
    kind: MultiPooler
    name: {{ .Name }}
    uid: {{ .UID }}
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: multipooler
      app.kubernetes.io/instance: {{ .Name }}
      app.kubernetes.io/component: pooler
  endpoints:
  - port: metrics  # Assuming dedicated metrics port 9187
    path: /metrics
    interval: 30s
```

## VictoriaMetrics Deployment Options

Document two deployment patterns with OTLP support:

### Option 1: VMSingle with OTLP (Simpler, for development/small deployments)
```bash
# Install VictoriaMetrics Operator
kubectl apply -f https://github.com/VictoriaMetrics/operator/releases/download/v0.48.3/install.yaml

# Deploy VMSingle + VMAgent with OTLP
kubectl apply -f - <<EOF
apiVersion: victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: vmsingle
  namespace: monitoring
spec:
  retentionPeriod: "14d"
  storage:
    volumeClaimTemplate:
      spec:
        accessModes: [ReadWriteOnce]
        resources:
          requests:
            storage: 50Gi
---
apiVersion: victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: vmagent
  namespace: monitoring
spec:
  serviceScrapeNamespaceSelector: {}
  serviceScrapeSelector: {}
  remoteWrite:
  - url: http://vmsingle.monitoring.svc:8429/api/v1/write
  # Enable OTLP receiver
  extraArgs:
    promscrape.config: |
      global:
        external_labels:
          cluster: multigres-dev
  # OTLP ingestion endpoint
  ports:
  - name: otlp-grpc
    port: 4317
    targetPort: 4317
  - name: otlp-http
    port: 4318
    targetPort: 4318
EOF
```

### Option 2: VMCluster with OTLP (Production, for scalability)
```bash
# Deploy VMCluster + VMAgent
kubectl apply -f - <<EOF
apiVersion: victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: vmcluster
  namespace: monitoring
spec:
  retentionPeriod: "90d"
  replicationFactor: 2
  vmstorage:
    replicaCount: 3
    storage:
      volumeClaimTemplate:
        spec:
          accessModes: [ReadWriteOnce]
          resources:
            requests:
              storage: 200Gi
  vmselect:
    replicaCount: 2
  vminsert:
    replicaCount: 2
---
apiVersion: victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: vmagent
  namespace: monitoring
spec:
  serviceScrapeNamespaceSelector: {}
  serviceScrapeSelector: {}
  remoteWrite:
  - url: http://vminsert.monitoring.svc:8480/insert/0/prometheus/api/v1/write
  ports:
  - name: otlp-grpc
    port: 4317
    targetPort: 4317
  - name: otlp-http
    port: 4318
    targetPort: 4318
EOF
```

## RBAC Requirements

The multigres-operator needs permissions to create VMServiceScrape resources:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: multigres-operator-vm-integration
rules:
- apiGroups:
  - victoriametrics.com
  resources:
  - vmservicescrapes
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
```

## Dependency Management

Add VictoriaMetrics Operator API and OpenTelemetry to go.mod:

```bash
# VictoriaMetrics CRDs
cd pkg/resource-handler
go get github.com/VictoriaMetrics/operator/api@v0.48.3

# OpenTelemetry SDK
cd ../../cmd/multigres-operator
go get go.opentelemetry.io/otel@latest
go get go.opentelemetry.io/otel/sdk/metric@latest
go get go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc@latest
go get go.opentelemetry.io/contrib/instrumentation/runtime@latest
```

## Test Plan

### Unit Tests
- Test VMServiceScrape resource builder functions
- Verify correct labels, selectors, and endpoints
- Validate owner reference configuration
- Test reconciliation logic for VMServiceScrape creation/update
- Test OTel meter initialization and metric recording (mock exporter)

### Integration Tests
- Deploy VMSingle in envtest (if feasible) or kind cluster
- Create Etcd CR and verify VMServiceScrape is created
- Verify VMServiceScrape has correct owner reference (cleanup test)
- Test updating component triggers VMServiceScrape update
- Verify OTel metrics are exported (use in-memory exporter)

### Manual Testing
```bash
# 1. Deploy VictoriaMetrics stack with OTLP
kubectl apply -f config/samples/victoria-metrics/vmsingle-otlp.yaml

# 2. Deploy operator with OTel enabled
export OTEL_EXPORTER_OTLP_ENDPOINT=vmagent.monitoring.svc:4317
make deploy-kind

# 3. Create Multigres components
kubectl apply -f config/samples/multigres_v1alpha1_etcd.yaml

# 4. Verify VMServiceScrape created
kubectl get vmservicescrape -A

# 5. Check VMAgent discovers targets
kubectl port-forward -n monitoring svc/vmagent 8429:8429
curl http://localhost:8429/targets  # Should show etcd endpoints

# 6. Verify OTLP metrics received
kubectl port-forward -n monitoring svc/vmsingle 8428:8428
curl 'http://localhost:8428/api/v1/query?query=etcd_reconcile_count'

# 7. Query OTel metrics
curl 'http://localhost:8428/api/v1/query?query=etcd_reconcile_duration_bucket'
```

### E2E Testing
- Full stack deployment (VMCluster + operator + components)
- Verify both OTLP and Prometheus metrics collection
- Test component scaling (VMServiceScrape should discover new pods)
- Test component deletion (VMServiceScrape cleanup via owner reference)
- Verify OTel resource attributes are present in metrics

## Implementation Phases

### MVP (Minimum Viable Product)
- [ ] OpenTelemetry SDK integrated in operator
- [ ] OTel metrics exported via OTLP to VMAgent
- [ ] VMServiceScrape creation for etcd component
- [ ] Operator self-monitoring VMServiceScrape in manifests
- [ ] Documentation for VMSingle deployment with OTLP
- [ ] Basic integration tests with OTel

### Complete Implementation
- [ ] VMServiceScrape for all components (Gateway, Orch, Pooler)
- [ ] Custom OTel metrics in all reconcilers
- [ ] Production deployment guide with VMCluster and OTLP
- [ ] Comprehensive E2E tests with OTel validation
- [ ] Metrics endpoint documentation for all components
- [ ] OTel semantic conventions compliance

### Future Enhancements
- [ ] Battle-tested in production deployments
- [ ] Performance benchmarks (metrics overhead < 5% CPU/memory)
- [ ] Alert rule templates based on OTel metrics
- [ ] Multi-tenant metrics isolation (if needed)
- [ ] OpenTelemetry Collector integration guide (alternative to direct OTLP)

## Upgrade / Downgrade Strategy

**Enabling Metrics Collection:**
1. Install VictoriaMetrics Operator (cluster-wide, one-time)
2. Deploy VMAgent with OTLP receiver enabled
3. Upgrade multigres-operator to version with OTel and VM integration
4. Set `OTEL_EXPORTER_OTLP_ENDPOINT` environment variable in operator deployment
5. Existing components: VMServiceScrape created on next reconciliation
6. New components: VMServiceScrape created automatically

**Disabling Metrics Collection:**
1. VMServiceScrape resources have owner references - deleted automatically with component
2. To stop OTLP export: unset `OTEL_EXPORTER_OTLP_ENDPOINT` (operator falls back to Prometheus only)
3. To stop all collection: delete VMAgent
4. Historical metrics retained in VictoriaMetrics storage

**Downgrading Operator:**
- Older operator versions ignore VMServiceScrape resources (no-op)
- Older operator versions without OTel SDK won't export OTLP (Prometheus endpoint still works)
- VMServiceScrapes become orphaned but harmless
- Manually clean up: `kubectl delete vmservicescrape -l app.kubernetes.io/managed-by=multigres-operator`

## Version Skew Strategy

**VictoriaMetrics Operator Versions:**
- multigres-operator targets VM Operator v0.48.3 API (v1beta1)
- Backward compatible with VM Operator v0.40.0+
- Test against latest VM Operator in CI

**OpenTelemetry Version Strategy:**
- Use stable OTel SDK (v1.x)
- Pin to specific OTel version for reproducibility
- Update OTel dependencies quarterly (or for security fixes)

**Component Version Skew:**
- VMServiceScrape uses label selectors - works regardless of component version
- Metrics endpoints assumed stable (etcd /metrics is stable since v3.0)
- If component changes metrics port: update VMServiceScrape in reconciler code
- OTLP export independent of component versions

# Implementation History

- 2025-10-16: Initial draft created with VictoriaMetrics and OpenTelemetry integration

# Drawbacks

**Why should we not do this?**

- **Additional Dependencies**: Requires VictoriaMetrics Operator cluster-wide + OpenTelemetry SDK
  - Mitigation: Operators are standard in production Kubernetes; VM Operator and OTel SDK are lightweight and stable

- **API Version Coupling**: Tied to VictoriaMetrics Operator API (v1beta1) and OTel SDK versions
  - Mitigation: Both APIs are stable; v1beta1 has been stable since 2021, OTel SDK is v1.x

- **Metrics Endpoint Assumptions**: Assumes components expose Prometheus-format /metrics or OTLP
  - Mitigation: Prometheus format is industry standard; validate assumptions with Multigres upstream

- **Complexity for Simple Deployments**: Overkill for single-node development
  - Mitigation: Metrics are optional; operator functions without VM stack or OTLP enabled

- **OTel SDK Learning Curve**: Team needs to learn OpenTelemetry patterns
  - Mitigation: OTel is industry standard; investment pays off for future traces/logs

# Alternatives

## Alternative 1: Prometheus Operator + ServiceMonitor

**Pros:**
- More widely adopted (CNCF project)
- Larger ecosystem and community
- Built-in alerting with AlertManager

**Cons:**
- Higher resource overhead (Prometheus stores all data in memory)
- Complex HA setup (Thanos or Cortex required for long-term storage)
- Slower query performance on large datasets
- No native OTLP support

**Decision:** VictoriaMetrics chosen for better resource efficiency, built-in clustering, and OTLP ingestion support.

## Alternative 2: OpenTelemetry Collector Only

**Pros:**
- Vendor-neutral, full OTLP support
- Single pipeline for metrics, traces, logs
- Flexible export to any backend

**Cons:**
- Still need metrics storage backend (VictoriaMetrics or Prometheus)
- More complex configuration (Collector + backend)
- Adds extra hop in metrics pipeline

**Decision:** Direct OTLP export to VictoriaMetrics is simpler; OTel Collector can be added later for advanced pipelines.

## Alternative 3: Manual Prometheus ConfigMap

**Pros:**
- No additional CRDs or operators required
- Simple and predictable

**Cons:**
- Manual configuration for each component
- No automatic service discovery
- No automatic cleanup when components deleted
- Doesn't scale to multi-tenant deployments
- No OTLP support

**Decision:** Operator-managed VMServiceScrape provides better automation; OTel provides modern instrumentation.

## Alternative 4: Prometheus Endpoint Only (No OTel)

**Pros:**
- Simpler implementation, no OTel SDK
- controller-runtime already exposes Prometheus endpoint

**Cons:**
- Misses opportunity to adopt industry standard (OTel)
- Harder to add traces/logs later (different instrumentation)
- No semantic conventions or resource attributes
- Less flexible metric naming and attributes

**Decision:** OTel is the future; adopt now for metrics to ease future trace/log integration.

# Infrastructure Needed

**Required:**
- VictoriaMetrics Operator v0.48.3 installed in cluster
- VMAgent deployment with OTLP receiver enabled
- VMSingle or VMCluster for metrics storage
- OpenTelemetry SDK dependencies in operator

**Optional:**
- OpenTelemetry Collector (for advanced metric pipelines)
- cert-manager for TLS certificate management (production)
- Persistent volumes for metrics storage

**Development/Testing:**
- kind cluster with sufficient resources
- Namespace: `monitoring` for VictoriaMetrics stack
- Storage: 10Gi for VMSingle (development), 50Gi+ (production)

**CI/CD:**
- VictoriaMetrics Operator installation in test environments
- E2E tests with OTLP and Prometheus metrics validation
- OTel SDK test dependencies

# History

- 2025-10-16: Created initial draft with VictoriaMetrics and OpenTelemetry integration

# References

**VictoriaMetrics:**
- Operator Documentation: https://docs.victoriametrics.com/operator/
- VMServiceScrape API: https://docs.victoriametrics.com/operator/resources/vmservicescrape/
- Release v0.48.3: https://github.com/VictoriaMetrics/operator/releases/tag/v0.48.3
- OTLP Ingestion: https://docs.victoriametrics.com/#how-to-send-data-from-opentelemetry-agent

**OpenTelemetry:**
- Go SDK: https://opentelemetry.io/docs/languages/go/
- Metrics API: https://opentelemetry.io/docs/specs/otel/metrics/api/
- Semantic Conventions: https://opentelemetry.io/docs/specs/semconv/

**Component Metrics:**
- etcd Metrics: https://etcd.io/docs/v3.5/metrics/
- Multigres Documentation: https://multigres.com/
- controller-runtime Metrics: https://book.kubebuilder.io/reference/metrics.html

**Related Planning:**
- Architecture: docs/architecture.md (OpenTelemetry integration planned)
- Implementation Guide: docs/implementation-guide.md (testing standards)


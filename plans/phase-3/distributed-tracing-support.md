---
title: Distributed tracing support
state: draft
tags: [observability, tracing, opentelemetry, production-readiness]
---

# Summary

Add distributed tracing support to the multigres-operator using OpenTelemetry's tracing SDK. Building on the existing OpenTelemetry metrics integration, this enhancement enables end-to-end request tracing across reconciliation loops, Kubernetes API calls, and component lifecycle operations. Traces will be exported via OTLP to backends like Tempo, Jaeger, or Zipkin for debugging complex reconciliation flows and performance analysis.

# Motivation

While metrics provide aggregate visibility into operator health, distributed tracing is essential for:

- **Debugging Complex Reconciliations**: Understand the sequence of operations and where time is spent during reconciliation
- **Identifying Bottlenecks**: Pinpoint slow Kubernetes API calls, resource creation delays, or component startup issues
- **Root Cause Analysis**: Trace errors through the entire reconciliation flow to find the origin
- **Performance Optimization**: Measure latency breakdown across different reconciliation phases
- **Cross-Component Correlation**: Link operator actions to component behavior when debugging issues
- **Production Troubleshooting**: Investigate intermittent failures or slow reconciliations in production environments

The operator already has OpenTelemetry SDK integrated for metrics (from VictoriaMetrics integration). Adding tracing leverages the same infrastructure and provides complementary observability.

## Goals

- **Reconciliation Tracing**: Instrument all reconciliation loops with spans (MultigresCluster, Etcd, MultiGateway, MultiOrch, MultiPooler, Cell)
- **Kubernetes API Tracing**: Trace client-go calls (Get, Create, Update, Delete, Patch)
- **Component Lifecycle Spans**: Track resource creation, status updates, and cleanup operations
- **Context Propagation**: Ensure trace context flows through all reconciliation functions
- **OTLP Export**: Export traces via OpenTelemetry Protocol to trace backends
- **Sampling Configuration**: Support configurable sampling rates to control trace volume
- **Error Correlation**: Link errors and logs to trace spans for unified debugging
- **Performance Overhead**: Keep tracing overhead under 5% CPU/memory impact

## Non-Goals

- **Component-Internal Tracing**: Not tracing inside Multigres components (etcd, gateway, etc.) - only operator behavior
- **Custom Trace Backends**: Only support OTLP-compatible backends, not proprietary formats
- **Trace Analysis Tools**: Focus on trace generation, not building analysis/visualization tools
- **Historical Trace Storage**: Trace retention and storage are backend responsibilities
- **Multi-Cluster Tracing**: Single cluster only; cross-cluster trace correlation is future work
- **User Request Tracing**: Not tracing user database queries through Multigres components

# Proposal

Extend the existing OpenTelemetry integration to include distributed tracing alongside metrics. Use the OpenTelemetry Tracing SDK to instrument reconciliation loops, Kubernetes API calls, and resource builders. Export traces via OTLP to a trace backend (Tempo, Jaeger, Zipkin) deployed in the cluster or externally.

## Architecture Overview

```
┌──────────────────────────────────────────────────────────┐
│              Trace Backend (Tempo/Jaeger)                 │
│  ┌──────────────┐                                        │
│  │ Trace Storage│ ← Query UI for trace visualization     │
│  │   & Query    │                                        │
│  └──────────────┘                                        │
│         ↑                                                 │
│         │ OTLP/gRPC or OTLP/HTTP                         │
└─────────┼─────────────────────────────────────────────────┘
          │
          │ Trace export
          │
    ┌─────┴──────────────────────────────────────────────┐
    │       Multigres Operator (with OTel Tracing)       │
    │                                                      │
    │  ┌─────────────────────────────────────────────┐  │
    │  │  TracerProvider (OTLP Exporter)             │  │
    │  │  - Batch span processor                      │  │
    │  │  - Sampling: parent-based + trace ID ratio   │  │
    │  │  - Resource attributes (service, version)    │  │
    │  └─────────────────────────────────────────────┘  │
    │                                                      │
    │  Reconciliation Flows (with spans):                 │
    │  ┌──────────────────────────────────────────────┐ │
    │  │  MultigresClusterReconciler                   │ │
    │  │  ├─ Reconcile span                            │ │
    │  │  │  ├─ Fetch CR span                          │ │
    │  │  │  ├─ Reconcile Etcd span                    │ │
    │  │  │  ├─ Reconcile Gateway span                 │ │
    │  │  │  ├─ Update Status span                     │ │
    │  │  │  └─ Context: trace_id, span_id, parent     │ │
    │  └──────────────────────────────────────────────┘ │
    │                                                      │
    │  Component Reconcilers (each with own spans):       │
    │  ┌─ EtcdReconciler                                 │
    │  ├─ MultiGatewayReconciler                          │
    │  ├─ MultiOrchReconciler                             │
    │  └─ MultiPoolerReconciler                           │
    └──────────────────────────────────────────────────────┘
```

## Span Hierarchy Example

```
Trace: MultigresCluster "my-db" reconciliation
  └─ Span: Reconcile(my-db)  [12.3s]
      ├─ Span: Get(MultigresCluster/my-db)  [50ms]
      ├─ Span: ReconcileEtcd(my-db-etcd)  [8.2s]
      │   ├─ Span: Get(Etcd/my-db-etcd)  [45ms]
      │   ├─ Span: BuildEtcdStatefulSet  [2ms]
      │   ├─ Span: CreateOrUpdate(StatefulSet/my-db-etcd)  [120ms]
      │   ├─ Span: CreateOrUpdate(Service/my-db-etcd-client)  [80ms]
      │   └─ Span: UpdateStatus(Etcd/my-db-etcd)  [60ms]
      ├─ Span: ReconcileGateway(my-db-gateway)  [2.1s]
      │   ├─ Span: Get(MultiGateway/my-db-gateway)  [40ms]
      │   ├─ Span: BuildGatewayDeployment  [1ms]
      │   ├─ Span: CreateOrUpdate(Deployment/my-db-gateway)  [100ms]
      │   └─ Span: UpdateStatus(MultiGateway/my-db-gateway)  [55ms]
      └─ Span: UpdateStatus(MultigresCluster/my-db)  [95ms]
```

# Design Details

## OpenTelemetry Tracing Integration

### Dependencies

The operator already has OpenTelemetry SDK for metrics. Add tracing exporter:

```bash
cd cmd/multigres-operator
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@latest
go get go.opentelemetry.io/otel/sdk/trace@latest
go get go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp@latest
```

### Tracer Provider Initialization

Update `cmd/multigres-operator/main.go` to initialize both metrics and tracing:

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/sdk/resource"
    semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

func initOTelTracing(ctx context.Context) (*sdktrace.TracerProvider, error) {
    // Resource with operator identity (shared with metrics)
    res, err := resource.New(ctx,
        resource.WithAttributes(
            semconv.ServiceName("multigres-operator"),
            semconv.ServiceVersion(version),
        ),
    )
    if err != nil {
        return nil, err
    }

    // OTLP trace exporter
    otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
    if otlpEndpoint == "" {
        otlpEndpoint = "tempo.monitoring.svc:4317" // default Tempo gRPC
    }

    exporter, err := otlptracegrpc.New(ctx,
        otlptracegrpc.WithEndpoint(otlpEndpoint),
        otlptracegrpc.WithInsecure(), // use TLS in production
    )
    if err != nil {
        return nil, err
    }

    // Sampling strategy
    samplingRate := 1.0 // 100% for development, lower in production
    if rate := os.Getenv("OTEL_TRACE_SAMPLING_RATE"); rate != "" {
        if parsed, err := strconv.ParseFloat(rate, 64); err == nil {
            samplingRate = parsed
        }
    }

    sampler := sdktrace.ParentBased(
        sdktrace.TraceIDRatioBased(samplingRate),
    )

    // Tracer provider with batch span processor
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithResource(res),
        sdktrace.WithSampler(sampler),
        sdktrace.WithBatcher(exporter,
            sdktrace.WithMaxExportBatchSize(512),
            sdktrace.WithBatchTimeout(5*time.Second),
        ),
    )

    otel.SetTracerProvider(tp)

    return tp, nil
}

func main() {
    // ... existing setup ...

    // Initialize OpenTelemetry tracing
    tracerProvider, err := initOTelTracing(context.Background())
    if err != nil {
        setupLog.Error(err, "failed to initialize OpenTelemetry tracing")
        os.Exit(1)
    }
    defer func() {
        if err := tracerProvider.Shutdown(context.Background()); err != nil {
            setupLog.Error(err, "failed to shutdown tracer provider")
        }
    }()

    // ... initialize metrics (already done in VictoriaMetrics integration) ...

    // ... rest of main ...
}
```

## Instrumenting Reconcilers

### Base Reconciler Pattern

Create a helper for consistent span creation across all reconcilers:

```go
// pkg/common/tracing/reconciler.go
package tracing

import (
    "context"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/codes"
    "go.opentelemetry.io/otel/trace"
)

// StartReconcileSpan creates a span for reconciliation with standard attributes
func StartReconcileSpan(ctx context.Context, tracerName, spanName string, namespace, name string) (context.Context, trace.Span) {
    tracer := otel.Tracer(tracerName)
    ctx, span := tracer.Start(ctx, spanName,
        trace.WithSpanKind(trace.SpanKindInternal),
        trace.WithAttributes(
            attribute.String("k8s.namespace", namespace),
            attribute.String("k8s.name", name),
        ),
    )
    return ctx, span
}

// RecordError records an error on the span with proper status
func RecordError(span trace.Span, err error, description string) {
    span.RecordError(err)
    span.SetStatus(codes.Error, description)
}

// RecordSuccess marks span as successful
func RecordSuccess(span trace.Span) {
    span.SetStatus(codes.Ok, "")
}
```

### Example: Etcd Reconciler

```go
// pkg/resource-handler/controller/etcd/etcd_controller.go
package etcd

import (
    "context"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    ctrl "sigs.k8s.io/controller-runtime"

    "github.com/numtide/multigres-operator/pkg/common/tracing"
)

const tracerName = "multigres.io/etcd-controller"

func (r *EtcdReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // Start top-level reconcile span
    ctx, span := tracing.StartReconcileSpan(ctx, tracerName, "Reconcile", req.Namespace, req.Name)
    defer span.End()

    log := log.FromContext(ctx)

    // Fetch CR with span
    instance := &multigresv1alpha1.Etcd{}
    ctx, fetchSpan := otel.Tracer(tracerName).Start(ctx, "Get(Etcd)")
    err := r.Get(ctx, req.NamespacedName, instance)
    fetchSpan.End()

    if err != nil {
        if apierrors.IsNotFound(err) {
            span.SetAttributes(attribute.Bool("cr.found", false))
            return ctrl.Result{}, nil
        }
        tracing.RecordError(span, err, "failed to get Etcd CR")
        return ctrl.Result{}, err
    }

    span.SetAttributes(
        attribute.String("cr.uid", string(instance.UID)),
        attribute.Int64("cr.generation", instance.Generation),
    )

    // Reconcile StatefulSet with span
    if err := r.reconcileStatefulSet(ctx, instance); err != nil {
        tracing.RecordError(span, err, "failed to reconcile StatefulSet")
        return ctrl.Result{}, err
    }

    // Reconcile Services with span
    if err := r.reconcileServices(ctx, instance); err != nil {
        tracing.RecordError(span, err, "failed to reconcile Services")
        return ctrl.Result{}, err
    }

    // Update status with span
    if err := r.updateStatus(ctx, instance); err != nil {
        tracing.RecordError(span, err, "failed to update status")
        return ctrl.Result{}, err
    }

    tracing.RecordSuccess(span)
    return ctrl.Result{}, nil
}

func (r *EtcdReconciler) reconcileStatefulSet(ctx context.Context, instance *multigresv1alpha1.Etcd) error {
    ctx, span := otel.Tracer(tracerName).Start(ctx, "ReconcileStatefulSet")
    defer span.End()

    // Build StatefulSet
    ctx, buildSpan := otel.Tracer(tracerName).Start(ctx, "BuildEtcdStatefulSet")
    sts := BuildEtcdStatefulSet(instance)
    buildSpan.SetAttributes(
        attribute.Int("sts.replicas", int(*sts.Spec.Replicas)),
        attribute.String("sts.image", sts.Spec.Template.Spec.Containers[0].Image),
    )
    buildSpan.End()

    // Set owner reference
    if err := ctrl.SetControllerReference(instance, sts, r.Scheme); err != nil {
        tracing.RecordError(span, err, "failed to set owner reference")
        return err
    }

    // Create or update
    ctx, applySpan := otel.Tracer(tracerName).Start(ctx, "CreateOrUpdate(StatefulSet)")
    err := r.createOrUpdate(ctx, sts)
    if err != nil {
        tracing.RecordError(applySpan, err, "StatefulSet apply failed")
        applySpan.End()
        tracing.RecordError(span, err, "failed to create or update StatefulSet")
        return err
    }
    applySpan.SetAttributes(attribute.String("apply.result", "success"))
    applySpan.End()

    tracing.RecordSuccess(span)
    return nil
}

func (r *EtcdReconciler) updateStatus(ctx context.Context, instance *multigresv1alpha1.Etcd) error {
    ctx, span := otel.Tracer(tracerName).Start(ctx, "UpdateStatus")
    defer span.End()

    // Get current StatefulSet
    sts := &appsv1.StatefulSet{}
    if err := r.Get(ctx, types.NamespacedName{Name: instance.Name + "-etcd", Namespace: instance.Namespace}, sts); err != nil {
        tracing.RecordError(span, err, "failed to get StatefulSet for status")
        return err
    }

    // Update status fields
    instance.Status.Replicas = sts.Status.Replicas
    instance.Status.ReadyReplicas = sts.Status.ReadyReplicas
    instance.Status.Ready = sts.Status.ReadyReplicas == *sts.Spec.Replicas

    span.SetAttributes(
        attribute.Int("status.replicas", int(instance.Status.Replicas)),
        attribute.Int("status.ready_replicas", int(instance.Status.ReadyReplicas)),
        attribute.Bool("status.ready", instance.Status.Ready),
    )

    if err := r.Status().Update(ctx, instance); err != nil {
        tracing.RecordError(span, err, "failed to update status")
        return err
    }

    tracing.RecordSuccess(span)
    return nil
}
```

### Cluster Handler Reconciler (Parallel Component Reconciliation)

```go
// pkg/cluster-handler/controller/multigrescluster/multigrescluster_controller.go
package multigrescluster

import (
    "context"
    "sync"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"

    "github.com/numtide/multigres-operator/pkg/common/tracing"
)

const tracerName = "multigres.io/multigrescluster-controller"

func (r *MultigresClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    ctx, span := tracing.StartReconcileSpan(ctx, tracerName, "Reconcile", req.Namespace, req.Name)
    defer span.End()

    // Fetch CR
    instance := &multigresv1alpha1.MultigresCluster{}
    ctx, fetchSpan := otel.Tracer(tracerName).Start(ctx, "Get(MultigresCluster)")
    err := r.Get(ctx, req.NamespacedName, instance)
    fetchSpan.End()

    if err != nil {
        // ... error handling ...
    }

    // Reconcile components in parallel with spans
    var wg sync.WaitGroup
    errors := make([]error, 4)

    // Each goroutine gets its own child span
    wg.Add(4)

    go func() {
        defer wg.Done()
        ctx, span := otel.Tracer(tracerName).Start(ctx, "ReconcileEtcd")
        defer span.End()
        errors[0] = r.reconcileEtcd(ctx, instance)
        if errors[0] != nil {
            tracing.RecordError(span, errors[0], "etcd reconciliation failed")
        } else {
            tracing.RecordSuccess(span)
        }
    }()

    go func() {
        defer wg.Done()
        ctx, span := otel.Tracer(tracerName).Start(ctx, "ReconcileGateway")
        defer span.End()
        errors[1] = r.reconcileGateway(ctx, instance)
        if errors[1] != nil {
            tracing.RecordError(span, errors[1], "gateway reconciliation failed")
        } else {
            tracing.RecordSuccess(span)
        }
    }()

    go func() {
        defer wg.Done()
        ctx, span := otel.Tracer(tracerName).Start(ctx, "ReconcileOrch")
        defer span.End()
        errors[2] = r.reconcileOrch(ctx, instance)
        if errors[2] != nil {
            tracing.RecordError(span, errors[2], "orch reconciliation failed")
        } else {
            tracing.RecordSuccess(span)
        }
    }()

    go func() {
        defer wg.Done()
        ctx, span := otel.Tracer(tracerName).Start(ctx, "ReconcilePooler")
        defer span.End()
        errors[3] = r.reconcilePooler(ctx, instance)
        if errors[3] != nil {
            tracing.RecordError(span, errors[3], "pooler reconciliation failed")
        } else {
            tracing.RecordSuccess(span)
        }
    }()

    wg.Wait()

    // Check for errors
    for i, err := range errors {
        if err != nil {
            span.SetAttributes(attribute.String("error.component", []string{"etcd", "gateway", "orch", "pooler"}[i]))
            tracing.RecordError(span, err, "component reconciliation failed")
            return ctrl.Result{}, err
        }
    }

    // Update status
    ctx, statusSpan := otel.Tracer(tracerName).Start(ctx, "UpdateStatus")
    if err := r.updateStatus(ctx, instance); err != nil {
        tracing.RecordError(statusSpan, err, "status update failed")
        statusSpan.End()
        tracing.RecordError(span, err, "failed to update status")
        return ctrl.Result{}, err
    }
    statusSpan.End()

    tracing.RecordSuccess(span)
    return ctrl.Result{}, nil
}
```

## Kubernetes Client Instrumentation

Instrument the Kubernetes client to trace all API calls:

```go
// cmd/multigres-operator/main.go

import (
    "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
    "k8s.io/client-go/rest"
)

func main() {
    // ... existing setup ...

    // Get Kubernetes config
    cfg, err := ctrl.GetConfig()
    if err != nil {
        setupLog.Error(err, "unable to get kubeconfig")
        os.Exit(1)
    }

    // Wrap HTTP client with OTel instrumentation
    cfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
        return otelhttp.NewTransport(rt,
            otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
                return fmt.Sprintf("K8s %s %s", r.Method, r.URL.Path)
            }),
        )
    })

    // Create manager with instrumented config
    mgr, err := ctrl.NewManager(cfg, ctrl.Options{
        Scheme: scheme,
        // ... other options ...
    })

    // ... rest of setup ...
}
```

This will automatically create spans for all Kubernetes API calls like:
- `K8s GET /apis/multigres.io/v1alpha1/namespaces/default/etcds/my-db-etcd`
- `K8s PATCH /apis/apps/v1/namespaces/default/statefulsets/my-db-etcd`
- `K8s POST /api/v1/namespaces/default/services`

## Trace Context Propagation

The controller-runtime framework already propagates context through reconciliation. Just ensure all functions accept and use `context.Context`:

```go
// Good: Context propagated through all functions
func (r *Reconciler) reconcileComponent(ctx context.Context, instance *API) error {
    ctx, span := otel.Tracer("...").Start(ctx, "ReconcileComponent")
    defer span.End()

    // Pass ctx to nested calls
    return r.buildResource(ctx, instance)
}

func (r *Reconciler) buildResource(ctx context.Context, instance *API) error {
    ctx, span := otel.Tracer("...").Start(ctx, "BuildResource")
    defer span.End()
    // ...
}
```

## Sampling Configuration

Support configurable sampling to control trace volume:

```go
// Environment variables for sampling configuration:
// OTEL_TRACE_SAMPLING_RATE: 0.0 to 1.0 (default 1.0 = 100%)
// OTEL_TRACE_SAMPLER: always_on, always_off, traceidratio, parentbased_traceidratio (default)

func getSampler() sdktrace.Sampler {
    samplerType := os.Getenv("OTEL_TRACE_SAMPLER")

    switch samplerType {
    case "always_on":
        return sdktrace.AlwaysSample()
    case "always_off":
        return sdktrace.NeverSample()
    case "traceidratio":
        rate := getSamplingRate()
        return sdktrace.TraceIDRatioBased(rate)
    case "parentbased_traceidratio", "":
        rate := getSamplingRate()
        return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(rate))
    default:
        setupLog.Info("unknown sampler type, using default", "sampler", samplerType)
        return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1.0))
    }
}

func getSamplingRate() float64 {
    rateStr := os.Getenv("OTEL_TRACE_SAMPLING_RATE")
    if rateStr == "" {
        return 1.0
    }
    rate, err := strconv.ParseFloat(rateStr, 64)
    if err != nil || rate < 0 || rate > 1 {
        setupLog.Info("invalid sampling rate, using 1.0", "rate", rateStr)
        return 1.0
    }
    return rate
}
```

## Trace Backend Deployment

### Option 1: Grafana Tempo (Recommended)

Tempo is a cost-effective, scalable trace backend that integrates well with Grafana:

```yaml
# config/samples/tempo/tempo-deployment.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: tempo-config
  namespace: monitoring
data:
  tempo.yaml: |
    server:
      http_listen_port: 3200
    distributor:
      receivers:
        otlp:
          protocols:
            grpc:
              endpoint: 0.0.0.0:4317
            http:
              endpoint: 0.0.0.0:4318
    storage:
      trace:
        backend: local
        local:
          path: /var/tempo/traces
    query_frontend:
      search:
        max_duration: 24h
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tempo
  namespace: monitoring
spec:
  replicas: 1
  selector:
    matchLabels:
      app: tempo
  template:
    metadata:
      labels:
        app: tempo
    spec:
      containers:
      - name: tempo
        image: grafana/tempo:2.3.1
        args:
        - -config.file=/etc/tempo/tempo.yaml
        ports:
        - containerPort: 3200
          name: http
        - containerPort: 4317
          name: otlp-grpc
        - containerPort: 4318
          name: otlp-http
        volumeMounts:
        - name: config
          mountPath: /etc/tempo
        - name: storage
          mountPath: /var/tempo
        resources:
          requests:
            cpu: 100m
            memory: 256Mi
          limits:
            cpu: 500m
            memory: 1Gi
      volumes:
      - name: config
        configMap:
          name: tempo-config
      - name: storage
        emptyDir: {}  # Use PVC in production
---
apiVersion: v1
kind: Service
metadata:
  name: tempo
  namespace: monitoring
spec:
  selector:
    app: tempo
  ports:
  - name: http
    port: 3200
    targetPort: 3200
  - name: otlp-grpc
    port: 4317
    targetPort: 4317
  - name: otlp-http
    port: 4318
    targetPort: 4318
```

### Option 2: Jaeger (Alternative)

Jaeger provides a complete tracing solution with UI:

```yaml
# config/samples/jaeger/jaeger-all-in-one.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: jaeger
  namespace: monitoring
spec:
  replicas: 1
  selector:
    matchLabels:
      app: jaeger
  template:
    metadata:
      labels:
        app: jaeger
    spec:
      containers:
      - name: jaeger
        image: jaegertracing/all-in-one:1.52
        env:
        - name: COLLECTOR_OTLP_ENABLED
          value: "true"
        ports:
        - containerPort: 16686  # UI
          name: ui
        - containerPort: 4317   # OTLP gRPC
          name: otlp-grpc
        - containerPort: 4318   # OTLP HTTP
          name: otlp-http
        resources:
          requests:
            cpu: 100m
            memory: 512Mi
          limits:
            cpu: 500m
            memory: 2Gi
---
apiVersion: v1
kind: Service
metadata:
  name: jaeger
  namespace: monitoring
spec:
  selector:
    app: jaeger
  ports:
  - name: ui
    port: 16686
    targetPort: 16686
  - name: otlp-grpc
    port: 4317
    targetPort: 4317
  - name: otlp-http
    port: 4318
    targetPort: 4318
```

## RBAC Requirements

No additional RBAC permissions needed - tracing uses the same Kubernetes API calls as normal reconciliation, which already have appropriate permissions.

## Test Plan

### Unit Tests

Test span creation and attributes without exporting traces:

```go
// pkg/resource-handler/controller/etcd/etcd_controller_test.go
package etcd

import (
    "context"
    "testing"

    "go.opentelemetry.io/otel"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestReconcile_TracingSpans(t *testing.T) {
    // Setup in-memory span recorder
    spanRecorder := tracetest.NewSpanRecorder()
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithSpanProcessor(spanRecorder),
    )
    otel.SetTracerProvider(tp)

    // Run reconciliation
    r := &EtcdReconciler{/* ... */}
    ctx := context.Background()
    _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"}})

    // Verify spans were created
    spans := spanRecorder.Ended()
    require.NoError(t, err)
    require.NotEmpty(t, spans, "expected spans to be created")

    // Check top-level reconcile span
    reconcileSpan := findSpanByName(spans, "Reconcile")
    require.NotNil(t, reconcileSpan, "expected Reconcile span")
    assert.Equal(t, "default", reconcileSpan.Attributes()["k8s.namespace"])
    assert.Equal(t, "test", reconcileSpan.Attributes()["k8s.name"])

    // Check child spans exist
    assert.NotNil(t, findSpanByName(spans, "Get(Etcd)"))
    assert.NotNil(t, findSpanByName(spans, "ReconcileStatefulSet"))
    assert.NotNil(t, findSpanByName(spans, "UpdateStatus"))
}

func TestReconcile_TracingErrorRecording(t *testing.T) {
    spanRecorder := tracetest.NewSpanRecorder()
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithSpanProcessor(spanRecorder),
    )
    otel.SetTracerProvider(tp)

    // Setup reconciler that will fail
    r := &EtcdReconciler{/* inject error condition */}
    ctx := context.Background()
    _, err := r.Reconcile(ctx, ctrl.Request{/* ... */})

    require.Error(t, err)

    // Verify error recorded in span
    spans := spanRecorder.Ended()
    reconcileSpan := findSpanByName(spans, "Reconcile")
    require.NotNil(t, reconcileSpan)
    assert.Equal(t, codes.Error, reconcileSpan.Status().Code)
    assert.NotEmpty(t, reconcileSpan.Events(), "expected error event in span")
}
```

### Integration Tests

Test tracing in envtest with OTLP export:

```go
// test/integration/tracing_test.go
package integration

import (
    "context"
    "testing"
    "time"

    "go.opentelemetry.io/otel"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracing_EndToEnd(t *testing.T) {
    // Setup test environment with span recorder
    spanRecorder := tracetest.NewSpanRecorder()
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithSpanProcessor(spanRecorder),
    )
    otel.SetTracerProvider(tp)

    // Create Etcd CR
    etcd := &multigresv1alpha1.Etcd{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "test-etcd",
            Namespace: "default",
        },
        Spec: multigresv1alpha1.EtcdSpec{
            Replicas: 3,
        },
    }
    err := k8sClient.Create(context.Background(), etcd)
    require.NoError(t, err)

    // Wait for reconciliation
    time.Sleep(2 * time.Second)

    // Verify traces were created
    spans := spanRecorder.Ended()
    require.NotEmpty(t, spans, "expected spans from reconciliation")

    // Verify span hierarchy
    reconcileSpan := findSpanByName(spans, "Reconcile")
    require.NotNil(t, reconcileSpan)

    // Verify child spans have correct parent
    childSpans := findChildSpans(spans, reconcileSpan.SpanContext().SpanID())
    assert.GreaterOrEqual(t, len(childSpans), 3, "expected multiple child spans")
}
```

### Manual Testing

```bash
# 1. Deploy Tempo in monitoring namespace
kubectl create namespace monitoring
kubectl apply -f config/samples/tempo/tempo-deployment.yaml

# 2. Configure operator to export traces
kubectl set env deployment/multigres-operator -n multigres-system \
  OTEL_EXPORTER_OTLP_ENDPOINT=tempo.monitoring.svc:4317 \
  OTEL_TRACE_SAMPLING_RATE=1.0

# 3. Create a MultigresCluster
kubectl apply -f config/samples/multigres_v1alpha1_multigrescluster.yaml

# 4. Query Tempo for traces
kubectl port-forward -n monitoring svc/tempo 3200:3200

# Search for traces by service name
curl 'http://localhost:3200/api/search?tags=service.name=multigres-operator&limit=10'

# Get specific trace
curl 'http://localhost:3200/api/traces/<trace-id>'

# 5. If using Grafana, connect Tempo as datasource and use Explore view
# - Datasource: http://tempo.monitoring.svc:3200
# - Query: { service.name="multigres-operator" }
```

### Performance Testing

Measure tracing overhead:

```bash
# Run reconciliation benchmark without tracing
OTEL_TRACE_SAMPLER=always_off go test -bench=BenchmarkReconcile -benchmem

# Run reconciliation benchmark with tracing (100% sampling)
OTEL_TRACE_SAMPLING_RATE=1.0 go test -bench=BenchmarkReconcile -benchmem

# Compare CPU and memory overhead
# Target: < 5% overhead with 100% sampling
```

## Implementation Phases

### MVP (Minimum Viable Product)
- [ ] OpenTelemetry Tracing SDK integrated in operator
- [ ] All reconcilers instrumented with spans (MultigresCluster, Etcd, Gateway, Orch, Pooler, Cell)
- [ ] Kubernetes client wrapped with OTEL HTTP instrumentation
- [ ] OTLP trace export to Tempo working
- [ ] Sampling configuration via environment variables
- [ ] Basic unit tests with span recorder
- [ ] Documentation for Tempo deployment and trace querying

### Complete Implementation
- [ ] Trace context propagation validated across all reconciliation paths
- [ ] Integration tests with envtest and span validation
- [ ] Error spans recorded with proper status codes
- [ ] Performance benchmarks showing < 5% overhead
- [ ] Structured logging correlation with trace IDs
- [ ] Production deployment guide with Tempo or Jaeger
- [ ] Example Grafana dashboards for trace visualization

### Future Enhancements
- [ ] Battle-tested in production deployments
- [ ] Comprehensive span attributes following OTel semantic conventions
- [ ] Trace correlation with component logs (if components support tracing)
- [ ] Advanced sampling strategies documented (e.g., error-based, latency-based)
- [ ] Multi-backend support validated (Tempo, Jaeger, Zipkin)
- [ ] Trace-based alerting examples
- [ ] Performance validated at scale (1000+ components, high reconciliation rate)

## Upgrade / Downgrade Strategy

**Enabling Tracing:**
1. Deploy trace backend (Tempo or Jaeger) in cluster
2. Upgrade multigres-operator to version with tracing support
3. Set `OTEL_EXPORTER_OTLP_ENDPOINT` environment variable in operator deployment
4. Optionally configure sampling rate with `OTEL_TRACE_SAMPLING_RATE`
5. Traces start exporting immediately on next reconciliation

**Disabling Tracing:**
1. Set `OTEL_TRACE_SAMPLER=always_off` to stop trace generation
2. Or remove `OTEL_EXPORTER_OTLP_ENDPOINT` to disable export (spans still created but not exported)
3. Or delete trace backend (traces still generated but export fails silently)

**Downgrading Operator:**
- Older operator versions without tracing won't export spans
- No data loss - metrics and normal operation unaffected
- Trace backend can remain deployed for other services

**Version Skew:**
- Tracing is independent of component versions
- Only operator behavior is traced, not component internals
- Safe to mix operator versions with/without tracing in multi-cluster setups

## Version Skew Strategy

**OpenTelemetry SDK Versions:**
- Use stable OTel SDK (v1.x) with semantic versioning guarantees
- Pin to specific OTel version in go.mod for reproducibility
- Update OTel dependencies quarterly or for security fixes
- Test against latest OTel SDK in CI

**Trace Backend Compatibility:**
- OTLP protocol is stable and backward compatible
- Tempo supports OTLP 1.0+ (current stable version)
- Jaeger supports OTLP via native collector
- Zipkin requires OTLP-to-Zipkin translation (not recommended)

**Sampling Strategy Evolution:**
- Default sampling configuration is backward compatible
- New sampling strategies added as opt-in environment variables
- Existing deployments continue using default ParentBased sampler

# Implementation History

- 2025-10-16: Initial draft created based on OpenTelemetry metrics integration foundation

# Drawbacks

**Why should we not do this?**

- **Increased Complexity**: Adds another observability dimension requiring setup and maintenance
  - Mitigation: Tracing is optional; operator functions normally without it

- **Performance Overhead**: Span creation and export consume CPU/memory
  - Mitigation: Use sampling to reduce overhead; target < 5% impact

- **Additional Infrastructure**: Requires trace backend deployment (Tempo, Jaeger)
  - Mitigation: Lightweight backends like Tempo have minimal resource requirements; can be deployed on-demand

- **Storage Costs**: Traces consume storage, especially at high volume
  - Mitigation: Configure retention policies (e.g., 7 days); use sampling in production

- **Learning Curve**: Team needs to understand distributed tracing concepts
  - Mitigation: OpenTelemetry is industry standard; investment benefits future observability work

- **Vendor Lock-in Risk**: Trace backend choice affects tooling
  - Mitigation: OTLP is vendor-neutral; easy to switch backends (Tempo → Jaeger → Zipkin)

# Alternatives

## Alternative 1: Structured Logging Only

**Pros:**
- Simpler - no additional infrastructure
- Logs already exist and are familiar
- Correlation via request IDs in logs

**Cons:**
- Hard to visualize request flow across functions
- No timing breakdown for performance analysis
- Difficult to correlate spans across goroutines
- No native support for parent-child relationships

**Decision:** Tracing provides much better visualization of complex reconciliation flows; logs complement but don't replace traces.

## Alternative 2: Metrics-Based Profiling

**Pros:**
- Already have metrics integration
- Can measure aggregate latency with histograms
- Lower overhead than tracing

**Cons:**
- No per-request visibility
- Can't debug individual slow reconciliations
- No call graph or span hierarchy
- Aggregate data doesn't show outliers or edge cases

**Decision:** Metrics show "what" is slow, tracing shows "why" - both are complementary.

## Alternative 3: Jaeger Direct (Without OTLP)

**Pros:**
- Native Jaeger SDK is mature
- Direct export without OTLP translation

**Cons:**
- Vendor lock-in to Jaeger protocol
- Can't easily switch to Tempo or other backends
- Jaeger SDK is not as actively developed as OTel

**Decision:** OpenTelemetry OTLP is vendor-neutral and future-proof; Jaeger still supported as backend.

## Alternative 4: Custom Tracing Solution

**Pros:**
- Full control over span format and attributes
- No external dependencies

**Cons:**
- Reinventing the wheel - OTel is industry standard
- No ecosystem tools (Tempo, Jaeger, Grafana)
- Maintenance burden
- No interoperability with other systems

**Decision:** OpenTelemetry is the clear industry standard; no reason to build custom solution.

## Alternative 5: Profiling Tools (pprof)

**Pros:**
- Go-native profiling with pprof
- CPU and memory profiling built-in
- No external dependencies

**Cons:**
- Requires active profiling sessions (not always-on)
- Hard to correlate with specific reconciliations
- Not suitable for production debugging
- No distributed context across API calls

**Decision:** pprof is complementary for deep performance analysis; tracing is better for production observability.

# Infrastructure Needed

**Required:**
- Trace backend deployment (Tempo recommended, Jaeger alternative)
- OpenTelemetry SDK dependencies in operator
- OTLP endpoint configuration (environment variable)

**Optional:**
- Grafana for trace visualization (recommended with Tempo)
- Persistent storage for trace retention (production)
- Service mesh for cross-service trace propagation (future multi-component tracing)

**Development/Testing:**
- kind cluster with sufficient resources (4GB+ memory for Tempo)
- Namespace: `monitoring` for trace backend
- Storage: 10Gi for Tempo (development), 100Gi+ (production)

**CI/CD:**
- Trace backend deployment in test environments (optional - can use in-memory span recorder)
- Integration tests with OTLP validation
- Performance benchmarks with/without tracing

**Production:**
- Tempo/Jaeger deployment with persistent storage
- Trace retention policy (7-30 days typical)
- Sampling configuration (10-50% typical for high-volume clusters)
- Grafana or Jaeger UI for trace exploration

# References

**OpenTelemetry:**
- Tracing Specification: https://opentelemetry.io/docs/specs/otel/trace/api/
- Go SDK: https://opentelemetry.io/docs/languages/go/instrumentation/
- Semantic Conventions: https://opentelemetry.io/docs/specs/semconv/
- OTLP Protocol: https://opentelemetry.io/docs/specs/otlp/

**Trace Backends:**
- Grafana Tempo: https://grafana.com/docs/tempo/latest/
- Jaeger: https://www.jaegertracing.io/docs/
- Tempo OTLP Ingestion: https://grafana.com/docs/tempo/latest/configuration/protocols/

**Kubernetes Observability:**
- controller-runtime Metrics: https://book.kubebuilder.io/reference/metrics.html
- OTel HTTP Instrumentation: https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp
- Context Propagation: https://pkg.go.dev/context

**Related Planning:**
- Phase 2: VictoriaMetrics and OpenTelemetry Integration (metrics foundation)
- Architecture: docs/architecture.md (OpenTelemetry section)
- Implementation Guide: docs/implementation-guide.md (testing standards)


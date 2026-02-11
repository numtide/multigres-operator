package monitoring

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	tracerName = "multigres-operator"

	// Annotation keys for trace context propagation across the async
	// kubectl-apply → webhook → reconcile boundary.
	annotationTraceparent   = "multigres.com/traceparent"
	annotationTraceparentTS = "multigres.com/traceparent-ts"

	// staleThreshold is the maximum age of a traceparent annotation before
	// it is considered stale. Stale annotations create a new root span
	// with an OTel Link instead of a child span.
	staleThreshold = 10 * time.Minute
)

// Tracer is the package-level OTel tracer for the operator.
// It returns a noop tracer when no TracerProvider is registered,
// making instrumentation zero-cost in the default configuration.
var Tracer = otel.Tracer(tracerName)

// InitTracing initialises the OTel TracerProvider using autoexport.
// The transport protocol is determined by OTEL_EXPORTER_OTLP_PROTOCOL
// (default: "http/protobuf"). Supported values: "grpc", "http/protobuf".
// If OTEL_EXPORTER_OTLP_ENDPOINT is unset, tracing is left disabled and a
// noop shutdown function is returned.
func InitTracing(ctx context.Context, serviceName, version string) (func(context.Context) error, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("creating OTel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Re-acquire the tracer from the newly registered provider so all
	// existing call sites (StartReconcileSpan, StartChildSpan) produce
	// real spans.
	Tracer = otel.Tracer(tracerName)

	return tp.Shutdown, nil
}

// StartReconcileSpan starts a new span for a controller reconciliation.
// The span is annotated with the Kubernetes resource name, namespace, and kind.
// Callers must call span.End() when the operation completes.
func StartReconcileSpan(ctx context.Context, spanName, name, namespace, kind string) (context.Context, trace.Span) {
	ctx, span := Tracer.Start(ctx, spanName,
		trace.WithAttributes(
			attribute.String("k8s.resource.name", name),
			attribute.String("k8s.namespace", namespace),
			attribute.String("k8s.resource.kind", kind),
		),
	)
	return ctx, span
}

// StartChildSpan starts a child span under the current trace context.
// Use this for sub-operations within a reconciliation (e.g., ReconcileCells, UpdateStatus).
func StartChildSpan(ctx context.Context, spanName string) (context.Context, trace.Span) {
	return Tracer.Start(ctx, spanName)
}

// RecordSpanError records an error on a span and sets the span status to Error.
// If err is nil, this is a no-op.
func RecordSpanError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// InjectTraceContext writes the current span's W3C traceparent and a
// Unix-second timestamp into the given annotations map. The webhook
// uses this to bridge the async gap between admission and reconciliation.
func InjectTraceContext(ctx context.Context, annotations map[string]string) {
	sc := trace.SpanFromContext(ctx).SpanContext()
	if !sc.IsValid() {
		return
	}
	carrier := propagation.MapCarrier(annotations)
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	// Rename the standard "traceparent" key to our annotation key.
	if tp, ok := annotations["traceparent"]; ok {
		annotations[annotationTraceparent] = tp
		delete(annotations, "traceparent")
	}
	// Remove tracestate if the propagator injected it under the standard key.
	if ts, ok := annotations["tracestate"]; ok {
		annotations["multigres.com/tracestate"] = ts
		delete(annotations, "tracestate")
	}

	annotations[annotationTraceparentTS] = strconv.FormatInt(time.Now().Unix(), 10)
}

// ExtractTraceContext reads the traceparent annotation from a Kubernetes
// object and reconstructs the parent span context. The second return value
// is true when the annotation is older than staleThreshold (10 minutes),
// indicating the caller should create a new root span with a Link instead
// of a child span.
//
// Returns (background context, false) if no valid annotation is found.
func ExtractTraceContext(annotations map[string]string) (context.Context, bool) {
	tp, ok := annotations[annotationTraceparent]
	if !ok || tp == "" {
		return context.Background(), false
	}

	// Build a carrier with the standard W3C key so the propagator can parse it.
	carrier := propagation.MapCarrier{
		"traceparent": tp,
	}
	if ts, ok := annotations["multigres.com/tracestate"]; ok {
		carrier["tracestate"] = ts
	}

	ctx := otel.GetTextMapPropagator().Extract(context.Background(), carrier)

	// Check staleness.
	tsStr, ok := annotations[annotationTraceparentTS]
	if !ok {
		return ctx, true // No timestamp → treat as stale for safety.
	}
	tsSec, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return ctx, true
	}
	age := time.Since(time.Unix(tsSec, 0))
	return ctx, age > staleThreshold
}

// EnrichLoggerWithTrace extracts the trace ID and span ID from the current
// span context and injects them as structured key-value pairs into the logr
// logger carried by ctx. All downstream log lines will automatically include
// these fields, enabling "click log → view trace" in Grafana.
func EnrichLoggerWithTrace(ctx context.Context) context.Context {
	sc := trace.SpanFromContext(ctx).SpanContext()
	if !sc.IsValid() {
		return ctx
	}
	l := log.FromContext(ctx)
	l = l.WithValues(
		"trace_id", sc.TraceID().String(),
		"span_id", sc.SpanID().String(),
	)
	return logr.NewContext(ctx, l)
}

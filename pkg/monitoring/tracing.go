package monitoring

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the instrumentation scope name registered with OTel.
const tracerName = "multigres-operator"

// Tracer is the package-level OTel tracer for the operator.
// It returns a noop tracer when no TracerProvider is registered,
// making instrumentation zero-cost in the default configuration.
var Tracer = otel.Tracer(tracerName)

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

package monitoring

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func TestStartReconcileSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	// Point the package-level Tracer at our test provider.
	Tracer = tp.Tracer(tracerName)

	ctx := context.Background()
	ctx, span := StartReconcileSpan(
		ctx,
		"MultigresCluster.Reconcile",
		"my-cluster",
		"default",
		"MultigresCluster",
	)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	s := spans[0]
	if s.Name != "MultigresCluster.Reconcile" {
		t.Errorf("span name = %q, want %q", s.Name, "MultigresCluster.Reconcile")
	}

	wantAttrs := map[string]string{
		"k8s.resource.name": "my-cluster",
		"k8s.namespace":     "default",
		"k8s.resource.kind": "MultigresCluster",
	}
	for key, want := range wantAttrs {
		found := false
		for _, attr := range s.Attributes {
			if string(attr.Key) == key {
				found = true
				if attr.Value.AsString() != want {
					t.Errorf("attribute %q = %q, want %q", key, attr.Value.AsString(), want)
				}
			}
		}
		if !found {
			t.Errorf("attribute %q not found on span", key)
		}
	}

	// Verify the context carries the span.
	if ctx == context.Background() {
		t.Error("expected context to carry span")
	}
}

func TestStartChildSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	Tracer = tp.Tracer(tracerName)

	ctx := context.Background()
	ctx, parent := StartReconcileSpan(ctx, "Parent.Reconcile", "res", "ns", "Kind")
	_, child := StartChildSpan(ctx, "ChildOperation")
	child.End()
	parent.End()

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}

	// Child span should reference the parent's span context.
	childSpan := spans[0]
	parentSpan := spans[1]
	if childSpan.Parent.SpanID() != parentSpan.SpanContext.SpanID() {
		t.Errorf(
			"child parent span ID = %s, want %s",
			childSpan.Parent.SpanID(),
			parentSpan.SpanContext.SpanID(),
		)
	}
	if childSpan.Name != "ChildOperation" {
		t.Errorf("child span name = %q, want %q", childSpan.Name, "ChildOperation")
	}
}

func TestRecordSpanError(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	Tracer = tp.Tracer(tracerName)

	t.Run("records error on span", func(t *testing.T) {
		exporter.Reset()
		_, span := StartReconcileSpan(context.Background(), "Op", "n", "ns", "K")
		testErr := errors.New("something failed")
		RecordSpanError(span, testErr)
		span.End()

		spans := exporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}

		s := spans[0]
		if s.Status.Code != codes.Error {
			t.Errorf("span status = %v, want Error", s.Status.Code)
		}
		if s.Status.Description != "something failed" {
			t.Errorf(
				"span status description = %q, want %q",
				s.Status.Description,
				"something failed",
			)
		}

		// Check that an error event was recorded.
		foundErrorEvent := false
		for _, event := range s.Events {
			if event.Name == "exception" {
				foundErrorEvent = true
				for _, attr := range event.Attributes {
					if attr.Key == attribute.Key("exception.message") &&
						attr.Value.AsString() == "something failed" {
						break
					}
				}
			}
		}
		if !foundErrorEvent {
			t.Error("expected an exception event on the span")
		}
	})

	t.Run("nil error is no-op", func(t *testing.T) {
		exporter.Reset()
		_, span := StartReconcileSpan(context.Background(), "Op", "n", "ns", "K")
		RecordSpanError(span, nil)
		span.End()

		spans := exporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}
		if spans[0].Status.Code == codes.Error {
			t.Error("nil error should not set error status")
		}
	})
}

func TestInitTracing_NoopWhenEndpointUnset(t *testing.T) {
	// Ensure OTEL_EXPORTER_OTLP_ENDPOINT is unset.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	shutdown, err := InitTracing(context.Background(), "test-svc", "v0.0.1")
	if err != nil {
		t.Fatalf("InitTracing() returned error: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown() returned error: %v", err)
	}
}

func TestInjectAndExtractTraceContext(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	Tracer = tp.Tracer(tracerName)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	t.Run("round-trips trace context through annotations", func(t *testing.T) {
		ctx, span := Tracer.Start(context.Background(), "webhook")
		originalTraceID := span.SpanContext().TraceID()

		annotations := make(map[string]string)
		InjectTraceContext(ctx, annotations)
		span.End()

		if _, ok := annotations[annotationTraceparent]; !ok {
			t.Fatal("expected traceparent annotation to be set")
		}
		if _, ok := annotations[annotationTraceparentTS]; !ok {
			t.Fatal("expected traceparent-ts annotation to be set")
		}

		parentCtx, isStale := ExtractTraceContext(annotations)
		if isStale {
			t.Error("fresh annotation should not be stale")
		}
		sc := trace.SpanFromContext(parentCtx).SpanContext()
		if sc.TraceID() != originalTraceID {
			t.Errorf("extracted trace ID = %s, want %s", sc.TraceID(), originalTraceID)
		}
	})

	t.Run("stale annotation", func(t *testing.T) {
		ctx, span := Tracer.Start(context.Background(), "old-webhook")

		annotations := make(map[string]string)
		InjectTraceContext(ctx, annotations)
		span.End()

		// Backdate the timestamp by 15 minutes.
		staleTS := time.Now().Add(-15 * time.Minute).Unix()
		annotations[annotationTraceparentTS] = strconv.FormatInt(staleTS, 10)

		_, isStale := ExtractTraceContext(annotations)
		if !isStale {
			t.Error("expected stale annotation to be detected")
		}
	})

	t.Run("missing annotation returns background context", func(t *testing.T) {
		parentCtx, isStale := ExtractTraceContext(map[string]string{})
		if isStale {
			t.Error("empty annotations should not be stale")
		}
		sc := trace.SpanFromContext(parentCtx).SpanContext()
		if sc.IsValid() {
			t.Error("expected invalid span context from empty annotations")
		}
	})

	t.Run("missing timestamp treated as stale", func(t *testing.T) {
		ctx, span := Tracer.Start(context.Background(), "no-ts-webhook")
		annotations := make(map[string]string)
		InjectTraceContext(ctx, annotations)
		span.End()

		delete(annotations, annotationTraceparentTS)

		_, isStale := ExtractTraceContext(annotations)
		if !isStale {
			t.Error("missing timestamp should be treated as stale")
		}
	})
}

func TestEnrichLoggerWithTrace(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	Tracer = tp.Tracer(tracerName)

	t.Run("adds trace_id and span_id to logger", func(t *testing.T) {
		ctx, span := Tracer.Start(context.Background(), "test-op")
		defer span.End()

		// Set up a logger in context.
		ctx = logr.NewContext(ctx, logr.Discard())
		enrichedCtx := EnrichLoggerWithTrace(ctx)

		// The enriched context should have a logger that can extract values.
		logger := log.FromContext(enrichedCtx)
		// We can't easily inspect logr values, but we can verify the function
		// doesn't panic and returns a different context.
		if enrichedCtx == ctx {
			t.Error("expected enriched context to differ from original")
		}
		_ = logger
	})

	t.Run("noop for invalid span context", func(t *testing.T) {
		ctx := logr.NewContext(context.Background(), logr.Discard())
		result := EnrichLoggerWithTrace(ctx)
		// With no valid span, the context should be returned unchanged.
		if result != ctx {
			t.Error("expected unchanged context for invalid span")
		}
	})
}

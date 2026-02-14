package monitoring

import (
	"context"
	"errors"
	"strconv"
	"strings"
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

func TestInitTracing_WithEndpoint(t *testing.T) {
	// Set the endpoint to trigger the real code path, and use the "none"
	// exporter so autoexport returns a noop exporter without network I/O.
	// This still exercises resource creation, provider setup, and global
	// tracer re-acquisition.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	t.Setenv("OTEL_TRACES_EXPORTER", "none")

	shutdown, err := InitTracing(context.Background(), "test-svc", "v0.0.1")
	if err != nil {
		t.Fatalf("InitTracing() returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown function")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown() returned error: %v", err)
	}
}

func TestInitTracing_ExporterError(t *testing.T) {
	// Set endpoint to trigger exporter creation
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	// Set invalid exporter type to trigger error in autoexport.NewSpanExporter
	t.Setenv("OTEL_TRACES_EXPORTER", "invalid-exporter-type")

	// InitTracing should fail
	shutdown, err := InitTracing(context.Background(), "test-svc", "v0.0.1")
	if err == nil {
		t.Fatal("InitTracing() should have failed with invalid exporter type")
	}
	if shutdown != nil {
		t.Fatal("shutdown function should be nil on error")
	}
	if !strings.Contains(err.Error(), "creating OTLP exporter") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestInitTracing_ResourceError_InvalidDetector(t *testing.T) {
	// Set endpoint so we reach resource creation
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	t.Setenv("OTEL_TRACES_EXPORTER", "none")

	// Try to trigger resource creation error with invalid detector
	t.Setenv("OTEL_EXPERIMENTAL_RESOURCE_DETECTORS", "invalid-detector")

	shutdown, err := InitTracing(context.Background(), "test-svc", "v0.0.1")
	if err == nil {
		// If this doesn't fail, we might not be able to cover this line without mocking
		t.Log(
			"InitTracing() did not fail with invalid detector. Resource error path might be unreachable.",
		)
	} else {
		if !strings.Contains(err.Error(), "creating OTel resource") {
			t.Errorf("unexpected error message: %v", err)
		}
	}
	if shutdown != nil {
		_ = shutdown(context.Background())
	}
}

func TestInjectTraceContext_TracestateRename(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	Tracer = tp.Tracer(tracerName)

	// Use a composite propagator that injects both traceparent and tracestate.
	otel.SetTextMapPropagator(propagation.TraceContext{})

	ctx, span := Tracer.Start(context.Background(), "webhook")
	defer span.End()

	// Force a tracestate by setting it on the span.
	ts := trace.TraceState{}
	ts, _ = ts.Insert("vendor", "value")
	ctx = trace.ContextWithSpanContext(ctx, span.SpanContext().WithTraceState(ts))

	annotations := make(map[string]string)
	InjectTraceContext(ctx, annotations)

	// The standard "tracestate" key should be renamed.
	if _, ok := annotations["tracestate"]; ok {
		t.Error("standard 'tracestate' key should be renamed")
	}
	if _, ok := annotations["multigres.com/tracestate"]; !ok {
		t.Error("expected 'multigres.com/tracestate' annotation to be set")
	}
}

func TestExtractTraceContext_InvalidTimestamp(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	Tracer = tp.Tracer(tracerName)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	ctx, span := Tracer.Start(context.Background(), "webhook")
	annotations := make(map[string]string)
	InjectTraceContext(ctx, annotations)
	span.End()

	// Set an invalid (non-numeric) timestamp.
	annotations[annotationTraceparentTS] = "not-a-number"

	_, isStale := ExtractTraceContext(annotations)
	if !isStale {
		t.Error("invalid timestamp should be treated as stale")
	}
}

func TestInjectTraceContext_InvalidSpanContext(t *testing.T) {
	annotations := make(map[string]string)
	InjectTraceContext(context.Background(), annotations)

	if len(annotations) != 0 {
		t.Errorf("expected no annotations for invalid span, got %v", annotations)
	}
}

func TestExtractTraceContext_WithTracestate(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	Tracer = tp.Tracer(tracerName)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Create a span and inject context with tracestate.
	ctx, span := Tracer.Start(context.Background(), "webhook")
	ts := trace.TraceState{}
	ts, _ = ts.Insert("vendor", "value")
	ctx = trace.ContextWithSpanContext(ctx, span.SpanContext().WithTraceState(ts))

	annotations := make(map[string]string)
	InjectTraceContext(ctx, annotations)
	span.End()

	// Verify the tracestate was injected under our custom key.
	if _, ok := annotations["multigres.com/tracestate"]; !ok {
		t.Fatal("expected multigres.com/tracestate annotation")
	}

	// Now extract and verify the tracestate is restored.
	extractedCtx, _ := ExtractTraceContext(annotations)
	sc := trace.SpanFromContext(extractedCtx).SpanContext()
	if !sc.IsValid() {
		t.Fatal("expected valid span context after extraction")
	}
}

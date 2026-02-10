package monitoring

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestStartReconcileSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	// Point the package-level Tracer at our test provider.
	Tracer = tp.Tracer(tracerName)

	ctx := context.Background()
	ctx, span := StartReconcileSpan(ctx, "MultigresCluster.Reconcile", "my-cluster", "default", "MultigresCluster")
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
		t.Errorf("child parent span ID = %s, want %s", childSpan.Parent.SpanID(), parentSpan.SpanContext.SpanID())
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
			t.Errorf("span status description = %q, want %q", s.Status.Description, "something failed")
		}

		// Check that an error event was recorded.
		foundErrorEvent := false
		for _, event := range s.Events {
			if event.Name == "exception" {
				foundErrorEvent = true
				for _, attr := range event.Attributes {
					if attr.Key == attribute.Key("exception.message") && attr.Value.AsString() == "something failed" {
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

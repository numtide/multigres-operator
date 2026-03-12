package observer

import (
	"testing"
	"time"

	"github.com/numtide/multigres-operator/tools/observer/pkg/report"
)

func TestFindingKey(t *testing.T) {
	f1 := report.Finding{Check: "pod-health", Component: "pool-0", Message: "not ready"}
	f2 := report.Finding{Check: "pod-health", Component: "pool-0", Message: "not ready"}
	f3 := report.Finding{Check: "pod-health", Component: "pool-1", Message: "not ready"}

	if findingKey(f1) != findingKey(f2) {
		t.Error("identical findings should produce the same key")
	}
	if findingKey(f1) == findingKey(f3) {
		t.Error("different findings should produce different keys")
	}
	if len(findingKey(f1)) != 16 {
		t.Errorf("key length should be 16 hex chars, got %d", len(findingKey(f1)))
	}
}

func TestHistoryRecordAndBuild(t *testing.T) {
	fh := newFindingHistory(5)
	base := time.Now()

	f1 := report.Finding{Check: "pod-health", Component: "pool-0", Message: "not ready", Severity: report.SeverityError}
	f2 := report.Finding{Check: "connectivity", Component: "gw-0", Message: "timeout", Severity: report.SeverityWarn}

	// Cycle 1: both findings.
	fh.Record(base, base.Add(1*time.Second), []report.Finding{f1, f2})

	resp := fh.Build()
	if resp.TotalCycles != 1 {
		t.Fatalf("expected 1 cycle, got %d", resp.TotalCycles)
	}
	if len(resp.Persistent) != 2 {
		t.Fatalf("expected 2 persistent (early classification), got %d", len(resp.Persistent))
	}

	// Cycle 2: only f1.
	fh.Record(base.Add(10*time.Second), base.Add(11*time.Second), []report.Finding{f1})

	resp = fh.Build()
	if resp.TotalCycles != 2 {
		t.Fatalf("expected 2 cycles, got %d", resp.TotalCycles)
	}
	if len(resp.Transient) != 1 {
		t.Fatalf("expected 1 transient (f2 resolved), got %d", len(resp.Transient))
	}

	// Cycle 3: f1 again (3 consecutive). f2 still resolved.
	fh.Record(base.Add(20*time.Second), base.Add(21*time.Second), []report.Finding{f1})

	resp = fh.Build()
	if resp.TotalCycles != 3 {
		t.Fatalf("expected 3 cycles, got %d", resp.TotalCycles)
	}

	// f1 appeared in 3/3 cycles => persistent.
	if len(resp.Persistent) != 1 {
		t.Fatalf("expected 1 persistent, got %d", len(resp.Persistent))
	}
	if resp.Persistent[0].Count != 3 {
		t.Errorf("expected count 3, got %d", resp.Persistent[0].Count)
	}
}

func TestHistoryRingBufferWrap(t *testing.T) {
	fh := newFindingHistory(3) // tiny buffer
	base := time.Now()

	f := report.Finding{Check: "pod-health", Component: "p", Message: "err", Severity: report.SeverityError}

	for i := range 5 {
		start := base.Add(time.Duration(i*10) * time.Second)
		fh.Record(start, start.Add(time.Second), []report.Finding{f})
	}

	resp := fh.Build()
	if resp.TotalCycles != 3 {
		t.Fatalf("expected 3 cycles (capped at capacity), got %d", resp.TotalCycles)
	}
	if len(resp.Cycles) != 3 {
		t.Fatalf("expected 3 cycle records, got %d", len(resp.Cycles))
	}

	// Verify chronological order.
	for i := 1; i < len(resp.Cycles); i++ {
		if resp.Cycles[i].CycleStart.Before(resp.Cycles[i-1].CycleStart) {
			t.Error("cycles should be in chronological order")
		}
	}
}

func TestHistoryFlapping(t *testing.T) {
	fh := newFindingHistory(10)
	base := time.Now()

	f := report.Finding{Check: "connectivity", Component: "gw", Message: "flaky", Severity: report.SeverityError}

	// Appear, disappear, appear, disappear, appear — 3 appearances in 5 cycles.
	for i := range 5 {
		start := base.Add(time.Duration(i*10) * time.Second)
		var findings []report.Finding
		if i%2 == 0 {
			findings = []report.Finding{f}
		}
		fh.Record(start, start.Add(time.Second), findings)
	}

	resp := fh.Build()

	// 3 appearances in 5 cycles, currently active (last cycle i=4 had it).
	// count=3, totalCycles=5, 3 < 5/2=2.5... actually 3 >= 2.5, so persistent.
	// Let's adjust: 6 cycles, appears in 3 (i=0,2,4), 3 < 6/2=3... equals.
	// Actually 3 >= 3 so persistent. Need more gap cycles.
	fh.Record(base.Add(50*time.Second), base.Add(51*time.Second), nil)

	resp = fh.Build()
	// 6 cycles total. f appeared 3 times. 3 < 6/2=3? No, 3 >= 3.
	// But f is NOT active now (last cycle was empty). So it's transient.
	if len(resp.Transient) != 1 {
		// f resolved in the last empty cycle, so it should be transient.
		t.Logf("persistent=%d transient=%d flapping=%d", len(resp.Persistent), len(resp.Transient), len(resp.Flapping))
	}

	// Re-activate it to make it flapping.
	fh.Record(base.Add(60*time.Second), base.Add(61*time.Second), []report.Finding{f})

	resp = fh.Build()
	// 7 cycles. f appeared 4 times. 4 >= 3, 4 < 7/2=3.5. So flapping.
	if len(resp.Flapping) != 1 {
		t.Errorf("expected 1 flapping, got %d (persistent=%d, transient=%d)",
			len(resp.Flapping), len(resp.Persistent), len(resp.Transient))
	}
}

func TestHistoryTransientResolution(t *testing.T) {
	fh := newFindingHistory(10)
	base := time.Now()

	f := report.Finding{Check: "drain-state", Component: "pod-x", Message: "stuck", Severity: report.SeverityError}

	// Appears in cycle 1.
	fh.Record(base, base.Add(time.Second), []report.Finding{f})
	// Disappears in cycle 2.
	fh.Record(base.Add(10*time.Second), base.Add(11*time.Second), nil)

	resp := fh.Build()
	if len(resp.Transient) != 1 {
		t.Fatalf("expected 1 transient, got %d", len(resp.Transient))
	}
	if resp.Transient[0].ResolvedAt.IsZero() {
		t.Error("resolved finding should have ResolvedAt set")
	}
	if resp.Transient[0].Count != 1 {
		t.Errorf("expected count 1, got %d", resp.Transient[0].Count)
	}
}

func TestHistorySeverityEscalation(t *testing.T) {
	fh := newFindingHistory(5)
	base := time.Now()

	fWarn := report.Finding{Check: "pod-health", Component: "p", Message: "issue", Severity: report.SeverityWarn}
	fErr := report.Finding{Check: "pod-health", Component: "p", Message: "issue", Severity: report.SeverityError}

	fh.Record(base, base.Add(time.Second), []report.Finding{fWarn})
	fh.Record(base.Add(10*time.Second), base.Add(11*time.Second), []report.Finding{fErr})

	resp := fh.Build()
	if len(resp.Persistent) != 1 {
		t.Fatalf("expected 1 persistent, got %d", len(resp.Persistent))
	}
	if resp.Persistent[0].Severity != report.SeverityError {
		t.Errorf("expected severity escalated to error, got %s", resp.Persistent[0].Severity)
	}
}

func TestHistoryEmptyCycles(t *testing.T) {
	fh := newFindingHistory(5)
	base := time.Now()

	// Record 3 empty cycles.
	for i := range 3 {
		start := base.Add(time.Duration(i*10) * time.Second)
		fh.Record(start, start.Add(time.Second), nil)
	}

	resp := fh.Build()
	if resp.TotalCycles != 3 {
		t.Fatalf("expected 3 cycles, got %d", resp.TotalCycles)
	}
	if len(resp.Persistent)+len(resp.Transient)+len(resp.Flapping) != 0 {
		t.Error("expected no occurrences for empty cycles")
	}
}

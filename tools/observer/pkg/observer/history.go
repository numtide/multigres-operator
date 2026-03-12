package observer

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/numtide/multigres-operator/tools/observer/pkg/report"
)

// findingHistory tracks findings across observer cycles using a ring buffer.
// It classifies findings as persistent (active across 3+ cycles), transient
// (appeared then resolved), or flapping (active but with gaps in appearance).
type findingHistory struct {
	mu          sync.RWMutex
	cycles      []report.CycleRecord
	occurrences map[string]*report.FindingOccurrence
	capacity    int
	head        int // next write position
	count       int // total cycles stored (≤ capacity)
}

func newFindingHistory(capacity int) *findingHistory {
	if capacity < 1 {
		capacity = 30
	}
	return &findingHistory{
		cycles:      make([]report.CycleRecord, capacity),
		occurrences: make(map[string]*report.FindingOccurrence),
		capacity:    capacity,
	}
}

// findingKey produces a stable identity for a finding based on its check,
// component, and message. Uses a truncated SHA-256 hash.
func findingKey(f report.Finding) string {
	h := sha256.Sum256([]byte(f.Check + "|" + f.Component + "|" + f.Message))
	return fmt.Sprintf("%x", h[:8])
}

// Record stores a cycle's findings and updates occurrence tracking.
func (fh *findingHistory) Record(cycleStart, cycleEnd time.Time, findings []report.Finding) {
	fh.mu.Lock()
	defer fh.mu.Unlock()

	fh.cycles[fh.head] = report.CycleRecord{
		CycleStart: cycleStart,
		CycleEnd:   cycleEnd,
		Findings:   findings,
	}
	fh.head = (fh.head + 1) % fh.capacity
	if fh.count < fh.capacity {
		fh.count++
	}

	// Track which keys appeared this cycle.
	seen := make(map[string]bool, len(findings))

	for _, f := range findings {
		key := findingKey(f)
		seen[key] = true

		occ, exists := fh.occurrences[key]
		if !exists {
			fh.occurrences[key] = &report.FindingOccurrence{
				Key:       key,
				Check:     f.Check,
				Component: f.Component,
				Message:   f.Message,
				Severity:  f.Severity,
				FirstSeen: cycleEnd,
				LastSeen:  cycleEnd,
				Count:     1,
				Active:    true,
			}
			continue
		}

		occ.Count++
		occ.LastSeen = cycleEnd
		occ.Active = true
		occ.ResolvedAt = time.Time{}
		// Escalate severity if a finding reappears at a higher level.
		if severityRank(f.Severity) > severityRank(occ.Severity) {
			occ.Severity = f.Severity
		}
	}

	// Mark occurrences not seen this cycle as resolved.
	for key, occ := range fh.occurrences {
		if !seen[key] && occ.Active {
			occ.Active = false
			occ.ResolvedAt = cycleEnd
		}
	}

	// Prune resolved occurrences older than the observation window.
	fh.pruneResolved()
}

// Build constructs a HistoryResponse classifying all tracked occurrences.
func (fh *findingHistory) Build() report.HistoryResponse {
	fh.mu.RLock()
	defer fh.mu.RUnlock()

	resp := report.HistoryResponse{
		TotalCycles: fh.count,
		Cycles:      fh.orderedCycles(),
	}

	if len(resp.Cycles) > 0 {
		resp.WindowStart = resp.Cycles[0].CycleStart
		resp.WindowEnd = resp.Cycles[len(resp.Cycles)-1].CycleEnd
	}

	for _, occ := range fh.occurrences {
		o := *occ
		switch {
		case !o.Active:
			resp.Transient = append(resp.Transient, o)
		case o.Count >= 3 && o.Count*4 >= fh.count*3:
			// Present in >= 75% of cycles — persistent.
			resp.Persistent = append(resp.Persistent, o)
		case o.Count >= 3:
			// Present in 3+ cycles but with gaps — flapping.
			resp.Flapping = append(resp.Flapping, o)
		default:
			// Active but fewer than 3 cycles — too early to classify, treat as persistent.
			resp.Persistent = append(resp.Persistent, o)
		}
	}

	return resp
}

// orderedCycles returns cycles in chronological order from the ring buffer.
func (fh *findingHistory) orderedCycles() []report.CycleRecord {
	if fh.count == 0 {
		return nil
	}

	result := make([]report.CycleRecord, 0, fh.count)
	start := 0
	if fh.count == fh.capacity {
		start = fh.head // oldest entry is at head when buffer is full
	}
	for i := range fh.count {
		idx := (start + i) % fh.capacity
		result = append(result, fh.cycles[idx])
	}
	return result
}

// pruneResolved removes resolved occurrences whose lastSeen is older than
// the oldest cycle still in the buffer. This prevents unbounded growth.
func (fh *findingHistory) pruneResolved() {
	if fh.count == 0 {
		return
	}

	// Find the oldest cycle's start time.
	oldestIdx := 0
	if fh.count == fh.capacity {
		oldestIdx = fh.head
	}
	cutoff := fh.cycles[oldestIdx].CycleStart

	for key, occ := range fh.occurrences {
		if !occ.Active && occ.LastSeen.Before(cutoff) {
			delete(fh.occurrences, key)
		}
	}
}

func severityRank(s report.Severity) int {
	switch s {
	case report.SeverityInfo:
		return 0
	case report.SeverityWarn:
		return 1
	case report.SeverityError:
		return 2
	case report.SeverityFatal:
		return 3
	default:
		return 0
	}
}

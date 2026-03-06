package observer

import (
	"maps"
	"sync"
)

// ProbeResult captures a single connectivity probe outcome.
type ProbeResult struct {
	Check     string `json:"check"`
	Component string `json:"component"`
	Target    string `json:"target"`
	OK        bool   `json:"ok"`
	Latency   string `json:"latency,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ProbeCollector accumulates raw probe data during a single observer cycle.
type ProbeCollector struct {
	mu   sync.Mutex
	data map[string]any
}

func newProbeCollector() *ProbeCollector {
	return &ProbeCollector{data: make(map[string]any)}
}

// Set stores probe data for a given check.
func (pc *ProbeCollector) Set(check string, value any) {
	pc.mu.Lock()
	pc.data[check] = value
	pc.mu.Unlock()
}

// RecordProbe appends a connectivity probe result.
func (pc *ProbeCollector) RecordProbe(r ProbeResult) {
	pc.mu.Lock()
	results, _ := pc.data["connectivity"].([]ProbeResult)
	pc.data["connectivity"] = append(results, r)
	pc.mu.Unlock()
}

// Data returns a shallow copy of the collected probe data.
func (pc *ProbeCollector) Data() map[string]any {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	out := make(map[string]any, len(pc.data))
	maps.Copy(out, pc.data)
	return out
}

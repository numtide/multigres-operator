package report

import "time"

// Severity represents the importance of an observer finding.
type Severity string

const (
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
	SeverityFatal Severity = "fatal"
)

// Finding is a single observation from an observer check cycle.
type Finding struct {
	Timestamp time.Time      `json:"ts"`
	Severity  Severity       `json:"level"`
	Check     string         `json:"check"`
	Component string         `json:"component,omitempty"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
}

// Summary aggregates findings from a single observer cycle.
type Summary struct {
	CycleStart time.Time `json:"cycleStart"`
	CycleEnd   time.Time `json:"cycleEnd"`
	Counts     struct {
		Info  int `json:"info"`
		Warn  int `json:"warn"`
		Error int `json:"error"`
		Fatal int `json:"fatal"`
	} `json:"counts"`
	TotalFindings int `json:"totalFindings"`
}

// StatusResponse is the JSON body returned by the /api/status endpoint.
type StatusResponse struct {
	Summary  Summary        `json:"summary"`
	Healthy  map[string]bool `json:"healthy"`
	Findings []Finding      `json:"findings"`
	Probes   map[string]any `json:"probes"`
	Coverage CoverageInfo   `json:"coverage"`
}

// CoverageInfo describes what the observer was able to check this cycle.
type CoverageInfo struct {
	SQLProbeEnabled bool     `json:"sqlProbeEnabled"`
	ChecksRun       []string `json:"checksRun"`
	Namespace       string   `json:"namespace"`
}

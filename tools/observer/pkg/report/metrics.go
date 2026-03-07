package report

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds Prometheus metrics for the observer.
type Metrics struct {
	findings      *prometheus.CounterVec
	cycleDur      prometheus.Histogram
	cycleTotal    prometheus.Counter
	CheckHealthy  *prometheus.GaugeVec
	PodRestarts   *prometheus.CounterVec
	PodReady      *prometheus.GaugeVec
	ProbeLatency  *prometheus.HistogramVec
	CheckDuration *prometheus.HistogramVec
}

// NewMetrics registers and returns Prometheus metrics for the observer.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		findings: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multigres_observer",
			Subsystem: "observer",
			Name:      "findings_total",
			Help:      "Total observer findings by check and severity.",
		}, []string{"check", "severity"}),
		cycleDur: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "multigres_observer",
			Subsystem: "observer",
			Name:      "cycle_duration_seconds",
			Help:      "Duration of each observer check cycle.",
			Buckets:   prometheus.DefBuckets,
		}),
		cycleTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "multigres_observer",
			Subsystem: "observer",
			Name:      "cycles_total",
			Help:      "Total number of observer check cycles completed.",
		}),
		CheckHealthy: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "multigres_observer",
			Name:      "check_healthy",
			Help:      "Whether a check passed without errors (1=healthy, 0=unhealthy).",
		}, []string{"check"}),
		PodRestarts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multigres_observer",
			Name:      "pod_restarts_total",
			Help:      "Total observed container restarts by pod and container.",
		}, []string{"pod", "container"}),
		PodReady: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "multigres_observer",
			Name:      "pod_ready",
			Help:      "Whether a pod is ready (1=ready, 0=not ready).",
		}, []string{"pod", "namespace"}),
		ProbeLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "multigres_observer",
			Name:      "probe_latency_seconds",
			Help:      "Latency of connectivity probes by check and target.",
			Buckets:   []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		}, []string{"check", "target"}),
		CheckDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "multigres_observer",
			Name:      "check_duration_seconds",
			Help:      "Duration of individual observer checks.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"check"}),
	}

	reg.MustRegister(m.findings, m.cycleDur, m.cycleTotal,
		m.CheckHealthy, m.PodRestarts, m.PodReady, m.ProbeLatency, m.CheckDuration)
	return m
}

// RecordFinding increments the findings counter for the given check and severity.
func (m *Metrics) RecordFinding(f Finding) {
	m.findings.With(prometheus.Labels{
		"check":    f.Check,
		"severity": string(f.Severity),
	}).Inc()
}

// RecordCycle records the duration of one observer cycle.
func (m *Metrics) RecordCycle(d time.Duration) {
	m.cycleDur.Observe(d.Seconds())
	m.cycleTotal.Inc()
}

// SetCheckHealthy sets the health gauge for a check (1=healthy, 0=unhealthy).
func (m *Metrics) SetCheckHealthy(check string, healthy bool) {
	v := float64(0)
	if healthy {
		v = 1
	}
	m.CheckHealthy.With(prometheus.Labels{"check": check}).Set(v)
}

// RecordProbeLatency records the latency of a connectivity probe.
func (m *Metrics) RecordProbeLatency(check, target string, d time.Duration) {
	m.ProbeLatency.With(prometheus.Labels{"check": check, "target": target}).Observe(d.Seconds())
}

// RecordPodRestart increments the pod restart counter.
func (m *Metrics) RecordPodRestart(pod, container string, count int32) {
	m.PodRestarts.With(prometheus.Labels{"pod": pod, "container": container}).Add(float64(count))
}

// RecordPodReady sets the pod readiness gauge.
func (m *Metrics) RecordPodReady(pod, namespace string, ready bool) {
	v := float64(0)
	if ready {
		v = 1
	}
	m.PodReady.With(prometheus.Labels{"pod": pod, "namespace": namespace}).Set(v)
}

// RecordCheckDuration records the duration of an individual check.
func (m *Metrics) RecordCheckDuration(check string, d time.Duration) {
	m.CheckDuration.With(prometheus.Labels{"check": check}).Observe(d.Seconds())
}

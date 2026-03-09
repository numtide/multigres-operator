package observer

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/numtide/multigres-operator/tools/observer/pkg/common"
	"github.com/numtide/multigres-operator/tools/observer/pkg/report"
)

var allCheckNames = []string{
	"pod-health", "resource-validation", "crd-status", "drain-state",
	"connectivity", "operator-logs", "dataplane-logs", "events",
	"topology", "replication",
}

// podInfo tracks per-pod metadata used by the startup grace period logic.
type podInfo struct {
	createdAt time.Time
	ready     bool
}

// Observer runs continuous health validation checks against a multigres cluster.
type Observer struct {
	client            client.Client
	clientset         kubernetes.Interface
	httpClient        *http.Client
	reporter          *report.Reporter
	metrics           *report.Metrics
	namespace         string
	operatorNamespace string
	interval          time.Duration
	logger            *slog.Logger
	logTailLines      int
	enableSQLProbe    bool

	// Restart tracking: "namespace/pod/container" → last seen restartCount.
	prevRestarts map[string]int32

	// Pod phase tracking: "namespace/pod" → when first seen in current phase.
	podPhaseSince map[string]time.Time

	// Drain state tracking: "namespace/pod" → when current drain state was first seen.
	drainStateSince map[string]time.Time

	// Previous drain states: "namespace/pod" → last observed drain state.
	prevDrainState map[string]string

	// Generation divergence tracking: "kind/namespace/name" → when divergence started.
	generationDivergeSince map[string]time.Time

	// Primary role violation tracking: "pool-cell" → when violation started.
	primaryViolationSince map[string]time.Time

	// Log tracking: last time logs were checked.
	lastLogCheck time.Time

	// Event tracking: last event resource version seen.
	lastEventResourceVersion string

	// Pool pod startup info: populated each cycle by checkPodHealth.
	// Used by downstream checks to suppress transient findings from newly created pods.
	podStartup map[string]podInfo

	// Known objects: populated each cycle by earlier checks for event filtering.
	// Maps pod name → true for all currently-existing multigres-managed pods.
	knownPodNames map[string]bool

	// Event deduplication: tracks the last seen count for each event UID.
	seenEventCounts map[types.UID]int32

	// Per-cycle probe collector; replaced each cycle.
	probes *ProbeCollector

	// Latest cycle snapshot for the /api/status endpoint.
	snap snapshot
}

// Config holds the configuration for creating an Observer.
type Config struct {
	Client            client.Client
	Clientset         kubernetes.Interface
	Reporter          *report.Reporter
	Metrics           *report.Metrics
	Namespace         string
	OperatorNamespace string
	Interval          time.Duration
	Logger            *slog.Logger
	LogTailLines      int
	EnableSQLProbe    bool
}

// New creates an Observer from the provided configuration.
func New(cfg Config) *Observer {
	return &Observer{
		client:            cfg.Client,
		clientset:         cfg.Clientset,
		httpClient:        &http.Client{Timeout: 5 * time.Second},
		reporter:          cfg.Reporter,
		metrics:           cfg.Metrics,
		namespace:         cfg.Namespace,
		operatorNamespace: cfg.OperatorNamespace,
		interval:          cfg.Interval,
		logger:            cfg.Logger,
		logTailLines:      cfg.LogTailLines,
		enableSQLProbe:    cfg.EnableSQLProbe,

		prevRestarts:           make(map[string]int32),
		podPhaseSince:          make(map[string]time.Time),
		drainStateSince:        make(map[string]time.Time),
		prevDrainState:         make(map[string]string),
		generationDivergeSince: make(map[string]time.Time),
		primaryViolationSince:  make(map[string]time.Time),
		podStartup:             make(map[string]podInfo),
		knownPodNames:          make(map[string]bool),
		seenEventCounts:        make(map[types.UID]int32),
	}
}

// listOpts returns client.ListOptions scoped to the observer's namespace.
// When namespace is empty, it returns no namespace filter (cluster-scoped listing).
func (o *Observer) listOpts(extraOpts ...client.ListOption) []client.ListOption {
	var opts []client.ListOption
	if o.namespace != "" {
		opts = append(opts, client.InNamespace(o.namespace))
	}
	return append(opts, extraOpts...)
}

// isPodInGracePeriod returns true if a pool pod was created less than
// PodStartupGracePeriod ago. Findings for such pods are suppressed entirely
// because startup errors (connection refused, no WAL receiver, async standby)
// are expected and resolve once the pod finishes joining.
func (o *Observer) isPodInGracePeriod(podName string) bool {
	info, ok := o.podStartup[podName]
	if !ok {
		return false
	}
	return time.Since(info.createdAt) < common.PodStartupGracePeriod
}

// effectiveSeverity downgrades error/fatal findings to warn for pool pods that
// are past the grace period but still not Ready. Pods that Kubernetes marks as
// Ready are reported at full severity — those are the high-value findings that
// reveal issues invisible to kubectl and the operator.
func (o *Observer) effectiveSeverity(podName string, sev report.Severity) report.Severity {
	info, ok := o.podStartup[podName]
	if !ok {
		return sev
	}
	if info.ready {
		return sev
	}
	if sev == report.SeverityError || sev == report.SeverityFatal {
		return report.SeverityWarn
	}
	return sev
}

// hasAnyPodInGracePeriod returns true if any pool pod is younger than the
// startup grace period. Used to downgrade findings from non-pool components
// (e.g., multiorch logging "connection refused" because a pool pod is still starting).
func (o *Observer) hasAnyPodInGracePeriod() bool {
	for _, info := range o.podStartup {
		if time.Since(info.createdAt) < common.PodStartupGracePeriod {
			return true
		}
	}
	return false
}

// Run starts the observer loop, running all checks every interval until ctx is cancelled.
func (o *Observer) Run(ctx context.Context) error {
	o.logger.Info("starting observer", "namespace", o.namespace, "interval", o.interval)

	// Run first cycle immediately.
	o.runCycle(ctx)

	ticker := time.NewTicker(o.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			o.logger.Info("observer stopped")
			return ctx.Err()
		case <-ticker.C:
			o.runCycle(ctx)
		}
	}
}

func (o *Observer) runCycle(ctx context.Context) {
	start := time.Now()
	o.logger.Debug("starting observer cycle")

	o.probes = newProbeCollector()

	checksRun := make([]string, 0, len(allCheckNames))
	track := func(name string, fn func(context.Context)) {
		checkStart := time.Now()
		fn(ctx)
		checksRun = append(checksRun, name)
		if o.metrics != nil {
			o.metrics.RecordCheckDuration(name, time.Since(checkStart))
		}
	}

	// Reset known objects — populated by checkPodHealth for event filtering.
	clear(o.knownPodNames)
	clear(o.podStartup)

	track("pod-health", o.checkPodHealth)
	track("resource-validation", o.checkResources)
	track("crd-status", o.checkCRDStatus)
	track("drain-state", o.checkDrainState)
	track("connectivity", o.checkConnectivity)
	track("logs", o.checkLogs)
	track("events", o.checkEvents)
	track("topology", o.checkTopology)
	track("replication", o.checkReplication)

	dur := time.Since(start)
	if o.metrics != nil {
		o.metrics.RecordCycle(dur)
	}

	findings, s, checkHealthy := o.reporter.SummaryWithFindings()
	s.CycleStart = start
	s.CycleEnd = time.Now()

	if o.metrics != nil {
		for _, check := range allCheckNames {
			healthy, ok := checkHealthy[check]
			if !ok {
				healthy = true
			}
			o.metrics.SetCheckHealthy(check, healthy)
		}
	}

	o.snap.Store(&report.StatusResponse{
		Summary:  s,
		Healthy:  checkHealthy,
		Findings: findings,
		Probes:   o.probes.Data(),
		Coverage: report.CoverageInfo{
			SQLProbeEnabled: o.enableSQLProbe,
			ChecksRun:       checksRun,
			Namespace:       o.namespace,
		},
	})

	o.logger.Info("observer cycle complete",
		"duration", dur.Round(time.Millisecond),
		"findings", s.TotalFindings,
		"errors", s.Counts.Error,
		"fatals", s.Counts.Fatal,
	)
}

// StatusHandler returns an http.HandlerFunc that serves the latest cycle snapshot as JSON.
func (o *Observer) StatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := o.snap.Load()
		if data == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"no cycle completed yet"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	}
}

package observer

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/tools/observer/pkg/common"
	"github.com/numtide/multigres-operator/tools/observer/pkg/report"
)

func newTestObserver(objects ...runtime.Object) *Observer {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, obj := range objects {
		builder = builder.WithRuntimeObjects(obj)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return &Observer{
		client:                 builder.Build(),
		reporter:               report.NewReporter(logger, nil),
		logger:                 logger,
		namespace:              "test-ns",
		prevRestarts:           make(map[string]int32),
		podPhaseSince:          make(map[string]time.Time),
		drainStateSince:        make(map[string]time.Time),
		prevDrainState:         make(map[string]string),
		generationDivergeSince: make(map[string]time.Time),
		primaryViolationSince:  make(map[string]time.Time),
		prevPhase:              make(map[string]string),
		progressingSince:       make(map[string]time.Time),
		podStartup:             make(map[string]podInfo),
		knownPodNames:          make(map[string]bool),
	}
}

func collectFindings(o *Observer) []report.Finding {
	findings, _, _ := o.reporter.SummaryWithFindings()
	return findings
}

func TestCheckStatusMessage(t *testing.T) {
	o := newTestObserver()

	// Degraded with empty message -> warn.
	o.checkStatusMessage("shard/test-ns/s1", multigresv1alpha1.PhaseDegraded, "", false)
	findings := collectFindings(o)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for empty message on Degraded, got %d", len(findings))
	}
	if findings[0].Severity != report.SeverityWarn {
		t.Errorf("expected warn, got %s", findings[0].Severity)
	}

	// Degraded with message -> no finding.
	o.checkStatusMessage("shard/test-ns/s1", multigresv1alpha1.PhaseDegraded, "pods crashing", false)
	findings = collectFindings(o)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for non-empty message, got %d", len(findings))
	}

	// Healthy with empty message -> no finding.
	o.checkStatusMessage("shard/test-ns/s1", multigresv1alpha1.PhaseHealthy, "", false)
	findings = collectFindings(o)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for Healthy with empty message, got %d", len(findings))
	}

	// Unknown with empty message -> warn.
	o.checkStatusMessage("shard/test-ns/s1", multigresv1alpha1.PhaseUnknown, "", false)
	findings = collectFindings(o)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for Unknown empty message, got %d", len(findings))
	}

	// Deleting -> no finding regardless.
	o.checkStatusMessage("shard/test-ns/s1", multigresv1alpha1.PhaseDegraded, "", true)
	findings = collectFindings(o)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when deleting, got %d", len(findings))
	}
}

func TestCheckPhaseProgressionStuck(t *testing.T) {
	o := newTestObserver()
	comp := "shard/test-ns/s1"

	// First call: starts tracking, no finding.
	o.checkPhase(comp, multigresv1alpha1.PhaseProgressing, false)
	findings := collectFindings(o)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings on first Progressing, got %d", len(findings))
	}

	// Simulate being stuck by backdating the progressingSince entry.
	o.progressingSince[comp] = time.Now().Add(-11 * time.Minute)

	o.checkPhase(comp, multigresv1alpha1.PhaseProgressing, false)
	findings = collectFindings(o)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for stuck Progressing, got %d", len(findings))
	}
	if findings[0].Severity != report.SeverityWarn {
		t.Errorf("expected warn for stuck Progressing, got %s", findings[0].Severity)
	}

	// Transition to Healthy -> clears tracking.
	o.checkPhase(comp, multigresv1alpha1.PhaseHealthy, false)
	_ = collectFindings(o)
	if _, ok := o.progressingSince[comp]; ok {
		t.Error("progressingSince should be cleared after Healthy")
	}
}

func TestCheckPhaseTransitionInvalid(t *testing.T) {
	o := newTestObserver()
	comp := "shard/test-ns/s1"

	// Set previous phase to Healthy.
	o.checkPhase(comp, multigresv1alpha1.PhaseHealthy, false)
	_ = collectFindings(o)

	// Transition Healthy -> Initializing -> fatal.
	o.checkPhase(comp, multigresv1alpha1.PhaseInitializing, false)
	findings := collectFindings(o)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for invalid transition, got %d", len(findings))
	}
	if findings[0].Severity != report.SeverityFatal {
		t.Errorf("expected fatal for Healthy->Initializing, got %s", findings[0].Severity)
	}

	// Valid transition: Progressing -> Healthy should NOT produce invalid transition finding.
	o.prevPhase[comp] = string(multigresv1alpha1.PhaseProgressing)
	o.checkPhase(comp, multigresv1alpha1.PhaseHealthy, false)
	findings = collectFindings(o)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for valid transition, got %d", len(findings))
	}
}

func TestCheckBackupStaleness(t *testing.T) {
	o := newTestObserver()

	freshTime := metav1.NewTime(time.Now().Add(-1 * time.Hour))
	warnTime := metav1.NewTime(time.Now().Add(-26 * time.Hour))
	errorTime := metav1.NewTime(time.Now().Add(-50 * time.Hour))

	// No backup condition -> no findings.
	shardNoBackup := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "test-ns"},
		Status:     multigresv1alpha1.ShardStatus{},
	}
	o.checkBackupStaleness(shardNoBackup, "shard/test-ns/s1")
	findings := collectFindings(o)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings without backup condition, got %d", len(findings))
	}

	backupCondition := metav1.Condition{Type: "BackupHealthy", Status: "True"}

	// Backup configured but never completed -> warn.
	shardNoTime := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{Name: "s2", Namespace: "test-ns"},
		Status: multigresv1alpha1.ShardStatus{
			Conditions: []metav1.Condition{backupCondition},
		},
	}
	o.checkBackupStaleness(shardNoTime, "shard/test-ns/s2")
	findings = collectFindings(o)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for never completed, got %d", len(findings))
	}
	if findings[0].Severity != report.SeverityWarn {
		t.Errorf("expected warn, got %s", findings[0].Severity)
	}

	// Fresh backup -> no findings.
	shardFresh := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{Name: "s3", Namespace: "test-ns"},
		Status: multigresv1alpha1.ShardStatus{
			Conditions:     []metav1.Condition{backupCondition},
			LastBackupTime: &freshTime,
			LastBackupType: "full",
		},
	}
	o.checkBackupStaleness(shardFresh, "shard/test-ns/s3")
	findings = collectFindings(o)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for fresh backup, got %d", len(findings))
	}

	// Warn-age backup -> warn.
	shardWarn := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{Name: "s4", Namespace: "test-ns"},
		Status: multigresv1alpha1.ShardStatus{
			Conditions:     []metav1.Condition{backupCondition},
			LastBackupTime: &warnTime,
			LastBackupType: "diff",
		},
	}
	o.checkBackupStaleness(shardWarn, "shard/test-ns/s4")
	findings = collectFindings(o)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for warn-age backup, got %d", len(findings))
	}
	if findings[0].Severity != report.SeverityWarn {
		t.Errorf("expected warn, got %s", findings[0].Severity)
	}

	// Error-age backup -> error.
	shardError := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{Name: "s5", Namespace: "test-ns"},
		Status: multigresv1alpha1.ShardStatus{
			Conditions:     []metav1.Condition{backupCondition},
			LastBackupTime: &errorTime,
			LastBackupType: "incr",
		},
	}
	o.checkBackupStaleness(shardError, "shard/test-ns/s5")
	findings = collectFindings(o)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for error-age backup, got %d", len(findings))
	}
	if findings[0].Severity != report.SeverityError {
		t.Errorf("expected error, got %s", findings[0].Severity)
	}

	// Unknown backup type -> warn.
	shardBadType := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{Name: "s6", Namespace: "test-ns"},
		Status: multigresv1alpha1.ShardStatus{
			Conditions:     []metav1.Condition{backupCondition},
			LastBackupTime: &freshTime,
			LastBackupType: "bogus",
		},
	}
	o.checkBackupStaleness(shardBadType, "shard/test-ns/s6")
	findings = collectFindings(o)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for unknown backup type, got %d", len(findings))
	}
}

func TestCheckCellStatusFields(t *testing.T) {
	o := newTestObserver()
	ctx := context.Background()

	// ReadyReplicas > Replicas -> error.
	cellBad := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "c1",
			Namespace: "test-ns",
			Labels:    map[string]string{common.LabelMultigresCell: "zone-a"},
		},
		Status: multigresv1alpha1.CellStatus{
			GatewayReplicas:      2,
			GatewayReadyReplicas: 5,
			Phase:                multigresv1alpha1.PhaseHealthy,
			GatewayServiceName:   "gw-svc",
		},
	}
	o.checkCellStatusFields(ctx, cellBad)
	findings := collectFindings(o)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for ready > total, got %d", len(findings))
	}
	if findings[0].Severity != report.SeverityError {
		t.Errorf("expected error, got %s", findings[0].Severity)
	}

	// Healthy with empty GatewayServiceName -> warn.
	cellNoSvc := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "c2",
			Namespace: "test-ns",
			Labels:    map[string]string{common.LabelMultigresCell: "zone-b"},
		},
		Status: multigresv1alpha1.CellStatus{
			GatewayReplicas:      2,
			GatewayReadyReplicas: 2,
			Phase:                multigresv1alpha1.PhaseHealthy,
			GatewayServiceName:   "",
		},
	}
	o.checkCellStatusFields(ctx, cellNoSvc)
	findings = collectFindings(o)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for empty service name, got %d", len(findings))
	}
	if findings[0].Severity != report.SeverityWarn {
		t.Errorf("expected warn, got %s", findings[0].Severity)
	}

	// Normal cell -> no findings.
	cellOK := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "c3",
			Namespace: "test-ns",
			Labels:    map[string]string{common.LabelMultigresCell: "zone-c"},
		},
		Status: multigresv1alpha1.CellStatus{
			GatewayReplicas:      3,
			GatewayReadyReplicas: 3,
			Phase:                multigresv1alpha1.PhaseHealthy,
			GatewayServiceName:   "gw-zone-c",
		},
	}
	o.checkCellStatusFields(ctx, cellOK)
	findings = collectFindings(o)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for normal cell, got %d", len(findings))
	}
}

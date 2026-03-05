package observer

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/numtide/multigres-operator/tools/chaos/pkg/common"
	"github.com/numtide/multigres-operator/tools/chaos/pkg/report"
)

// errorPattern defines a log pattern to watch for with its severity.
type errorPattern struct {
	substring string
	severity  report.Severity
	check     string
}

var operatorErrorPatterns = []errorPattern{
	{"error reconciling", report.SeverityError, "operator-logs"},
	{"failed to", report.SeverityError, "operator-logs"},
	{"stuck in Terminating", report.SeverityError, "operator-logs"},
	{"topology error", report.SeverityError, "operator-logs"},
	{"status error", report.SeverityError, "operator-logs"},
	{"panic", report.SeverityFatal, "operator-logs"},
	{"runtime error", report.SeverityFatal, "operator-logs"},
	{"backup stale", report.SeverityWarn, "operator-logs"},
	{"pod replaced", report.SeverityWarn, "operator-logs"},
	{"config error", report.SeverityWarn, "operator-logs"},
	{"expand PVC failed", report.SeverityWarn, "operator-logs"},
}

var dataPlaneErrorPatterns = []errorPattern{
	{"connection refused", report.SeverityError, "dataplane-logs"},
	{"connection reset", report.SeverityError, "dataplane-logs"},
	{"topology registration", report.SeverityError, "dataplane-logs"},
	{"replication error", report.SeverityError, "dataplane-logs"},
	{"FATAL", report.SeverityError, "dataplane-logs"},
	{"panic", report.SeverityFatal, "dataplane-logs"},
	{"OOM", report.SeverityError, "dataplane-logs"},
	{"out of memory", report.SeverityError, "dataplane-logs"},
}

func (o *Observer) checkLogs(ctx context.Context) {
	sinceSeconds := int64(o.interval.Seconds()) + 5
	if o.lastLogCheck.IsZero() {
		sinceSeconds = 30
	}
	o.lastLogCheck = time.Now()

	// Check operator logs.
	o.checkOperatorLogs(ctx, sinceSeconds)

	// Check data plane component logs (pool pods, multigateway, multiorch, etcd).
	o.checkDataPlaneLogs(ctx, sinceSeconds)
}

func (o *Observer) checkOperatorLogs(ctx context.Context, sinceSeconds int64) {
	var pods corev1.PodList
	if err := o.client.List(ctx, &pods,
		client.InNamespace(o.operatorNamespace),
		client.MatchingLabels{"control-plane": "controller-manager"},
	); err != nil {
		return
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		o.scanPodLogs(ctx, pod, "manager", sinceSeconds, operatorErrorPatterns)
	}
}

func (o *Observer) checkDataPlaneLogs(ctx context.Context, sinceSeconds int64) {
	var pods corev1.PodList
	if err := o.client.List(ctx, &pods,
		o.listOpts(client.MatchingLabels{common.LabelAppManagedBy: common.ManagedByMultigres})...,
	); err != nil {
		return
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		comp := pod.Labels[common.LabelAppComponent]
		switch comp {
		case common.ComponentPool:
			o.scanPodLogs(ctx, pod, "multipooler", sinceSeconds, dataPlaneErrorPatterns)
			o.scanPodLogs(ctx, pod, "postgres", sinceSeconds, dataPlaneErrorPatterns)
		case common.ComponentMultiGateway:
			o.scanPodLogs(ctx, pod, "", sinceSeconds, dataPlaneErrorPatterns)
		case common.ComponentMultiOrch:
			o.scanPodLogs(ctx, pod, "", sinceSeconds, dataPlaneErrorPatterns)
		case common.ComponentGlobalTopo:
			o.scanPodLogs(ctx, pod, "", sinceSeconds, dataPlaneErrorPatterns)
		}
	}
}

func (o *Observer) scanPodLogs(ctx context.Context, pod *corev1.Pod, container string, sinceSeconds int64, patterns []errorPattern) {
	opts := &corev1.PodLogOptions{
		SinceSeconds: &sinceSeconds,
	}
	if container != "" {
		opts.Container = container
	}

	req := o.clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		// Don't report log access errors for pods that may be initializing.
		if pod.Status.Phase == corev1.PodRunning {
			o.logger.Debug("failed to stream logs", "pod", pod.Name, "container", container, "error", err)
		}
		return
	}
	defer stream.Close()

	component := componentForPod(pod)
	if container != "" {
		component += "/" + container
	}

	scanner := bufio.NewScanner(stream)
	// Limit individual line size to prevent memory issues.
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	lineCount := 0
	maxLines := o.logTailLines
	if maxLines <= 0 {
		maxLines = 100
	}

	for scanner.Scan() {
		lineCount++
		if lineCount > maxLines {
			break
		}

		line := scanner.Text()
		for _, p := range patterns {
			if strings.Contains(strings.ToLower(line), strings.ToLower(p.substring)) {
				o.reporter.Report(report.Finding{
					Severity:  p.severity,
					Check:     p.check,
					Component: component,
					Message:   fmt.Sprintf("Error pattern %q detected in %s/%s", p.substring, pod.Name, container),
					Details: map[string]any{
						"pod":       pod.Name,
						"container": container,
						"pattern":   p.substring,
						"line":      truncate(line, 500),
					},
				})
				break
			}
		}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

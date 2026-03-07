package observer

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/numtide/multigres-operator/tools/observer/pkg/common"
	"github.com/numtide/multigres-operator/tools/observer/pkg/report"
)

// warningEventReasons maps operator-emitted warning event reasons to their expected severity.
var warningEventReasons = map[string]report.Severity{
	common.EventReasonBackupStale:      report.SeverityWarn,
	common.EventReasonConfigError:      report.SeverityError,
	common.EventReasonExpandPVCFailed:  report.SeverityError,
	common.EventReasonPodReplaced:      report.SeverityWarn,
	common.EventReasonStatusError:      report.SeverityError,
	common.EventReasonStuckTerminating: report.SeverityError,
	common.EventReasonTopologyError:    report.SeverityError,
}

// kubeWarningReasons are Kubernetes-native warning events that indicate problems.
var kubeWarningReasons = map[string]report.Severity{
	"FailedScheduling":     report.SeverityError,
	"FailedMount":          report.SeverityError,
	"Unhealthy":            report.SeverityWarn,
	"BackOff":              report.SeverityError,
	"OOMKilling":           report.SeverityFatal,
	"EvictionThresholdMet": report.SeverityWarn,
}

func (o *Observer) checkEvents(ctx context.Context) {
	opts := metav1.ListOptions{}
	if o.lastEventResourceVersion != "" {
		opts.ResourceVersion = o.lastEventResourceVersion
	}

	events, err := o.clientset.CoreV1().Events(o.namespace).List(ctx, opts)
	if err != nil {
		o.reporter.Report(report.Finding{
			Severity: report.SeverityWarn,
			Check:    "events",
			Message:  fmt.Sprintf("failed to list events: %v", err),
		})
		return
	}

	if events.ResourceVersion != "" {
		o.lastEventResourceVersion = events.ResourceVersion
	}

	currentUIDs := make(map[types.UID]bool)

	for i := range events.Items {
		event := &events.Items[i]
		currentUIDs[event.UID] = true
		o.processEvent(event)
	}

	// Prune events that have expired from the Kubernetes API
	for uid := range o.seenEventCounts {
		if !currentUIDs[uid] {
			delete(o.seenEventCounts, uid)
		}
	}
}

func (o *Observer) processEvent(event *corev1.Event) {
	// Deduplicate events by only reporting when the occurrence count increases
	if count, ok := o.seenEventCounts[event.UID]; ok && count >= event.Count {
		return
	}
	o.seenEventCounts[event.UID] = event.Count
	if event.Type == corev1.EventTypeNormal {
		// Track normal events as info for audit trail.
		switch event.Reason {
		case "Synced", "DrainStarted", "DrainCompleted", "ReadyForDeletion",
			"BackupHealthy", "FilesystemResize", "PoolersPruned":
			o.reporter.Report(report.Finding{
				Severity:  report.SeverityInfo,
				Check:     "events",
				Component: eventComponent(event),
				Message:   fmt.Sprintf("[%s] %s: %s", event.Reason, event.InvolvedObject.Name, event.Message),
			})
		}
		return
	}

	if event.Type != corev1.EventTypeWarning {
		return
	}

	// Check operator warning events.
	if severity, ok := warningEventReasons[event.Reason]; ok {
		o.reporter.Report(report.Finding{
			Severity:  severity,
			Check:     "events",
			Component: eventComponent(event),
			Message:   fmt.Sprintf("[%s] %s: %s", event.Reason, event.InvolvedObject.Name, event.Message),
			Details: map[string]any{
				"reason": event.Reason,
				"object": event.InvolvedObject.Name,
				"kind":   event.InvolvedObject.Kind,
				"count":  event.Count,
			},
		})
		return
	}

	// Check Kubernetes-native warning events.
	if severity, ok := kubeWarningReasons[event.Reason]; ok {
		// Only report kube events for multigres-managed resources.
		if !isMultigresEvent(event) {
			return
		}
		// Skip events for pods that no longer exist (stale events from deleted clusters
		// remain in the K8s API until their TTL expires, typically 1 hour).
		if event.InvolvedObject.Kind == "Pod" && !o.knownPodNames[event.InvolvedObject.Name] {
			return
		}
		o.reporter.Report(report.Finding{
			Severity:  severity,
			Check:     "events",
			Component: eventComponent(event),
			Message:   fmt.Sprintf("[%s] %s: %s", event.Reason, event.InvolvedObject.Name, event.Message),
			Details: map[string]any{
				"reason": event.Reason,
				"object": event.InvolvedObject.Name,
				"kind":   event.InvolvedObject.Kind,
				"count":  event.Count,
			},
		})
	}
}

func eventComponent(event *corev1.Event) string {
	return fmt.Sprintf("%s/%s", event.InvolvedObject.Kind, event.InvolvedObject.Name)
}

func isMultigresEvent(event *corev1.Event) bool {
	// Check if the event's reporting component is the multigres operator
	// or if the involved object belongs to multigres API group.
	if event.ReportingController == "multigres-operator" {
		return true
	}
	apiGroup := event.InvolvedObject.APIVersion
	if apiGroup == "multigres.com/v1alpha1" {
		return true
	}
	// For pod events, we can't easily determine if it's multigres-managed
	// without looking up the pod. Include all pod events in the namespace.
	return event.InvolvedObject.Kind == "Pod"
}

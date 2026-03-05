package observer

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/tools/chaos/pkg/common"
	"github.com/numtide/multigres-operator/tools/chaos/pkg/report"
)

func (o *Observer) checkPodHealth(ctx context.Context) {
	var pods corev1.PodList
	if err := o.client.List(ctx, &pods,
		o.listOpts(client.MatchingLabels{common.LabelAppManagedBy: common.ManagedByMultigres})...,
	); err != nil {
		o.reporter.Report(report.Finding{
			Severity: report.SeverityError,
			Check:    "pod-health",
			Message:  fmt.Sprintf("failed to list managed pods: %v", err),
		})
		return
	}

	// Also check the operator pod in the operator namespace.
	o.checkOperatorPod(ctx)

	activePods := make(map[string]bool)
	for i := range pods.Items {
		pod := &pods.Items[i]
		key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
		activePods[key] = true

		o.checkPodPhase(pod, key)
		o.checkContainerReadiness(pod, key)
		o.checkRestarts(pod)
		o.checkTerminating(pod, key)

		if o.metrics != nil {
			ready := pod.Status.Phase == corev1.PodRunning && isPodReady(pod)
			o.metrics.RecordPodReady(pod.Name, pod.Namespace, ready)
		}
	}

	// Clean up tracking state for pods that no longer exist.
	// Keys may be "ns/pod" (phase), "ns/pod/container" (readiness),
	// or "ns/pod/terminating" (termination tracking).
	for key := range o.podPhaseSince {
		// Find the "ns/pod" prefix by matching against known active pods.
		owned := false
		for podKey := range activePods {
			if key == podKey || strings.HasPrefix(key, podKey+"/") {
				owned = true
				break
			}
		}
		if !owned {
			delete(o.podPhaseSince, key)
		}
	}

	o.checkPodCounts(ctx, &pods)
}

func (o *Observer) checkPodPhase(pod *corev1.Pod, key string) {
	now := time.Now()
	phase := pod.Status.Phase

	switch {
	case phase == corev1.PodRunning || phase == corev1.PodSucceeded:
		delete(o.podPhaseSince, key)
		return

	case phase == corev1.PodPending:
		since, tracked := o.podPhaseSince[key]
		if !tracked {
			o.podPhaseSince[key] = now
			return
		}
		if now.Sub(since) > common.PendingTimeout {
			o.reporter.Report(report.Finding{
				Severity:  report.SeverityError,
				Check:     "pod-health",
				Component: componentForPod(pod),
				Message:   fmt.Sprintf("Pod %s stuck in Pending for %s", pod.Name, now.Sub(since).Round(time.Second)),
				Details: map[string]any{
					"pod":   pod.Name,
					"phase": string(phase),
				},
			})
		}

	default:
		// Check for CrashLoopBackOff, ImagePullBackOff, ErrImagePull via container statuses.
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil {
				reason := cs.State.Waiting.Reason
				if reason == "CrashLoopBackOff" || reason == "ImagePullBackOff" || reason == "ErrImagePull" {
					o.reporter.Report(report.Finding{
						Severity:  report.SeverityError,
						Check:     "pod-health",
						Component: componentForPod(pod),
						Message:   fmt.Sprintf("Pod %s container %s in %s", pod.Name, cs.Name, reason),
						Details: map[string]any{
							"pod":       pod.Name,
							"container": cs.Name,
							"reason":    reason,
						},
					})
				}
			}
		}
	}
}

func (o *Observer) checkContainerReadiness(pod *corev1.Pod, key string) {
	if pod.Status.Phase != corev1.PodRunning {
		return
	}

	now := time.Now()
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Ready {
			continue
		}
		readyKey := key + "/" + cs.Name
		since, tracked := o.podPhaseSince[readyKey]
		if !tracked {
			o.podPhaseSince[readyKey] = now
			continue
		}
		if now.Sub(since) > common.NotReadyTimeout {
			o.reporter.Report(report.Finding{
				Severity:  report.SeverityWarn,
				Check:     "pod-health",
				Component: componentForPod(pod),
				Message:   fmt.Sprintf("Container %s in pod %s not ready for %s", cs.Name, pod.Name, now.Sub(since).Round(time.Second)),
				Details: map[string]any{
					"pod":       pod.Name,
					"container": cs.Name,
				},
			})
		}
	}
}

func (o *Observer) checkRestarts(pod *corev1.Pod) {
	for _, cs := range pod.Status.ContainerStatuses {
		key := fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, cs.Name)
		prev, tracked := o.prevRestarts[key]
		o.prevRestarts[key] = cs.RestartCount

		if !tracked {
			continue
		}

		if cs.RestartCount > prev {
			if o.metrics != nil {
				o.metrics.RecordPodRestart(pod.Name, cs.Name, cs.RestartCount-prev)
			}
			severity := report.SeverityWarn
			if cs.RestartCount >= int32(common.RestartErrorThreshold) {
				severity = report.SeverityError
			}

			details := map[string]any{
				"pod":          pod.Name,
				"container":    cs.Name,
				"restartCount": cs.RestartCount,
			}

			if cs.LastTerminationState.Terminated != nil {
				details["lastState"] = cs.LastTerminationState.Terminated.Reason
				if cs.LastTerminationState.Terminated.Reason == "OOMKilled" {
					severity = report.SeverityError
					details["oomKilled"] = true
				}
			}

			o.reporter.Report(report.Finding{
				Severity:  severity,
				Check:     "pod-health",
				Component: componentForPod(pod),
				Message:   fmt.Sprintf("Container %s in pod %s restarted (count: %d)", cs.Name, pod.Name, cs.RestartCount),
				Details:   details,
			})
		}
	}
}

func (o *Observer) checkTerminating(pod *corev1.Pod, key string) {
	now := time.Now()

	if pod.DeletionTimestamp == nil {
		delete(o.podPhaseSince, key+"/terminating")
		return
	}

	termKey := key + "/terminating"
	since, tracked := o.podPhaseSince[termKey]
	if !tracked {
		o.podPhaseSince[termKey] = now
		return
	}

	if now.Sub(since) > common.TerminatingTimeout {
		drainState := pod.Annotations[common.AnnotationDrainState]
		severity := report.SeverityError
		msg := fmt.Sprintf("Pod %s stuck Terminating for %s", pod.Name, now.Sub(since).Round(time.Second))
		if drainState != "" {
			msg += fmt.Sprintf(" (drain state: %s)", drainState)
		}

		o.reporter.Report(report.Finding{
			Severity:  severity,
			Check:     "pod-health",
			Component: componentForPod(pod),
			Message:   msg,
			Details: map[string]any{
				"pod":        pod.Name,
				"drainState": drainState,
			},
		})
	}
}

func (o *Observer) checkOperatorPod(ctx context.Context) {
	var pods corev1.PodList
	if err := o.client.List(ctx, &pods,
		client.InNamespace(o.operatorNamespace),
		client.MatchingLabels{"control-plane": "controller-manager"},
	); err != nil {
		o.reporter.Report(report.Finding{
			Severity: report.SeverityWarn,
			Check:    "pod-health",
			Message:  fmt.Sprintf("failed to find operator pod: %v", err),
		})
		return
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Status.Phase != corev1.PodRunning {
			o.reporter.Report(report.Finding{
				Severity:  report.SeverityError,
				Check:     "pod-health",
				Component: "operator",
				Message:   fmt.Sprintf("Operator pod %s is %s, expected Running", pod.Name, pod.Status.Phase),
			})
		}
	}
}

func (o *Observer) checkPodCounts(ctx context.Context, pods *corev1.PodList) {
	var shards multigresv1alpha1.ShardList
	if err := o.client.List(ctx, &shards, o.listOpts()...); err != nil {
		o.reporter.Report(report.Finding{
			Severity: report.SeverityError,
			Check:    "pod-health",
			Message:  fmt.Sprintf("failed to list shards for pod count validation: %v", err),
		})
		return
	}

	for i := range shards.Items {
		shard := &shards.Items[i]
		if shard.DeletionTimestamp != nil {
			continue
		}

		// Pod labels use the short shard name from the label, not the CRD name.
		shardLabelValue := shard.Labels[common.LabelMultigresShard]

		for poolName, poolSpec := range shard.Spec.Pools {
			expectedPerCell := int32(1)
			if poolSpec.ReplicasPerCell != nil {
				expectedPerCell = *poolSpec.ReplicasPerCell
			}

			for _, cellName := range poolSpec.Cells {
				actual := countPodsForPoolCell(pods, shardLabelValue, string(poolName), string(cellName))
				if int32(actual) != expectedPerCell {
					o.reporter.Report(report.Finding{
						Severity:  report.SeverityError,
						Check:     "pod-health",
						Component: fmt.Sprintf("shard/%s/%s", shard.Namespace, shard.Name),
						Message:   fmt.Sprintf("Pool %s cell %s: expected %d pods, found %d", poolName, cellName, expectedPerCell, actual),
						Details: map[string]any{
							"shard":    shard.Name,
							"pool":     string(poolName),
							"cell":     string(cellName),
							"expected": expectedPerCell,
							"actual":   actual,
						},
					})
				}
			}
		}
	}
}

func countPodsForPoolCell(pods *corev1.PodList, shardName, poolName, cellName string) int {
	count := 0
	for i := range pods.Items {
		labels := pods.Items[i].Labels
		if labels[common.LabelMultigresShard] == shardName &&
			labels[common.LabelMultigresPool] == poolName &&
			labels[common.LabelMultigresCell] == cellName {
			// Don't count pods that are being deleted.
			if pods.Items[i].DeletionTimestamp == nil {
				count++
			}
		}
	}
	return count
}

func componentForPod(pod *corev1.Pod) string {
	comp := pod.Labels[common.LabelAppComponent]
	shard := pod.Labels[common.LabelMultigresShard]
	cell := pod.Labels[common.LabelMultigresCell]

	if shard != "" && cell != "" {
		return fmt.Sprintf("%s/%s/%s", comp, shard, cell)
	}
	if comp != "" {
		return comp
	}
	return pod.Name
}

func isPodReady(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

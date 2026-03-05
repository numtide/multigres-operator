package observer

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/tools/chaos/pkg/common"
	"github.com/numtide/multigres-operator/tools/chaos/pkg/report"
)

// drainStateOrder defines the valid forward progression of drain states.
var drainStateOrder = map[string]int{
	common.DrainStateRequested:        0,
	common.DrainStateDraining:         1,
	common.DrainStateAcknowledged:     2,
	common.DrainStateReadyForDeletion: 3,
}

type namespacedShard struct {
	ns, name string
}

func (o *Observer) checkDrainState(ctx context.Context) {
	var pods corev1.PodList
	if err := o.client.List(ctx, &pods,
		o.listOpts(client.MatchingLabels{
			common.LabelAppManagedBy: common.ManagedByMultigres,
			common.LabelAppComponent: common.ComponentPool,
		})...,
	); err != nil {
		o.reporter.Report(report.Finding{
			Severity: report.SeverityError,
			Check:    "drain-state",
			Message:  fmt.Sprintf("failed to list pool pods for drain check: %v", err),
		})
		return
	}

	now := time.Now()
	activePods := make(map[string]bool)

	// Track draining pods per shard for concurrent drain detection.
	drainingPerShard := make(map[namespacedShard][]string)

	for i := range pods.Items {
		pod := &pods.Items[i]
		key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
		activePods[key] = true

		state := pod.Annotations[common.AnnotationDrainState]
		if state == "" {
			delete(o.drainStateSince, key)
			delete(o.prevDrainState, key)
			continue
		}

		// Validate drain state is a known value.
		if _, valid := drainStateOrder[state]; !valid {
			o.reporter.Report(report.Finding{
				Severity:  report.SeverityFatal,
				Check:     "drain-state",
				Component: componentForPod(pod),
				Message:   fmt.Sprintf("Pod %s has unknown drain state: %q", pod.Name, state),
				Details:   map[string]any{"pod": pod.Name, "state": state},
			})
			continue
		}

		// Check for backward transitions.
		if prev, ok := o.prevDrainState[key]; ok && prev != state {
			prevOrder := drainStateOrder[prev]
			currOrder := drainStateOrder[state]
			if currOrder < prevOrder {
				o.reporter.Report(report.Finding{
					Severity:  report.SeverityFatal,
					Check:     "drain-state",
					Component: componentForPod(pod),
					Message:   fmt.Sprintf("Pod %s drain state went backward: %s → %s", pod.Name, prev, state),
					Details: map[string]any{
						"pod":       pod.Name,
						"prevState": prev,
						"currState": state,
					},
				})
			}

			// State changed — reset timer.
			o.drainStateSince[key] = now
		}
		o.prevDrainState[key] = state

		// Start tracking if new.
		if _, tracked := o.drainStateSince[key]; !tracked {
			o.drainStateSince[key] = now
		}

		// Check for stuck transitions.
		since := o.drainStateSince[key]
		var timeout time.Duration
		switch state {
		case common.DrainStateRequested:
			timeout = common.DrainRequestedTimeout
		case common.DrainStateDraining:
			timeout = common.DrainDrainingTimeout
		case common.DrainStateAcknowledged:
			timeout = common.DrainAcknowledgedTimeout
		case common.DrainStateReadyForDeletion:
			// Terminal state, no timeout.
			continue
		}

		if timeout > 0 && now.Sub(since) > timeout {
			o.reporter.Report(report.Finding{
				Severity:  report.SeverityError,
				Check:     "drain-state",
				Component: componentForPod(pod),
				Message:   fmt.Sprintf("Pod %s stuck in drain state %q for %s (timeout: %s)", pod.Name, state, now.Sub(since).Round(time.Second), timeout),
				Details: map[string]any{
					"pod":     pod.Name,
					"state":   state,
					"timeout": timeout.String(),
				},
			})
		}

		// Track draining pods per shard.
		if state == common.DrainStateDraining {
			shardName := pod.Labels[common.LabelMultigresShard]
			if shardName != "" {
				sk := namespacedShard{ns: pod.Namespace, name: shardName}
				drainingPerShard[sk] = append(drainingPerShard[sk], pod.Name)
			}
		}
	}

	// Check for concurrent drains within a shard.
	o.checkConcurrentDrains(ctx, drainingPerShard)

	// Clean up tracking state for pods that no longer exist.
	for key := range o.drainStateSince {
		if !activePods[key] {
			delete(o.drainStateSince, key)
			delete(o.prevDrainState, key)
		}
	}
}

func (o *Observer) checkConcurrentDrains(ctx context.Context, drainingPerShard map[namespacedShard][]string) {
	for sk, drainingPods := range drainingPerShard {
		if len(drainingPods) <= 1 {
			continue
		}

		// Concurrent drains are expected during shard deletion.
		var shard multigresv1alpha1.Shard
		objKey := client.ObjectKey{Namespace: sk.ns, Name: sk.name}
		if err := o.client.Get(ctx, objKey, &shard); err == nil {
			if shard.DeletionTimestamp != nil || shard.Annotations[multigresv1alpha1.AnnotationPendingDeletion] == "true" {
				continue
			}
		}

		o.reporter.Report(report.Finding{
			Severity:  report.SeverityError,
			Check:     "drain-state",
			Component: fmt.Sprintf("shard/%s/%s", sk.ns, sk.name),
			Message:   fmt.Sprintf("Shard %s has %d pods draining concurrently: %v", sk.name, len(drainingPods), drainingPods),
			Details: map[string]any{
				"shard":        sk.name,
				"drainingPods": drainingPods,
			},
		})
	}
}

/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package status

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// ComputePhase determines the phase of a resource based on its readiness.
// This is a shared helper used by resources with simple replica counts.
func ComputePhase(ready, total int32) multigresv1alpha1.Phase {
	if total == 0 {
		return multigresv1alpha1.PhaseInitializing
	}
	if ready == total {
		return multigresv1alpha1.PhaseHealthy
	}
	return multigresv1alpha1.PhaseProgressing
}

// crashLoopRestartThreshold is the minimum RestartCount at which a non-ready
// container is considered crash-looping, even if it is not currently in a
// Waiting state (e.g., between restarts in Terminated/Completed state).
const crashLoopRestartThreshold int32 = 3

// IsCrashLooping returns true if any container in the pod is unhealthy:
//   - in CrashLoopBackOff, OOMKilled, or ImagePullBackOff waiting state, OR
//   - terminated with repeated restarts (catches the gap between backoff
//     restarts when the container is in Completed/Error state).
func IsCrashLooping(pod *corev1.Pod) bool {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			switch cs.State.Waiting.Reason {
			case "CrashLoopBackOff", "OOMKilled", "ImagePullBackOff":
				return true
			}
		}
		if cs.State.Terminated != nil && cs.RestartCount >= crashLoopRestartThreshold {
			return true
		}
	}
	return false
}

// AnyCrashLooping returns true if any non-terminating pod in the slice
// is crash-looping.
func AnyCrashLooping(pods []corev1.Pod) bool {
	for i := range pods {
		if pods[i].DeletionTimestamp != nil {
			continue
		}
		if IsCrashLooping(&pods[i]) {
			return true
		}
	}
	return false
}

// WorkloadPhaseInput describes the observed state of a workload for phase computation.
type WorkloadPhaseInput struct {
	Ready, Total       int32
	GenerationCurrent  int64
	GenerationObserved int64
	Pods               []corev1.Pod
	ComponentName      string
}

// WorkloadPhaseResult holds the computed phase and a human-readable message.
type WorkloadPhaseResult struct {
	Phase   multigresv1alpha1.Phase
	Message string
}

// ComputeWorkloadPhase determines phase from replica counts, pod health, and
// generation freshness. Degraded (crash-looping) takes priority over all other
// non-Healthy states.
func ComputeWorkloadPhase(in WorkloadPhaseInput) WorkloadPhaseResult {
	if in.Pods != nil && AnyCrashLooping(in.Pods) {
		return WorkloadPhaseResult{
			Phase:   multigresv1alpha1.PhaseDegraded,
			Message: fmt.Sprintf("%s: one or more pods are crash-looping", in.ComponentName),
		}
	}

	phase := ComputePhase(in.Ready, in.Total)

	if in.GenerationCurrent != in.GenerationObserved {
		return WorkloadPhaseResult{
			Phase:   multigresv1alpha1.PhaseProgressing,
			Message: fmt.Sprintf("%s is progressing", in.ComponentName),
		}
	}

	if phase != multigresv1alpha1.PhaseHealthy {
		return WorkloadPhaseResult{
			Phase:   phase,
			Message: fmt.Sprintf("%s: %d/%d replicas ready", in.ComponentName, in.Ready, in.Total),
		}
	}

	return WorkloadPhaseResult{
		Phase:   multigresv1alpha1.PhaseHealthy,
		Message: "Ready",
	}
}

package shard

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/monitoring"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
)

// reconcilePoolPods ensures all pods and PVCs for a pool in a specific cell
// match the desired state. It handles creation of missing resources, scale-down,
// and rolling updates.
func (r *ShardReconciler) reconcilePoolPods(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	poolName string,
	cellName string,
	poolSpec multigresv1alpha1.PoolSpec,
) error {
	logger := log.FromContext(ctx)
	logger.V(1).Info("reconcilePoolPods started", "pool", poolName, "cell", cellName)

	replicas := DefaultPoolReplicas
	if poolSpec.ReplicasPerCell != nil {
		replicas = *poolSpec.ReplicasPerCell
	}

	// List existing pods and PVCs for this pool
	labels := buildPoolLabelsWithCell(shard, poolName, cellName)
	selector := metadata.GetSelectorLabels(labels)

	podList := &corev1.PodList{}
	if err := r.List(
		ctx,
		podList,
		client.InNamespace(shard.Namespace),
		client.MatchingLabels(selector),
	); err != nil {
		return fmt.Errorf("failed to list pods for pool %s cell %s: %w", poolName, cellName, err)
	}

	pvcList := &corev1.PersistentVolumeClaimList{}
	if err := r.List(
		ctx,
		pvcList,
		client.InNamespace(shard.Namespace),
		client.MatchingLabels(selector),
	); err != nil {
		return fmt.Errorf("failed to list PVCs for pool %s cell %s: %w", poolName, cellName, err)
	}

	existingPods := make(map[string]*corev1.Pod, len(podList.Items))
	for i := range podList.Items {
		pod := &podList.Items[i]
		existingPods[pod.Name] = pod
	}

	existingPVCs := make(map[string]*corev1.PersistentVolumeClaim, len(pvcList.Items))
	for i := range pvcList.Items {
		pvc := &pvcList.Items[i]
		existingPVCs[pvc.Name] = pvc
	}

	// Phase 1: Create missing resources and handle terminal/deleted pods
	driftedCount, actionTaken, err := r.createMissingResources(
		ctx, shard, poolName, cellName, poolSpec, existingPods, existingPVCs, replicas,
	)
	if err != nil {
		return err
	}

	// Phase 2: Handle scale-down (cleanup, DRAINED replacement, extra pod draining)
	actionTaken, inProgress, err := r.handleScaleDown(
		ctx, shard, poolName, poolSpec, existingPods, replicas, actionTaken,
	)
	if err != nil {
		return err
	}

	// Phase 3: Handle rolling updates
	if err := r.handleRollingUpdates(
		ctx,
		shard,
		poolName,
		cellName,
		poolSpec,
		existingPods,
		driftedCount,
		actionTaken,
		inProgress,
	); err != nil {
		return err
	}

	// Record drift metric
	clusterName := shard.Labels[metadata.LabelMultigresCluster]
	monitoring.SetPoolPodsDrifted(
		clusterName,
		shard.Name,
		poolName,
		cellName,
		shard.Namespace,
		driftedCount,
	)

	return nil
}

// createMissingResources creates PVCs and Pods that should exist but don't.
// It also handles terminal pods (Failed/Succeeded) and externally-deleted pods.
// Returns the number of drifted pods and whether an action was taken this reconcile.
func (r *ShardReconciler) createMissingResources(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	poolName, cellName string,
	poolSpec multigresv1alpha1.PoolSpec,
	existingPods map[string]*corev1.Pod,
	existingPVCs map[string]*corev1.PersistentVolumeClaim,
	replicas int32,
) (driftedCount int, actionTaken bool, err error) {
	logger := log.FromContext(ctx)

	for i := int32(0); i < replicas; i++ {
		podName := BuildPoolPodName(shard, poolName, cellName, int(i))
		pvcName := BuildPoolDataPVCName(shard, poolName, cellName, int(i))

		// Create PVC if missing
		if _, exists := existingPVCs[pvcName]; !exists {
			if !actionTaken {
				desiredPVC, buildErr := BuildPoolDataPVC(
					shard,
					poolName,
					cellName,
					poolSpec,
					int(i),
					r.Scheme,
				)
				if buildErr != nil {
					return 0, false, fmt.Errorf("failed to build PVC %s: %w", pvcName, buildErr)
				}
				if createErr := r.Create(
					ctx,
					desiredPVC,
				); createErr != nil &&
					!errors.IsAlreadyExists(createErr) {
					return 0, false, fmt.Errorf("failed to create PVC %s: %w", pvcName, createErr)
				}
				logger.Info("Created missing pool PVC", "pvc", pvcName)
			}
		}

		// Create Pod if missing
		pod, exists := existingPods[podName]
		if !exists {
			if !actionTaken {
				desiredPod, buildErr := BuildPoolPod(
					shard,
					poolName,
					cellName,
					poolSpec,
					int(i),
					r.Scheme,
				)
				if buildErr != nil {
					return 0, false, fmt.Errorf("failed to build pod %s: %w", podName, buildErr)
				}
				if createErr := r.Create(
					ctx,
					desiredPod,
				); createErr != nil &&
					!errors.IsAlreadyExists(createErr) {
					return 0, false, fmt.Errorf("failed to create pod %s: %w", podName, createErr)
				}
				logger.Info("Created missing pool pod", "pod", podName)
				r.Recorder.Eventf(
					shard,
					"Normal",
					"PodCreated",
					"Created pod %s for pool %s",
					podName,
					poolName,
				)
				actionTaken = true
			}
			continue
		}

		// Handle terminal pods (Failed/Succeeded) — delete for recreation
		if (pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded) &&
			pod.DeletionTimestamp.IsZero() {
			if !actionTaken {
				logger.Info(
					"Deleting terminal pool pod for recreation",
					"pod",
					pod.Name,
					"phase",
					pod.Status.Phase,
				)
				if delErr := r.Delete(ctx, pod); delErr != nil && !errors.IsNotFound(delErr) {
					return 0, false, fmt.Errorf(
						"failed to delete terminal pod %s: %w",
						pod.Name,
						delErr,
					)
				}
				actionTaken = true
			}
			continue
		}

		// Handle pods being deleted externally
		if !pod.DeletionTimestamp.IsZero() &&
			controllerutil.ContainsFinalizer(pod, PoolPodFinalizer) {
			if !actionTaken {
				if handleErr := r.handleExternalDeletion(ctx, shard, pod); handleErr != nil {
					return 0, false, handleErr
				}
				actionTaken = true
			}
			continue
		}

		// Check if it's drifted
		isDrifted := podNeedsUpdate(pod, shard, poolName, cellName, poolSpec, int(i), r.Scheme)
		if isDrifted {
			driftedCount++
		}

		// Block subsequent creations or updates if this pod is not ready and not already pending an update.
		// DRAINED pods are naturally not ready, so we allow them to proceed so they can be replaced.
		role := resolvePodRole(shard, pod.Name)
		if pod.DeletionTimestamp.IsZero() && !isPodReady(pod) && !isDrifted && role != "DRAINED" {
			actionTaken = true
		}
	}

	return driftedCount, actionTaken, nil
}

// isPodReady returns true if a pod is fully ready
func isPodReady(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// handleExternalDeletion handles a pod that has been deleted externally (e.g. kubectl delete).
// Unscheduled pods get their finalizer removed directly; scheduled pods enter the drain state machine.
func (r *ShardReconciler) handleExternalDeletion(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	pod *corev1.Pod,
) error {
	logger := log.FromContext(ctx)

	isScheduled := false
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionTrue {
			isScheduled = true
			break
		}
	}

	if !isScheduled {
		// Never scheduled -> can't be registered in etcd, safe to remove finalizer.
		logger.Info(
			"Removing finalizer from unscheduled pod to clear deletion deadlock",
			"pod",
			pod.Name,
		)
		if controllerutil.RemoveFinalizer(pod, PoolPodFinalizer) {
			if err := r.Update(ctx, pod); err != nil && !errors.IsNotFound(err) {
				return fmt.Errorf(
					"failed to remove finalizer from unscheduled pod %s: %w",
					pod.Name,
					err,
				)
			}
		}
		return nil
	}

	if pod.Annotations[metadata.AnnotationDrainState] == "" {
		// Scheduled pod deleted externally without a drain annotation.
		// Initiate the drain state machine so the data-handler can
		// unregister it from etcd before the finalizer is removed.
		logger.Info("Initiating drain for externally-deleted pod", "pod", pod.Name)
		if err := r.initiateDrain(ctx, pod); err != nil {
			return err
		}
		r.Recorder.Eventf(
			shard,
			"Warning",
			"ExternalDeletion",
			"Initiating drain for externally-deleted pod %s",
			pod.Name,
		)
	}

	return nil
}

// handleScaleDown processes pods that need removal: ready-for-deletion cleanup,
// DRAINED pod replacement, and draining extra pods beyond the desired replica count.
// Returns whether an action was taken and whether any drain is in progress.
func (r *ShardReconciler) handleScaleDown(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	poolName string,
	poolSpec multigresv1alpha1.PoolSpec,
	existingPods map[string]*corev1.Pod,
	replicas int32,
	actionTaken bool,
) (bool, bool, error) {
	logger := log.FromContext(ctx)

	var extraPods []*corev1.Pod
	var readyForDeletion []*corev1.Pod
	inProgress := false

	// Sort pods by name for deterministic processing
	podNames := make([]string, 0, len(existingPods))
	for k := range existingPods {
		podNames = append(podNames, k)
	}
	slices.Sort(podNames)

	for _, podName := range podNames {
		pod := existingPods[podName]
		drainState := pod.Annotations[metadata.AnnotationDrainState]

		if drainState == metadata.DrainStateReadyForDeletion {
			readyForDeletion = append(readyForDeletion, pod)
			continue
		}

		if drainState != "" || !pod.DeletionTimestamp.IsZero() {
			inProgress = true
		}

		index := resolvePodIndex(pod.Name)
		if index >= int(replicas) {
			extraPods = append(extraPods, pod)
		}
	}

	// Process external deletions for extra pods to avoid deadlocks
	for _, pod := range extraPods {
		if !pod.DeletionTimestamp.IsZero() &&
			controllerutil.ContainsFinalizer(pod, PoolPodFinalizer) &&
			pod.Annotations[metadata.AnnotationDrainState] == "" {
			if !actionTaken {
				if err := r.handleExternalDeletion(ctx, shard, pod); err != nil {
					return actionTaken, inProgress, err
				}
				actionTaken = true
			}
		}
	}

	// Cleanup pods ready for deletion: remove finalizer first, then delete
	for _, pod := range readyForDeletion {
		logger.Info("Cleaning up pod in ready-for-deletion state", "pod", pod.Name)
		if err := r.cleanupDrainedPod(ctx, shard, pod, poolName, poolSpec, replicas); err != nil {
			return actionTaken, inProgress, fmt.Errorf(
				"failed to cleanup drained pod %s: %w",
				pod.Name,
				err,
			)
		}
		if err := r.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
			return actionTaken, inProgress, fmt.Errorf(
				"failed to delete ready-for-deletion pod %s: %w",
				pod.Name,
				err,
			)
		}
		actionTaken = true
	}

	// Replace DRAINED pods (skip if another drain is already in progress)
	for _, name := range podNames {
		pod := existingPods[name]
		if actionTaken || inProgress {
			break
		}
		role := resolvePodRole(shard, pod.Name)
		state := pod.Annotations[metadata.AnnotationDrainState]
		logger.V(1).
			Info("Checking pod for replacement", "pod", pod.Name, "role", role, "drainState", state)

		if role == "DRAINED" && state == "" {
			if err := r.initiateDrain(ctx, pod); err != nil {
				return actionTaken, inProgress, fmt.Errorf(
					"failed to initiate drain for DRAINED pod %s: %w",
					pod.Name,
					err,
				)
			}
			logger.Info("Requested drain for DRAINED pod", "pod", pod.Name)
			r.Recorder.Eventf(shard, "Warning", "PodReplaced", "Replacing DRAINED pod %s", pod.Name)
			actionTaken = true
		}
	}

	// Drain extra pods (scale-down, skip if another drain is already in progress)
	logger.V(1).
		Info("Scale-down check", "extraPods", len(extraPods), "actionTaken", actionTaken, "inProgress", inProgress, "desiredReplicas", replicas)
	if !actionTaken && !inProgress && len(extraPods) > 0 {
		podToDrain := r.selectPodToDrain(ctx, extraPods, shard)
		if podToDrain != nil && podToDrain.Annotations[metadata.AnnotationDrainState] == "" {
			if err := r.initiateDrain(ctx, podToDrain); err != nil {
				return actionTaken, inProgress, fmt.Errorf(
					"failed to initiate drain for extra pod %s: %w",
					podToDrain.Name,
					err,
				)
			}
			logger.Info("Requested drain for extra pod", "pod", podToDrain.Name)
			r.Recorder.Eventf(
				shard,
				"Normal",
				"DrainStarted",
				"Initiated drain for extra pod %s",
				podToDrain.Name,
			)
			actionTaken = true
		}
	}

	return actionTaken, inProgress, nil
}

// handleRollingUpdates drains drifted pods one at a time (replicas first, primary last).
func (r *ShardReconciler) handleRollingUpdates(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	poolName, cellName string,
	poolSpec multigresv1alpha1.PoolSpec,
	existingPods map[string]*corev1.Pod,
	driftedCount int,
	actionTaken, isAnyPodDraining bool,
) error {
	logger := log.FromContext(ctx)

	inProgress := isAnyPodDraining || driftedCount > 0
	clusterName := shard.Labels[metadata.LabelMultigresCluster]
	monitoring.SetRollingUpdateInProgress(
		clusterName,
		shard.Name,
		poolName,
		cellName,
		shard.Namespace,
		inProgress,
	)

	if inProgress {
		msg := fmt.Sprintf("%d pods need update in pool %s", driftedCount, poolName)
		meta.SetStatusCondition(&shard.Status.Conditions, metav1.Condition{
			Type:    "RollingUpdate",
			Status:  metav1.ConditionTrue,
			Reason:  "PodsDrifted",
			Message: msg,
		})
	} else {
		msg := fmt.Sprintf("All pods up to date in pool %s", poolName)
		meta.SetStatusCondition(&shard.Status.Conditions, metav1.Condition{
			Type:    "RollingUpdate",
			Status:  metav1.ConditionFalse,
			Reason:  "PodsUpToDate",
			Message: msg,
		})
	}

	if actionTaken || isAnyPodDraining || driftedCount == 0 {
		return nil
	}

	// Sort pods for deterministic ordering
	podNames := make([]string, 0, len(existingPods))
	for k := range existingPods {
		podNames = append(podNames, k)
	}
	slices.Sort(podNames)

	var waitPrimary *corev1.Pod
	for _, name := range podNames {
		pod := existingPods[name]
		if !podNeedsUpdate(
			pod,
			shard,
			poolName,
			cellName,
			poolSpec,
			resolvePodIndex(pod.Name),
			r.Scheme,
		) {
			continue
		}

		isPrimary := resolvePodRole(shard, pod.Name) == "PRIMARY"
		if !isPrimary {
			if err := r.initiateDrain(ctx, pod); err != nil {
				return fmt.Errorf("failed to initiate drain for drifted pod %s: %w", pod.Name, err)
			}
			logger.Info(
				"Initiated drain for drifted replica pod during rolling update",
				"pod",
				pod.Name,
			)
			r.Recorder.Eventf(
				shard,
				"Normal",
				"PodUpdated",
				"Initiated drain for drifted replica pod %s",
				pod.Name,
			)
			return nil // Only one per reconcile loop
		}
		waitPrimary = pod
	}

	// If the only pod that needs updating is the PRIMARY, initiate a switchover.
	if waitPrimary != nil && waitPrimary.Annotations[metadata.AnnotationDrainState] == "" {
		if err := r.initiateDrain(ctx, waitPrimary); err != nil {
			return fmt.Errorf(
				"failed to request drain for primary pod %s: %w",
				waitPrimary.Name,
				err,
			)
		}
		logger.Info("Requested switchover for primary pod rolling update", "pod", waitPrimary.Name)
		r.Recorder.Eventf(
			shard,
			"Normal",
			"RollingUpdateStarted",
			"Initiating primary switchover for rolling update of pod %s",
			waitPrimary.Name,
		)
	}

	return nil
}

// selectPodToDrain chooses the best pod to drain during scale-down.
// Preference: non-ready, non-primary, highest index.
func (r *ShardReconciler) selectPodToDrain(
	ctx context.Context,
	extraPods []*corev1.Pod,
	shard *multigresv1alpha1.Shard,
) *corev1.Pod {
	logger := log.FromContext(ctx)
	if len(extraPods) == 0 {
		return nil
	}

	var bestPod *corev1.Pod
	var bestScore int

	for _, pod := range extraPods {
		if pod == nil {
			continue
		}
		score := resolvePodIndex(pod.Name) // higher index gets higher score

		role := resolvePodRole(shard, pod.Name)
		if role == "PRIMARY" {
			score -= 1000
		}

		// Prefer non-ready pods
		isReady := false
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				isReady = true
				break
			}
		}
		if !isReady {
			score += 500
		}

		logger.V(1).
			Info("Pod score for drain", "pod", pod.Name, "score", score, "isReady", isReady, "role", role)
		if bestPod == nil || score > bestScore {
			bestPod = pod
			bestScore = score
		}
	}

	if bestPod != nil {
		logger.Info("Selected pod to drain", "pod", bestPod.Name, "score", bestScore)
	} else {
		logger.Info("No pod selected to drain despite extraPods being non-empty")
	}

	return bestPod
}

func (r *ShardReconciler) cleanupDrainedPod(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	pod *corev1.Pod,
	poolName string,
	poolSpec multigresv1alpha1.PoolSpec,
	replicas int32,
) error {
	logger := log.FromContext(ctx)

	// Handle PVC deletion based on policy FIRST
	// If this fails, we leave the finalizer so the pod isn't GC'd and we retry later.
	mergedPolicy := multigresv1alpha1.MergePVCDeletionPolicy(
		poolSpec.PVCDeletionPolicy,
		shard.Spec.PVCDeletionPolicy,
	)
	policy := multigresv1alpha1.PVCDeletionPolicy{
		WhenScaled: multigresv1alpha1.RetainPVCRetentionPolicy,
	}
	if mergedPolicy != nil && mergedPolicy.WhenScaled != "" {
		policy = *mergedPolicy
	}

	if policy.WhenScaled == multigresv1alpha1.DeletePVCRetentionPolicy {
		idx := resolvePodIndex(pod.Name)
		isDrainedReplacement := idx < int(replicas) && resolvePodRole(shard, pod.Name) == "DRAINED"
		if idx >= int(replicas) || isDrainedReplacement {
			cellName := pod.Labels[metadata.LabelMultigresCell]
			pvcName := BuildPoolDataPVCName(shard, poolName, cellName, idx)
			pvc := &corev1.PersistentVolumeClaim{}
			err := r.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pvcName}, pvc)
			if err != nil {
				if !errors.IsNotFound(err) {
					logger.Error(err, "Failed to fetch PVC for deletion", "pvc", pvcName)
					return fmt.Errorf("failed to fetch PVC %s for deletion: %w", pvcName, err)
				}
			} else {
				reason := "scaled down"
				if isDrainedReplacement {
					reason = "DRAINED replacement"
				}
				if err := r.Delete(ctx, pvc); err != nil && !errors.IsNotFound(err) {
					logger.Error(err, "Failed to delete PVC for "+reason+" pod", "pvc", pvcName)
					return fmt.Errorf("failed to delete PVC %s: %w", pvcName, err)
				}
				logger.Info("Deleted PVC for "+reason+" pod", "pvc", pvcName)
			}
		} else {
			logger.Info(
				"Retaining PVC for pod during rolling update",
				"pod",
				pod.Name,
				"index",
				idx,
				"replicas",
				replicas,
			)
		}
	}

	// Remove finalizer to allow Kubernetes to delete the pod
	// We only do this after PVC deletion (if applicable) succeeds to prevent PVC leaks.
	if controllerutil.RemoveFinalizer(pod, PoolPodFinalizer) {
		if err := r.Update(ctx, pod); err != nil {
			return fmt.Errorf("failed to remove finalizer from pod %s: %w", pod.Name, err)
		}
		logger.Info("Removed finalizer from drained pod", "pod", pod.Name)
		r.Recorder.Eventf(shard, "Normal", "DrainCompleted", "Completed drain for pod %s", pod.Name)
	}

	return nil
}

// podNeedsUpdate checks if a pod requires recreation due to spec changes.
// Since most pod fields are immutable, we rely on the pre-computed spec-hash annotation.
func podNeedsUpdate(
	existing *corev1.Pod,
	shard *multigresv1alpha1.Shard,
	poolName, cellName string,
	poolSpec multigresv1alpha1.PoolSpec,
	index int,
	scheme *runtime.Scheme,
) bool {
	// If it has a deletion timestamp, let it die
	if !existing.DeletionTimestamp.IsZero() {
		return false
	}

	// If missing the annotation entirely, it needs an update
	existingHash, ok := existing.Annotations[metadata.AnnotationSpecHash]
	if !ok {
		return true
	}

	// Compute desired hash by building the ideal pod spec
	// NOTE: BuildPoolPod doesn't make API calls, it's safe to call frequently.
	desired, err := BuildPoolPod(shard, poolName, cellName, poolSpec, index, scheme)
	if err != nil {
		return false // Assume no update needed if we can't build it
	}

	desiredHash := ComputeSpecHash(desired)
	return existingHash != desiredHash
}

// resolvePodIndex parses the index from the pod name (the number after the last dash).
func resolvePodIndex(podName string) int {
	lastDash := strings.LastIndex(podName, "-")
	if lastDash == -1 {
		return -1
	}
	index, err := strconv.Atoi(podName[lastDash+1:])
	if err != nil {
		return -1
	}
	return index
}

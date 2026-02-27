package shard

import (
	"context"
	"fmt"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/monitoring"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
	"github.com/numtide/multigres-operator/pkg/util/name"
)

// updateStatus updates the Shard status based on observed state.
func (r *ShardReconciler) updateStatus(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
	oldPhase := shard.Status.Phase
	cellsSet := make(map[multigresv1alpha1.CellName]bool)

	// Update pools status
	totalPods, readyPods, err := r.updatePoolsStatus(ctx, shard, cellsSet)
	if err != nil {
		return err
	}

	// Update MultiOrch status
	if err := r.updateMultiOrchStatus(ctx, shard, cellsSet); err != nil {
		return err
	}

	// Update cells list from all observed cells
	shard.Status.Cells = cellSetToSlice(cellsSet)

	// Update aggregate status fields
	shard.Status.PoolsReady = (totalPods > 0 && totalPods == readyPods)
	shard.Status.ReadyReplicas = readyPods

	// Update Phase
	if shard.Status.PoolsReady && shard.Status.OrchReady {
		shard.Status.Phase = multigresv1alpha1.PhaseHealthy
		shard.Status.Message = "Ready"
	} else {
		shard.Status.Phase = multigresv1alpha1.PhaseProgressing
		shard.Status.Message = fmt.Sprintf(
			"PoolsReady: %v, OrchReady: %v",
			shard.Status.PoolsReady,
			shard.Status.OrchReady,
		)
	}

	// Update conditions
	r.setConditions(shard, totalPods, readyPods)

	shard.Status.ObservedGeneration = shard.Generation

	// 1. Construct the Patch Object
	patchObj := &multigresv1alpha1.Shard{
		TypeMeta: metav1.TypeMeta{
			APIVersion: multigresv1alpha1.GroupVersion.String(),
			Kind:       "Shard",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      shard.Name,
			Namespace: shard.Namespace,
		},
		Status: shard.Status,
	}

	// Exclude fields owned by the data-handler to prevent SSA from overwriting
	// them with stale cached values from the resource-handler's informer.
	patchObj.Status.PodRoles = nil
	patchObj.Status.LastBackupTime = nil
	patchObj.Status.LastBackupType = ""

	// 2. Apply the Patch
	if oldPhase != shard.Status.Phase {
		r.Recorder.Eventf(
			shard,
			"Normal",
			"PhaseChange",
			"Transitioned from '%s' to '%s'",
			oldPhase,
			shard.Status.Phase,
		)
	}

	// Note: We rely on Server-Side Apply (SSA) to handle idempotency.
	// If the status hasn't changed, the API server will treat this Patch as a no-op,
	// so we don't need a manual DeepEqual check here.
	if err := r.Status().Patch(
		ctx,
		patchObj,
		client.Apply,
		client.FieldOwner("multigres-resource-handler"),
		client.ForceOwnership,
	); err != nil {
		return fmt.Errorf("failed to patch status: %w", err)
	}

	return nil
}

// updatePoolsStatus aggregates status from all pool pods.
// Returns total desired pods, ready pods, and tracks cells in the cellsSet.
func (r *ShardReconciler) updatePoolsStatus(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	cellsSet map[multigresv1alpha1.CellName]bool,
) (int32, int32, error) {
	var totalPods, readyPods int32
	clusterName := shard.Labels[metadata.LabelMultigresCluster]

	for poolName, poolSpec := range shard.Spec.Pools {
		var poolDesired, poolReady int32

		// TODO(#91): Pool.Cells may contain duplicates - add +listType=set validation at API level
		for _, cell := range poolSpec.Cells {
			cellName := string(cell)
			cellsSet[cell] = true

			// List pods for this specific pool and cell
			labels := buildPoolLabelsWithCell(shard, string(poolName), cellName)
			selector := metadata.GetSelectorLabels(labels)
			podList := &corev1.PodList{}
			if err := r.List(
				ctx,
				podList,
				client.InNamespace(shard.Namespace),
				client.MatchingLabels(selector),
			); err != nil {
				return 0, 0, fmt.Errorf("failed to list pods for status: %w", err)
			}

			var cellReady int32
			for i := range podList.Items {
				pod := &podList.Items[i]

				// Exclude terminating pods from total/ready counts
				if !pod.DeletionTimestamp.IsZero() {
					continue
				}

				// Check if pod is ready
				isReady := false
				for _, cond := range pod.Status.Conditions {
					if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
						isReady = true
						break
					}
				}
				if isReady {
					cellReady++
				}
			}

			// Emit a warning explicitly if the cell pool should have replicas but is empty
			replicas := DefaultPoolReplicas
			if poolSpec.ReplicasPerCell != nil {
				replicas = *poolSpec.ReplicasPerCell
			}
			if replicas > 0 && cellReady == 0 {
				r.Recorder.Eventf(
					shard,
					"Warning",
					"PoolEmpty",
					"Pool %s in cell %s has 0 ready replicas",
					poolName,
					cellName,
				)
			}

			poolDesired += replicas
			poolReady += cellReady
		}

		totalPods += poolDesired
		readyPods += poolReady

		monitoring.SetShardPoolReplicas(
			clusterName, shard.Name, string(poolName), "", shard.Namespace,
			poolDesired, poolReady,
		)
	}

	return totalPods, readyPods, nil
}

// updateMultiOrchStatus checks MultiOrch Deployments and sets OrchReady status.
// Also tracks cells in the cellsSet.
func (r *ShardReconciler) updateMultiOrchStatus(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	cellsSet map[multigresv1alpha1.CellName]bool,
) error {
	multiOrchCells, err := getMultiOrchCells(shard)
	if err != nil {
		shard.Status.OrchReady = false
		return nil
	}

	orchReady := true
	for _, cell := range multiOrchCells {
		cellName := string(cell)
		cellsSet[cell] = true

		// Check MultiOrch Deployment status (deployments use long names)
		deployName := buildMultiOrchNameWithCell(shard, cellName, name.DefaultConstraints)
		deploy := &appsv1.Deployment{}
		err := r.Get(
			ctx,
			client.ObjectKey{Namespace: shard.Namespace, Name: deployName},
			deploy,
		)
		if err != nil {
			if errors.IsNotFound(err) {
				orchReady = false
				break
			}
			return fmt.Errorf("failed to get MultiOrch Deployment for status: %w", err)
		}

		// Check if deployment is ready
		if deploy.Spec.Replicas == nil ||
			deploy.Status.ObservedGeneration != deploy.Generation ||
			deploy.Status.ReadyReplicas != *deploy.Spec.Replicas {
			orchReady = false
			break
		}
	}

	shard.Status.OrchReady = orchReady
	return nil
}

// cellSetToSlice converts a cell set (map) to a slice.
func cellSetToSlice(cellsSet map[multigresv1alpha1.CellName]bool) []multigresv1alpha1.CellName {
	cells := make([]multigresv1alpha1.CellName, 0, len(cellsSet))
	for cell := range cellsSet {
		cells = append(cells, cell)
	}
	slices.Sort(cells)
	return cells
}

// setConditions creates status conditions based on observed state.
func (r *ShardReconciler) setConditions(
	shard *multigresv1alpha1.Shard,
	totalPods, readyPods int32,
) {
	// Available condition
	availableCondition := metav1.Condition{
		Type:               "Available",
		ObservedGeneration: shard.Generation,
		Status:             metav1.ConditionFalse,
		Reason:             "NotAllPodsReady",
		Message:            fmt.Sprintf("%d/%d pods ready", readyPods, totalPods),
	}

	if readyPods == totalPods && totalPods > 0 {
		availableCondition.Status = metav1.ConditionTrue
		availableCondition.Reason = "AllPodsReady"
		availableCondition.Message = fmt.Sprintf("All %d pods are ready", readyPods)
	}

	meta.SetStatusCondition(&shard.Status.Conditions, availableCondition)
}

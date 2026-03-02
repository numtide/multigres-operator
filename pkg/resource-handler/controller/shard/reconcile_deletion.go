package shard

import (
	"context"
	"fmt"
	"time"

	"github.com/multigres/multigres/go/common/topoclient"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/data-handler/drain"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
	"github.com/numtide/multigres-operator/pkg/util/status"
)

// handleDeletion performs best-effort cleanup when a Shard is deleted.
// Without finalizers, Kubernetes GC handles cascade deletion via ownerRefs.
// This method does best-effort topo cleanup and PVC policy enforcement.
func (r *ShardReconciler) handleDeletion(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Determine matching labels for all resources belonging to this shard.
	clusterName := shard.Labels[metadata.LabelMultigresCluster]
	selector := map[string]string{
		metadata.LabelMultigresCluster:    clusterName,
		metadata.LabelMultigresDatabase:   string(shard.Spec.DatabaseName),
		metadata.LabelMultigresTableGroup: string(shard.Spec.TableGroupName),
		metadata.LabelMultigresShard:      string(shard.Spec.ShardName),
	}

	// Delete all Deployments owned by this shard.
	deployList := &appsv1.DeploymentList{}
	if err := r.List(
		ctx,
		deployList,
		client.InNamespace(shard.Namespace),
		client.MatchingLabels(selector),
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list deployments for deletion: %w", err)
	}
	for i := range deployList.Items {
		deploy := &deployList.Items[i]
		if deploy.DeletionTimestamp.IsZero() {
			logger.Info("Initiating deployment deletion during shard cleanup", "deployment", deploy.Name)
			if err := r.Delete(ctx, deploy); err != nil && !errors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("failed to delete deployment %s: %w", deploy.Name, err)
			}
		}
	}

	// Evaluate and process PVC deletions based on PVCDeletionPolicy.
	pvcList := &corev1.PersistentVolumeClaimList{}
	if err := r.List(
		ctx,
		pvcList,
		client.InNamespace(shard.Namespace),
		client.MatchingLabels(selector),
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list PVCs for deletion: %w", err)
	}

	for i := range pvcList.Items {
		pvc := &pvcList.Items[i]

		poolName := pvc.Labels[metadata.LabelMultigresPool]
		var policy *multigresv1alpha1.PVCDeletionPolicy

		if poolName != "" {
			if poolSpec, exists := shard.Spec.Pools[multigresv1alpha1.PoolName(poolName)]; exists {
				policy = multigresv1alpha1.MergePVCDeletionPolicy(
					poolSpec.PVCDeletionPolicy,
					shard.Spec.PVCDeletionPolicy,
				)
			} else {
				policy = shard.Spec.PVCDeletionPolicy
			}
		} else {
			policy = shard.Spec.PVCDeletionPolicy
		}

		whenDeleted := multigresv1alpha1.RetainPVCRetentionPolicy
		if policy != nil && policy.WhenDeleted != "" {
			whenDeleted = policy.WhenDeleted
		}

		if whenDeleted == multigresv1alpha1.DeletePVCRetentionPolicy {
			if pvc.DeletionTimestamp.IsZero() {
				logger.Info("Deleting PVC per WhenDeleted: Delete policy", "pvc", pvc.Name)
				if err := r.Delete(ctx, pvc); err != nil && !errors.IsNotFound(err) {
					return ctrl.Result{}, fmt.Errorf("failed to delete PVC %s: %w", pvc.Name, err)
				}
			}
		} else {
			logger.Info("Retaining PVC per WhenDeleted: Retain policy", "pvc", pvc.Name)
		}
	}

	// Best-effort pod deletion. Without finalizers, pods are deleted directly.
	podList := &corev1.PodList{}
	if err := r.List(
		ctx,
		podList,
		client.InNamespace(shard.Namespace),
		client.MatchingLabels(selector),
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list pods for deletion: %w", err)
	}
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.DeletionTimestamp.IsZero() {
			if err := r.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("failed to delete pod %s: %w", pod.Name, err)
			}
		}
	}

	logger.Info("Shard best-effort cleanup complete")
	return ctrl.Result{}, nil
}

// handlePendingDeletion handles graceful shard deletion. When a shard is marked
// with the PendingDeletion annotation, this method drains all pods via the drain
// state machine. Once all pods are drained (or gone), it sets the
// ReadyForDeletion condition so the TableGroup controller can safely delete
// the Shard CR.
func (r *ShardReconciler) handlePendingDeletion(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling PendingDeletion")

	// List all pods belonging to this shard.
	lbls := map[string]string{
		metadata.LabelMultigresCluster: shard.Labels[metadata.LabelMultigresCluster],
		metadata.LabelMultigresShard:   string(shard.Spec.ShardName),
	}
	podList := &corev1.PodList{}
	if err := r.List(
		ctx,
		podList,
		client.InNamespace(shard.Namespace),
		client.MatchingLabels(lbls),
	); err != nil {
		logger.Error(err, "Failed to list pods for PendingDeletion")
		return ctrl.Result{}, fmt.Errorf("failed to list pods for pending deletion: %w", err)
	}

	// Open topo store if we have pods that need drain state machine execution.
	var store topoclient.Store
	if len(podList.Items) > 0 {
		var err error
		store, err = r.getTopoStore(shard)
		if err != nil {
			logger.Error(err, "Failed to get topo store for PendingDeletion")
			r.Recorder.Eventf(shard, "Warning", "TopologyError",
				"Cannot connect to topology during pending deletion: %v", err)
			return ctrl.Result{RequeueAfter: topoUnavailableRequeueDelay}, nil
		}
		defer func() { _ = store.Close() }()

		// Update PodRoles so the drain state machine has current role info.
		r.reconcilePodRoles(ctx, store, shard)
	}

	allDrained := true
	for i := range podList.Items {
		pod := &podList.Items[i]

		// Skip pods already being deleted.
		if !pod.DeletionTimestamp.IsZero() {
			allDrained = false
			continue
		}

		drainState := pod.Annotations[metadata.AnnotationDrainState]

		switch drainState {
		case metadata.DrainStateReadyForDeletion:
			// Pod is fully drained, delete it.
			logger.Info("Deleting drained pod during PendingDeletion", "pod", pod.Name)
			if err := r.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf(
					"failed to delete drained pod %s: %w", pod.Name, err)
			}
			allDrained = false

		case "":
			// Not draining yet — initiate drain.
			logger.Info("Initiating drain for PendingDeletion", "pod", pod.Name)
			if err := r.initiateDrain(ctx, pod); err != nil {
				return ctrl.Result{}, fmt.Errorf(
					"failed to initiate drain for pod %s: %w", pod.Name, err)
			}
			r.Recorder.Eventf(shard, "Normal", "DrainStarted",
				"Initiated drain for pod %s (pending deletion)", pod.Name)
			allDrained = false

		default:
			// Drain in progress — run the drain state machine.
			if store != nil {
				if _, derr := drain.ExecuteDrainStateMachine(
					ctx, r.Client, r.RPCClient, r.Recorder, store, shard, pod,
				); derr != nil {
					logger.Error(derr, "Failed to execute drain state machine", "pod", pod.Name)
				}
			}
			allDrained = false
		}
	}

	if !allDrained {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	// All pods drained and deleted — set ReadyForDeletion condition.
	if !status.IsConditionTrue(shard.Status.Conditions, multigresv1alpha1.ConditionReadyForDeletion) {
		statusBase := shard.DeepCopy()
		status.SetCondition(&shard.Status.Conditions, metav1.Condition{
			Type:               multigresv1alpha1.ConditionReadyForDeletion,
			Status:             metav1.ConditionTrue,
			Reason:             "DrainComplete",
			Message:            "All pods drained; shard is ready for deletion",
			ObservedGeneration: shard.Generation,
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, shard, client.MergeFrom(statusBase)); err != nil {
			return ctrl.Result{}, fmt.Errorf("setting ReadyForDeletion condition: %w", err)
		}
		logger.Info("Set ReadyForDeletion condition")
		r.Recorder.Event(shard, "Normal", "ReadyForDeletion",
			"All pods drained; shard is ready for deletion")
	}

	return ctrl.Result{}, nil
}

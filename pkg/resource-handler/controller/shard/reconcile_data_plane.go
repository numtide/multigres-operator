package shard

import (
	"context"
	"time"

	"github.com/multigres/multigres/go/common/topoclient"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/data-handler/backuphealth"
	"github.com/numtide/multigres-operator/pkg/data-handler/drain"
	"github.com/numtide/multigres-operator/pkg/data-handler/topo"
	"github.com/numtide/multigres-operator/pkg/monitoring"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
	"github.com/numtide/multigres-operator/pkg/util/status"
)

// reconcileDataPlane opens a topo connection and runs all data-plane phases:
// PodRoles update, drain state machine, and backup health evaluation.
// Database registration is handled by the MultigresCluster controller.
func (r *ShardReconciler) reconcileDataPlane(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Open a single topo connection for PodRoles, drain, and backup health.
	store, err := r.getTopoStore(shard)
	if err != nil {
		if !topo.IsTopoUnavailable(err) {
			r.Recorder.Eventf(shard, "Warning", "TopologyError",
				"Failed to connect to topology store: %v", err)
		}
		logger.Error(err, "Failed to get topo store, cannot update roles or execute drain")
		return ctrl.Result{RequeueAfter: topoUnavailableRequeueDelay}, nil
	}
	defer func() { _ = store.Close() }()

	// Phase: Update PodRoles from topology
	{
		_, childSpan := monitoring.StartChildSpan(ctx, "Shard.ReconcilePodRoles")
		r.reconcilePodRoles(ctx, store, shard)
		childSpan.End()
	}

	// Phase: Prune stale pooler entries from topology
	{
		_, childSpan := monitoring.StartChildSpan(ctx, "Shard.ReconcilePoolerPrune")
		r.reconcilePoolerPrune(ctx, store, shard)
		childSpan.End()
	}

	// Phase: Execute drain state machine for pods with drain annotations
	{
		_, childSpan := monitoring.StartChildSpan(ctx, "Shard.ReconcileDrainState")
		requeue, err := r.reconcileDrainState(ctx, store, shard)
		if err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			return ctrl.Result{}, err
		}
		childSpan.End()
		if requeue {
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		}
	}

	// Phase: Evaluate backup health
	if r.RPCClient != nil {
		_, childSpan := monitoring.StartChildSpan(ctx, "Shard.ReconcileBackupHealth")
		result, err := backuphealth.EvaluateBackupHealth(ctx, store, r.RPCClient, shard)
		if err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			logger.Error(err, "Failed to evaluate backup health")
			r.Recorder.Eventf(
				shard,
				"Warning",
				"BackupCheckFailed",
				"Failed to check backup health: %v",
				err,
			)
		} else if result != nil {
			backupBase := shard.DeepCopy()
			prevHealthy := status.IsConditionTrue(
				shard.Status.Conditions,
				backuphealth.ConditionBackupHealthy,
			)
			backuphealth.ApplyBackupHealth(shard, result)

			if result.Healthy && !prevHealthy {
				r.Recorder.Event(shard, "Normal", "BackupHealthy", result.Message)
			} else if !result.Healthy && prevHealthy {
				r.Recorder.Eventf(shard, "Warning", "BackupStale", result.Message)
			}

			if err := r.Status().Patch(ctx, shard, client.MergeFrom(backupBase)); err != nil {
				monitoring.RecordSpanError(childSpan, err)
				childSpan.End()
				logger.Error(err, "Failed to update shard backup status")
				return ctrl.Result{}, err
			}
			childSpan.End()
		} else {
			childSpan.End()
		}
	}

	return ctrl.Result{}, nil
}

// reconcilePodRoles queries the topology for pooler status and updates
// shard.Status.PodRoles.
func (r *ShardReconciler) reconcilePodRoles(
	ctx context.Context,
	store topoclient.Store,
	shard *multigresv1alpha1.Shard,
) {
	logger := log.FromContext(ctx)
	statusBase := shard.DeepCopy()
	poolerStatus := topo.GetPoolerStatus(ctx, store, shard)

	if shard.Status.PodRoles == nil {
		shard.Status.PodRoles = make(map[string]string)
	}
	rolesChanged := false

	for hostname, role := range poolerStatus.Roles {
		if shard.Status.PodRoles[hostname] != role {
			shard.Status.PodRoles[hostname] = role
			rolesChanged = true
		}
	}

	// Prune entries for poolers that no longer exist in the topology.
	if poolerStatus.QuerySuccess {
		for hostname := range shard.Status.PodRoles {
			if _, exists := poolerStatus.Roles[hostname]; !exists {
				delete(shard.Status.PodRoles, hostname)
				rolesChanged = true
			}
		}
	}

	if rolesChanged {
		if err := r.Status().Patch(ctx, shard, client.MergeFrom(statusBase)); err != nil {
			logger.Error(err, "Failed to update shard pod roles")
		}
	}
}

// reconcileDrainState iterates pods with drain annotations and runs the
// drain state machine for each one.
func (r *ShardReconciler) reconcileDrainState(
	ctx context.Context,
	store topoclient.Store,
	shard *multigresv1alpha1.Shard,
) (bool, error) {
	logger := log.FromContext(ctx)

	lbls := map[string]string{
		metadata.LabelMultigresCluster:    shard.Labels[metadata.LabelMultigresCluster],
		metadata.LabelMultigresDatabase:   string(shard.Spec.DatabaseName),
		metadata.LabelMultigresTableGroup: string(shard.Spec.TableGroupName),
		metadata.LabelMultigresShard:      string(shard.Spec.ShardName),
	}
	podList := &corev1.PodList{}
	if err := r.List(
		ctx,
		podList,
		client.InNamespace(shard.Namespace),
		client.MatchingLabels(lbls),
	); err != nil {
		logger.Error(err, "Failed to list pods for drain state machine")
		return false, err
	}

	requeue := false
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Annotations[metadata.AnnotationDrainState] != "" {
			shouldRequeue, derr := drain.ExecuteDrainStateMachine(
				ctx, r.Client, r.RPCClient, r.Recorder, store, shard, pod,
			)
			if derr != nil {
				logger.Error(derr, "Failed to execute drain state machine", "pod", pod.Name)
			}
			if shouldRequeue {
				requeue = true
			}
		}
	}

	return requeue, nil
}

// getTopoStore returns a topology store, using the custom factory if set, otherwise the default.
func (r *ShardReconciler) getTopoStore(shard *multigresv1alpha1.Shard) (topoclient.Store, error) {
	if r.CreateTopoStore != nil {
		return r.CreateTopoStore(shard)
	}
	return topo.NewStoreFromShard(shard)
}

// reconcilePoolerPrune lists active pods for the shard and prunes topology
// entries for poolers that no longer have a running pod. Pruning is skipped
// when the parent cluster has disabled topology pruning (propagated via
// the Cell's TopologyReconciliation.PrunePoolers field).
func (r *ShardReconciler) reconcilePoolerPrune(
	ctx context.Context,
	store topoclient.Store,
	shard *multigresv1alpha1.Shard,
) {
	logger := log.FromContext(ctx)

	if !isPoolerPruningEnabled(shard) {
		return
	}

	lbls := map[string]string{
		metadata.LabelMultigresCluster:    shard.Labels[metadata.LabelMultigresCluster],
		metadata.LabelMultigresDatabase:   string(shard.Spec.DatabaseName),
		metadata.LabelMultigresTableGroup: string(shard.Spec.TableGroupName),
		metadata.LabelMultigresShard:      string(shard.Spec.ShardName),
	}
	podList := &corev1.PodList{}
	if err := r.List(
		ctx,
		podList,
		client.InNamespace(shard.Namespace),
		client.MatchingLabels(lbls),
	); err != nil {
		logger.Error(err, "Failed to list pods for pooler pruning")
		return
	}

	activePodNames := make(map[string]bool, len(podList.Items))
	for _, pod := range podList.Items {
		activePodNames[pod.Name] = true
	}

	pruned, err := topo.PrunePoolers(ctx, store, shard, activePodNames)
	if err != nil {
		logger.Error(err, "Failed to prune stale poolers")
	}
	if pruned > 0 {
		r.Recorder.Eventf(shard, "Normal", "PoolersPruned",
			"Pruned %d stale pooler(s) from topology", pruned)
	}
}

// isPoolerPruningEnabled returns true when topology pruning is enabled for
// the shard. The setting is inherited from MultigresCluster via the
// TableGroup builder. Defaults to true when unset.
func isPoolerPruningEnabled(shard *multigresv1alpha1.Shard) bool {
	if shard.Spec.TopologyPruning == nil || shard.Spec.TopologyPruning.Enabled == nil {
		return true
	}
	return *shard.Spec.TopologyPruning.Enabled
}

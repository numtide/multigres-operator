package shard

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/multigres/multigres/go/common/rpcclient"
	"github.com/multigres/multigres/go/common/topoclient"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/data-handler/backuphealth"
	"github.com/numtide/multigres-operator/pkg/data-handler/drain"
	"github.com/numtide/multigres-operator/pkg/data-handler/topo"
	"github.com/numtide/multigres-operator/pkg/monitoring"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
	"github.com/numtide/multigres-operator/pkg/util/status"
)

const (
	// topoUnavailableGracePeriod is the duration after resource creation during
	// which topology UNAVAILABLE errors are silently requeued instead of being
	// reported as reconcile errors. This prevents noisy error metrics during
	// normal cluster startup while the toposerver is still initializing.
	topoUnavailableGracePeriod = 2 * time.Minute

	// topoUnavailableRequeueDelay is the delay before retrying when the topology
	// server is unavailable during the grace period.
	topoUnavailableRequeueDelay = 5 * time.Second
)

// ShardReconciler reconciles a Shard object.
type ShardReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	// APIReader is an uncached client that reads directly from the API server.
	// The default cached client (r.Get) only sees Secrets labeled with
	// "app.kubernetes.io/managed-by: multigres-operator" due to the informer
	// cache's label filter. External Secrets (e.g., cert-manager) lack this
	// label, so we need APIReader to validate user-provided pgBackRest TLS Secrets.
	APIReader       client.Reader
	RPCClient       rpcclient.MultiPoolerClient
	CreateTopoStore func(*multigresv1alpha1.Shard) (topoclient.Store, error)
}

// Reconcile handles Shard resource reconciliation.
func (r *ShardReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	start := time.Now()
	ctx, span := monitoring.StartReconcileSpan(
		ctx,
		"Shard.Reconcile",
		req.Name,
		req.Namespace,
		"Shard",
	)
	defer span.End()
	ctx = monitoring.EnrichLoggerWithTrace(ctx)

	logger := log.FromContext(ctx)
	logger.V(1).Info("Reconcile started for shard", "shard", req.Name)

	// Fetch the Shard instance
	shard := &multigresv1alpha1.Shard{}
	if err := r.Get(ctx, req.NamespacedName, shard); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Shard resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		monitoring.RecordSpanError(span, err)
		logger.Error(err, "Failed to get Shard")
		return ctrl.Result{}, err
	}

	// Best-effort cleanup on deletion — no finalizers are used.
	if !shard.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, shard)
	}

	// Handle graceful deletion via PendingDeletion annotation.
	// The TableGroup controller sets this annotation when a shard is removed from
	// spec. The shard controller drains all pods and sets ReadyForDeletion
	// condition, at which point the TableGroup controller calls Delete.
	if shard.Annotations[multigresv1alpha1.AnnotationPendingDeletion] != "" {
		return r.handlePendingDeletion(ctx, shard)
	}

	// Reconcile pg_hba ConfigMap first (required by all pools before starting)
	if err := r.reconcilePgHbaConfigMap(ctx, shard); err != nil {
		monitoring.RecordSpanError(span, err)
		logger.Error(err, "Failed to reconcile pg_hba ConfigMap")
		r.Recorder.Eventf(shard, "Warning", "ConfigError", "Failed to generate pg_hba: %v", err)
		return ctrl.Result{}, err
	}

	// Reconcile postgres password Secret (required by pgctld and multipooler)
	if err := r.reconcilePostgresPasswordSecret(ctx, shard); err != nil {
		monitoring.RecordSpanError(span, err)
		logger.Error(err, "Failed to reconcile postgres password Secret")
		r.Recorder.Eventf(
			shard,
			"Warning",
			"ConfigError",
			"Failed to generate postgres password Secret: %v",
			err,
		)
		return ctrl.Result{}, err
	}

	// Reconcile pgBackRest TLS certificates (required for inter-node backup communication)
	if err := r.reconcilePgBackRestCerts(ctx, shard); err != nil {
		monitoring.RecordSpanError(span, err)
		logger.Error(err, "Failed to reconcile pgBackRest TLS certificates")
		r.Recorder.Eventf(
			shard,
			"Warning",
			"CertError",
			"Failed to reconcile pgBackRest TLS certificates: %v",
			err,
		)
		return ctrl.Result{}, err
	}

	// Compute MultiOrch cells
	multiOrchCells, err := getMultiOrchCells(shard)
	if err != nil {
		monitoring.RecordSpanError(span, err)
		logger.Error(err, "Failed to determine MultiOrch cells")
		r.Recorder.Eventf(
			shard,
			"Warning",
			"ConfigError",
			"Failed to determine MultiOrch cells: %v",
			err,
		)
		return ctrl.Result{}, err
	}

	// Compute pool cells for shared backup PVCs (only cells with pool pods need backup storage)
	poolCells := getPoolCells(shard)

	// Reconcile MultiOrch - one Deployment and Service per cell
	{
		ctx, childSpan := monitoring.StartChildSpan(ctx, "Shard.ReconcileMultiOrch")
		for _, cell := range multiOrchCells {
			cellName := string(cell)

			// Reconcile MultiOrch Deployment for this cell
			if err := r.reconcileMultiOrchDeployment(ctx, shard, cellName); err != nil {
				monitoring.RecordSpanError(childSpan, err)
				childSpan.End()
				logger.Error(err, "Failed to reconcile MultiOrch Deployment", "cell", cellName)
				r.Recorder.Eventf(
					shard,
					"Warning",
					"FailedApply",
					"Failed to supply MultiOrch Deployment for cell %s: %v",
					cellName,
					err,
				)
				return ctrl.Result{}, err
			}

			// Reconcile MultiOrch Service for this cell
			if err := r.reconcileMultiOrchService(ctx, shard, cellName); err != nil {
				monitoring.RecordSpanError(childSpan, err)
				childSpan.End()
				logger.Error(err, "Failed to reconcile MultiOrch Service", "cell", cellName)
				r.Recorder.Eventf(
					shard,
					"Warning",
					"FailedApply",
					"Failed to supply MultiOrch Service for cell %s: %v",
					cellName,
					err,
				)
				return ctrl.Result{}, err
			}
		}
		childSpan.End()
	}

	// Reconcile Shared Backup PVCs (one per cell)
	{
		ctx, childSpan := monitoring.StartChildSpan(ctx, "Shard.ReconcileBackupPVCs")
		for _, cell := range poolCells {
			cellName := string(cell)
			if err := r.reconcileSharedBackupPVC(ctx, shard, cellName); err != nil {
				monitoring.RecordSpanError(childSpan, err)
				childSpan.End()
				logger.Error(err, "Failed to reconcile shared backup PVC", "cell", cellName)
				r.Recorder.Eventf(
					shard,
					"Warning",
					"FailedApply",
					"Failed to reconcile shared backup PVC for cell %s: %v",
					cellName,
					err,
				)
				return ctrl.Result{}, err
			}
		}
		childSpan.End()
	}

	{
		ctx, childSpan := monitoring.StartChildSpan(ctx, "Shard.ReconcilePools")
		for poolName, pool := range shard.Spec.Pools {
			if err := r.reconcilePool(ctx, shard, string(poolName), pool); err != nil {
				monitoring.RecordSpanError(childSpan, err)
				childSpan.End()
				logger.Error(err, "Failed to reconcile pool", "poolName", poolName)
				r.Recorder.Eventf(
					shard,
					"Warning",
					"FailedApply",
					"Failed to reconcile pool %s: %v",
					poolName,
					err,
				)
				return ctrl.Result{}, err
			}
		}
		childSpan.End()
	}

	// Data-plane phases: open a single topo connection shared across all phases.
	result, err := r.reconcileDataPlane(ctx, shard)
	if err != nil || result.RequeueAfter > 0 || result.Requeue {
		return result, err
	}

	// Update status
	{
		_, childSpan := monitoring.StartChildSpan(ctx, "Shard.UpdateStatus")
		if err := r.updateStatus(ctx, shard); err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			logger.Error(err, "Failed to update status")
			r.Recorder.Eventf(shard, "Warning", "StatusError", "Failed to update status: %v", err)
			return ctrl.Result{}, err
		}
		childSpan.End()
	}

	logger.V(1).Info("reconcile complete", "duration", time.Since(start).String())
	r.Recorder.Event(shard, "Normal", "Synced", "Successfully reconciled Shard")
	return ctrl.Result{}, nil
}

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

	// Phase: Execute drain state machine for pods with drain annotations
	{
		_, childSpan := monitoring.StartChildSpan(ctx, "Shard.ReconcileDrainState")
		requeue, err := r.reconcileDrainState(ctx, store, shard)
		if err != nil {
			monitoring.RecordSpanError(childSpan, err)
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
			} else if !result.Healthy {
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

// reconcilePool creates or updates the Pods, PVCs and headless Service for a pool.
// For pools spanning multiple cells, this creates resources per cell.
func (r *ShardReconciler) reconcilePool(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	poolName string,
	poolSpec multigresv1alpha1.PoolSpec,
) error {
	// Pools must have cells specified
	if len(poolSpec.Cells) == 0 {
		return fmt.Errorf(
			"pool %s has no cells specified - cannot deploy without cell information",
			poolName,
		)
	}

	// Create Pods and PVCs per cell
	// TODO(#91): Pool.Cells may contain duplicates - add +listType=set validation at API level
	for _, cell := range poolSpec.Cells {
		cellName := string(cell)

		// Reconcile pool Pods and PVCs for this cell
		if err := r.reconcilePoolPods(ctx, shard, poolName, cellName, poolSpec); err != nil {
			return fmt.Errorf("failed to reconcile pool pods for cell %s: %w", cellName, err)
		}

		// Reconcile pool PDB for this cell
		if err := r.reconcilePoolPDB(ctx, shard, poolName, cellName); err != nil {
			return fmt.Errorf("failed to reconcile pool PDB for cell %s: %w", cellName, err)
		}

		// Reconcile pool headless Service for this cell
		if err := r.reconcilePoolHeadlessService(
			ctx,
			shard,
			poolName,
			cellName,
			poolSpec,
		); err != nil {
			return fmt.Errorf(
				"failed to reconcile pool headless Service for cell %s: %w",
				cellName,
				err,
			)
		}
	}

	return nil
}

// getMultiOrchCells returns the list of cells where MultiOrch should be deployed.
// If MultiOrch.Cells is specified, it uses that.
// Otherwise, it infers cells from all pools (union of pool cells).
func getMultiOrchCells(shard *multigresv1alpha1.Shard) ([]multigresv1alpha1.CellName, error) {
	cells := shard.Spec.MultiOrch.Cells

	// If MultiOrch specifies cells explicitly, use them
	// TODO(#91): Add +listType=set validation to MultiOrch.Cells to prevent duplicates at API level
	if len(cells) > 0 {
		return cells, nil
	}

	// Otherwise, collect unique cells from all pools
	cellSet := make(map[multigresv1alpha1.CellName]bool)
	for _, pool := range shard.Spec.Pools {
		for _, cell := range pool.Cells {
			cellSet[cell] = true
		}
	}

	// Convert set to slice
	cells = make([]multigresv1alpha1.CellName, 0, len(cellSet))
	for cell := range cellSet {
		cells = append(cells, cell)
	}

	// If still no cells found, error
	if len(cells) == 0 {
		return nil, fmt.Errorf(
			"MultiOrch has no cells specified and no cells found in pools - cannot deploy without cell information",
		)
	}

	slices.Sort(cells)
	return cells, nil
}

// getPoolCells returns the deduplicated, sorted set of cells from all pools.
// Used for infrastructure that only needs to exist where pool pods run
// (e.g., shared backup PVCs).
func getPoolCells(shard *multigresv1alpha1.Shard) []multigresv1alpha1.CellName {
	cellSet := make(map[multigresv1alpha1.CellName]bool)
	for _, pool := range shard.Spec.Pools {
		for _, cell := range pool.Cells {
			cellSet[cell] = true
		}
	}

	cells := make([]multigresv1alpha1.CellName, 0, len(cellSet))
	for cell := range cellSet {
		cells = append(cells, cell)
	}

	slices.Sort(cells)
	return cells
}

// SetupWithManager sets up the controller with the Manager.
func (r *ShardReconciler) SetupWithManager(mgr ctrl.Manager, opts ...controller.Options) error {
	controllerOpts := controller.Options{
		MaxConcurrentReconciles: 20,
	}
	if len(opts) > 0 {
		controllerOpts = opts[0]
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&multigresv1alpha1.Shard{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		WithOptions(controllerOpts).
		Complete(r)
}

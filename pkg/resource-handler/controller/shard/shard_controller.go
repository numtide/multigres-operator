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
	"github.com/numtide/multigres-operator/pkg/monitoring"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
)

const (
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

	// Update status early so observedGeneration and pod-health phase are
	// always current. Later steps (especially reconcileDataPlane) may block
	// on the topo connection for an extended period; placing updateStatus
	// here guarantees the status subresource is written every reconcile.
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

	// Ensure PVC ownerRefs match the current PVCDeletionPolicy.
	// This handles mid-lifecycle policy changes (e.g., Retain → Delete).
	{
		ctx, childSpan := monitoring.StartChildSpan(ctx, "Shard.ReconcilePVCOwnerRefs")
		if err := r.reconcilePVCOwnerRefs(ctx, shard); err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			logger.Error(err, "Failed to reconcile PVC ownerRefs")
			r.Recorder.Eventf(
				shard,
				"Warning",
				"PVCOwnerRefError",
				"Failed to reconcile PVC ownerRefs: %v",
				err,
			)
			return ctrl.Result{}, err
		}
		childSpan.End()
	}

	// Data-plane phases: open a single topo connection shared across all phases.
	// Use a deadline so a hanging topo dial cannot block the reconcile
	// goroutine indefinitely, which would prevent future reconciles for
	// this shard and leave observedGeneration stale.
	dataPlaneCtx, dataPlaneCancel := context.WithTimeout(ctx, 30*time.Second)
	defer dataPlaneCancel()
	result, err := r.reconcileDataPlane(dataPlaneCtx, shard)
	if err != nil || result.RequeueAfter > 0 {
		return result, err
	}

	logger.V(1).Info("reconcile complete", "duration", time.Since(start).String())
	r.Recorder.Event(shard, "Normal", "Synced", "Successfully reconciled Shard")
	return ctrl.Result{}, nil
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

// ShouldDeletePVCOnShardRemoval returns true when the effective PVCDeletionPolicy
// for a pool resolves to Delete. Used by PVC builders to conditionally set
// a controller ownerRef so Kubernetes GC cascade-deletes the PVC with the Shard.
func ShouldDeletePVCOnShardRemoval(
	shard *multigresv1alpha1.Shard,
	poolSpec multigresv1alpha1.PoolSpec,
) bool {
	policy := multigresv1alpha1.MergePVCDeletionPolicy(
		poolSpec.PVCDeletionPolicy,
		shard.Spec.PVCDeletionPolicy,
	)
	return policy != nil && policy.WhenDeleted == multigresv1alpha1.DeletePVCRetentionPolicy
}

// ShouldDeleteShardLevelPVCOnRemoval returns true when the shard-level
// PVCDeletionPolicy resolves to Delete. Used for shared infrastructure PVCs
// (e.g., backup PVCs) that are not pool-specific.
func ShouldDeleteShardLevelPVCOnRemoval(shard *multigresv1alpha1.Shard) bool {
	p := shard.Spec.PVCDeletionPolicy
	return p != nil && p.WhenDeleted == multigresv1alpha1.DeletePVCRetentionPolicy
}

// reconcilePVCOwnerRefs ensures ownerRefs on existing PVCs match the current
// PVCDeletionPolicy. When policy is Delete, a controller ownerRef is added so
// Kubernetes GC cascade-deletes PVCs with the Shard. When policy is Retain,
// any existing controller ownerRef is removed so PVCs survive Shard deletion.
func (r *ShardReconciler) reconcilePVCOwnerRefs(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
	logger := log.FromContext(ctx)

	selector := map[string]string{
		metadata.LabelMultigresCluster:    shard.Labels[metadata.LabelMultigresCluster],
		metadata.LabelMultigresDatabase:   string(shard.Spec.DatabaseName),
		metadata.LabelMultigresTableGroup: string(shard.Spec.TableGroupName),
		metadata.LabelMultigresShard:      string(shard.Spec.ShardName),
	}

	pvcList := &corev1.PersistentVolumeClaimList{}
	if err := r.List(
		ctx,
		pvcList,
		client.InNamespace(shard.Namespace),
		client.MatchingLabels(selector),
	); err != nil {
		return fmt.Errorf("failed to list PVCs for ownerRef reconciliation: %w", err)
	}

	shardUID := shard.GetUID()

	for i := range pvcList.Items {
		pvc := &pvcList.Items[i]

		// Resolve effective policy for this PVC.
		poolName := pvc.Labels[metadata.LabelMultigresPool]
		wantOwnerRef := false

		if poolName != "" {
			if poolSpec, exists := shard.Spec.Pools[multigresv1alpha1.PoolName(poolName)]; exists {
				wantOwnerRef = ShouldDeletePVCOnShardRemoval(shard, poolSpec)
			} else {
				// Pool removed from spec — fall back to shard-level policy.
				wantOwnerRef = ShouldDeleteShardLevelPVCOnRemoval(shard)
			}
		} else {
			// Shared backup PVC — use shard-level policy.
			wantOwnerRef = ShouldDeleteShardLevelPVCOnRemoval(shard)
		}

		hasOwnerRef := false
		for _, ref := range pvc.OwnerReferences {
			if ref.UID == shardUID {
				hasOwnerRef = true
				break
			}
		}

		if wantOwnerRef && !hasOwnerRef {
			if err := ctrl.SetControllerReference(shard, pvc, r.Scheme); err != nil {
				return fmt.Errorf("failed to add ownerRef to PVC %s: %w", pvc.Name, err)
			}
			if err := r.Update(ctx, pvc); err != nil {
				return fmt.Errorf("failed to update PVC %s ownerRef: %w", pvc.Name, err)
			}
			logger.Info("Added ownerRef to PVC for Delete policy", "pvc", pvc.Name)
		} else if !wantOwnerRef && hasOwnerRef {
			filtered := make([]metav1.OwnerReference, 0, len(pvc.OwnerReferences))
			for _, ref := range pvc.OwnerReferences {
				if ref.UID != shardUID {
					filtered = append(filtered, ref)
				}
			}
			pvc.OwnerReferences = filtered
			if err := r.Update(ctx, pvc); err != nil {
				return fmt.Errorf("failed to remove ownerRef from PVC %s: %w", pvc.Name, err)
			}
			logger.Info("Removed ownerRef from PVC for Retain policy", "pvc", pvc.Name)
		}
	}

	return nil
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

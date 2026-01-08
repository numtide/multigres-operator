package shard

import (
	"context"
	"fmt"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

const (
	finalizerName = "shard.multigres.com/finalizer"
)

// ShardReconciler reconciles a Shard object.
type ShardReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile handles Shard resource reconciliation.
func (r *ShardReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Shard instance
	shard := &multigresv1alpha1.Shard{}
	if err := r.Get(ctx, req.NamespacedName, shard); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Shard resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Shard")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !shard.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, shard)
	}

	// Add finalizer if not present
	if !slices.Contains(shard.Finalizers, finalizerName) {
		shard.Finalizers = append(shard.Finalizers, finalizerName)
		if err := r.Update(ctx, shard); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Reconcile pg_hba ConfigMap first (required by all pools before StatefulSets start)
	if err := r.reconcilePgHbaConfigMap(ctx, shard); err != nil {
		logger.Error(err, "Failed to reconcile pg_hba ConfigMap")
		return ctrl.Result{}, err
	}

	// Reconcile MultiOrch - one Deployment and Service per cell
	multiOrchCells, err := getMultiOrchCells(shard)
	if err != nil {
		return ctrl.Result{}, err
	}

	for _, cell := range multiOrchCells {
		cellName := string(cell)

		// Reconcile MultiOrch Deployment for this cell
		if err := r.reconcileMultiOrchDeployment(ctx, shard, cellName); err != nil {
			logger.Error(err, "Failed to reconcile MultiOrch Deployment", "cell", cellName)
			return ctrl.Result{}, err
		}

		// Reconcile MultiOrch Service for this cell
		if err := r.reconcileMultiOrchService(ctx, shard, cellName); err != nil {
			logger.Error(err, "Failed to reconcile MultiOrch Service", "cell", cellName)
			return ctrl.Result{}, err
		}
	}

	// Reconcile each pool
	for poolName, pool := range shard.Spec.Pools {
		if err := r.reconcilePool(ctx, shard, poolName, pool); err != nil {
			logger.Error(err, "Failed to reconcile pool", "poolName", poolName)
			return ctrl.Result{}, err
		}
	}

	// Update status
	if err := r.updateStatus(ctx, shard); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// handleDeletion handles cleanup when Shard is being deleted.
func (r *ShardReconciler) handleDeletion(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if slices.Contains(shard.Finalizers, finalizerName) {
		// Perform cleanup if needed
		// Currently no special cleanup required - owner references handle resource deletion

		// Remove finalizer
		shard.Finalizers = slices.DeleteFunc(shard.Finalizers, func(s string) bool {
			return s == finalizerName
		})
		if err := r.Update(ctx, shard); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// reconcileMultiOrchDeployment creates or updates the MultiOrch Deployment for a specific cell.
func (r *ShardReconciler) reconcileMultiOrchDeployment(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	cellName string,
) error {
	desired, err := BuildMultiOrchDeployment(shard, cellName, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build MultiOrch Deployment: %w", err)
	}

	existing := &appsv1.Deployment{}
	err = r.Get(
		ctx,
		client.ObjectKey{Namespace: shard.Namespace, Name: desired.Name},
		existing,
	)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new Deployment
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create MultiOrch Deployment: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get MultiOrch Deployment: %w", err)
	}

	// Update existing Deployment
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update MultiOrch Deployment: %w", err)
	}

	return nil
}

// reconcilePgHbaConfigMap creates or updates the pg_hba ConfigMap for a shard.
// This ConfigMap is shared across all pools and contains the authentication template.
func (r *ShardReconciler) reconcilePgHbaConfigMap(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
	desired, err := BuildPgHbaConfigMap(shard, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build pg_hba ConfigMap: %w", err)
	}

	existing := &corev1.ConfigMap{}
	err = r.Get(
		ctx,
		client.ObjectKey{Namespace: shard.Namespace, Name: desired.Name},
		existing,
	)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new ConfigMap
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create pg_hba ConfigMap: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get pg_hba ConfigMap: %w", err)
	}

	// Update existing ConfigMap
	existing.Data = desired.Data
	existing.Labels = desired.Labels
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update pg_hba ConfigMap: %w", err)
	}

	return nil
}

// reconcileMultiOrchService creates or updates the MultiOrch Service for a specific cell.
func (r *ShardReconciler) reconcileMultiOrchService(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	cellName string,
) error {
	desired, err := BuildMultiOrchService(shard, cellName, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build MultiOrch Service: %w", err)
	}

	existing := &corev1.Service{}
	err = r.Get(
		ctx,
		client.ObjectKey{Namespace: shard.Namespace, Name: desired.Name},
		existing,
	)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new Service
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create MultiOrch Service: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get MultiOrch Service: %w", err)
	}

	// Update existing Service
	existing.Spec.Ports = desired.Spec.Ports
	existing.Spec.Selector = desired.Spec.Selector
	existing.Labels = desired.Labels
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update MultiOrch Service: %w", err)
	}

	return nil
}

// reconcilePool creates or updates the StatefulSet and headless Service for a pool.
// For pools spanning multiple cells, this creates one StatefulSet per cell.
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

	// Create one StatefulSet per cell
	// TODO(#91): Pool.Cells may contain duplicates - add +listType=set validation at API level
	for _, cell := range poolSpec.Cells {
		cellName := string(cell)

		// Reconcile backup PVC before StatefulSet (PVC must exist first) - only if using PVC storage
		if poolSpec.BackupStorageType == "pvc" {
			if err := r.reconcilePoolBackupPVC(ctx, shard, poolName, cellName, poolSpec); err != nil {
				return fmt.Errorf("failed to reconcile backup PVC for cell %s: %w", cellName, err)
			}
		}

		// Reconcile pool StatefulSet for this cell
		if err := r.reconcilePoolStatefulSet(ctx, shard, poolName, cellName, poolSpec); err != nil {
			return fmt.Errorf("failed to reconcile pool StatefulSet for cell %s: %w", cellName, err)
		}

		// Reconcile pool headless Service for this cell
		if err := r.reconcilePoolHeadlessService(ctx, shard, poolName, cellName, poolSpec); err != nil {
			return fmt.Errorf(
				"failed to reconcile pool headless Service for cell %s: %w",
				cellName,
				err,
			)
		}
	}

	return nil
}

// reconcilePoolStatefulSet creates or updates the StatefulSet for a pool in a specific cell.
func (r *ShardReconciler) reconcilePoolStatefulSet(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	poolName string,
	cellName string,
	poolSpec multigresv1alpha1.PoolSpec,
) error {
	desired, err := BuildPoolStatefulSet(shard, poolName, cellName, poolSpec, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build pool StatefulSet: %w", err)
	}

	existing := &appsv1.StatefulSet{}
	err = r.Get(
		ctx,
		client.ObjectKey{Namespace: shard.Namespace, Name: desired.Name},
		existing,
	)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new StatefulSet
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create pool StatefulSet: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get pool StatefulSet: %w", err)
	}

	// Update existing StatefulSet
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update pool StatefulSet: %w", err)
	}

	return nil
}

// reconcilePoolBackupPVC creates or updates the shared backup PVC for a pool in a specific cell.
func (r *ShardReconciler) reconcilePoolBackupPVC(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	poolName string,
	cellName string,
	poolSpec multigresv1alpha1.PoolSpec,
) error {
	desired, err := BuildBackupPVC(shard, poolName, cellName, poolSpec, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build backup PVC: %w", err)
	}

	existing := &corev1.PersistentVolumeClaim{}
	err = r.Get(
		ctx,
		client.ObjectKey{Namespace: shard.Namespace, Name: desired.Name},
		existing,
	)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new PVC
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create backup PVC: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get backup PVC: %w", err)
	}

	// PVCs are immutable after creation, only update labels/annotations if needed
	if desired.Labels != nil {
		existing.Labels = desired.Labels
		if err := r.Update(ctx, existing); err != nil {
			return fmt.Errorf("failed to update backup PVC labels: %w", err)
		}
	}

	return nil
}

// reconcilePoolHeadlessService creates or updates the headless Service for a pool in a specific cell.
func (r *ShardReconciler) reconcilePoolHeadlessService(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	poolName string,
	cellName string,
	poolSpec multigresv1alpha1.PoolSpec,
) error {
	desired, err := BuildPoolHeadlessService(shard, poolName, cellName, poolSpec, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build pool headless Service: %w", err)
	}

	existing := &corev1.Service{}
	err = r.Get(
		ctx,
		client.ObjectKey{Namespace: shard.Namespace, Name: desired.Name},
		existing,
	)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new Service
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create pool headless Service: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get pool headless Service: %w", err)
	}

	// Update existing Service
	existing.Spec.Ports = desired.Spec.Ports
	existing.Spec.Selector = desired.Spec.Selector
	existing.Labels = desired.Labels
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update pool headless Service: %w", err)
	}

	return nil
}

// updateStatus updates the Shard status based on observed state.
func (r *ShardReconciler) updateStatus(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
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

	// Update conditions
	shard.Status.Conditions = r.buildConditions(shard, totalPods, readyPods)

	if err := r.Status().Update(ctx, shard); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	return nil
}

// updatePoolsStatus aggregates status from all pool StatefulSets.
// Returns total pods, ready pods, and tracks cells in the cellsSet.
func (r *ShardReconciler) updatePoolsStatus(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	cellsSet map[multigresv1alpha1.CellName]bool,
) (int32, int32, error) {
	var totalPods, readyPods int32

	for poolName, poolSpec := range shard.Spec.Pools {
		// TODO(#91): Pool.Cells may contain duplicates - add +listType=set validation at API level
		for _, cell := range poolSpec.Cells {
			cellName := string(cell)
			cellsSet[cell] = true

			stsName := buildPoolNameWithCell(shard.Name, poolName, cellName)
			sts := &appsv1.StatefulSet{}
			err := r.Get(
				ctx,
				client.ObjectKey{Namespace: shard.Namespace, Name: stsName},
				sts,
			)
			if err != nil {
				if errors.IsNotFound(err) {
					continue
				}
				return 0, 0, fmt.Errorf("failed to get pool StatefulSet for status: %w", err)
			}

			totalPods += sts.Status.Replicas
			readyPods += sts.Status.ReadyReplicas
		}
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

		deployName := buildMultiOrchNameWithCell(shard.Name, cellName)
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
		if deploy.Spec.Replicas == nil || deploy.Status.ReadyReplicas != *deploy.Spec.Replicas {
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
	return cells
}

// buildConditions creates status conditions based on observed state.
func (r *ShardReconciler) buildConditions(
	shard *multigresv1alpha1.Shard,
	totalPods, readyPods int32,
) []metav1.Condition {
	conditions := []metav1.Condition{}

	// Available condition
	availableCondition := metav1.Condition{
		Type:               "Available",
		ObservedGeneration: shard.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if readyPods == totalPods && totalPods > 0 {
		availableCondition.Status = metav1.ConditionTrue
		availableCondition.Reason = "AllPodsReady"
		availableCondition.Message = fmt.Sprintf("All %d pods are ready", readyPods)
	} else {
		availableCondition.Status = metav1.ConditionFalse
		availableCondition.Reason = "NotAllPodsReady"
		availableCondition.Message = fmt.Sprintf("%d/%d pods ready", readyPods, totalPods)
	}

	conditions = append(conditions, availableCondition)
	return conditions
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

	return cells, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ShardReconciler) SetupWithManager(mgr ctrl.Manager, opts ...controller.Options) error {
	controllerOpts := controller.Options{}
	if len(opts) > 0 {
		controllerOpts = opts[0]
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&multigresv1alpha1.Shard{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		WithOptions(controllerOpts).
		Complete(r)
}

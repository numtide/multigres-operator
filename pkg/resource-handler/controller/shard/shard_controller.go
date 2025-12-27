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

	// Reconcile MultiOrch Deployment
	if err := r.reconcileMultiOrchDeployment(ctx, shard); err != nil {
		logger.Error(err, "Failed to reconcile MultiOrch Deployment")
		return ctrl.Result{}, err
	}

	// Reconcile MultiOrch Service
	if err := r.reconcileMultiOrchService(ctx, shard); err != nil {
		logger.Error(err, "Failed to reconcile MultiOrch Service")
		return ctrl.Result{}, err
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

// reconcileMultiOrchDeployment creates or updates the MultiOrch Deployment.
func (r *ShardReconciler) reconcileMultiOrchDeployment(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
	desired, err := BuildMultiOrchDeployment(shard, r.Scheme)
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

// reconcileMultiOrchService creates or updates the MultiOrch Service.
func (r *ShardReconciler) reconcileMultiOrchService(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
	desired, err := BuildMultiOrchService(shard, r.Scheme)
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
	cells := poolSpec.Cells
	if len(cells) == 0 {
		return fmt.Errorf(
			"pool %s has no cells specified - cannot deploy without cell information",
			poolName,
		)
	}

	// Create one StatefulSet per cell
	for _, cell := range cells {
		cellName := string(cell)

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
	var totalPods, readyPods int32

	// Aggregate status from all pool StatefulSets
	for poolName := range shard.Spec.Pools {
		stsName := buildPoolName(shard.Name, poolName)
		sts := &appsv1.StatefulSet{}
		err := r.Get(
			ctx,
			client.ObjectKey{Namespace: shard.Namespace, Name: stsName},
			sts,
		)
		if err != nil {
			if errors.IsNotFound(err) {
				// StatefulSet not created yet, skip
				continue
			}
			return fmt.Errorf("failed to get pool StatefulSet for status: %w", err)
		}

		totalPods += sts.Status.Replicas
		readyPods += sts.Status.ReadyReplicas
	}

	// Update status fields
	shard.Status.PoolsReady = (totalPods > 0 && totalPods == readyPods)
	// TODO: Add OrchReady status check when MultiOrch deployment is implemented
	// shard.Status.OrchReady = ...

	// Update conditions
	shard.Status.Conditions = r.buildConditions(shard, totalPods, readyPods)

	if err := r.Status().Update(ctx, shard); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	return nil
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

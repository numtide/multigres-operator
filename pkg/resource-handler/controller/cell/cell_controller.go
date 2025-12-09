package cell

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
	finalizerName = "cell.multigres.com/finalizer"
)

// CellReconciler reconciles a Cell object.
type CellReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=multigres.com,resources=cells,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multigres.com,resources=cells/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=multigres.com,resources=cells/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles Cell resource reconciliation.
func (r *CellReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Cell instance
	cell := &multigresv1alpha1.Cell{}
	if err := r.Get(ctx, req.NamespacedName, cell); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Cell resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Cell")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !cell.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, cell)
	}

	// Add finalizer if not present
	if !slices.Contains(cell.Finalizers, finalizerName) {
		cell.Finalizers = append(cell.Finalizers, finalizerName)
		if err := r.Update(ctx, cell); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Reconcile MultiGateway Deployment
	if err := r.reconcileMultiGatewayDeployment(ctx, cell); err != nil {
		logger.Error(err, "Failed to reconcile MultiGateway Deployment")
		return ctrl.Result{}, err
	}

	// Reconcile MultiGateway Service
	if err := r.reconcileMultiGatewayService(ctx, cell); err != nil {
		logger.Error(err, "Failed to reconcile MultiGateway Service")
		return ctrl.Result{}, err
	}

	// Update status
	if err := r.updateStatus(ctx, cell); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// handleDeletion handles cleanup when Cell is being deleted.
func (r *CellReconciler) handleDeletion(
	ctx context.Context,
	cell *multigresv1alpha1.Cell,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if slices.Contains(cell.Finalizers, finalizerName) {
		// Perform cleanup if needed
		// Currently no special cleanup required - owner references handle resource deletion

		// Remove finalizer
		cell.Finalizers = slices.DeleteFunc(cell.Finalizers, func(s string) bool {
			return s == finalizerName
		})
		if err := r.Update(ctx, cell); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// reconcileMultiGatewayDeployment creates or updates the MultiGateway Deployment.
func (r *CellReconciler) reconcileMultiGatewayDeployment(
	ctx context.Context,
	cell *multigresv1alpha1.Cell,
) error {
	desired, err := BuildMultiGatewayDeployment(cell, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build MultiGateway Deployment: %w", err)
	}

	existing := &appsv1.Deployment{}
	name := cell.Name + "-multigateway"
	err = r.Get(ctx, client.ObjectKey{Namespace: cell.Namespace, Name: name}, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new Deployment
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create MultiGateway Deployment: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get MultiGateway Deployment: %w", err)
	}

	// Update existing Deployment
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update MultiGateway Deployment: %w", err)
	}

	return nil
}

// reconcileMultiGatewayService creates or updates the MultiGateway Service.
func (r *CellReconciler) reconcileMultiGatewayService(
	ctx context.Context,
	cell *multigresv1alpha1.Cell,
) error {
	desired, err := BuildMultiGatewayService(cell, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build MultiGateway Service: %w", err)
	}

	existing := &corev1.Service{}
	name := cell.Name + "-multigateway"
	err = r.Get(ctx, client.ObjectKey{Namespace: cell.Namespace, Name: name}, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new Service
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create MultiGateway Service: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get MultiGateway Service: %w", err)
	}

	// Update existing Service
	existing.Spec.Ports = desired.Spec.Ports
	existing.Spec.Selector = desired.Spec.Selector
	existing.Labels = desired.Labels
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update MultiGateway Service: %w", err)
	}

	return nil
}

// updateStatus updates the Cell status based on observed state.
func (r *CellReconciler) updateStatus(ctx context.Context, cell *multigresv1alpha1.Cell) error {
	// Get the MultiGateway Deployment to check status
	mgDeploy := &appsv1.Deployment{}
	err := r.Get(
		ctx,
		client.ObjectKey{Namespace: cell.Namespace, Name: cell.Name + "-multigateway"},
		mgDeploy,
	)
	if err != nil {
		if errors.IsNotFound(err) {
			// Deployment not created yet
			return nil
		}
		return fmt.Errorf("failed to get MultiGateway Deployment for status: %w", err)
	}

	// Update status fields
	cell.Status.ObservedGeneration = cell.Generation

	// Update conditions
	cell.Status.Conditions = r.buildConditions(cell, mgDeploy)

	if err := r.Status().Update(ctx, cell); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	return nil
}

// buildConditions creates status conditions based on observed state.
func (r *CellReconciler) buildConditions(
	cell *multigresv1alpha1.Cell,
	mgDeploy *appsv1.Deployment,
) []metav1.Condition {
	conditions := []metav1.Condition{}

	// Ready condition - MultiGateway must be ready
	readyCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: cell.Generation,
		LastTransitionTime: metav1.Now(),
	}

	mgReady := mgDeploy.Status.ReadyReplicas == mgDeploy.Status.Replicas &&
		mgDeploy.Status.Replicas > 0

	if mgReady {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "MultiGatewayReady"
		readyCondition.Message = fmt.Sprintf(
			"MultiGateway %d/%d ready",
			mgDeploy.Status.ReadyReplicas,
			mgDeploy.Status.Replicas,
		)
	} else {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "MultiGatewayNotReady"
		readyCondition.Message = fmt.Sprintf("MultiGateway %d/%d ready", mgDeploy.Status.ReadyReplicas, mgDeploy.Status.Replicas)
	}

	conditions = append(conditions, readyCondition)
	return conditions
}

// SetupWithManager sets up the controller with the Manager.
func (r *CellReconciler) SetupWithManager(mgr ctrl.Manager, opts ...controller.Options) error {
	controllerOpts := controller.Options{}
	if len(opts) > 0 {
		controllerOpts = opts[0]
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&multigresv1alpha1.Cell{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		WithOptions(controllerOpts).
		Complete(r)
}

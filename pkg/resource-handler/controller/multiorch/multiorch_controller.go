package multiorch

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
	"sigs.k8s.io/controller-runtime/pkg/log"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

const (
	finalizerName = "multiorch.multigres.com/finalizer"
)

// MultiOrchReconciler reconciles a MultiOrch object.
type MultiOrchReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=multigres.com,resources=multiorches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multigres.com,resources=multiorches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=multigres.com,resources=multiorches/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles MultiOrch resource reconciliation.
func (r *MultiOrchReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the MultiOrch instance
	multiorch := &multigresv1alpha1.MultiOrch{}
	if err := r.Get(ctx, req.NamespacedName, multiorch); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("MultiOrch resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get MultiOrch")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !multiorch.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, multiorch)
	}

	// Add finalizer if not present
	if !slices.Contains(multiorch.Finalizers, finalizerName) {
		multiorch.Finalizers = append(multiorch.Finalizers, finalizerName)
		if err := r.Update(ctx, multiorch); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Reconcile Deployment
	if err := r.reconcileDeployment(ctx, multiorch); err != nil {
		logger.Error(err, "Failed to reconcile Deployment")
		return ctrl.Result{}, err
	}

	// Reconcile Service
	if err := r.reconcileService(ctx, multiorch); err != nil {
		logger.Error(err, "Failed to reconcile Service")
		return ctrl.Result{}, err
	}

	// Update status
	if err := r.updateStatus(ctx, multiorch); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// handleDeletion handles cleanup when MultiOrch is being deleted.
func (r *MultiOrchReconciler) handleDeletion(
	ctx context.Context,
	multiorch *multigresv1alpha1.MultiOrch,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if slices.Contains(multiorch.Finalizers, finalizerName) {
		// Perform cleanup if needed
		// Currently no special cleanup required - owner references handle resource deletion

		// Remove finalizer
		multiorch.Finalizers = slices.DeleteFunc(multiorch.Finalizers, func(s string) bool {
			return s == finalizerName
		})
		if err := r.Update(ctx, multiorch); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// reconcileDeployment creates or updates the Deployment for MultiOrch.
func (r *MultiOrchReconciler) reconcileDeployment(
	ctx context.Context,
	multiorch *multigresv1alpha1.MultiOrch,
) error {
	desired, err := BuildDeployment(multiorch, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build Deployment: %w", err)
	}

	existing := &appsv1.Deployment{}
	err = r.Get(ctx, client.ObjectKey{Namespace: multiorch.Namespace, Name: multiorch.Name}, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new Deployment
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create Deployment: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get Deployment: %w", err)
	}

	// Update existing Deployment
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update Deployment: %w", err)
	}

	return nil
}

// reconcileService creates or updates the Service for MultiOrch.
func (r *MultiOrchReconciler) reconcileService(
	ctx context.Context,
	multiorch *multigresv1alpha1.MultiOrch,
) error {
	desired, err := BuildService(multiorch, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build Service: %w", err)
	}

	existing := &corev1.Service{}
	err = r.Get(ctx, client.ObjectKey{Namespace: multiorch.Namespace, Name: multiorch.Name}, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new Service
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create Service: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get Service: %w", err)
	}

	// Update existing Service
	existing.Spec.Ports = desired.Spec.Ports
	existing.Spec.Selector = desired.Spec.Selector
	existing.Spec.Type = desired.Spec.Type
	existing.Labels = desired.Labels
	existing.Annotations = desired.Annotations
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update Service: %w", err)
	}

	return nil
}

// updateStatus updates the MultiOrch status based on observed state.
func (r *MultiOrchReconciler) updateStatus(
	ctx context.Context,
	multiorch *multigresv1alpha1.MultiOrch,
) error {
	// Get the Deployment to check status
	dp := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{Namespace: multiorch.Namespace, Name: multiorch.Name}, dp)
	if err != nil {
		if errors.IsNotFound(err) {
			// Deployment not created yet
			return nil
		}
		return fmt.Errorf("failed to get Deployment for status: %w", err)
	}

	// Update status fields
	multiorch.Status.Replicas = dp.Status.Replicas
	multiorch.Status.ReadyReplicas = dp.Status.ReadyReplicas
	multiorch.Status.Ready = dp.Status.ReadyReplicas == dp.Status.Replicas && dp.Status.Replicas > 0
	multiorch.Status.ObservedGeneration = multiorch.Generation

	// Update conditions
	multiorch.Status.Conditions = r.buildConditions(multiorch, dp)

	if err := r.Status().Update(ctx, multiorch); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	return nil
}

// buildConditions creates status conditions based on observed state.
func (r *MultiOrchReconciler) buildConditions(
	multiorch *multigresv1alpha1.MultiOrch,
	dp *appsv1.Deployment,
) []metav1.Condition {
	conditions := []metav1.Condition{}

	// Ready condition
	readyCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: multiorch.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if dp.Status.ReadyReplicas == dp.Status.Replicas && dp.Status.Replicas > 0 {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "AllReplicasReady"
		readyCondition.Message = fmt.Sprintf("All %d replicas are ready", dp.Status.ReadyReplicas)
	} else {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "NotAllReplicasReady"
		readyCondition.Message = fmt.Sprintf("%d/%d replicas ready", dp.Status.ReadyReplicas, dp.Status.Replicas)
	}

	conditions = append(conditions, readyCondition)
	return conditions
}

// SetupWithManager sets up the controller with the Manager.
// TODO: This is missing test coverage, and will need to use envtest setup.
func (r *MultiOrchReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&multigresv1alpha1.MultiOrch{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}

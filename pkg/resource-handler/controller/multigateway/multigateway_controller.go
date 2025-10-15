package multigateway

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
	finalizerName = "multigateway.multigres.com/finalizer"
)

// MultiGatewayReconciler reconciles an MultiGateway object.
type MultiGatewayReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=multigres.com,resources=multigateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multigres.com,resources=multigateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=multigres.com,resources=multigateways/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles MultiGateway resource reconciliation.
func (r *MultiGatewayReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the MultiGateway instance
	mg := &multigresv1alpha1.MultiGateway{}
	if err := r.Get(ctx, req.NamespacedName, mg); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("MultiGateway resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get MultiGateway")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !mg.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, mg)
	}

	// Add finalizer if not present
	if !slices.Contains(mg.Finalizers, finalizerName) {
		mg.Finalizers = append(mg.Finalizers, finalizerName)
		if err := r.Update(ctx, mg); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Reconcile StatefulSet
	if err := r.reconcileDeployment(ctx, mg); err != nil {
		logger.Error(err, "Failed to reconcile Deployment")
		return ctrl.Result{}, err
	}

	// Reconcile Service
	if err := r.reconcileClientService(ctx, mg); err != nil {
		logger.Error(err, "Failed to reconcile client Service")
		return ctrl.Result{}, err
	}

	// Update status
	if err := r.updateStatus(ctx, mg); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// handleDeletion handles cleanup when MultiGateway is being deleted.
func (r *MultiGatewayReconciler) handleDeletion(
	ctx context.Context,
	mg *multigresv1alpha1.MultiGateway,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if slices.Contains(mg.Finalizers, finalizerName) {
		// Perform cleanup if needed
		// Currently no special cleanup required - owner references handle resource deletion

		// Remove finalizer
		mg.Finalizers = slices.DeleteFunc(mg.Finalizers, func(s string) bool {
			return s == finalizerName
		})
		if err := r.Update(ctx, mg); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// reconcileDeployment creates or updates the Deployment for MultiGateway.
func (r *MultiGatewayReconciler) reconcileDeployment(
	ctx context.Context,
	mg *multigresv1alpha1.MultiGateway,
) error {
	desired, err := BuildDeployment(mg, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build Deployment: %w", err)
	}

	existing := &appsv1.Deployment{}
	err = r.Get(ctx, client.ObjectKey{Namespace: mg.Namespace, Name: mg.Name}, existing)
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

// reconcileClientService creates or updates the client Service for MultiGateway.
func (r *MultiGatewayReconciler) reconcileClientService(
	ctx context.Context,
	mg *multigresv1alpha1.MultiGateway,
) error {
	desired, err := BuildClientService(mg, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build client Service: %w", err)
	}

	existing := &corev1.Service{}
	err = r.Get(ctx, client.ObjectKey{Namespace: mg.Namespace, Name: mg.Name}, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new Service
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create client Service: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get client Service: %w", err)
	}

	// Update existing Service
	existing.Spec.Ports = desired.Spec.Ports
	existing.Spec.Selector = desired.Spec.Selector
	existing.Labels = desired.Labels
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update client Service: %w", err)
	}

	return nil
}

// updateStatus updates the Etcd status based on observed state.
func (r *MultiGatewayReconciler) updateStatus(
	ctx context.Context,
	mg *multigresv1alpha1.MultiGateway,
) error {
	// Get the Deployment to check status
	dp := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{Namespace: mg.Namespace, Name: mg.Name}, dp)
	if err != nil {
		if errors.IsNotFound(err) {
			// Deployment not created yet
			return nil
		}
		return fmt.Errorf("failed to get Deployment for status: %w", err)
	}

	// Update status fields
	mg.Status.Replicas = dp.Status.Replicas
	mg.Status.ReadyReplicas = dp.Status.ReadyReplicas
	mg.Status.Ready = dp.Status.ReadyReplicas == dp.Status.Replicas && dp.Status.Replicas > 0
	mg.Status.ObservedGeneration = mg.Generation

	// Update conditions
	mg.Status.Conditions = r.buildConditions(mg, dp)

	if err := r.Status().Update(ctx, mg); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	return nil
}

// buildConditions creates status conditions based on observed state.
func (r *MultiGatewayReconciler) buildConditions(
	mg *multigresv1alpha1.MultiGateway,
	sts *appsv1.Deployment,
) []metav1.Condition {
	conditions := []metav1.Condition{}

	// Ready condition
	readyCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: mg.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if sts.Status.ReadyReplicas == sts.Status.Replicas && sts.Status.Replicas > 0 {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "AllReplicasReady"
		readyCondition.Message = fmt.Sprintf("All %d replicas are ready", sts.Status.ReadyReplicas)
	} else {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "NotAllReplicasReady"
		readyCondition.Message = fmt.Sprintf("%d/%d replicas ready", sts.Status.ReadyReplicas, sts.Status.Replicas)
	}

	conditions = append(conditions, readyCondition)
	return conditions
}

// SetupWithManager sets up the controller with the Manager.
func (r *MultiGatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&multigresv1alpha1.MultiGateway{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}

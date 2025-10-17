package multipooler

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
	finalizerName = "multipooler.multigres.com/finalizer"
)

// MultiPoolerReconciler reconciles a MultiPooler object.
type MultiPoolerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=multigres.com,resources=multipoolers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multigres.com,resources=multipoolers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=multigres.com,resources=multipoolers/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles MultiPooler resource reconciliation.
func (r *MultiPoolerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the MultiPooler instance
	multipooler := &multigresv1alpha1.MultiPooler{}
	if err := r.Get(ctx, req.NamespacedName, multipooler); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("MultiPooler resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get MultiPooler")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !multipooler.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, multipooler)
	}

	// Add finalizer if not present
	if !slices.Contains(multipooler.Finalizers, finalizerName) {
		multipooler.Finalizers = append(multipooler.Finalizers, finalizerName)
		if err := r.Update(ctx, multipooler); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Reconcile StatefulSet
	if err := r.reconcileStatefulSet(ctx, multipooler); err != nil {
		logger.Error(err, "Failed to reconcile StatefulSet")
		return ctrl.Result{}, err
	}

	// Reconcile headless Service
	if err := r.reconcileHeadlessService(ctx, multipooler); err != nil {
		logger.Error(err, "Failed to reconcile headless Service")
		return ctrl.Result{}, err
	}

	// Update status
	if err := r.updateStatus(ctx, multipooler); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// handleDeletion handles cleanup when MultiPooler is being deleted.
func (r *MultiPoolerReconciler) handleDeletion(
	ctx context.Context,
	multipooler *multigresv1alpha1.MultiPooler,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if slices.Contains(multipooler.Finalizers, finalizerName) {
		// Perform cleanup if needed
		// Currently no special cleanup required - owner references handle resource deletion

		// Remove finalizer
		multipooler.Finalizers = slices.DeleteFunc(multipooler.Finalizers, func(s string) bool {
			return s == finalizerName
		})
		if err := r.Update(ctx, multipooler); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// reconcileStatefulSet creates or updates the StatefulSet for MultiPooler.
func (r *MultiPoolerReconciler) reconcileStatefulSet(
	ctx context.Context,
	multipooler *multigresv1alpha1.MultiPooler,
) error {
	desired, err := BuildStatefulSet(multipooler, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build StatefulSet: %w", err)
	}

	existing := &appsv1.StatefulSet{}
	err = r.Get(ctx, client.ObjectKey{Namespace: multipooler.Namespace, Name: multipooler.Name}, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new StatefulSet
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create StatefulSet: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	// Update existing StatefulSet
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update StatefulSet: %w", err)
	}

	return nil
}

// reconcileHeadlessService creates or updates the headless Service for MultiPooler.
func (r *MultiPoolerReconciler) reconcileHeadlessService(
	ctx context.Context,
	multipooler *multigresv1alpha1.MultiPooler,
) error {
	desired, err := BuildHeadlessService(multipooler, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build headless Service: %w", err)
	}

	existing := &corev1.Service{}
	err = r.Get(
		ctx,
		client.ObjectKey{Namespace: multipooler.Namespace, Name: multipooler.Name + "-headless"},
		existing,
	)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new Service
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create headless Service: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get headless Service: %w", err)
	}

	// Update existing Service
	existing.Spec.Ports = desired.Spec.Ports
	existing.Spec.Selector = desired.Spec.Selector
	existing.Labels = desired.Labels
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update headless Service: %w", err)
	}

	return nil
}

// updateStatus updates the MultiPooler status based on observed state.
func (r *MultiPoolerReconciler) updateStatus(ctx context.Context, multipooler *multigresv1alpha1.MultiPooler) error {
	// Get the StatefulSet to check status
	sts := &appsv1.StatefulSet{}
	err := r.Get(ctx, client.ObjectKey{Namespace: multipooler.Namespace, Name: multipooler.Name}, sts)
	if err != nil {
		if errors.IsNotFound(err) {
			// StatefulSet not created yet
			return nil
		}
		return fmt.Errorf("failed to get StatefulSet for status: %w", err)
	}

	// Update status fields
	multipooler.Status.Replicas = sts.Status.Replicas
	multipooler.Status.ReadyReplicas = sts.Status.ReadyReplicas
	multipooler.Status.Ready = sts.Status.ReadyReplicas == sts.Status.Replicas && sts.Status.Replicas > 0
	multipooler.Status.ObservedGeneration = multipooler.Generation

	// Update conditions
	multipooler.Status.Conditions = r.buildConditions(multipooler, sts)

	if err := r.Status().Update(ctx, multipooler); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	return nil
}

// buildConditions creates status conditions based on observed state.
func (r *MultiPoolerReconciler) buildConditions(
	multipooler *multigresv1alpha1.MultiPooler,
	sts *appsv1.StatefulSet,
) []metav1.Condition {
	conditions := []metav1.Condition{}

	// Ready condition
	readyCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: multipooler.Generation,
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
// TODO: This is missing test coverage, and will need to use envtest setup.
func (r *MultiPoolerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&multigresv1alpha1.MultiPooler{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Complete(r)
}

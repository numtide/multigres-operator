package etcd

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
	finalizerName = "etcd.multigres.com/finalizer"
)

// EtcdReconciler reconciles an Etcd object.
type EtcdReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=multigres.com,resources=etcds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multigres.com,resources=etcds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=multigres.com,resources=etcds/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles Etcd resource reconciliation.
func (r *EtcdReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Etcd instance
	etcd := &multigresv1alpha1.Etcd{}
	if err := r.Get(ctx, req.NamespacedName, etcd); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Etcd resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Etcd")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !etcd.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, etcd)
	}

	// Add finalizer if not present
	if !slices.Contains(etcd.Finalizers, finalizerName) {
		etcd.Finalizers = append(etcd.Finalizers, finalizerName)
		if err := r.Update(ctx, etcd); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Reconcile StatefulSet
	if err := r.reconcileStatefulSet(ctx, etcd); err != nil {
		logger.Error(err, "Failed to reconcile StatefulSet")
		return ctrl.Result{}, err
	}

	// Reconcile headless Service
	if err := r.reconcileHeadlessService(ctx, etcd); err != nil {
		logger.Error(err, "Failed to reconcile headless Service")
		return ctrl.Result{}, err
	}

	// Reconcile client Service
	if err := r.reconcileClientService(ctx, etcd); err != nil {
		logger.Error(err, "Failed to reconcile client Service")
		return ctrl.Result{}, err
	}

	// Update status
	if err := r.updateStatus(ctx, etcd); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// handleDeletion handles cleanup when Etcd is being deleted.
func (r *EtcdReconciler) handleDeletion(ctx context.Context, etcd *multigresv1alpha1.Etcd) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if slices.Contains(etcd.Finalizers, finalizerName) {
		// Perform cleanup if needed
		// Currently no special cleanup required - owner references handle resource deletion

		// Remove finalizer
		etcd.Finalizers = slices.DeleteFunc(etcd.Finalizers, func(s string) bool {
			return s == finalizerName
		})
		if err := r.Update(ctx, etcd); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// reconcileStatefulSet creates or updates the StatefulSet for Etcd.
func (r *EtcdReconciler) reconcileStatefulSet(ctx context.Context, etcd *multigresv1alpha1.Etcd) error {
	desired, err := BuildStatefulSet(etcd, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build StatefulSet: %w", err)
	}

	existing := &appsv1.StatefulSet{}
	err = r.Get(ctx, client.ObjectKey{Namespace: etcd.Namespace, Name: etcd.Name}, existing)
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

// reconcileHeadlessService creates or updates the headless Service for Etcd.
func (r *EtcdReconciler) reconcileHeadlessService(ctx context.Context, etcd *multigresv1alpha1.Etcd) error {
	desired, err := BuildHeadlessService(etcd, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build headless Service: %w", err)
	}

	existing := &corev1.Service{}
	err = r.Get(ctx, client.ObjectKey{Namespace: etcd.Namespace, Name: etcd.Name + "-headless"}, existing)
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

// reconcileClientService creates or updates the client Service for Etcd.
func (r *EtcdReconciler) reconcileClientService(ctx context.Context, etcd *multigresv1alpha1.Etcd) error {
	desired, err := BuildClientService(etcd, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build client Service: %w", err)
	}

	existing := &corev1.Service{}
	err = r.Get(ctx, client.ObjectKey{Namespace: etcd.Namespace, Name: etcd.Name}, existing)
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
func (r *EtcdReconciler) updateStatus(ctx context.Context, etcd *multigresv1alpha1.Etcd) error {
	// Get the StatefulSet to check status
	sts := &appsv1.StatefulSet{}
	err := r.Get(ctx, client.ObjectKey{Namespace: etcd.Namespace, Name: etcd.Name}, sts)
	if err != nil {
		if errors.IsNotFound(err) {
			// StatefulSet not created yet
			return nil
		}
		return fmt.Errorf("failed to get StatefulSet for status: %w", err)
	}

	// Update status fields
	etcd.Status.Replicas = sts.Status.Replicas
	etcd.Status.ReadyReplicas = sts.Status.ReadyReplicas
	etcd.Status.Ready = sts.Status.ReadyReplicas == sts.Status.Replicas && sts.Status.Replicas > 0
	etcd.Status.ObservedGeneration = etcd.Generation

	// Update conditions
	etcd.Status.Conditions = r.buildConditions(etcd, sts)

	if err := r.Status().Update(ctx, etcd); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	return nil
}

// buildConditions creates status conditions based on observed state.
func (r *EtcdReconciler) buildConditions(etcd *multigresv1alpha1.Etcd, sts *appsv1.StatefulSet) []metav1.Condition {
	conditions := []metav1.Condition{}

	// Ready condition
	readyCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: etcd.Generation,
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
func (r *EtcdReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&multigresv1alpha1.Etcd{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Complete(r)
}

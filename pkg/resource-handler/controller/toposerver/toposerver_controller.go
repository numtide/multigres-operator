package toposerver

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/util/status"
)

// TopoServerReconciler reconciles a TopoServer object.
type TopoServerReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// Reconcile handles TopoServer resource reconciliation.
func (r *TopoServerReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the TopoServer instance
	toposerver := &multigresv1alpha1.TopoServer{}
	if err := r.Get(ctx, req.NamespacedName, toposerver); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("TopoServer resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get TopoServer")
		return ctrl.Result{}, err
	}

	// If being deleted, let Kubernetes GC handle cleanup
	if !toposerver.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// Reconcile StatefulSet
	if err := r.reconcileStatefulSet(ctx, toposerver); err != nil {
		logger.Error(err, "Failed to reconcile StatefulSet")
		r.Recorder.Eventf(
			toposerver,
			"Warning",
			"FailedApply",
			"Failed to reconcile StatefulSet: %v",
			err,
		)
		return ctrl.Result{}, err
	}

	// Reconcile headless Service
	if err := r.reconcileHeadlessService(ctx, toposerver); err != nil {
		logger.Error(err, "Failed to reconcile headless Service")
		r.Recorder.Eventf(
			toposerver,
			"Warning",
			"FailedApply",
			"Failed to reconcile headless Service: %v",
			err,
		)
		return ctrl.Result{}, err
	}

	// Reconcile client Service
	if err := r.reconcileClientService(ctx, toposerver); err != nil {
		logger.Error(err, "Failed to reconcile client Service")
		r.Recorder.Eventf(
			toposerver,
			"Warning",
			"FailedApply",
			"Failed to reconcile client Service: %v",
			err,
		)
		return ctrl.Result{}, err
	}

	// Update status
	if err := r.updateStatus(ctx, toposerver); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	r.Recorder.Event(toposerver, "Normal", "Synced", "Successfully reconciled TopoServer")
	return ctrl.Result{}, nil
}

// reconcileStatefulSet creates or updates the StatefulSet for TopoServer.
func (r *TopoServerReconciler) reconcileStatefulSet(
	ctx context.Context,
	toposerver *multigresv1alpha1.TopoServer,
) error {
	desired, err := BuildStatefulSet(toposerver, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build StatefulSet: %w", err)
	}

	// Server Side Apply
	desired.SetGroupVersionKind(appsv1.SchemeGroupVersion.WithKind("StatefulSet"))
	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf("failed to apply StatefulSet: %w", err)
	}

	r.Recorder.Eventf(
		toposerver,
		"Normal",
		"Applied",
		"Applied %s %s",
		desired.GroupVersionKind().Kind,
		desired.Name,
	)

	return nil
}

// reconcileHeadlessService creates or updates the headless Service for TopoServer.
func (r *TopoServerReconciler) reconcileHeadlessService(
	ctx context.Context,
	toposerver *multigresv1alpha1.TopoServer,
) error {
	desired, err := BuildHeadlessService(toposerver, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build headless Service: %w", err)
	}

	// Server Side Apply
	desired.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))
	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf("failed to apply headless Service: %w", err)
	}

	r.Recorder.Eventf(
		toposerver,
		"Normal",
		"Applied",
		"Applied %s %s",
		desired.GroupVersionKind().Kind,
		desired.Name,
	)

	return nil
}

// reconcileClientService creates or updates the client Service for TopoServer.
func (r *TopoServerReconciler) reconcileClientService(
	ctx context.Context,
	toposerver *multigresv1alpha1.TopoServer,
) error {
	desired, err := BuildClientService(toposerver, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build client Service: %w", err)
	}

	// Server Side Apply
	desired.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))
	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf("failed to apply client Service: %w", err)
	}

	r.Recorder.Eventf(
		toposerver,
		"Normal",
		"Applied",
		"Applied %s %s",
		desired.GroupVersionKind().Kind,
		desired.Name,
	)

	return nil
}

// updateStatus updates the TopoServer status based on observed state.
func (r *TopoServerReconciler) updateStatus(
	ctx context.Context,
	toposerver *multigresv1alpha1.TopoServer,
) error {
	oldPhase := toposerver.Status.Phase
	// Get the StatefulSet to check status
	sts := &appsv1.StatefulSet{}
	err := r.Get(ctx, client.ObjectKey{Namespace: toposerver.Namespace, Name: toposerver.Name}, sts)
	if err != nil {
		if errors.IsNotFound(err) {
			// StatefulSet not created yet
			return nil
		}
		return fmt.Errorf("failed to get StatefulSet for status: %w", err)
	}

	// Set service names
	toposerver.Status.ClientService = toposerver.Name
	toposerver.Status.PeerService = toposerver.Name + "-headless"

	// Update conditions
	toposerver.Status.Conditions = r.buildConditions(toposerver, sts)

	// Update Phase
	toposerver.Status.Phase = status.ComputePhase(sts.Status.ReadyReplicas, sts.Status.Replicas)
	if toposerver.Status.Phase != multigresv1alpha1.PhaseHealthy {
		toposerver.Status.Message = fmt.Sprintf(
			"%d/%d replicas ready",
			sts.Status.ReadyReplicas,
			sts.Status.Replicas,
		)
	} else {
		toposerver.Status.Message = "Ready"
	}

	// 1. Construct the Patch Object
	patchObj := &multigresv1alpha1.TopoServer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: multigresv1alpha1.GroupVersion.String(),
			Kind:       "TopoServer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      toposerver.Name,
			Namespace: toposerver.Namespace,
		},
		Status: toposerver.Status,
	}

	// 2. Apply the Patch
	if oldPhase != toposerver.Status.Phase {
		r.Recorder.Eventf(
			toposerver,
			"Normal",
			"PhaseChange",
			"Transitioned from '%s' to '%s'",
			oldPhase,
			toposerver.Status.Phase,
		)
	}

	if err := r.Status().Patch(
		ctx,
		patchObj,
		client.Apply,
		client.FieldOwner("multigres-operator"),
		client.ForceOwnership,
	); err != nil {
		return fmt.Errorf("failed to patch status: %w", err)
	}

	return nil
}

// buildConditions creates status conditions based on observed state.
func (r *TopoServerReconciler) buildConditions(
	toposerver *multigresv1alpha1.TopoServer,
	sts *appsv1.StatefulSet,
) []metav1.Condition {
	conditions := []metav1.Condition{}

	// Ready condition
	readyCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: toposerver.Generation,
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
func (r *TopoServerReconciler) SetupWithManager(
	mgr ctrl.Manager,
	opts ...controller.Options,
) error {
	controllerOpts := controller.Options{}
	if len(opts) > 0 {
		controllerOpts = opts[0]
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&multigresv1alpha1.TopoServer{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		WithOptions(controllerOpts).
		Complete(r)
}

package cell

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/monitoring"
	"github.com/numtide/multigres-operator/pkg/util/status"
)

// CellReconciler reconciles a Cell object.
type CellReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// Reconcile handles Cell resource reconciliation.
func (r *CellReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	start := time.Now()
	ctx, span := monitoring.StartReconcileSpan(ctx, "Cell.Reconcile", req.Name, req.Namespace, "Cell")
	defer span.End()
	ctx = monitoring.EnrichLoggerWithTrace(ctx)

	logger := log.FromContext(ctx)
	logger.V(1).Info("reconcile started")

	// Fetch the Cell instance
	cell := &multigresv1alpha1.Cell{}
	if err := r.Get(ctx, req.NamespacedName, cell); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Cell resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		monitoring.RecordSpanError(span, err)
		logger.Error(err, "Failed to get Cell")
		return ctrl.Result{}, err
	}

	// If being deleted, let Kubernetes GC handle cleanup
	if !cell.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// Reconcile MultiGateway Deployment
	{
		ctx, childSpan := monitoring.StartChildSpan(ctx, "Cell.ReconcileDeployment")
		if err := r.reconcileMultiGatewayDeployment(ctx, cell); err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			logger.Error(err, "Failed to reconcile MultiGateway Deployment")
			r.Recorder.Eventf(
				cell,
				"Warning",
				"FailedApply",
				"Failed to sync Gateway Deployment: %v",
				err,
			)
			return ctrl.Result{}, err
		}
		childSpan.End()
	}

	// Reconcile MultiGateway Service
	{
		ctx, childSpan := monitoring.StartChildSpan(ctx, "Cell.ReconcileService")
		if err := r.reconcileMultiGatewayService(ctx, cell); err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			logger.Error(err, "Failed to reconcile MultiGateway Service")
			r.Recorder.Eventf(
				cell,
				"Warning",
				"FailedApply",
				"Failed to reconcile MultiGateway Service: %v",
				err,
			)
			return ctrl.Result{}, err
		}
		childSpan.End()
	}

	// Update status
	{
		_, childSpan := monitoring.StartChildSpan(ctx, "Cell.UpdateStatus")
		if err := r.updateStatus(ctx, cell); err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			logger.Error(err, "Failed to update status")
			return ctrl.Result{}, err
		}
		childSpan.End()
	}

	logger.V(1).Info("reconcile complete", "duration", time.Since(start).String())
	r.Recorder.Event(cell, "Normal", "Synced", "Successfully reconciled Cell")
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

	// Server Side Apply
	desired.SetGroupVersionKind(appsv1.SchemeGroupVersion.WithKind("Deployment"))
	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf("failed to apply MultiGateway Deployment: %w", err)
	}

	r.Recorder.Eventf(
		cell,
		"Normal",
		"Applied",
		"Applied %s %s",
		desired.GroupVersionKind().Kind,
		desired.Name,
	)

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

	// Server Side Apply
	desired.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))
	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf("failed to apply MultiGateway Service: %w", err)
	}

	r.Recorder.Eventf(
		cell,
		"Normal",
		"Applied",
		"Applied %s %s",
		desired.GroupVersionKind().Kind,
		desired.Name,
	)

	return nil
}

// updateStatus updates the Cell status based on observed state.
func (r *CellReconciler) updateStatus(ctx context.Context, cell *multigresv1alpha1.Cell) error {
	oldPhase := cell.Status.Phase
	// Get the MultiGateway Deployment to check status
	mgDeploy := &appsv1.Deployment{}
	err := r.Get(
		ctx,
		client.ObjectKey{Namespace: cell.Namespace, Name: BuildMultiGatewayDeploymentName(cell)},
		mgDeploy,
	)
	if err != nil {
		if errors.IsNotFound(err) {
			// Deployment not created yet
			return nil
		}
		return fmt.Errorf("failed to get MultiGateway Deployment for status: %w", err)
	}

	// Update conditions
	r.setConditions(cell, mgDeploy)
	cell.Status.GatewayReplicas = mgDeploy.Status.Replicas
	cell.Status.GatewayReadyReplicas = mgDeploy.Status.ReadyReplicas
	monitoring.SetCellGatewayReplicas(cell.Name, cell.Namespace, mgDeploy.Status.Replicas, mgDeploy.Status.ReadyReplicas)

	// Update Phase
	cell.Status.Phase = status.ComputePhase(mgDeploy.Status.ReadyReplicas, mgDeploy.Status.Replicas)
	if mgDeploy.Status.ObservedGeneration != mgDeploy.Generation {
		cell.Status.Phase = multigresv1alpha1.PhaseProgressing
		cell.Status.Message = "Gateway Deployment is progressing"
	} else if cell.Status.Phase != multigresv1alpha1.PhaseHealthy {
		cell.Status.Message = fmt.Sprintf(
			"Gateway: %d/%d replicas ready",
			mgDeploy.Status.ReadyReplicas,
			mgDeploy.Status.Replicas,
		)
	} else {
		cell.Status.Message = "Ready"
	}

	cell.Status.ObservedGeneration = cell.Generation

	// 1. Construct the Patch Object
	patchObj := &multigresv1alpha1.Cell{
		TypeMeta: metav1.TypeMeta{
			APIVersion: multigresv1alpha1.GroupVersion.String(),
			Kind:       "Cell",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cell.Name,
			Namespace: cell.Namespace,
		},
		Status: cell.Status,
	}

	// 2. Apply the Patch
	if oldPhase != cell.Status.Phase {
		r.Recorder.Eventf(
			cell,
			"Normal",
			"PhaseChange",
			"Transitioned from '%s' to '%s'",
			oldPhase,
			cell.Status.Phase,
		)
	}

	// Note: We rely on Server-Side Apply (SSA) to handle idempotency.
	// If the status hasn't changed, the API server will treat this Patch as a no-op,
	// so we don't need a manual DeepEqual check here.
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

// setConditions creates status conditions based on observed state using meta.SetStatusCondition.
func (r *CellReconciler) setConditions(
	cell *multigresv1alpha1.Cell,
	mgDeploy *appsv1.Deployment,
) {
	// Available condition - True if at least one replica is ready (serviceable)
	availCond := metav1.Condition{
		Type:               "Available",
		ObservedGeneration: cell.Generation,
		Status:             metav1.ConditionFalse,
		Reason:             "MultiGatewayUnavailable",
		Message:            "No ready replicas available",
	}

	if mgDeploy.Status.ReadyReplicas > 0 {
		availCond.Status = metav1.ConditionTrue
		availCond.Reason = "MultiGatewayAvailable"
		availCond.Message = fmt.Sprintf(
			"MultiGateway %d/%d replicas ready",
			mgDeploy.Status.ReadyReplicas,
			mgDeploy.Status.Replicas,
		)
	}
	meta.SetStatusCondition(&cell.Status.Conditions, availCond)

	// Ready condition - True if all replicas are ready (desired state reached)
	readyCond := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: cell.Generation,
		Status:             metav1.ConditionFalse,
		Reason:             "MultiGatewayNotReady",
		Message: fmt.Sprintf(
			"MultiGateway %d/%d ready, waiting for full convergence",
			mgDeploy.Status.ReadyReplicas,
			mgDeploy.Status.Replicas,
		),
	}

	allReady := mgDeploy.Status.ObservedGeneration == mgDeploy.Generation &&
		mgDeploy.Status.ReadyReplicas == mgDeploy.Status.Replicas &&
		mgDeploy.Status.Replicas > 0

	if allReady {
		readyCond.Status = metav1.ConditionTrue
		readyCond.Reason = "MultiGatewayReady"
		readyCond.Message = "All replicas match desired state"
	}
	meta.SetStatusCondition(&cell.Status.Conditions, readyCond)
}

// SetupWithManager sets up the controller with the Manager.
func (r *CellReconciler) SetupWithManager(mgr ctrl.Manager, opts ...controller.Options) error {
	controllerOpts := controller.Options{
		MaxConcurrentReconciles: 20,
	}
	if len(opts) > 0 {
		controllerOpts = opts[0]
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&multigresv1alpha1.Cell{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		WithOptions(controllerOpts).
		Complete(r)
}

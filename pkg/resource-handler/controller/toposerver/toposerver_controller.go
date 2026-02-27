package toposerver

import (
	"context"
	"fmt"
	"slices"
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
	"github.com/numtide/multigres-operator/pkg/util/metadata"
	"github.com/numtide/multigres-operator/pkg/util/status"
)

const topoFinalizerName = "multigres.com/topo-deletion-ordering"

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
	start := time.Now()
	ctx, span := monitoring.StartReconcileSpan(
		ctx,
		"TopoServer.Reconcile",
		req.Name,
		req.Namespace,
		"TopoServer",
	)
	defer span.End()
	ctx = monitoring.EnrichLoggerWithTrace(ctx)

	logger := log.FromContext(ctx)
	logger.V(1).Info("reconcile started")

	// Fetch the TopoServer instance
	toposerver := &multigresv1alpha1.TopoServer{}
	if err := r.Get(ctx, req.NamespacedName, toposerver); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("TopoServer resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		monitoring.RecordSpanError(span, err)
		logger.Error(err, "Failed to get TopoServer")
		return ctrl.Result{}, err
	}

	// Add finalizer to prevent premature GC during cluster deletion.
	// Without this, Kubernetes GC can delete the TopoServer (via ownerReference)
	// before shard drains complete, leaving the drain state machine without a topo.
	if toposerver.DeletionTimestamp.IsZero() {
		if !slices.Contains(toposerver.Finalizers, topoFinalizerName) {
			toposerver.Finalizers = append(toposerver.Finalizers, topoFinalizerName)
			if err := r.Update(ctx, toposerver); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
			}
		}
	}

	if !toposerver.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, toposerver)
	}

	// Reconcile StatefulSet
	{
		ctx, childSpan := monitoring.StartChildSpan(ctx, "TopoServer.ReconcileStatefulSet")
		if err := r.reconcileStatefulSet(ctx, toposerver); err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
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
		childSpan.End()
	}

	// Reconcile headless Service
	{
		ctx, childSpan := monitoring.StartChildSpan(ctx, "TopoServer.ReconcileHeadlessService")
		if err := r.reconcileHeadlessService(ctx, toposerver); err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
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
		childSpan.End()
	}

	// Reconcile client Service
	{
		ctx, childSpan := monitoring.StartChildSpan(ctx, "TopoServer.ReconcileClientService")
		if err := r.reconcileClientService(ctx, toposerver); err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
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
		childSpan.End()
	}

	// Update status
	{
		_, childSpan := monitoring.StartChildSpan(ctx, "TopoServer.UpdateStatus")
		if err := r.updateStatus(ctx, toposerver); err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			logger.Error(err, "Failed to update status")
			r.Recorder.Eventf(
				toposerver,
				"Warning",
				"StatusError",
				"Failed to update status: %v",
				err,
			)
			return ctrl.Result{}, err
		}
		childSpan.End()
	}

	logger.V(1).Info("reconcile complete", "duration", time.Since(start).String())
	r.Recorder.Event(toposerver, "Normal", "Synced", "Successfully reconciled TopoServer")
	return ctrl.Result{}, nil
}

// handleDeletion blocks TopoServer deletion until all Shards and Cells
// belonging to the same cluster have been fully removed. This guarantees
// the topo (etcd) stays alive for the drain state machine to complete.
//
// If the topo was never ready (0 ready replicas), there is no etcd data
// to protect, so we skip the wait to avoid deadlocking with shards that
// are themselves waiting for the topo to come up.
func (r *TopoServerReconciler) handleDeletion(
	ctx context.Context,
	toposerver *multigresv1alpha1.TopoServer,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	clusterName := toposerver.Labels[metadata.LabelMultigresCluster]
	if clusterName == "" {
		logger.Info("TopoServer has no cluster label, removing finalizer")
		return r.removeFinalizer(ctx, toposerver)
	}

	// If the topo StatefulSet never had ready replicas, there is no etcd
	// data to drain from. Release the finalizer immediately to prevent a
	// deadlock where shards wait for topo and topo waits for shards.
	if !r.topoWasEverReady(ctx, toposerver) {
		logger.Info("TopoServer was never ready, skipping drain wait")
		return r.removeFinalizer(ctx, toposerver)
	}

	clusterLabels := client.MatchingLabels{metadata.LabelMultigresCluster: clusterName}
	ns := client.InNamespace(toposerver.Namespace)

	shards := &multigresv1alpha1.ShardList{}
	if err := r.List(ctx, shards, ns, clusterLabels); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list shards: %w", err)
	}

	cells := &multigresv1alpha1.CellList{}
	if err := r.List(ctx, cells, ns, clusterLabels); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list cells: %w", err)
	}

	remaining := len(shards.Items) + len(cells.Items)
	if remaining > 0 {
		logger.Info("Waiting for shards and cells to be deleted before allowing topo cleanup",
			"remainingShards", len(shards.Items),
			"remainingCells", len(cells.Items),
		)
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	logger.Info("All shards and cells deleted, removing topo finalizer")
	return r.removeFinalizer(ctx, toposerver)
}

// topoWasEverReady returns true if the TopoServer's StatefulSet has (or
// had) at least one ready replica. When the topo was never initialised,
// there is nothing in etcd to protect, so callers can skip the drain wait.
func (r *TopoServerReconciler) topoWasEverReady(
	ctx context.Context,
	toposerver *multigresv1alpha1.TopoServer,
) bool {
	// Status phase is set to Healthy once all replicas are ready.
	// If it was ever healthy, the status condition will reflect it.
	for _, cond := range toposerver.Status.Conditions {
		if cond.Type == "Ready" && cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	// Fall back to checking the live StatefulSet.
	sts := &appsv1.StatefulSet{}
	key := client.ObjectKey{Namespace: toposerver.Namespace, Name: toposerver.Name}
	if err := r.Get(ctx, key, sts); err != nil {
		return false
	}
	return sts.Status.ReadyReplicas > 0
}

func (r *TopoServerReconciler) removeFinalizer(
	ctx context.Context,
	toposerver *multigresv1alpha1.TopoServer,
) (ctrl.Result, error) {
	toposerver.Finalizers = slices.DeleteFunc(toposerver.Finalizers, func(s string) bool {
		return s == topoFinalizerName
	})
	if err := r.Update(ctx, toposerver); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
	}
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
	r.setConditions(toposerver, sts)

	// Update Phase
	toposerver.Status.Phase = status.ComputePhase(sts.Status.ReadyReplicas, sts.Status.Replicas)
	monitoring.SetTopoServerReplicas(
		toposerver.Name,
		toposerver.Namespace,
		sts.Status.Replicas,
		sts.Status.ReadyReplicas,
	)
	if sts.Status.ObservedGeneration != sts.Generation {
		toposerver.Status.Phase = multigresv1alpha1.PhaseProgressing
		toposerver.Status.Message = "StatefulSet is progressing"
	} else if toposerver.Status.Phase != multigresv1alpha1.PhaseHealthy {
		toposerver.Status.Message = fmt.Sprintf(
			"%d/%d replicas ready",
			sts.Status.ReadyReplicas,
			sts.Status.Replicas,
		)
	} else {
		toposerver.Status.Message = "Ready"
	}

	toposerver.Status.ObservedGeneration = toposerver.Generation

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

// setConditions creates status conditions based on observed state.
func (r *TopoServerReconciler) setConditions(
	toposerver *multigresv1alpha1.TopoServer,
	sts *appsv1.StatefulSet,
) {
	// Ready condition
	readyCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: toposerver.Generation,
		Status:             metav1.ConditionFalse,
		Reason:             "NotAllReplicasReady",
		Message: fmt.Sprintf(
			"%d/%d replicas ready",
			sts.Status.ReadyReplicas,
			sts.Status.Replicas,
		),
	}

	if sts.Status.ObservedGeneration == sts.Generation &&
		sts.Status.ReadyReplicas == sts.Status.Replicas &&
		sts.Status.Replicas > 0 {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "AllReplicasReady"
		readyCondition.Message = fmt.Sprintf("All %d replicas are ready", sts.Status.ReadyReplicas)
	} else if sts.Status.ObservedGeneration != sts.Generation {
		readyCondition.Message = "StatefulSet update in progress"
	}

	meta.SetStatusCondition(&toposerver.Status.Conditions, readyCondition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *TopoServerReconciler) SetupWithManager(
	mgr ctrl.Manager,
	opts ...controller.Options,
) error {
	controllerOpts := controller.Options{
		MaxConcurrentReconciles: 20,
	}
	if len(opts) > 0 {
		controllerOpts = opts[0]
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&multigresv1alpha1.TopoServer{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		WithOptions(controllerOpts).
		Complete(r)
}

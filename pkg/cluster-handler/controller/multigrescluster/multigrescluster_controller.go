package multigrescluster

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"go.opentelemetry.io/otel/trace"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/monitoring"
	"github.com/numtide/multigres-operator/pkg/resolver"
)

// MultigresClusterReconciler reconciles a MultigresCluster object.
type MultigresClusterReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// Reconcile reads that state of the cluster for a MultigresCluster object and makes changes based on the state read
// and what is in the MultigresCluster.Spec.
//
// +kubebuilder:rbac:groups=multigres.com,resources=multigresclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multigres.com,resources=multigresclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=multigres.com,resources=multigresclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=multigres.com,resources=coretemplates;celltemplates;shardtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=multigres.com,resources=cells;tablegroups;toposervers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
func (r *MultigresClusterReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	start := time.Now()
	ctx, span := monitoring.StartReconcileSpan(ctx, "MultigresCluster.Reconcile", req.Name, req.Namespace, "MultigresCluster")
	defer span.End()
	ctx = monitoring.EnrichLoggerWithTrace(ctx)

	l := log.FromContext(ctx)
	l.V(1).Info("reconcile started")

	cluster := &multigresv1alpha1.MultigresCluster{}
	err := r.Get(ctx, req.NamespacedName, cluster)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		monitoring.RecordSpanError(span, err)
		return ctrl.Result{}, fmt.Errorf("failed to get MultigresCluster: %w", err)
	}

	// Bridge the async webhook → reconcile trace gap.
	// If the webhook injected a traceparent into the cluster's annotations,
	// restart the span under that parent context (or link if stale).
	if parentCtx, isStale := monitoring.ExtractTraceContext(cluster.GetAnnotations()); trace.SpanFromContext(parentCtx).SpanContext().IsValid() {
		span.End() // End the initial orphan span.
		if isStale {
			ctx, span = monitoring.Tracer.Start(ctx, "MultigresCluster.Reconcile",
				trace.WithLinks(trace.LinkFromContext(parentCtx)),
			)
		} else {
			ctx, span = monitoring.StartReconcileSpan(parentCtx, "MultigresCluster.Reconcile", req.Name, req.Namespace, "MultigresCluster")
		}
		ctx = monitoring.EnrichLoggerWithTrace(ctx)
		l = log.FromContext(ctx)
	}

	res := resolver.NewResolver(r.Client, cluster.Namespace)

	// Apply defaults (in-memory) to ensure we have images/configs/system-catalog even if webhook didn't run.
	{
		ctx, childSpan := monitoring.StartChildSpan(ctx, "MultigresCluster.PopulateDefaults")
		decisions, err := res.PopulateClusterDefaults(ctx, cluster)
		if err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			l.Error(err, "Failed to populate cluster defaults")
			return ctrl.Result{}, err
		}
		childSpan.End()

		for _, decision := range decisions {
			r.Recorder.Event(cluster, "Normal", "ImplicitDefault", decision)
		}
	}

	// If being deleted, let Kubernetes GC handle cleanup
	if !cluster.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	{
		ctx, childSpan := monitoring.StartChildSpan(ctx, "MultigresCluster.ReconcileGlobalComponents")
		if err := r.reconcileGlobalComponents(ctx, cluster, res); err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			l.Error(err, "Failed to reconcile global components")
			r.Recorder.Eventf(
				cluster,
				"Warning",
				"FailedApply",
				"Failed to reconcile global components: %v",
				err,
			)
			return ctrl.Result{}, err
		}
		childSpan.End()
	}

	{
		ctx, childSpan := monitoring.StartChildSpan(ctx, "MultigresCluster.ReconcileCells")
		if err := r.reconcileCells(ctx, cluster, res); err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			l.Error(err, "Failed to reconcile cells")
			r.Recorder.Eventf(cluster, "Warning", "FailedApply", "Failed to reconcile cells: %v", err)
			return ctrl.Result{}, err
		}
		childSpan.End()
	}

	{
		ctx, childSpan := monitoring.StartChildSpan(ctx, "MultigresCluster.ReconcileDatabases")
		if err := r.reconcileDatabases(ctx, cluster, res); err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			l.Error(err, "Failed to reconcile databases")
			r.Recorder.Eventf(
				cluster,
				"Warning",
				"FailedApply",
				"Failed to reconcile databases: %v",
				err,
			)
			return ctrl.Result{}, err
		}
		childSpan.End()
	}

	{
		_, childSpan := monitoring.StartChildSpan(ctx, "MultigresCluster.UpdateStatus")
		if err := r.updateStatus(ctx, cluster); err != nil {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			l.Error(err, "Failed to update status")
			r.Recorder.Eventf(cluster, "Warning", "FailedApply", "Failed to update status: %v", err)
			return ctrl.Result{}, err
		}
		childSpan.End()
	}

	// Emit cluster-level metrics
	monitoring.SetClusterInfo(cluster.Name, cluster.Namespace, string(cluster.Status.Phase))
	totalShards := int(0)
	for _, db := range cluster.Status.Databases {
		totalShards += int(db.TotalShards)
	}
	monitoring.SetClusterTopology(cluster.Name, cluster.Namespace, len(cluster.Status.Cells), totalShards)

	l.V(1).Info("reconcile complete", "duration", time.Since(start).String())
	r.Recorder.Event(cluster, "Normal", "Synced", "Successfully reconciled MultigresCluster")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MultigresClusterReconciler) SetupWithManager(
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
		For(&multigresv1alpha1.MultigresCluster{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&multigresv1alpha1.Cell{}).
		Owns(&multigresv1alpha1.TableGroup{}).
		Owns(&multigresv1alpha1.TopoServer{}).
		Owns(&appsv1.Deployment{}).
		Watches(
			&multigresv1alpha1.CoreTemplate{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueRequestsFromTemplate),
		).
		Watches(
			&multigresv1alpha1.CellTemplate{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueRequestsFromTemplate),
		).
		Watches(
			&multigresv1alpha1.ShardTemplate{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueRequestsFromTemplate),
		).
		WithOptions(controllerOpts).
		Complete(r)
}

// enqueueRequestsFromTemplate returns a list of requests for all MultigresClusters in the same namespace
// as the triggered Template. This ensures that if a default template changes, all clusters using it (potentially) are updated.
func (r *MultigresClusterReconciler) enqueueRequestsFromTemplate(
	ctx context.Context,
	o client.Object,
) []reconcile.Request {
	clusters := &multigresv1alpha1.MultigresClusterList{}
	if err := r.List(ctx, clusters, client.InNamespace(o.GetNamespace())); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, c := range clusters.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&c),
		})
	}
	return requests
}

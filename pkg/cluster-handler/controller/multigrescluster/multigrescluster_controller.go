package multigrescluster

import (
	"context"
	"fmt"
	"slices"
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
	"github.com/numtide/multigres-operator/pkg/util/metadata"
)

const finalizerName = "multigres.com/cluster-cleanup"

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
// +kubebuilder:rbac:groups=multigres.com,resources=shards,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
func (r *MultigresClusterReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	start := time.Now()
	ctx, span := monitoring.StartReconcileSpan(
		ctx,
		"MultigresCluster.Reconcile",
		req.Name,
		req.Namespace,
		"MultigresCluster",
	)
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
	if parentCtx, isStale := monitoring.ExtractTraceContext(cluster.GetAnnotations()); trace.SpanFromContext(parentCtx).
		SpanContext().
		IsValid() {
		span.End() // End the initial orphan span.
		if isStale {
			ctx, span = monitoring.Tracer.Start(ctx, "MultigresCluster.Reconcile",
				trace.WithLinks(trace.LinkFromContext(parentCtx)),
			)
		} else {
			ctx, span = monitoring.StartReconcileSpan(parentCtx, "MultigresCluster.Reconcile", req.Name, req.Namespace, "MultigresCluster")
		}
		defer span.End()
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

	// Add finalizer on first reconcile to guarantee ordered deletion.
	// We do not return early here because GenerationChangedPredicate would
	// filter out the metadata-only update, preventing child resource creation.
	if !slices.Contains(cluster.Finalizers, finalizerName) {
		cluster.Finalizers = append(cluster.Finalizers, finalizerName)
		if err := r.Update(ctx, cluster); err != nil {
			l.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
	}

	if !cluster.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, cluster)
	}

	{
		ctx, childSpan := monitoring.StartChildSpan(
			ctx,
			"MultigresCluster.ReconcileGlobalComponents",
		)
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
			r.Recorder.Eventf(
				cluster,
				"Warning",
				"FailedApply",
				"Failed to reconcile cells: %v",
				err,
			)
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
		ctx, childSpan := monitoring.StartChildSpan(ctx, "MultigresCluster.UpdateStatus")
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
	monitoring.SetClusterTopology(
		cluster.Name,
		cluster.Namespace,
		len(cluster.Status.Cells),
		totalShards,
	)

	l.V(1).Info("reconcile complete", "duration", time.Since(start).String())
	r.Recorder.Event(cluster, "Normal", "Synced", "Successfully reconciled MultigresCluster")
	return ctrl.Result{}, nil
}

// handleDeletion orchestrates phased deletion of the MultigresCluster.
// It deletes Cells and TableGroups first so their data-handler finalizers
// can run against the still-live topo servers. Once all Cells and Shards
// are fully removed, it removes our finalizer, allowing Kubernetes GC
// to clean up the remaining resources (topo servers, deployments).
func (r *MultigresClusterReconciler) handleDeletion(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	clusterLabels := client.MatchingLabels{metadata.LabelMultigresCluster: cluster.Name}
	ns := client.InNamespace(cluster.Namespace)

	// Delete all Cells owned by this cluster.
	cells := &multigresv1alpha1.CellList{}
	if err := r.List(ctx, cells, ns, clusterLabels); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list cells: %w", err)
	}
	for i := range cells.Items {
		if cells.Items[i].DeletionTimestamp.IsZero() {
			if err := r.Delete(ctx, &cells.Items[i]); err != nil && !errors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf(
					"failed to delete cell %q: %w",
					cells.Items[i].Name,
					err,
				)
			}
			l.Info("Initiated cell deletion", "cell", cells.Items[i].Name)
		}
	}

	// Delete all TableGroups owned by this cluster (cascades to Shards).
	tableGroups := &multigresv1alpha1.TableGroupList{}
	if err := r.List(ctx, tableGroups, ns, clusterLabels); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list tablegroups: %w", err)
	}
	for i := range tableGroups.Items {
		if tableGroups.Items[i].DeletionTimestamp.IsZero() {
			if err := r.Delete(ctx, &tableGroups.Items[i]); err != nil && !errors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf(
					"failed to delete tablegroup %q: %w",
					tableGroups.Items[i].Name,
					err,
				)
			}
			l.Info("Initiated tablegroup deletion", "tablegroup", tableGroups.Items[i].Name)
		}
	}

	// Check if any Cells or Shards still exist (waiting for data-handler finalizer processing).
	shards := &multigresv1alpha1.ShardList{}
	if err := r.List(ctx, shards, ns, clusterLabels); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list shards: %w", err)
	}

	remaining := len(cells.Items) + len(shards.Items)
	if remaining > 0 {
		l.Info("Waiting for data-handler finalizers",
			"remainingCells", len(cells.Items),
			"remainingShards", len(shards.Items),
		)
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	// All Cells and Shards are gone — safe to remove our finalizer.
	cluster.Finalizers = slices.DeleteFunc(cluster.Finalizers, func(s string) bool {
		return s == finalizerName
	})
	if err := r.Update(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
	}

	l.Info("Cluster cleanup complete, finalizer removed")
	r.Recorder.Event(cluster, "Normal", "CleanupComplete", "All data-handler resources cleaned up")
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

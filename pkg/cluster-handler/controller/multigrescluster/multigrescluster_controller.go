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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resolver"
)

const (
	finalizerName = "multigres.com/finalizer"
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
	l := log.FromContext(ctx)

	cluster := &multigresv1alpha1.MultigresCluster{}
	err := r.Get(ctx, req.NamespacedName, cluster)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get MultigresCluster: %w", err)
	}

	if !cluster.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, cluster)
	}

	if !controllerutil.ContainsFinalizer(cluster, finalizerName) {
		controllerutil.AddFinalizer(cluster, finalizerName)
		if err := r.Update(ctx, cluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
		return ctrl.Result{}, nil
	}

	res := resolver.NewResolver(r.Client, cluster.Namespace, cluster.Spec.TemplateDefaults)

	// Apply defaults (in-memory) to ensure we have images/configs/system-catalog even if webhook didn't run.
	res.PopulateClusterDefaults(cluster)

	if err := r.reconcileGlobalComponents(ctx, cluster, res); err != nil {
		l.Error(err, "Failed to reconcile global components")
		return ctrl.Result{}, err
	}

	if err := r.reconcileCells(ctx, cluster, res); err != nil {
		l.Error(err, "Failed to reconcile cells")
		return ctrl.Result{}, err
	}

	if err := r.reconcileDatabases(ctx, cluster, res); err != nil {
		l.Error(err, "Failed to reconcile databases")
		return ctrl.Result{}, err
	}

	if err := r.updateStatus(ctx, cluster); err != nil {
		l.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
}

func (r *MultigresClusterReconciler) handleDelete(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
) (ctrl.Result, error) {
	if controllerutil.ContainsFinalizer(cluster, finalizerName) {
		if err := r.checkChildrenDeleted(ctx, cluster); err != nil {
			// If we are waiting for children, emit an event so the user knows why it's stuck in Terminating
			r.Recorder.Event(
				cluster,
				"Normal",
				"Cleanup",
				"Waiting for child resources (Cells/TableGroups) to be deleted",
			)
			return ctrl.Result{}, err
		}
		controllerutil.RemoveFinalizer(cluster, finalizerName)
		if err := r.Update(ctx, cluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
		}
	}
	return ctrl.Result{}, nil
}

func (r *MultigresClusterReconciler) checkChildrenDeleted(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
) error {
	childrenStillExist := false

	// Helper to delete a list of objects
	deleteObjects := func(objects []client.Object) error {
		for _, obj := range objects {
			if obj.GetDeletionTimestamp().IsZero() {
				if err := r.Delete(ctx, obj); err != nil {
					if !errors.IsNotFound(err) {
						return fmt.Errorf("failed to delete child %s: %w", obj.GetName(), err)
					}
				}
			}
		}
		if len(objects) > 0 {
			childrenStillExist = true
		}
		return nil
	}

	// 1. Check Cells
	cells := &multigresv1alpha1.CellList{}
	if err := r.List(ctx, cells, client.InNamespace(cluster.Namespace), client.MatchingLabels{"multigres.com/cluster": cluster.Name}); err != nil {
		return fmt.Errorf("failed to list cells: %w", err)
	}
	cellObjs := make([]client.Object, len(cells.Items))
	for i := range cells.Items {
		cellObjs[i] = &cells.Items[i]
	}
	if err := deleteObjects(cellObjs); err != nil {
		return err
	}

	// 2. Check TableGroups
	tgs := &multigresv1alpha1.TableGroupList{}
	if err := r.List(ctx, tgs, client.InNamespace(cluster.Namespace), client.MatchingLabels{"multigres.com/cluster": cluster.Name}); err != nil {
		return fmt.Errorf("failed to list tablegroups: %w", err)
	}
	tgObjs := make([]client.Object, len(tgs.Items))
	for i := range tgs.Items {
		tgObjs[i] = &tgs.Items[i]
	}
	if err := deleteObjects(tgObjs); err != nil {
		return err
	}

	// 3. Check TopoServers
	ts := &multigresv1alpha1.TopoServerList{}
	if err := r.List(ctx, ts, client.InNamespace(cluster.Namespace), client.MatchingLabels{"multigres.com/cluster": cluster.Name}); err != nil {
		return fmt.Errorf("failed to list toposervers: %w", err)
	}
	tsObjs := make([]client.Object, len(ts.Items))
	for i := range ts.Items {
		tsObjs[i] = &ts.Items[i]
	}
	if err := deleteObjects(tsObjs); err != nil {
		return err
	}

	if childrenStillExist {
		return fmt.Errorf(
			"waiting for children to be deleted (cells, tablegroups, or toposervers still exist)",
		)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MultigresClusterReconciler) SetupWithManager(
	mgr ctrl.Manager,
	opts ...controller.Options,
) error {
	controllerOpts := controller.Options{}
	if len(opts) > 0 {
		controllerOpts = opts[0]
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&multigresv1alpha1.MultigresCluster{}).
		Owns(&multigresv1alpha1.Cell{}).
		Owns(&multigresv1alpha1.TableGroup{}).
		Owns(&multigresv1alpha1.TopoServer{}).
		Owns(&appsv1.Deployment{}).
		WithOptions(controllerOpts).
		Complete(r)
}

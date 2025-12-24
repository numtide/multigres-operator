package multigrescluster

import (
	"context"
	"fmt"
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/data-handler/defaults"
)

const (
	finalizerName = "multigres.com/finalizer"
)

// MultigresClusterReconciler reconciles a MultigresCluster object.
type MultigresClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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

	// This now returns *defaults.Resolver
	resolver := defaults.NewResolver(r.Client, cluster.Namespace, cluster.Spec.TemplateDefaults)

	if err := r.reconcileGlobalComponents(ctx, cluster, resolver); err != nil {
		l.Error(err, "Failed to reconcile global components")
		return ctrl.Result{}, err
	}

	if err := r.reconcileCells(ctx, cluster, resolver); err != nil {
		l.Error(err, "Failed to reconcile cells")
		return ctrl.Result{}, err
	}

	if err := r.reconcileDatabases(ctx, cluster, resolver); err != nil {
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
	cells := &multigresv1alpha1.CellList{}
	if err := r.List(ctx, cells, client.InNamespace(cluster.Namespace), client.MatchingLabels{"multigres.com/cluster": cluster.Name}); err != nil {
		return fmt.Errorf("failed to list cells: %w", err)
	}
	if len(cells.Items) > 0 {
		return fmt.Errorf("cells still exist")
	}

	tgs := &multigresv1alpha1.TableGroupList{}
	if err := r.List(ctx, tgs, client.InNamespace(cluster.Namespace), client.MatchingLabels{"multigres.com/cluster": cluster.Name}); err != nil {
		return fmt.Errorf("failed to list tablegroups: %w", err)
	}
	if len(tgs.Items) > 0 {
		return fmt.Errorf("tablegroups still exist")
	}

	ts := &multigresv1alpha1.TopoServerList{}
	if err := r.List(ctx, ts, client.InNamespace(cluster.Namespace), client.MatchingLabels{"multigres.com/cluster": cluster.Name}); err != nil {
		return fmt.Errorf("failed to list toposervers: %w", err)
	}
	if len(ts.Items) > 0 {
		return fmt.Errorf("toposervers still exist")
	}

	return nil
}

func (r *MultigresClusterReconciler) reconcileGlobalComponents(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
	resolver *defaults.Resolver,
) error {
	if err := r.reconcileGlobalTopoServer(ctx, cluster, resolver); err != nil {
		return err
	}
	if err := r.reconcileMultiAdmin(ctx, cluster, resolver); err != nil {
		return err
	}
	return nil
}

func (r *MultigresClusterReconciler) reconcileGlobalTopoServer(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
	resolver *defaults.Resolver,
) error {
	tplName := cluster.Spec.TemplateDefaults.CoreTemplate
	if cluster.Spec.GlobalTopoServer.TemplateRef != "" {
		tplName = cluster.Spec.GlobalTopoServer.TemplateRef
	}

	tpl, err := resolver.ResolveCoreTemplate(ctx, tplName)
	if err != nil {
		return fmt.Errorf("failed to resolve topo template: %w", err)
	}

	spec := defaults.ResolveGlobalTopo(&cluster.Spec.GlobalTopoServer, tpl)
	if spec.Etcd != nil {
		ts := &multigresv1alpha1.TopoServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cluster.Name + "-global-topo",
				Namespace: cluster.Namespace,
				Labels:    map[string]string{"multigres.com/cluster": cluster.Name},
			},
		}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, ts, func() error {
			replicas := defaults.DefaultEtcdReplicas
			if spec.Etcd.Replicas != nil {
				replicas = *spec.Etcd.Replicas
			}

			ts.Spec.Etcd = &multigresv1alpha1.EtcdSpec{
				Image:     spec.Etcd.Image,
				Replicas:  &replicas,
				Storage:   spec.Etcd.Storage,
				Resources: spec.Etcd.Resources,
			}
			return controllerutil.SetControllerReference(cluster, ts, r.Scheme)
		}); err != nil {
			return fmt.Errorf("failed to create/update global topo: %w", err)
		}
	}
	return nil
}

func (r *MultigresClusterReconciler) reconcileMultiAdmin(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
	resolver *defaults.Resolver,
) error {
	tplName := cluster.Spec.TemplateDefaults.CoreTemplate
	if cluster.Spec.MultiAdmin.TemplateRef != "" {
		tplName = cluster.Spec.MultiAdmin.TemplateRef
	}

	tpl, err := resolver.ResolveCoreTemplate(ctx, tplName)
	if err != nil {
		return fmt.Errorf("failed to resolve admin template: %w", err)
	}

	spec := defaults.ResolveMultiAdmin(&cluster.Spec.MultiAdmin, tpl)
	if spec != nil {
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cluster.Name + "-multiadmin",
				Namespace: cluster.Namespace,
				Labels: map[string]string{
					"multigres.com/cluster": cluster.Name,
					"app":                   "multiadmin",
				},
			},
		}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
			replicas := defaults.DefaultAdminReplicas
			if spec.Replicas != nil {
				replicas = *spec.Replicas
			}
			deploy.Spec.Replicas = &replicas
			deploy.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "multiadmin", "multigres.com/cluster": cluster.Name},
			}
			deploy.Spec.Template = corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "multiadmin", "multigres.com/cluster": cluster.Name},
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: cluster.Spec.Images.ImagePullSecrets,
					Containers: []corev1.Container{
						{
							Name:      "multiadmin",
							Image:     cluster.Spec.Images.MultiAdmin,
							Resources: spec.Resources,
						},
					},
					Affinity: spec.Affinity,
				},
			}
			return controllerutil.SetControllerReference(cluster, deploy, r.Scheme)
		}); err != nil {
			return fmt.Errorf("failed to create/update multiadmin: %w", err)
		}
	}
	return nil
}

func (r *MultigresClusterReconciler) reconcileCells(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
	resolver *defaults.Resolver,
) error {
	existingCells := &multigresv1alpha1.CellList{}
	if err := r.List(ctx, existingCells, client.InNamespace(cluster.Namespace), client.MatchingLabels{"multigres.com/cluster": cluster.Name}); err != nil {
		return fmt.Errorf("failed to list existing cells: %w", err)
	}

	globalTopoRef, err := r.getGlobalTopoRef(ctx, cluster, resolver)
	if err != nil {
		return fmt.Errorf("failed to get global topo ref: %w", err)
	}

	activeCellNames := make(map[string]bool, len(cluster.Spec.Cells))

	allCellNames := []multigresv1alpha1.CellName{}
	for _, cellCfg := range cluster.Spec.Cells {
		allCellNames = append(allCellNames, multigresv1alpha1.CellName(cellCfg.Name))
	}

	for _, cellCfg := range cluster.Spec.Cells {
		activeCellNames[cellCfg.Name] = true

		tpl, err := resolver.ResolveCellTemplate(ctx, cellCfg.CellTemplate)
		if err != nil {
			return fmt.Errorf("failed to resolve cell template '%s': %w", cellCfg.CellTemplate, err)
		}

		gatewaySpec, localTopoSpec := defaults.MergeCellConfig(tpl, cellCfg.Overrides, cellCfg.Spec)

		cellCR := &multigresv1alpha1.Cell{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cluster.Name + "-" + cellCfg.Name,
				Namespace: cluster.Namespace,
				Labels: map[string]string{
					"multigres.com/cluster": cluster.Name,
					"multigres.com/cell":    cellCfg.Name,
				},
			},
		}

		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, cellCR, func() error {
			cellCR.Spec.Name = cellCfg.Name
			cellCR.Spec.Zone = cellCfg.Zone
			cellCR.Spec.Region = cellCfg.Region
			cellCR.Spec.MultiGatewayImage = cluster.Spec.Images.MultiGateway
			cellCR.Spec.MultiGateway = gatewaySpec
			cellCR.Spec.AllCells = allCellNames

			cellCR.Spec.GlobalTopoServer = globalTopoRef

			cellCR.Spec.TopoServer = localTopoSpec

			cellCR.Spec.TopologyReconciliation = multigresv1alpha1.TopologyReconciliation{
				RegisterCell: true,
				PrunePoolers: true,
			}

			return controllerutil.SetControllerReference(cluster, cellCR, r.Scheme)
		}); err != nil {
			return fmt.Errorf("failed to create/update cell '%s': %w", cellCfg.Name, err)
		}
	}

	for _, item := range existingCells.Items {
		if !activeCellNames[item.Spec.Name] {
			if err := r.Delete(ctx, &item); err != nil {
				return fmt.Errorf("failed to delete orphaned cell '%s': %w", item.Name, err)
			}
		}
	}

	return nil
}

func (r *MultigresClusterReconciler) reconcileDatabases(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
	resolver *defaults.Resolver,
) error {
	existingTGs := &multigresv1alpha1.TableGroupList{}
	if err := r.List(ctx, existingTGs, client.InNamespace(cluster.Namespace), client.MatchingLabels{"multigres.com/cluster": cluster.Name}); err != nil {
		return fmt.Errorf("failed to list existing tablegroups: %w", err)
	}

	globalTopoRef, err := r.getGlobalTopoRef(ctx, cluster, resolver)
	if err != nil {
		return fmt.Errorf("failed to get global topo ref: %w", err)
	}

	activeTGNames := make(map[string]bool)

	for _, db := range cluster.Spec.Databases {
		for _, tg := range db.TableGroups {
			tgNameFull := fmt.Sprintf("%s-%s-%s", cluster.Name, db.Name, tg.Name)
			if len(tgNameFull) > 50 {
				return fmt.Errorf(
					"TableGroup name '%s' exceeds 50 characters; limit required to allow for shard resource suffixing",
					tgNameFull,
				)
			}

			activeTGNames[tgNameFull] = true

			resolvedShards := []multigresv1alpha1.ShardResolvedSpec{}

			for _, shard := range tg.Shards {
				tpl, err := resolver.ResolveShardTemplate(ctx, shard.ShardTemplate)
				if err != nil {
					return fmt.Errorf(
						"failed to resolve shard template '%s': %w",
						shard.ShardTemplate,
						err,
					)
				}

				orch, pools := defaults.MergeShardConfig(tpl, shard.Overrides, shard.Spec)

				// Default MultiOrch Cells if empty (Consensus safety)
				// If 'cells' is empty, it defaults to all cells where pools are defined.
				if len(orch.Cells) == 0 {
					uniqueCells := make(map[string]bool)
					for _, pool := range pools {
						for _, cell := range pool.Cells {
							uniqueCells[string(cell)] = true
						}
					}
					for c := range uniqueCells {
						orch.Cells = append(orch.Cells, multigresv1alpha1.CellName(c))
					}
					// Sort for deterministic output
					sort.Slice(orch.Cells, func(i, j int) bool {
						return string(orch.Cells[i]) < string(orch.Cells[j])
					})
				}

				resolvedShards = append(resolvedShards, multigresv1alpha1.ShardResolvedSpec{
					Name:      shard.Name,
					MultiOrch: orch,
					Pools:     pools,
				})
			}

			tgCR := &multigresv1alpha1.TableGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tgNameFull,
					Namespace: cluster.Namespace,
					Labels: map[string]string{
						"multigres.com/cluster":    cluster.Name,
						"multigres.com/database":   db.Name,
						"multigres.com/tablegroup": tg.Name,
					},
				},
			}

			if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, tgCR, func() error {
				tgCR.Spec.DatabaseName = db.Name
				tgCR.Spec.TableGroupName = tg.Name
				tgCR.Spec.IsDefault = tg.Default
				tgCR.Spec.Images = multigresv1alpha1.ShardImages{
					MultiOrch:   cluster.Spec.Images.MultiOrch,
					MultiPooler: cluster.Spec.Images.MultiPooler,
					Postgres:    cluster.Spec.Images.Postgres,
				}
				tgCR.Spec.GlobalTopoServer = globalTopoRef
				tgCR.Spec.Shards = resolvedShards

				return controllerutil.SetControllerReference(cluster, tgCR, r.Scheme)
			}); err != nil {
				return fmt.Errorf("failed to create/update tablegroup '%s': %w", tgNameFull, err)
			}
		}
	}

	for _, item := range existingTGs.Items {
		if !activeTGNames[item.Name] {
			if err := r.Delete(ctx, &item); err != nil {
				return fmt.Errorf("failed to delete orphaned tablegroup '%s': %w", item.Name, err)
			}
		}
	}

	return nil
}

func (r *MultigresClusterReconciler) getGlobalTopoRef(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
	resolver *defaults.Resolver,
) (multigresv1alpha1.GlobalTopoServerRef, error) {
	topoTplName := cluster.Spec.TemplateDefaults.CoreTemplate
	if cluster.Spec.GlobalTopoServer.TemplateRef != "" {
		topoTplName = cluster.Spec.GlobalTopoServer.TemplateRef
	}

	topoTpl, err := resolver.ResolveCoreTemplate(ctx, topoTplName)
	if err != nil {
		return multigresv1alpha1.GlobalTopoServerRef{}, fmt.Errorf(
			"failed to resolve global topo template: %w",
			err,
		)
	}

	topoSpec := defaults.ResolveGlobalTopo(&cluster.Spec.GlobalTopoServer, topoTpl)

	address := ""
	if topoSpec.Etcd != nil {
		address = fmt.Sprintf("%s-global-topo-client.%s.svc:2379", cluster.Name, cluster.Namespace)
	} else if topoSpec.External != nil && len(topoSpec.External.Endpoints) > 0 {
		address = string(topoSpec.External.Endpoints[0])
	}

	return multigresv1alpha1.GlobalTopoServerRef{
		Address:        address,
		RootPath:       "/multigres/global",
		Implementation: "etcd2",
	}, nil
}

func (r *MultigresClusterReconciler) updateStatus(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
) error {
	cluster.Status.ObservedGeneration = cluster.Generation
	cluster.Status.Cells = make(map[string]multigresv1alpha1.CellStatusSummary)
	cluster.Status.Databases = make(map[string]multigresv1alpha1.DatabaseStatusSummary)

	cells := &multigresv1alpha1.CellList{}
	if err := r.List(ctx, cells, client.InNamespace(cluster.Namespace), client.MatchingLabels{"multigres.com/cluster": cluster.Name}); err != nil {
		return fmt.Errorf("failed to list cells for status: %w", err)
	}

	for _, c := range cells.Items {
		ready := false
		for _, cond := range c.Status.Conditions {
			if cond.Type == "Available" && cond.Status == "True" {
				ready = true
				break
			}
		}
		cluster.Status.Cells[c.Spec.Name] = multigresv1alpha1.CellStatusSummary{
			Ready:           ready,
			GatewayReplicas: c.Status.GatewayReplicas,
		}
	}

	tgs := &multigresv1alpha1.TableGroupList{}
	if err := r.List(ctx, tgs, client.InNamespace(cluster.Namespace), client.MatchingLabels{"multigres.com/cluster": cluster.Name}); err != nil {
		return fmt.Errorf("failed to list tablegroups for status: %w", err)
	}

	dbShards := make(map[string]struct {
		Ready int32
		Total int32
	})

	for _, tg := range tgs.Items {
		stat := dbShards[tg.Spec.DatabaseName]
		stat.Ready += tg.Status.ReadyShards
		stat.Total += tg.Status.TotalShards
		dbShards[tg.Spec.DatabaseName] = stat
	}

	for dbName, stat := range dbShards {
		cluster.Status.Databases[dbName] = multigresv1alpha1.DatabaseStatusSummary{
			ReadyShards: stat.Ready,
			TotalShards: stat.Total,
		}
	}

	allCellsReady := true
	for _, c := range cluster.Status.Cells {
		if !c.Ready {
			allCellsReady = false
			break
		}
	}

	statusStr := metav1.ConditionFalse
	if allCellsReady && len(cluster.Status.Cells) > 0 {
		statusStr = metav1.ConditionTrue
	}

	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             statusStr,
		Reason:             "AggregatedStatus",
		Message:            "Aggregation of cell availability",
		LastTransitionTime: metav1.Now(),
	})

	if err := r.Status().Update(ctx, cluster); err != nil {
		return fmt.Errorf("failed to update cluster status: %w", err)
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

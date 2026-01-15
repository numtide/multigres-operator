package multigrescluster

import (
	"context"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resolver"
)

func (r *MultigresClusterReconciler) reconcileDatabases(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
	res *resolver.Resolver,
) error {
	existingTGs := &multigresv1alpha1.TableGroupList{}
	if err := r.List(ctx, existingTGs, client.InNamespace(cluster.Namespace), client.MatchingLabels{"multigres.com/cluster": cluster.Name}); err != nil {
		return fmt.Errorf("failed to list existing tablegroups: %w", err)
	}

	globalTopoRef, err := r.getGlobalTopoRef(ctx, cluster, res)
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
				orch, pools, err := res.ResolveShard(ctx, &shard)
				if err != nil {
					r.Recorder.Eventf(
						cluster,
						"Warning",
						"ConfigError",
						"Failed to resolve shard %s: %v",
						shard.Name,
						err,
					)
					return fmt.Errorf(
						"failed to resolve shard '%s': %w",
						shard.Name,
						err,
					)
				}

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
					sort.Slice(orch.Cells, func(i, j int) bool {
						return string(orch.Cells[i]) < string(orch.Cells[j])
					})
				}

				resolvedShards = append(resolvedShards, multigresv1alpha1.ShardResolvedSpec{
					Name:      shard.Name,
					MultiOrch: *orch,
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

			op, err := controllerutil.CreateOrUpdate(ctx, r.Client, tgCR, func() error {
				tgCR.Spec.DatabaseName = db.Name
				tgCR.Spec.TableGroupName = tg.Name
				tgCR.Spec.IsDefault = tg.Default

				tgCR.Spec.Images = multigresv1alpha1.ShardImages{
					MultiOrch:        cluster.Spec.Images.MultiOrch,
					MultiPooler:      cluster.Spec.Images.MultiPooler,
					Postgres:         cluster.Spec.Images.Postgres,
					ImagePullPolicy:  cluster.Spec.Images.ImagePullPolicy,
					ImagePullSecrets: cluster.Spec.Images.ImagePullSecrets,
				}

				tgCR.Spec.GlobalTopoServer = globalTopoRef
				tgCR.Spec.Shards = resolvedShards

				return controllerutil.SetControllerReference(cluster, tgCR, r.Scheme)
			})
			if err != nil {
				return fmt.Errorf("failed to create/update tablegroup '%s': %w", tgNameFull, err)
			}
			if op == controllerutil.OperationResultCreated {
				r.Recorder.Eventf(cluster, "Normal", "Created", "Created TableGroup %s", tgCR.Name)
			}
		}
	}

	for _, item := range existingTGs.Items {
		if !activeTGNames[item.Name] {
			if err := r.Delete(ctx, &item); err != nil {
				return fmt.Errorf("failed to delete orphaned tablegroup '%s': %w", item.Name, err)
			}
			r.Recorder.Eventf(
				cluster,
				"Normal",
				"Deleted",
				"Deleted orphaned TableGroup %s",
				item.Name,
			)
		}
	}

	return nil
}

package multigrescluster

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

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
			// ACTIVE MAP REGISTRATION MOVED DOWN: We must use the desired.Name (which might be hashed)
			// instead of the logical tgNameFull.

			resolvedShards := []multigresv1alpha1.ShardResolvedSpec{}

			// Extract all valid cell names for this cluster (Contextual Awareness)
			var allCellNames []string
			for _, c := range cluster.Spec.Cells {
				allCellNames = append(allCellNames, c.Name)
			}

			for _, shard := range tg.Shards {
				// Pass allCellNames to the resolver so it can perform "Empty means Everybody" defaulting
				orch, pools, err := res.ResolveShard(ctx, &shard, allCellNames)
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

				// The Resolver now handles the "Empty Cells = All Cells" logic authoritative.
				// We no longer need to manually infer or sort here, just trust the resolver.
				resolvedShards = append(resolvedShards, multigresv1alpha1.ShardResolvedSpec{
					Name:      shard.Name,
					MultiOrch: *orch,
					Pools:     pools,
				})
			}

			desired, err := BuildTableGroup(
				cluster,
				db.Name,
				&tg,
				resolvedShards,
				globalTopoRef,
				r.Scheme,
			)
			if err != nil {
				return fmt.Errorf("failed to build tablegroup '%s': %w", tgNameFull, err)
			}
			activeTGNames[desired.Name] = true

			// Server Side Apply
			desired.SetGroupVersionKind(multigresv1alpha1.GroupVersion.WithKind("TableGroup"))
			if err := r.Patch(
				ctx,
				desired,
				client.Apply,
				client.ForceOwnership,
				client.FieldOwner("multigres-operator"),
			); err != nil {
				return fmt.Errorf("failed to apply tablegroup '%s': %w", tgNameFull, err)
			}
			r.Recorder.Eventf(cluster, "Normal", "Applied", "Applied TableGroup %s", desired.Name)
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

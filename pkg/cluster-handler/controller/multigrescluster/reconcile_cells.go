package multigrescluster

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resolver"
)

func (r *MultigresClusterReconciler) reconcileCells(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
	res *resolver.Resolver,
) error {
	existingCells := &multigresv1alpha1.CellList{}
	if err := r.List(ctx, existingCells, client.InNamespace(cluster.Namespace), client.MatchingLabels{"multigres.com/cluster": cluster.Name}); err != nil {
		return fmt.Errorf("failed to list existing cells: %w", err)
	}

	globalTopoRef, err := r.getGlobalTopoRef(ctx, cluster, res)
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

		gatewaySpec, localTopoSpec, err := res.ResolveCell(ctx, &cellCfg)
		if err != nil {
			r.Recorder.Event(cluster, "Warning", "TemplateMissing", err.Error())
			return fmt.Errorf("failed to resolve cell '%s': %w", cellCfg.Name, err)
		}

		desired, err := BuildCell(
			cluster,
			&cellCfg,
			gatewaySpec,
			localTopoSpec,
			globalTopoRef,
			allCellNames,
			r.Scheme,
		)
		if err != nil {
			return fmt.Errorf("failed to build cell '%s': %w", cellCfg.Name, err)
		}

		existing := &multigresv1alpha1.Cell{}
		err = r.Get(
			ctx,
			client.ObjectKey{Namespace: cluster.Namespace, Name: desired.Name},
			existing,
		)
		if err != nil {
			if errors.IsNotFound(err) {
				if err := r.Create(ctx, desired); err != nil {
					return fmt.Errorf("failed to create cell '%s': %w", cellCfg.Name, err)
				}
				r.Recorder.Eventf(cluster, "Normal", "Created", "Created Cell %s", desired.Name)
				continue
			}
			return fmt.Errorf("failed to get cell '%s': %w", cellCfg.Name, err)
		}

		existing.Spec = desired.Spec
		existing.Labels = desired.Labels
		if err := r.Update(ctx, existing); err != nil {
			return fmt.Errorf("failed to update cell '%s': %w", cellCfg.Name, err)
		}
	}

	for _, item := range existingCells.Items {
		if !activeCellNames[item.Spec.Name] {
			if err := r.Delete(ctx, &item); err != nil {
				return fmt.Errorf("failed to delete orphaned cell '%s': %w", item.Name, err)
			}
			r.Recorder.Eventf(cluster, "Normal", "Deleted", "Deleted orphaned Cell %s", item.Name)
		}
	}

	return nil
}

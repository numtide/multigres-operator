package multigrescluster

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

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

		op, err := controllerutil.CreateOrUpdate(ctx, r.Client, cellCR, func() error {
			cellCR.Spec.Name = cellCfg.Name
			cellCR.Spec.Zone = cellCfg.Zone
			cellCR.Spec.Region = cellCfg.Region

			cellCR.Spec.Images = multigresv1alpha1.CellImages{
				MultiGateway:     cluster.Spec.Images.MultiGateway,
				ImagePullPolicy:  cluster.Spec.Images.ImagePullPolicy,
				ImagePullSecrets: cluster.Spec.Images.ImagePullSecrets,
			}

			cellCR.Spec.MultiGateway = *gatewaySpec
			cellCR.Spec.AllCells = allCellNames
			cellCR.Spec.GlobalTopoServer = globalTopoRef
			cellCR.Spec.TopoServer = localTopoSpec
			cellCR.Spec.TopologyReconciliation = multigresv1alpha1.TopologyReconciliation{
				RegisterCell: true,
				PrunePoolers: true,
			}

			return controllerutil.SetControllerReference(cluster, cellCR, r.Scheme)
		})
		if err != nil {
			return fmt.Errorf("failed to create/update cell '%s': %w", cellCfg.Name, err)
		}
		if op == controllerutil.OperationResultCreated {
			r.Recorder.Eventf(cluster, "Normal", "Created", "Created Cell %s", cellCR.Name)
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

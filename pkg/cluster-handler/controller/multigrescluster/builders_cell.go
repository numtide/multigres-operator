package multigrescluster

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/cluster-handler/names"
)

// BuildCell constructs the desired Cell resource.
func BuildCell(
	cluster *multigresv1alpha1.MultigresCluster,
	cellCfg *multigresv1alpha1.CellConfig,
	gatewaySpec *multigresv1alpha1.StatelessSpec,
	localTopoSpec *multigresv1alpha1.LocalTopoServerSpec,
	globalTopoRef multigresv1alpha1.GlobalTopoServerRef,
	allCellNames []multigresv1alpha1.CellName,
	scheme *runtime.Scheme,
) (*multigresv1alpha1.Cell, error) {
	cellCR := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.JoinWithConstraints(
				names.DefaultConstraints,
				cluster.Name,
				cellCfg.Name,
			),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"multigres.com/cluster": cluster.Name,
				"multigres.com/cell":    cellCfg.Name,
			},
		},
		Spec: multigresv1alpha1.CellSpec{
			Name:   cellCfg.Name,
			Zone:   cellCfg.Zone,
			Region: cellCfg.Region,
			Images: multigresv1alpha1.CellImages{
				MultiGateway:     cluster.Spec.Images.MultiGateway,
				ImagePullPolicy:  cluster.Spec.Images.ImagePullPolicy,
				ImagePullSecrets: cluster.Spec.Images.ImagePullSecrets,
			},
			MultiGateway:     *gatewaySpec,
			AllCells:         allCellNames,
			GlobalTopoServer: globalTopoRef,
			TopoServer:       localTopoSpec,
			TopologyReconciliation: multigresv1alpha1.TopologyReconciliation{
				RegisterCell: true,
				PrunePoolers: true,
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, cellCR, scheme); err != nil {
		return nil, err
	}

	return cellCR, nil
}

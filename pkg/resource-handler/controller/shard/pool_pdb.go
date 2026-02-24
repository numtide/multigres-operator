package shard

import (
	"fmt"

	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
	nameutil "github.com/numtide/multigres-operator/pkg/util/name"
)

// BuildPoolPodDisruptionBudget creates a PodDisruptionBudget that limits
// voluntary evictions to one pod at a time per pool/cell combination.
func BuildPoolPodDisruptionBudget(
	shard *multigresv1alpha1.Shard,
	poolName string,
	cellName string,
	scheme *runtime.Scheme,
) (*policyv1.PodDisruptionBudget, error) {
	clusterName := shard.Labels["multigres.com/cluster"]
	pdbName := nameutil.JoinWithConstraints(
		nameutil.ServiceConstraints,
		clusterName,
		string(shard.Spec.DatabaseName),
		string(shard.Spec.TableGroupName),
		string(shard.Spec.ShardName),
		"pool",
		poolName,
		cellName,
		"pdb",
	)

	labels := metadata.BuildStandardLabels(clusterName, PoolComponentName)
	metadata.AddShardLabel(labels, shard.Spec.ShardName)
	metadata.AddCellLabel(labels, multigresv1alpha1.CellName(cellName))
	metadata.AddPoolLabel(labels, multigresv1alpha1.PoolName(poolName))
	metadata.AddDatabaseLabel(labels, shard.Spec.DatabaseName)
	metadata.AddTableGroupLabel(labels, shard.Spec.TableGroupName)

	selectorLabels := metadata.GetSelectorLabels(labels)
	maxUnavailable := intstr.FromInt32(1)

	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pdbName,
			Namespace: shard.Namespace,
			Labels:    labels,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: &maxUnavailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
		},
	}

	if err := ctrl.SetControllerReference(shard, pdb, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return pdb, nil
}

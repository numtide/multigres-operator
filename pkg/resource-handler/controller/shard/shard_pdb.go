package shard

import (
	"fmt"
	"slices"

	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/multigres/multigres-operator/api/v1alpha1"
	"github.com/multigres/multigres-operator/pkg/util/metadata"
	nameutil "github.com/multigres/multigres-operator/pkg/util/name"
)

const multiCellAtLeast2Policy = "MULTI_CELL_AT_LEAST_2"

// BuildShardPodDisruptionBudget creates the primary shard-wide
// PodDisruptionBudget.
//
// The Shard scale subresource reports the total number of replicas across all
// pools and cells. The PDB selector must therefore cover that same set of pods;
// a narrower selector makes the disruption controller compare a pool-cell's
// pod count with the shard-wide expected replica count and block all evictions.
//
// minAvailable keeps both invariants that matter to Multigres: at least two
// poolers remain available for the currently supported durability policies,
// and no more than one desired pooler is voluntarily disrupted at a time.
func BuildShardPodDisruptionBudget(
	shard *multigresv1alpha1.Shard,
	scheme *runtime.Scheme,
) (*policyv1.PodDisruptionBudget, error) {
	clusterName := shard.Labels[metadata.LabelMultigresCluster]
	pdbName := nameutil.JoinWithConstraints(
		nameutil.ServiceConstraints,
		clusterName,
		string(shard.Spec.DatabaseName),
		string(shard.Spec.TableGroupName),
		string(shard.Spec.ShardName),
		"pdb",
	)

	labels := shardPDBLabels(shard)
	selectorLabels := metadata.GetSelectorLabels(labels)
	minAvailable := intstr.FromInt32(shardMinAvailable(shard))

	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pdbName,
			Namespace: shard.Namespace,
			Labels:    labels,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
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

// BuildCellPodDisruptionBudget keeps at least one pooler available in cellName.
// It is used only for a two-cell MULTI_CELL_AT_LEAST_2 shard, where losing the
// last pooler in either cell would make cross-cell durability impossible. An
// integer minAvailable is intentional: unlike maxUnavailable, it does not ask
// the Shard /scale subresource for a cell-local expected replica count.
func BuildCellPodDisruptionBudget(
	shard *multigresv1alpha1.Shard,
	cellName string,
	scheme *runtime.Scheme,
) (*policyv1.PodDisruptionBudget, error) {
	clusterName := shard.Labels[metadata.LabelMultigresCluster]
	pdbName := nameutil.JoinWithConstraints(
		nameutil.ServiceConstraints,
		clusterName,
		string(shard.Spec.DatabaseName),
		string(shard.Spec.TableGroupName),
		string(shard.Spec.ShardName),
		"cell",
		cellName,
		"pdb",
	)

	labels := shardPDBLabels(shard)
	metadata.AddCellLabel(labels, multigresv1alpha1.CellName(cellName))
	selectorLabels := metadata.GetSelectorLabels(labels)
	minAvailable := intstr.FromInt32(1)

	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pdbName,
			Namespace: shard.Namespace,
			Labels:    labels,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
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

// BuildShardPodDisruptionBudgets returns every PDB required by the shard. A
// two-cell cross-cell durability policy gets one additional integer budget per
// cell so Kubernetes cannot evict the final available member of either cell.
func BuildShardPodDisruptionBudgets(
	shard *multigresv1alpha1.Shard,
	scheme *runtime.Scheme,
) ([]*policyv1.PodDisruptionBudget, error) {
	shardPDB, err := BuildShardPodDisruptionBudget(shard, scheme)
	if err != nil {
		return nil, err
	}
	desired := []*policyv1.PodDisruptionBudget{shardPDB}

	cells := shardCells(shard)
	if shard.Spec.DurabilityPolicy != multiCellAtLeast2Policy || len(cells) != 2 {
		return desired, nil
	}
	for _, cell := range cells {
		cellPDB, err := BuildCellPodDisruptionBudget(shard, cell, scheme)
		if err != nil {
			return nil, err
		}
		desired = append(desired, cellPDB)
	}
	return desired, nil
}

func shardPDBLabels(shard *multigresv1alpha1.Shard) map[string]string {
	clusterName := shard.Labels[metadata.LabelMultigresCluster]
	labels := metadata.BuildStandardLabels(clusterName, PoolComponentName)
	metadata.AddClusterLabel(labels, clusterName)
	metadata.AddShardLabel(labels, shard.Spec.ShardName)
	metadata.AddDatabaseLabel(labels, shard.Spec.DatabaseName)
	metadata.AddTableGroupLabel(labels, shard.Spec.TableGroupName)
	return labels
}

func shardCells(shard *multigresv1alpha1.Shard) []string {
	seen := make(map[string]struct{})
	for _, pool := range shard.Spec.Pools {
		for _, cell := range pool.Cells {
			seen[string(cell)] = struct{}{}
		}
	}
	cells := make([]string, 0, len(seen))
	for cell := range seen {
		cells = append(cells, cell)
	}
	slices.Sort(cells)
	return cells
}

func shardTotalReplicas(shard *multigresv1alpha1.Shard) int32 {
	if shard.Spec.Replicas != nil {
		return *shard.Spec.Replicas
	}
	var total int32
	for _, pool := range shard.Spec.Pools {
		replicas := DefaultPoolReplicas
		if pool.ReplicasPerCell != nil {
			replicas = *pool.ReplicasPerCell
		}
		total += replicas * int32(len(pool.Cells)) // #nosec G115 -- cell count is API-bounded
	}
	return total
}

func shardMinAvailable(shard *multigresv1alpha1.Shard) int32 {
	return minAvailableForTotal(shardTotalReplicas(shard))
}

func minAvailableForTotal(total int32) int32 {
	minAvailable := total - 1
	if minAvailable < 2 {
		return 2
	}
	return minAvailable
}

package shard

import (
	multigresv1alpha1 "github.com/multigres/multigres-operator/api/v1alpha1"
	"github.com/multigres/multigres-operator/pkg/util/metadata"
)

// buildPoolLabelsWithCell creates labels for pool resources in a specific cell.
// cellName must not be empty - pools must belong to a cell.
func buildPoolLabelsWithCell(
	shard *multigresv1alpha1.Shard,
	poolName string,
	cellName string,
) map[string]string {
	clusterName := shard.Labels["multigres.com/cluster"]
	labels := metadata.BuildStandardLabels(clusterName, PoolComponentName)
	metadata.AddShardLabel(labels, shard.Spec.ShardName)
	metadata.AddCellLabel(labels, multigresv1alpha1.CellName(cellName))
	metadata.AddPoolLabel(labels, multigresv1alpha1.PoolName(poolName))
	metadata.AddDatabaseLabel(labels, shard.Spec.DatabaseName)
	metadata.AddTableGroupLabel(labels, shard.Spec.TableGroupName)

	labels = metadata.MergeLabels(labels, shard.GetObjectMeta().GetLabels())

	return labels
}

package shard

import (
	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/metadata"
)

// buildPoolLabelsWithCell creates labels for a pool StatefulSet in a specific cell.
// For pools spanning multiple cells, each StatefulSet gets its own specific cell label.
// cellName must not be empty - pools must belong to a cell.
func buildPoolLabelsWithCell(
	shard *multigresv1alpha1.Shard,
	poolName string,
	cellName string,
	poolSpec multigresv1alpha1.PoolSpec,
) map[string]string {
	clusterName := shard.Labels["multigres.com/cluster"]
	labels := metadata.BuildStandardLabels(clusterName, PoolComponentName)
	metadata.AddCellLabel(labels, cellName)
	metadata.AddDatabaseLabel(labels, string(shard.Spec.DatabaseName))
	metadata.AddTableGroupLabel(labels, string(shard.Spec.TableGroupName))

	labels = metadata.MergeLabels(labels, shard.GetObjectMeta().GetLabels())

	return labels
}

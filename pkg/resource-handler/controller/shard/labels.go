package shard

import (
	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/metadata"
)

// buildPoolLabels creates standard labels for pool resources, and uses the
// pool's database, table group, and cell details. Any additional labels are
// also merged, while keeping the main labels.
func buildPoolLabels(
	shard *multigresv1alpha1.Shard,
	poolName string,
	poolSpec multigresv1alpha1.ShardPoolSpec,
) map[string]string {
	fullPoolName := buildPoolName(shard.Name, poolName)
	cellName := poolSpec.Cell
	if cellName == "" {
		cellName = metadata.DefaultCellName
	}

	// TODO: Remove this once we figure what to do with the cell name.
	_ = cellName

	labels := metadata.BuildStandardLabels(fullPoolName, PoolComponentName)
	// TODO: Add multigres.com/* labels after finalizing label design:
	// metadata.AddCellLabel(labels, cellName)
	// metadata.AddDatabaseLabel(labels, poolSpec.Database)
	// metadata.AddTableGroupLabel(labels, poolSpec.TableGroup)

	metadata.MergeLabels(labels, shard.GetObjectMeta().GetLabels())

	return labels
}

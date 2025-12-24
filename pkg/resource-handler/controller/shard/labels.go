package shard

import (
	"strings"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/metadata"
)

// buildPoolLabels creates standard labels for pool resources, and uses the
// pool's database, table group, and cell details. Any additional labels are
// also merged, while keeping the main labels.
func buildPoolLabels(
	shard *multigresv1alpha1.Shard,
	poolName string,
	poolSpec multigresv1alpha1.PoolSpec,
) map[string]string {
	fullPoolName := buildPoolName(shard.Name, poolName)

	// Build comma-separated list of cells for the label
	cellNames := make([]string, len(poolSpec.Cells))
	for i, cell := range poolSpec.Cells {
		cellNames[i] = string(cell)
	}
	cellLabel := metadata.DefaultCellName
	if len(cellNames) > 0 {
		cellLabel = strings.Join(cellNames, ",")
	}

	labels := metadata.BuildStandardLabels(fullPoolName, PoolComponentName)
	metadata.AddCellLabel(labels, cellLabel)
	metadata.AddDatabaseLabel(labels, shard.Spec.DatabaseName)
	metadata.AddTableGroupLabel(labels, shard.Spec.TableGroupName)

	metadata.MergeLabels(labels, shard.GetObjectMeta().GetLabels())

	return labels
}

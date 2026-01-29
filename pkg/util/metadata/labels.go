package metadata

import (
	"maps"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// Standard Kubernetes label keys following kubernetes.io conventions.
//
// See: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
const (
	// LabelAppName is the standard label key for the application name.
	LabelAppName = "app.kubernetes.io/name"

	// LabelAppInstance is the standard label key for the unique instance name.
	LabelAppInstance = "app.kubernetes.io/instance"

	// LabelAppVersion is the standard label key for the application version.
	LabelAppVersion = "app.kubernetes.io/version"

	// LabelAppComponent is the standard label key for the component within the
	// application.
	LabelAppComponent = "app.kubernetes.io/component"

	// LabelAppPartOf is the standard label key for the name of a higher level
	// application this one is part of.
	LabelAppPartOf = "app.kubernetes.io/part-of"

	// LabelAppManagedBy is the standard label key for the tool managing the
	// resource.
	LabelAppManagedBy = "app.kubernetes.io/managed-by"
)

const (
	// AppNameMultigres is the fixed application name for all Multigres resources.
	AppNameMultigres = "multigres"

	// ManagedByMultigres identifies the operator managing these resources.
	ManagedByMultigres = "multigres-operator"
)

const (
	// ComponentMultiAdmin identifies the multiadmin component.
	ComponentMultiAdmin = "multiadmin"

	// ComponentGlobalTopo identifies the global-topo component.
	ComponentGlobalTopo = "global-topo"

	// ComponentCell identifies the cell component.
	ComponentCell = "cell"

	// ComponentShard identifies the shard component.
	ComponentShard = "shard"

	// ComponentTableGroup identifies the table group component.
	ComponentTableGroup = "tablegroup"
)

const (
	// LabelMultigresCell identifies which cell a resource belongs to.
	LabelMultigresCell = "multigres.com/cell"

	// LabelMultigresCluster identifies which cluster a resource belongs to.
	LabelMultigresCluster = "multigres.com/cluster"

	// LabelMultigresShard identifies which shard a resource belongs to.
	LabelMultigresShard = "multigres.com/shard"

	// LabelMultigresDatabase identifies which database a resource belongs to.
	LabelMultigresDatabase = "multigres.com/database"

	// LabelMultigresTableGroup identifies which table group a resource belongs to.
	LabelMultigresTableGroup = "multigres.com/tablegroup"

	// LabelMultigresPool identifies which pool a resource belongs to.
	LabelMultigresPool = "multigres.com/pool"

	// LabelMultigresZone identifies which zone a resource belongs to.
	LabelMultigresZone = "multigres.com/zone"

	// LabelMultigresRegion identifies which region a resource belongs to.
	LabelMultigresRegion = "multigres.com/region"

	// DefaultCellName is the default cell name when none is specified.
	DefaultCellName = "multigres-global-topo"
)

const (
	// LabelMultigresCellTemplate identifies which cell template was used for
	// the given resource. This is needed for the operator to efficiently find
	// all the relevant resources using the CellTemplate.
	LabelMultigresCellTemplate = "multigres.com/cell-template"

	// LabelMultigresShardTemplate identifies which cell template was used for
	// the given resource. This is needed for the operator to efficiently find
	// all the relevant resources using the ShardTemplate.
	LabelMultigresShardTemplate = "multigres.com/shard-template"
)

// BuildStandardLabels returns a map of standard kubernetes labels.
// clusterName should be the name of the MultigresCluster CR (used for instance label).
// component is the name of the component (e.g. multigateway, multiorch, pool).
func BuildStandardLabels(clusterName, component string) map[string]string {
	return map[string]string{
		LabelAppName:      AppNameMultigres,
		LabelAppInstance:  clusterName,
		LabelAppComponent: component,
		LabelAppPartOf:    AppNameMultigres,
		LabelAppManagedBy: ManagedByMultigres,
	}
}

// AddCellLabel adds the cell label to the provided labels map.
func AddCellLabel(labels map[string]string, cellName multigresv1alpha1.CellName) map[string]string {
	labels[LabelMultigresCell] = string(cellName)
	return labels
}

// AddClusterLabel adds the cluster label to the provided labels map.
func AddClusterLabel(labels map[string]string, clusterName string) map[string]string {
	labels[LabelMultigresCluster] = clusterName
	return labels
}

// AddShardLabel adds the shard label to the provided labels map.
func AddShardLabel(
	labels map[string]string,
	shardName multigresv1alpha1.ShardName,
) map[string]string {
	labels[LabelMultigresShard] = string(shardName)
	return labels
}

// AddDatabaseLabel adds the database label to the provided labels map.
func AddDatabaseLabel(
	labels map[string]string,
	databaseName multigresv1alpha1.DatabaseName,
) map[string]string {
	labels[LabelMultigresDatabase] = string(databaseName)
	return labels
}

// AddTableGroupLabel adds the table group label to the provided labels map.
func AddTableGroupLabel(
	labels map[string]string,
	tableGroupName multigresv1alpha1.TableGroupName,
) map[string]string {
	labels[LabelMultigresTableGroup] = string(tableGroupName)
	return labels
}

// AddPoolLabel adds the pool label to the provided labels map.
func AddPoolLabel(
	labels map[string]string,
	poolName multigresv1alpha1.PoolName,
) map[string]string {
	labels[LabelMultigresPool] = string(poolName)
	return labels
}

// AddZoneLabel adds the zone label to the provided labels map.
func AddZoneLabel(
	labels map[string]string,
	zoneName multigresv1alpha1.Zone,
) map[string]string {
	labels[LabelMultigresZone] = string(zoneName)
	return labels
}

// AddRegionLabel adds the region label to the provided labels map.
func AddRegionLabel(
	labels map[string]string,
	regionName multigresv1alpha1.Region,
) map[string]string {
	labels[LabelMultigresRegion] = string(regionName)
	return labels
}

// selectorLabelsAllowList contains the keys that are allowed in label selectors.
// These must be stable identity labels, not mutable metadata.
var selectorLabelsAllowList = map[string]bool{
	LabelAppComponent:        true,
	LabelAppInstance:         true,
	LabelMultigresCluster:    true,
	LabelMultigresDatabase:   true,
	LabelMultigresTableGroup: true,
	LabelMultigresShard:      true,
	LabelMultigresPool:       true,
	LabelMultigresCell:       true,
}

// GetSelectorLabels filters the provided labels map to return only those keys
// allowed in resource selectors (Identity Labels).
//
// This separates stable identity labels from mutable metadata labels like
// versions or location tags, ensuring that changes to mutable metadata do not
// trigger unnecessary recreation of immutable resources (like StatefulSets).
func GetSelectorLabels(labels map[string]string) map[string]string {
	selectorLabels := make(map[string]string)
	for k, v := range labels {
		if selectorLabelsAllowList[k] {
			selectorLabels[k] = v
		}
	}
	return selectorLabels
}

// MergeLabels merges custom labels with standard labels.
//
// Note that standard labels take precedence over custom labels to prevent users
// from overriding critical operator-managed labels.
func MergeLabels(standardLabels, customLabels map[string]string) map[string]string {
	merged := make(map[string]string)

	// Copy custom labels first (if provided)
	maps.Copy(merged, customLabels)

	// Copy standard labels (overwriting any duplicates from custom)
	maps.Copy(merged, standardLabels)

	return merged
}

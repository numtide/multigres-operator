package metadata

import "maps"

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

	// DefaultCellName is the default cell name when none is specified.
	DefaultCellName = "multigres-global-topo"
)

// BuildStandardLabels builds the standard Kubernetes labels for a Multigres
// component. These labels are applied to all resources managed by the operator.
//
// Parameters:
//   - resourceName: The name of the custom resource instance (e.g., "my-etcd-cluster")
//   - componentName: The component type (e.g., "etcd", "gateway", "orch", "pooler")
//
// Standard labels include:
//   - app.kubernetes.io/name: "multigres"
//   - app.kubernetes.io/instance: <resourceName>
//   - app.kubernetes.io/component: <componentName>
//   - app.kubernetes.io/part-of: "multigres"
//   - app.kubernetes.io/managed-by: "multigres-operator"
//
// Example usage:
//
//	labels := BuildStandardLabels("my-etcd", "etcd")
//	// Returns: {
//	//   "app.kubernetes.io/name": "multigres",
//	//   "app.kubernetes.io/instance": "my-etcd",
//	//   "app.kubernetes.io/component": "etcd",
//	//   "app.kubernetes.io/part-of": "multigres",
//	//   "app.kubernetes.io/managed-by": "multigres-operator",
//	// }
func BuildStandardLabels(resourceName, componentName string) map[string]string {
	labels := map[string]string{
		LabelAppName:      AppNameMultigres,
		LabelAppInstance:  resourceName,
		LabelAppComponent: componentName,
		LabelAppPartOf:    AppNameMultigres,
		LabelAppManagedBy: ManagedByMultigres,
	}

	return labels
}

// AddCellLabel adds the cell label to the provided labels map.
func AddCellLabel(labels map[string]string, cellName string) map[string]string {
	labels[LabelMultigresCell] = cellName
	return labels
}

// AddClusterLabel adds the cluster label to the provided labels map.
func AddClusterLabel(labels map[string]string, clusterName string) map[string]string {
	labels[LabelMultigresCluster] = clusterName
	return labels
}

// AddShardLabel adds the shard label to the provided labels map.
func AddShardLabel(labels map[string]string, shardName string) map[string]string {
	labels[LabelMultigresShard] = shardName
	return labels
}

// AddDatabaseLabel adds the database label to the provided labels map.
func AddDatabaseLabel(labels map[string]string, databaseName string) map[string]string {
	labels[LabelMultigresDatabase] = databaseName
	return labels
}

// AddTableGroupLabel adds the table group label to the provided labels map.
func AddTableGroupLabel(labels map[string]string, tableGroupName string) map[string]string {
	labels[LabelMultigresTableGroup] = tableGroupName
	return labels
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

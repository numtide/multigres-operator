package metadata_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/metadata"
)

func TestBuildStandardLabels(t *testing.T) {
	tests := map[string]struct {
		clusterName   string
		componentName string
		want          map[string]string
	}{
		"typical case": {
			clusterName:   "my-etcd-cluster",
			componentName: "etcd",
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "my-etcd-cluster",
				"app.kubernetes.io/component":  "etcd",
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
			},
		},
		"empty strings allowed": {
			clusterName:   "",
			componentName: "",
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "",
				"app.kubernetes.io/component":  "",
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := metadata.BuildStandardLabels(tc.clusterName, tc.componentName)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("BuildStandardLabels() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMergeLabels(t *testing.T) {
	tests := map[string]struct {
		standardLabels map[string]string
		customLabels   map[string]string
		want           map[string]string
	}{
		"standard labels win on conflicts": {
			standardLabels: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "my-resource",
				"app.kubernetes.io/component":  "etcd",
				"app.kubernetes.io/managed-by": "multigres-operator",
			},
			customLabels: map[string]string{
				"app.kubernetes.io/name":      "user-app",      // conflict
				"app.kubernetes.io/component": "user-override", // conflict
				"env":                         "production",    // no conflict
				"team":                        "platform",      // no conflict
			},
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "my-resource",
				"app.kubernetes.io/component":  "etcd",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"env":                          "production",
				"team":                         "platform",
			},
		},
		"nil maps handled correctly": {
			standardLabels: nil,
			customLabels:   nil,
			want:           map[string]string{},
		},
		"only custom labels": {
			standardLabels: nil,
			customLabels: map[string]string{
				"env":  "dev",
				"team": "platform",
			},
			want: map[string]string{
				"env":  "dev",
				"team": "platform",
			},
		},
		"only standard labels": {
			standardLabels: map[string]string{
				"app.kubernetes.io/name":      "multigres",
				"app.kubernetes.io/component": "gateway",
			},
			customLabels: nil,
			want: map[string]string{
				"app.kubernetes.io/name":      "multigres",
				"app.kubernetes.io/component": "gateway",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := metadata.MergeLabels(tc.standardLabels, tc.customLabels)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("MergeLabels() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAddMultigresLabels(t *testing.T) {
	tests := map[string]struct {
		addFunc func(map[string]string, string) map[string]string
		value   string
		key     string
	}{
		"AddCellLabel": {
			addFunc: metadata.AddCellLabel,
			value:   "zone1",
			key:     "multigres.com/cell",
		},
		"AddClusterLabel": {
			addFunc: metadata.AddClusterLabel,
			value:   "prod-cluster",
			key:     "multigres.com/cluster",
		},
		"AddShardLabel": {
			addFunc: metadata.AddShardLabel,
			value:   "shard-0",
			key:     "multigres.com/shard",
		},
		"AddDatabaseLabel": {
			addFunc: metadata.AddDatabaseLabel,
			value:   "proddb",
			key:     "multigres.com/database",
		},
		"AddTableGroupLabel": {
			addFunc: metadata.AddTableGroupLabel,
			value:   "orders",
			key:     "multigres.com/tablegroup",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			labels := map[string]string{
				"app.kubernetes.io/name": "multigres",
			}
			result := tc.addFunc(labels, tc.value)

			want := map[string]string{
				"app.kubernetes.io/name": "multigres",
				tc.key:                   tc.value,
			}

			if diff := cmp.Diff(want, result); diff != "" {
				t.Errorf("%s mismatch (-want +got):\n%s", name, diff)
			}

			// Verify it modified the original map
			if labels[tc.key] != tc.value {
				t.Errorf("%s should modify the original map", name)
			}
		})
	}
}

func TestLabelOperations_ComplexScenarios(t *testing.T) {
	tests := map[string]struct {
		setupFunc func() map[string]string
		want      map[string]string
	}{
		"build standard labels then add all multigres labels": {
			setupFunc: func() map[string]string {
				labels := metadata.BuildStandardLabels("my-cluster", "shard-pool")
				metadata.AddCellLabel(labels, "zone1")
				metadata.AddClusterLabel(labels, "prod-cluster")
				metadata.AddShardLabel(labels, "shard-0")
				metadata.AddDatabaseLabel(labels, "proddb")
				metadata.AddTableGroupLabel(labels, "orders")
				return labels
			},
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "my-cluster",
				"app.kubernetes.io/component":  "shard-pool",
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"multigres.com/cell":           "zone1",
				"multigres.com/cluster":        "prod-cluster",
				"multigres.com/shard":          "shard-0",
				"multigres.com/database":       "proddb",
				"multigres.com/tablegroup":     "orders",
			},
		},
		"merge custom labels then add multigres labels": {
			setupFunc: func() map[string]string {
				standard := metadata.BuildStandardLabels("etcd-1", "etcd")
				custom := map[string]string{
					"env":  "production",
					"team": "platform",
				}
				labels := metadata.MergeLabels(standard, custom)
				metadata.AddCellLabel(labels, "zone2")
				metadata.AddClusterLabel(labels, "main-cluster")
				return labels
			},
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "etcd-1",
				"app.kubernetes.io/component":  "etcd",
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"multigres.com/cell":           "zone2",
				"multigres.com/cluster":        "main-cluster",
				"env":                          "production",
				"team":                         "platform",
			},
		},
		"chain all label operations for shard pool": {
			setupFunc: func() map[string]string {
				poolName := "shard-0-pool-primary"
				labels := metadata.BuildStandardLabels(poolName, "shard-pool")
				metadata.AddCellLabel(labels, "us-east-1a")
				metadata.AddClusterLabel(labels, "production")
				metadata.AddShardLabel(labels, "0")
				metadata.AddDatabaseLabel(labels, "orders_db")
				metadata.AddTableGroupLabel(labels, "orders_tg")

				custom := map[string]string{
					"monitoring": "prometheus",
					"backup":     "enabled",
				}
				return metadata.MergeLabels(labels, custom)
			},
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "shard-0-pool-primary",
				"app.kubernetes.io/component":  "shard-pool",
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"multigres.com/cell":           "us-east-1a",
				"multigres.com/cluster":        "production",
				"multigres.com/shard":          "0",
				"multigres.com/database":       "orders_db",
				"multigres.com/tablegroup":     "orders_tg",
				"monitoring":                   "prometheus",
				"backup":                       "enabled",
			},
		},
		"merge with conflicting multigres labels - standard wins": {
			setupFunc: func() map[string]string {
				labels := metadata.BuildStandardLabels("resource", "component")
				metadata.AddCellLabel(labels, "zone1")
				metadata.AddShardLabel(labels, "shard-0")

				conflicting := map[string]string{
					"multigres.com/cell":  "zone-override",
					"multigres.com/shard": "shard-override",
					"custom":              "value",
				}
				return metadata.MergeLabels(labels, conflicting)
			},
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "resource",
				"app.kubernetes.io/component":  "component",
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"multigres.com/cell":           "zone1",
				"multigres.com/shard":          "shard-0",
				"custom":                       "value",
			},
		},
		"add labels to empty map": {
			setupFunc: func() map[string]string {
				labels := make(map[string]string)
				metadata.AddCellLabel(labels, "zone3")
				metadata.AddShardLabel(labels, "shard-1")
				metadata.AddDatabaseLabel(labels, "testdb")
				return labels
			},
			want: map[string]string{
				"multigres.com/cell":     "zone3",
				"multigres.com/shard":    "shard-1",
				"multigres.com/database": "testdb",
			},
		},
		"overwrite cell label multiple times - last wins": {
			setupFunc: func() map[string]string {
				labels := metadata.BuildStandardLabels("resource", "component")
				metadata.AddCellLabel(labels, "zone1")
				metadata.AddCellLabel(labels, "zone2")
				metadata.AddCellLabel(labels, "zone3")
				return labels
			},
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "resource",
				"app.kubernetes.io/component":  "component",
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"multigres.com/cell":           "zone3",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := tc.setupFunc()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Label operations mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

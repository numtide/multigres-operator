package metadata

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestBuildStandardLabels(t *testing.T) {
	tests := map[string]struct {
		resourceName  string
		componentName string
		cellName      string
		want          map[string]string
	}{
		"basic case with all parameters": {
			resourceName:  "my-etcd-cluster",
			componentName: "etcd",
			cellName:      "cell-1",
			want: map[string]string{
				LabelAppName:       AppNameMultigres,
				LabelAppInstance:   "my-etcd-cluster",
				LabelAppComponent:  "etcd",
				LabelAppPartOf:     AppNameMultigres,
				LabelAppManagedBy:  ManagedByMultigres,
				LabelMultigresCell: "cell-1",
			},
		},
		"empty cellName uses default": {
			resourceName:  "my-gateway",
			componentName: "gateway",
			cellName:      "",
			want: map[string]string{
				LabelAppName:       AppNameMultigres,
				LabelAppInstance:   "my-gateway",
				LabelAppComponent:  "gateway",
				LabelAppPartOf:     AppNameMultigres,
				LabelAppManagedBy:  ManagedByMultigres,
				LabelMultigresCell: DefaultCellName,
			},
		},
		"with cellName should add cell label": {
			resourceName:  "my-orch",
			componentName: "orch",
			cellName:      "cell-alpha",
			want: map[string]string{
				LabelAppName:       AppNameMultigres,
				LabelAppInstance:   "my-orch",
				LabelAppComponent:  "orch",
				LabelAppPartOf:     AppNameMultigres,
				LabelAppManagedBy:  ManagedByMultigres,
				LabelMultigresCell: "cell-alpha",
			},
		},
		"empty resourceName": {
			resourceName:  "",
			componentName: "pooler",
			cellName:      "cell-2",
			want: map[string]string{
				LabelAppName:       AppNameMultigres,
				LabelAppInstance:   "",
				LabelAppComponent:  "pooler",
				LabelAppPartOf:     AppNameMultigres,
				LabelAppManagedBy:  ManagedByMultigres,
				LabelMultigresCell: "cell-2",
			},
		},
		"empty componentName": {
			resourceName:  "my-resource",
			componentName: "",
			cellName:      "cell-3",
			want: map[string]string{
				LabelAppName:       AppNameMultigres,
				LabelAppInstance:   "my-resource",
				LabelAppComponent:  "",
				LabelAppPartOf:     AppNameMultigres,
				LabelAppManagedBy:  ManagedByMultigres,
				LabelMultigresCell: "cell-3",
			},
		},
		"all empty strings - uses default cell": {
			resourceName:  "",
			componentName: "",
			cellName:      "",
			want: map[string]string{
				LabelAppName:       AppNameMultigres,
				LabelAppInstance:   "",
				LabelAppComponent:  "",
				LabelAppPartOf:     AppNameMultigres,
				LabelAppManagedBy:  ManagedByMultigres,
				LabelMultigresCell: DefaultCellName,
			},
		},
		"special characters in names": {
			resourceName:  "my-resource-123",
			componentName: "etcd-v3",
			cellName:      "cell-prod-1",
			want: map[string]string{
				LabelAppName:       AppNameMultigres,
				LabelAppInstance:   "my-resource-123",
				LabelAppComponent:  "etcd-v3",
				LabelAppPartOf:     AppNameMultigres,
				LabelAppManagedBy:  ManagedByMultigres,
				LabelMultigresCell: "cell-prod-1",
			},
		},
		"long names": {
			resourceName:  "very-long-resource-name-with-many-segments",
			componentName: "gateway-proxy-component",
			cellName:      "cell-production-region-1",
			want: map[string]string{
				LabelAppName:       AppNameMultigres,
				LabelAppInstance:   "very-long-resource-name-with-many-segments",
				LabelAppComponent:  "gateway-proxy-component",
				LabelAppPartOf:     AppNameMultigres,
				LabelAppManagedBy:  ManagedByMultigres,
				LabelMultigresCell: "cell-production-region-1",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := BuildStandardLabels(tc.resourceName, tc.componentName, tc.cellName)
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
		"both maps populated - standard labels should win on conflicts": {
			standardLabels: map[string]string{
				LabelAppName:      AppNameMultigres,
				LabelAppInstance:  "my-resource",
				LabelAppComponent: "etcd",
				LabelAppManagedBy: ManagedByMultigres,
			},
			customLabels: map[string]string{
				"app.kubernetes.io/name": "user-app", // conflict: standard should win
				"custom-label-1":         "value1",
				"custom-label-2":         "value2",
			},
			want: map[string]string{
				LabelAppName:      AppNameMultigres, // Standard wins
				LabelAppInstance:  "my-resource",
				LabelAppComponent: "etcd",
				LabelAppManagedBy: ManagedByMultigres,
				"custom-label-1":  "value1",
				"custom-label-2":  "value2",
			},
		},
		"standardLabels nil, customLabels populated": {
			standardLabels: nil,
			customLabels: map[string]string{
				"custom-label-1": "value1",
				"custom-label-2": "value2",
			},
			want: map[string]string{
				"custom-label-1": "value1",
				"custom-label-2": "value2",
			},
		},
		"customLabels nil, standardLabels populated": {
			standardLabels: map[string]string{
				LabelAppName:      AppNameMultigres,
				LabelAppInstance:  "my-resource",
				LabelAppComponent: "gateway",
			},
			customLabels: nil,
			want: map[string]string{
				LabelAppName:      AppNameMultigres,
				LabelAppInstance:  "my-resource",
				LabelAppComponent: "gateway",
			},
		},
		"both maps nil": {
			standardLabels: nil,
			customLabels:   nil,
			want:           map[string]string{},
		},
		"overlapping keys - standard should override custom": {
			standardLabels: map[string]string{
				LabelAppName:      AppNameMultigres,
				LabelAppInstance:  "resource-1",
				LabelAppComponent: "orch",
			},
			customLabels: map[string]string{
				"app.kubernetes.io/instance":  "user-defined-name", // conflict: standard should win
				"app.kubernetes.io/component": "user-component",    // conflict: standard should win
				"user-label":                  "user-value",        // no conflict: should be preserved
			},
			want: map[string]string{
				LabelAppName:      AppNameMultigres,
				LabelAppInstance:  "resource-1", // Standard wins
				LabelAppComponent: "orch",       // Standard wins
				"user-label":      "user-value", // Custom preserved
			},
		},
		"custom labels with keys not in standard - should be preserved": {
			standardLabels: map[string]string{
				LabelAppName:      AppNameMultigres,
				LabelAppManagedBy: ManagedByMultigres,
			},
			customLabels: map[string]string{
				"env":             "production",
				"team":            "platform",
				"version":         "v1.2.3",
				"app.custom.io/x": "custom-value",
			},
			want: map[string]string{
				LabelAppName:      AppNameMultigres,
				LabelAppManagedBy: ManagedByMultigres,
				"env":             "production",
				"team":            "platform",
				"version":         "v1.2.3",
				"app.custom.io/x": "custom-value",
			},
		},
		"empty standard labels map": {
			standardLabels: map[string]string{},
			customLabels: map[string]string{
				"custom-label": "value",
			},
			want: map[string]string{
				"custom-label": "value",
			},
		},
		"empty custom labels map": {
			standardLabels: map[string]string{
				LabelAppName: AppNameMultigres,
			},
			customLabels: map[string]string{},
			want: map[string]string{
				LabelAppName: AppNameMultigres,
			},
		},
		"both maps empty": {
			standardLabels: map[string]string{},
			customLabels:   map[string]string{},
			want:           map[string]string{},
		},
		"all standard labels can override custom labels": {
			standardLabels: map[string]string{
				LabelAppName:       AppNameMultigres,
				LabelAppInstance:   "standard-instance",
				LabelAppComponent:  "standard-component",
				LabelAppManagedBy:  ManagedByMultigres,
				LabelMultigresCell: "standard-cell",
			},
			customLabels: map[string]string{
				"app.kubernetes.io/name":       "custom-app",       // conflict: standard wins
				"app.kubernetes.io/instance":   "custom-instance",  // conflict: standard wins
				"app.kubernetes.io/component":  "custom-component", // conflict: standard wins
				"app.kubernetes.io/managed-by": "custom-operator",  // conflict: standard wins
				"multigres.com/cell":           "custom-cell",      // conflict: standard wins
			},
			want: map[string]string{
				LabelAppName:       AppNameMultigres, // All standard values win
				LabelAppInstance:   "standard-instance",
				LabelAppComponent:  "standard-component",
				LabelAppManagedBy:  ManagedByMultigres,
				LabelMultigresCell: "standard-cell",
			},
		},
		"complex scenario with many labels": {
			standardLabels: map[string]string{
				LabelAppName:      AppNameMultigres,
				LabelAppInstance:  "my-cluster",
				LabelAppComponent: "pooler",
				LabelAppManagedBy: ManagedByMultigres,
			},
			customLabels: map[string]string{
				"app.kubernetes.io/component": "user-override", // conflict: standard should win
				"app.custom.io/name":          "custom-app",
				"kubernetes.io/name":          "k8s-name",
				"monitoring":                  "enabled",
				"backup":                      "true",
				"tier":                        "critical",
				"owner":                       "team-database",
				"cost-center":                 "engineering",
			},
			want: map[string]string{
				LabelAppName:         AppNameMultigres,
				LabelAppInstance:     "my-cluster",
				LabelAppComponent:    "pooler", // Standard wins
				LabelAppManagedBy:    ManagedByMultigres,
				"monitoring":         "enabled",
				"backup":             "true",
				"tier":               "critical",
				"owner":              "team-database",
				"cost-center":        "engineering",
				"app.custom.io/name": "custom-app",
				"kubernetes.io/name": "k8s-name",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := MergeLabels(tc.standardLabels, tc.customLabels)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("MergeLabels() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

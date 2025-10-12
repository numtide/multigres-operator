package metadata_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/metadata"
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
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "my-etcd-cluster",
				"app.kubernetes.io/component":  "etcd",
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"multigres.com/cell":           "cell-1",
			},
		},
		"empty cellName uses default": {
			resourceName:  "my-gateway",
			componentName: "gateway",
			cellName:      "",
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "my-gateway",
				"app.kubernetes.io/component":  "gateway",
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"multigres.com/cell":           "multigres-global-topo",
			},
		},
		"with cellName should add cell label": {
			resourceName:  "my-orch",
			componentName: "orch",
			cellName:      "cell-alpha",
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "my-orch",
				"app.kubernetes.io/component":  "orch",
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"multigres.com/cell":           "cell-alpha",
			},
		},
		"empty resourceName": {
			resourceName:  "",
			componentName: "pooler",
			cellName:      "cell-2",
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "",
				"app.kubernetes.io/component":  "pooler",
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"multigres.com/cell":           "cell-2",
			},
		},
		"empty componentName": {
			resourceName:  "my-resource",
			componentName: "",
			cellName:      "cell-3",
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "my-resource",
				"app.kubernetes.io/component":  "",
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"multigres.com/cell":           "cell-3",
			},
		},
		"all empty strings - uses default cell": {
			resourceName:  "",
			componentName: "",
			cellName:      "",
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "",
				"app.kubernetes.io/component":  "",
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"multigres.com/cell":           "multigres-global-topo",
			},
		},
		"special characters in names": {
			resourceName:  "my-resource-123",
			componentName: "etcd-v3",
			cellName:      "cell-prod-1",
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "my-resource-123",
				"app.kubernetes.io/component":  "etcd-v3",
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"multigres.com/cell":           "cell-prod-1",
			},
		},
		"long names": {
			resourceName:  "very-long-resource-name-with-many-segments",
			componentName: "gateway-proxy-component",
			cellName:      "cell-production-region-1",
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "very-long-resource-name-with-many-segments",
				"app.kubernetes.io/component":  "gateway-proxy-component",
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"multigres.com/cell":           "cell-production-region-1",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := metadata.BuildStandardLabels(tc.resourceName, tc.componentName, tc.cellName)
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
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "my-resource",
				"app.kubernetes.io/component":  "etcd",
				"app.kubernetes.io/managed-by": "multigres-operator",
			},
			customLabels: map[string]string{
				"app.kubernetes.io/name": "user-app", // conflict: standard should win
				"custom-label-1":         "value1",
				"custom-label-2":         "value2",
			},
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres", // Standard wins
				"app.kubernetes.io/instance":   "my-resource",
				"app.kubernetes.io/component":  "etcd",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"custom-label-1":               "value1",
				"custom-label-2":               "value2",
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
				"app.kubernetes.io/name":      "multigres",
				"app.kubernetes.io/instance":  "my-resource",
				"app.kubernetes.io/component": "gateway",
			},
			customLabels: nil,
			want: map[string]string{
				"app.kubernetes.io/name":      "multigres",
				"app.kubernetes.io/instance":  "my-resource",
				"app.kubernetes.io/component": "gateway",
			},
		},
		"both maps nil": {
			standardLabels: nil,
			customLabels:   nil,
			want:           map[string]string{},
		},
		"overlapping keys - standard should override custom": {
			standardLabels: map[string]string{
				"app.kubernetes.io/name":      "multigres",
				"app.kubernetes.io/instance":  "resource-1",
				"app.kubernetes.io/component": "orch",
			},
			customLabels: map[string]string{
				"app.kubernetes.io/instance":  "user-defined-name", // conflict: standard should win
				"app.kubernetes.io/component": "user-component",    // conflict: standard should win
				"user-label":                  "user-value",        // no conflict: should be preserved
			},
			want: map[string]string{
				"app.kubernetes.io/name":      "multigres",
				"app.kubernetes.io/instance":  "resource-1", // Standard wins
				"app.kubernetes.io/component": "orch",       // Standard wins
				"user-label":                  "user-value", // Custom preserved
			},
		},
		"custom labels with keys not in standard - should be preserved": {
			standardLabels: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
			},
			customLabels: map[string]string{
				"env":             "production",
				"team":            "platform",
				"version":         "v1.2.3",
				"app.custom.io/x": "custom-value",
			},
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"app.custom.io/x":              "custom-value",
				"env":                          "production",
				"team":                         "platform",
				"version":                      "v1.2.3",
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
				"app.kubernetes.io/name": "multigres",
			},
			customLabels: map[string]string{},
			want: map[string]string{
				"app.kubernetes.io/name": "multigres",
			},
		},
		"both maps empty": {
			standardLabels: map[string]string{},
			customLabels:   map[string]string{},
			want:           map[string]string{},
		},
		"all standard labels can override custom labels": {
			standardLabels: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "standard-instance",
				"app.kubernetes.io/component":  "standard-component",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"multigres.com/cell":           "standard-cell",
			},
			customLabels: map[string]string{
				"app.kubernetes.io/name":       "custom-app",       // conflict: standard wins
				"app.kubernetes.io/instance":   "custom-instance",  // conflict: standard wins
				"app.kubernetes.io/component":  "custom-component", // conflict: standard wins
				"app.kubernetes.io/managed-by": "custom-operator",  // conflict: standard wins
				"multigres.com/cell":           "custom-cell",      // conflict: standard wins
			},
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres", // All standard values win
				"app.kubernetes.io/instance":   "standard-instance",
				"app.kubernetes.io/component":  "standard-component",
				"app.kubernetes.io/managed-by": "multigres-operator",
				"multigres.com/cell":           "standard-cell",
			},
		},
		"part-of label conflict - standard wins": {
			standardLabels: map[string]string{
				"app.kubernetes.io/name":    "multigres",
				"app.kubernetes.io/part-of": "multigres",
			},
			customLabels: map[string]string{
				"app.kubernetes.io/part-of": "custom-app", // conflict: standard should win
				"custom-label":              "value",
			},
			want: map[string]string{
				"app.kubernetes.io/name":    "multigres",
				"app.kubernetes.io/part-of": "multigres", // Standard wins
				"custom-label":              "value",
			},
		},
		"complex scenario with many labels": {
			standardLabels: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "my-cluster",
				"app.kubernetes.io/component":  "pooler",
				"app.kubernetes.io/part-of":    "multigres",
				"app.kubernetes.io/managed-by": "multigres-operator",
			},
			customLabels: map[string]string{
				"app.kubernetes.io/component": "user-override", // conflict: standard should win
				"app.kubernetes.io/part-of":   "custom-part",   // conflict: standard should win
				"app.custom.io/name":          "custom-app",
				"kubernetes.io/name":          "k8s-name",
				"monitoring":                  "enabled",
				"backup":                      "true",
				"tier":                        "critical",
				"owner":                       "team-database",
				"cost-center":                 "engineering",
			},
			want: map[string]string{
				"app.kubernetes.io/name":       "multigres",
				"app.kubernetes.io/instance":   "my-cluster",
				"app.kubernetes.io/component":  "pooler",    // Standard wins
				"app.kubernetes.io/part-of":    "multigres", // Standard wins
				"app.kubernetes.io/managed-by": "multigres-operator",
				"app.custom.io/name":           "custom-app",
				"kubernetes.io/name":           "k8s-name",
				"monitoring":                   "enabled",
				"backup":                       "true",
				"tier":                         "critical",
				"owner":                        "team-database",
				"cost-center":                  "engineering",
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

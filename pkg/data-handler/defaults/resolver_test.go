package defaults

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// setupFixtures helper returns a fresh set of test objects.
func setupFixtures() (
	*multigresv1alpha1.CoreTemplate,
	*multigresv1alpha1.CellTemplate,
	*multigresv1alpha1.ShardTemplate,
	string,
) {
	namespace := "default"

	coreTpl := &multigresv1alpha1.CoreTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: namespace},
		Spec: multigresv1alpha1.CoreTemplateSpec{
			GlobalTopoServer: &multigresv1alpha1.TopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{Image: "core-default"},
			},
		},
	}

	cellTpl := &multigresv1alpha1.CellTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: namespace},
		Spec: multigresv1alpha1.CellTemplateSpec{
			MultiGateway: &multigresv1alpha1.StatelessSpec{
				Replicas: ptr.To(int32(1)),
			},
		},
	}

	shardTpl := &multigresv1alpha1.ShardTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: namespace},
		Spec: multigresv1alpha1.ShardTemplateSpec{
			MultiOrch: &multigresv1alpha1.MultiOrchSpec{
				StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
			},
		},
	}

	return coreTpl, cellTpl, shardTpl, namespace
}

func TestNewResolver(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().Build()
	defaults := multigresv1alpha1.TemplateDefaults{CoreTemplate: "foo"}
	r := NewResolver(c, "ns", defaults)

	if r.Client != c {
		t.Error("Client not set correctly")
	}
	if r.Namespace != "ns" {
		t.Error("Namespace not set correctly")
	}
	if r.TemplateDefaults.CoreTemplate != "foo" {
		t.Error("Defaults not set correctly")
	}
}

func TestResolver_ResolveTemplates(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	coreTpl, _, _, ns := setupFixtures()

	// Create a custom template to test explicit resolution
	customCore := coreTpl.DeepCopy()
	customCore.Name = "custom-core"

	tests := map[string]struct {
		existingObjects []client.Object
		defaults        multigresv1alpha1.TemplateDefaults
		// Test inputs (one is chosen based on test type logic below)
		reqName string
		// Expectations
		wantErr     bool
		errContains string
		wantFound   bool
		wantResName string // For CoreTemplate check
	}{
		"Core: Explicit Found": {
			existingObjects: []client.Object{customCore},
			reqName:         "custom-core",
			wantFound:       true,
			wantResName:     "custom-core",
		},
		"Core: Explicit Not Found (Error)": {
			existingObjects: []client.Object{},
			reqName:         "missing-core",
			wantErr:         true,
			errContains:     "referenced CoreTemplate 'missing-core' not found",
		},
		"Core: Default from Config Found": {
			existingObjects: []client.Object{customCore},
			defaults:        multigresv1alpha1.TemplateDefaults{CoreTemplate: "custom-core"},
			reqName:         "", // Empty implies use default
			wantFound:       true,
			wantResName:     "custom-core",
		},
		"Core: Default from Config Not Found (Error)": {
			existingObjects: []client.Object{},
			defaults:        multigresv1alpha1.TemplateDefaults{CoreTemplate: "missing"},
			reqName:         "",
			wantErr:         true,
			errContains:     "referenced CoreTemplate 'missing' not found",
		},
		"Core: Implicit Fallback Found": {
			existingObjects: []client.Object{coreTpl}, // Name is "default"
			defaults:        multigresv1alpha1.TemplateDefaults{},
			reqName:         "",
			wantFound:       true,
			wantResName:     "default",
		},
		"Core: Implicit Fallback Not Found (Safe Empty Return)": {
			existingObjects: []client.Object{}, // "default" does not exist
			defaults:        multigresv1alpha1.TemplateDefaults{},
			reqName:         "",
			wantFound:       false, // Should return empty object, no error
			wantErr:         false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingObjects...).
				Build()
			r := NewResolver(c, ns, tc.defaults)

			// We use CoreTemplate for the main logic drive
			res, err := r.ResolveCoreTemplate(context.Background(), tc.reqName)

			if tc.wantErr {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if tc.errContains != "" && err.Error() != tc.errContains {
					t.Errorf(
						"Error message mismatch. Want substring %q, got %q",
						tc.errContains,
						err.Error(),
					)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// For Implicit Fallback Not Found, we expect an empty pointer/struct that isn't nil, but empty content
			if !tc.wantFound {
				if res == nil {
					t.Fatal(
						"Expected non-nil result structure even for not-found implicit fallback",
					)
				}
				if res.Name != "" {
					t.Errorf("Expected empty result, got object with name %q", res.Name)
				}
				return
			}

			if res.Name != tc.wantResName {
				t.Errorf("Result name mismatch. Want %q, got %q", tc.wantResName, res.Name)
			}
		})
	}
}

// TestResolver_Types_Coverage ensures ResolveCellTemplate and ResolveShardTemplate
// work correctly, covering the "Implicit Not Found" paths which were missing coverage.
func TestResolver_Types_Coverage(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	_, cellTpl, shardTpl, ns := setupFixtures()

	// Case 1: Templates EXIST (Happy Path)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cellTpl, shardTpl).Build()
	r := NewResolver(c, ns, multigresv1alpha1.TemplateDefaults{})

	// Cell Found
	cell, err := r.ResolveCellTemplate(context.Background(), "default")
	if err != nil || cell.Name != "default" {
		t.Errorf("ResolveCellTemplate explicit found failed")
	}
	// Shard Found
	shard, err := r.ResolveShardTemplate(context.Background(), "default")
	if err != nil || shard.Name != "default" {
		t.Errorf("ResolveShardTemplate explicit found failed")
	}

	// Case 2: Templates DO NOT EXIST + Implicit Request (Implicit Fallback Logic)
	// This covers the: if errors.IsNotFound { if isImplicitFallback { return empty } } block
	cEmpty := fake.NewClientBuilder().WithScheme(scheme).Build() // No objects
	rEmpty := NewResolver(cEmpty, ns, multigresv1alpha1.TemplateDefaults{})

	// Cell Implicit Not Found
	cellEmpty, err := rEmpty.ResolveCellTemplate(context.Background(), "") // Empty name = implicit
	if err != nil {
		t.Errorf("ResolveCellTemplate implicit fallback failed: %v", err)
	}
	if cellEmpty == nil || cellEmpty.Name != "" {
		t.Error("ResolveCellTemplate expected empty struct for implicit missing")
	}

	// Shard Implicit Not Found
	shardEmpty, err := rEmpty.ResolveShardTemplate(
		context.Background(),
		"",
	) // Empty name = implicit
	if err != nil {
		t.Errorf("ResolveShardTemplate implicit fallback failed: %v", err)
	}
	if shardEmpty == nil || shardEmpty.Name != "" {
		t.Error("ResolveShardTemplate expected empty struct for implicit missing")
	}
}

// mockClient is a partial implementation of client.Client to force errors
type mockClient struct {
	client.Client
	failGet bool
}

func (m *mockClient) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	if m.failGet {
		return errors.New("simulated database connection error")
	}
	return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
}

func TestResolver_ClientErrors(t *testing.T) {
	t.Parallel()

	mc := &mockClient{failGet: true}
	r := NewResolver(mc, "default", multigresv1alpha1.TemplateDefaults{})
	ctx := context.Background()

	_, err := r.ResolveCoreTemplate(ctx, "any")
	if err == nil ||
		err.Error() != "failed to get CoreTemplate: simulated database connection error" {
		t.Errorf("Core error mismatch: %v", err)
	}

	_, err = r.ResolveCellTemplate(ctx, "any")
	if err == nil ||
		err.Error() != "failed to get CellTemplate: simulated database connection error" {
		t.Errorf("Cell error mismatch: %v", err)
	}

	_, err = r.ResolveShardTemplate(ctx, "any")
	if err == nil ||
		err.Error() != "failed to get ShardTemplate: simulated database connection error" {
		t.Errorf("Shard error mismatch: %v", err)
	}
}

func TestMergeCellConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		tpl       *multigresv1alpha1.CellTemplate
		overrides *multigresv1alpha1.CellOverrides
		inline    *multigresv1alpha1.CellInlineSpec
		wantGw    multigresv1alpha1.StatelessSpec
		wantTopo  *multigresv1alpha1.LocalTopoServerSpec
	}{
		"Full Merge": {
			tpl: &multigresv1alpha1.CellTemplate{
				Spec: multigresv1alpha1.CellTemplateSpec{
					MultiGateway: &multigresv1alpha1.StatelessSpec{
						Replicas:       ptr.To(int32(1)),
						PodAnnotations: map[string]string{"foo": "bar"},
					},
					LocalTopoServer: &multigresv1alpha1.LocalTopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{Image: "base"},
					},
				},
			},
			overrides: &multigresv1alpha1.CellOverrides{
				MultiGateway: &multigresv1alpha1.StatelessSpec{
					Replicas:       ptr.To(int32(2)),
					PodAnnotations: map[string]string{"baz": "qux"},
				},
			},
			wantGw: multigresv1alpha1.StatelessSpec{
				Replicas:       ptr.To(int32(2)),
				PodAnnotations: map[string]string{"foo": "bar", "baz": "qux"},
			},
			wantTopo: &multigresv1alpha1.LocalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{Image: "base"},
			},
		},
		"Template Only (Nil Overrides)": {
			// CRITICAL: Covers the 'false' branch of 'if overrides != nil'
			tpl: &multigresv1alpha1.CellTemplate{
				Spec: multigresv1alpha1.CellTemplateSpec{
					MultiGateway: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
				},
			},
			overrides: nil,
			wantGw:    multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
		},
		"Preserve Base (Empty Override)": {
			tpl: &multigresv1alpha1.CellTemplate{
				Spec: multigresv1alpha1.CellTemplateSpec{
					MultiGateway: &multigresv1alpha1.StatelessSpec{
						Replicas:       ptr.To(int32(1)),
						PodAnnotations: map[string]string{"foo": "bar"},
					},
				},
			},
			overrides: &multigresv1alpha1.CellOverrides{
				MultiGateway: &multigresv1alpha1.StatelessSpec{}, // Empty struct!
			},
			wantGw: multigresv1alpha1.StatelessSpec{
				Replicas:       ptr.To(int32(1)), // Should preserve base
				PodAnnotations: map[string]string{"foo": "bar"},
			},
		},
		"Map Init (Nil Base)": {
			tpl: &multigresv1alpha1.CellTemplate{
				Spec: multigresv1alpha1.CellTemplateSpec{
					MultiGateway: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
				},
			},
			overrides: &multigresv1alpha1.CellOverrides{
				MultiGateway: &multigresv1alpha1.StatelessSpec{
					PodAnnotations: map[string]string{"a": "b"},
					PodLabels:      map[string]string{"c": "d"},
				},
			},
			wantGw: multigresv1alpha1.StatelessSpec{
				Replicas:       ptr.To(int32(1)),
				PodAnnotations: map[string]string{"a": "b"},
				PodLabels:      map[string]string{"c": "d"},
			},
		},
		"Inline Priority": {
			tpl: &multigresv1alpha1.CellTemplate{
				Spec: multigresv1alpha1.CellTemplateSpec{
					MultiGateway: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
				},
			},
			inline: &multigresv1alpha1.CellInlineSpec{
				MultiGateway: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(99))},
			},
			wantGw: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(99))},
		},
		"Nil Template (Override Only)": {
			tpl: nil,
			overrides: &multigresv1alpha1.CellOverrides{
				MultiGateway: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(2))},
			},
			wantGw: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(2))},
		},
		"Nil Everything": {
			tpl:      nil,
			wantGw:   multigresv1alpha1.StatelessSpec{},
			wantTopo: nil,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			gw, topo := MergeCellConfig(tc.tpl, tc.overrides, tc.inline)

			if diff := cmp.Diff(tc.wantGw, gw, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
				t.Errorf("Gateway mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantTopo, topo); diff != "" {
				t.Errorf("Topo mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMergeShardConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		tpl       *multigresv1alpha1.ShardTemplate
		overrides *multigresv1alpha1.ShardOverrides
		inline    *multigresv1alpha1.ShardInlineSpec
		wantOrch  multigresv1alpha1.MultiOrchSpec
		wantPools map[string]multigresv1alpha1.PoolSpec
	}{
		"Full Merge": {
			tpl: &multigresv1alpha1.ShardTemplate{
				Spec: multigresv1alpha1.ShardTemplateSpec{
					MultiOrch: &multigresv1alpha1.MultiOrchSpec{
						StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
						Cells:         []multigresv1alpha1.CellName{"a"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"p1": {Type: "read"},
					},
				},
			},
			overrides: &multigresv1alpha1.ShardOverrides{
				MultiOrch: &multigresv1alpha1.MultiOrchSpec{
					Cells: []multigresv1alpha1.CellName{"b"},
				},
				Pools: map[string]multigresv1alpha1.PoolSpec{
					"p1": {Type: "write"},
					"p2": {Type: "internal"},
				},
			},
			wantOrch: multigresv1alpha1.MultiOrchSpec{
				StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
				Cells:         []multigresv1alpha1.CellName{"b"},
			},
			wantPools: map[string]multigresv1alpha1.PoolSpec{
				"p1": {Type: "write"},
				"p2": {Type: "internal"},
			},
		},
		"Template Only (Nil Overrides)": {
			// CRITICAL: Covers the 'false' branch of 'if overrides != nil'
			tpl: &multigresv1alpha1.ShardTemplate{
				Spec: multigresv1alpha1.ShardTemplateSpec{
					MultiOrch: &multigresv1alpha1.MultiOrchSpec{
						StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
					},
				},
			},
			overrides: nil,
			wantOrch: multigresv1alpha1.MultiOrchSpec{
				StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
			},
			wantPools: map[string]multigresv1alpha1.PoolSpec{},
		},
		"Pool Deep Merge": {
			tpl: &multigresv1alpha1.ShardTemplate{
				Spec: multigresv1alpha1.ShardTemplateSpec{
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"p1": {Type: "read"},
					},
				},
			},
			overrides: &multigresv1alpha1.ShardOverrides{
				Pools: map[string]multigresv1alpha1.PoolSpec{
					"p1": {
						Type:            "write",
						Cells:           []multigresv1alpha1.CellName{"zone-a"},
						ReplicasPerCell: ptr.To(int32(5)),
						Storage:         multigresv1alpha1.StorageSpec{Size: "10Gi"},
						Postgres: multigresv1alpha1.ContainerConfig{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{corev1.ResourceCPU: parseQty("1")},
							},
						},
						Multipooler: multigresv1alpha1.ContainerConfig{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{corev1.ResourceCPU: parseQty("1")},
							},
						},
						Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}},
					},
				},
			},
			wantOrch: multigresv1alpha1.MultiOrchSpec{},
			wantPools: map[string]multigresv1alpha1.PoolSpec{
				"p1": {
					Type:            "write",
					Cells:           []multigresv1alpha1.CellName{"zone-a"},
					ReplicasPerCell: ptr.To(int32(5)),
					Storage:         multigresv1alpha1.StorageSpec{Size: "10Gi"},
					Postgres: multigresv1alpha1.ContainerConfig{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: parseQty("1")},
						},
					},
					Multipooler: multigresv1alpha1.ContainerConfig{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: parseQty("1")},
						},
					},
					Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}},
				},
			},
		},
		"Preserve Base Pool (Empty Override)": {
			tpl: &multigresv1alpha1.ShardTemplate{
				Spec: multigresv1alpha1.ShardTemplateSpec{
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"p1": {Type: "read", ReplicasPerCell: ptr.To(int32(1))},
					},
				},
			},
			overrides: &multigresv1alpha1.ShardOverrides{
				Pools: map[string]multigresv1alpha1.PoolSpec{
					"p1": {}, // Empty override
				},
			},
			wantOrch: multigresv1alpha1.MultiOrchSpec{},
			wantPools: map[string]multigresv1alpha1.PoolSpec{
				"p1": {Type: "read", ReplicasPerCell: ptr.To(int32(1))},
			},
		},
		"Inline Priority": {
			tpl: &multigresv1alpha1.ShardTemplate{
				Spec: multigresv1alpha1.ShardTemplateSpec{
					MultiOrch: &multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"a"},
					},
				},
			},
			inline: &multigresv1alpha1.ShardInlineSpec{
				MultiOrch: multigresv1alpha1.MultiOrchSpec{
					Cells: []multigresv1alpha1.CellName{"inline"},
				},
			},
			wantOrch: multigresv1alpha1.MultiOrchSpec{
				Cells: []multigresv1alpha1.CellName{"inline"},
			},
		},
		"Nil Template": {
			tpl: nil,
			overrides: &multigresv1alpha1.ShardOverrides{
				MultiOrch: &multigresv1alpha1.MultiOrchSpec{
					Cells: []multigresv1alpha1.CellName{"b"},
				},
			},
			wantOrch:  multigresv1alpha1.MultiOrchSpec{Cells: []multigresv1alpha1.CellName{"b"}},
			wantPools: map[string]multigresv1alpha1.PoolSpec{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			orch, pools := MergeShardConfig(tc.tpl, tc.overrides, tc.inline)

			if diff := cmp.Diff(tc.wantOrch, orch, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
				t.Errorf("Orch mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantPools, pools, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
				t.Errorf("Pools mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResolveGlobalTopo(t *testing.T) {
	t.Parallel()

	// 1. Inline Priority (Etcd)
	spec := &multigresv1alpha1.GlobalTopoServerSpec{
		Etcd: &multigresv1alpha1.EtcdSpec{Image: "inline"},
	}
	res := ResolveGlobalTopo(spec, nil)
	if res.Etcd.Image != "inline" {
		t.Error("Expected inline spec priority")
	}

	// 2. Inline Priority (External) - Covers the || spec.External check
	spec = &multigresv1alpha1.GlobalTopoServerSpec{
		External: &multigresv1alpha1.ExternalTopoServerSpec{
			Endpoints: []multigresv1alpha1.EndpointUrl{"http://foo"},
		},
	}
	res = ResolveGlobalTopo(spec, nil)
	if res.External == nil {
		t.Error("Expected inline external priority")
	}

	// 3. Template Fallback
	spec = &multigresv1alpha1.GlobalTopoServerSpec{}
	tpl := &multigresv1alpha1.CoreTemplate{
		Spec: multigresv1alpha1.CoreTemplateSpec{
			GlobalTopoServer: &multigresv1alpha1.TopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{Image: "template"},
			},
		},
	}
	res = ResolveGlobalTopo(spec, tpl)
	if res.Etcd.Image != "template" {
		t.Error("Expected template fallback")
	}

	// 4. Template Found but Nil Content (Covers coreTemplate.Spec.GlobalTopoServer != nil check)
	tplEmpty := &multigresv1alpha1.CoreTemplate{
		Spec: multigresv1alpha1.CoreTemplateSpec{
			GlobalTopoServer: nil, // This is explicitly nil
		},
	}
	res = ResolveGlobalTopo(spec, tplEmpty)
	if res.Etcd != nil {
		t.Error("Expected nil result when template content is nil")
	}

	// 5. No-op (Nil Template)
	res = ResolveGlobalTopo(spec, nil)
	if res.Etcd != nil {
		t.Error("Expected nil result for empty inputs")
	}
}

func TestResolveMultiAdmin(t *testing.T) {
	t.Parallel()

	// 1. Inline Priority
	spec := &multigresv1alpha1.MultiAdminConfig{
		Spec: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(5))},
	}
	res := ResolveMultiAdmin(spec, nil)
	if *res.Replicas != 5 {
		t.Error("Expected inline spec priority")
	}

	// 2. Template Fallback
	spec = &multigresv1alpha1.MultiAdminConfig{}
	tpl := &multigresv1alpha1.CoreTemplate{
		Spec: multigresv1alpha1.CoreTemplateSpec{
			MultiAdmin: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(3))},
		},
	}
	res = ResolveMultiAdmin(spec, tpl)
	if *res.Replicas != 3 {
		t.Error("Expected template fallback")
	}

	// 3. Template Found but Nil Content (Covers coreTemplate.Spec.MultiAdmin != nil check)
	tplEmpty := &multigresv1alpha1.CoreTemplate{
		Spec: multigresv1alpha1.CoreTemplateSpec{
			MultiAdmin: nil, // This is explicitly nil
		},
	}
	res = ResolveMultiAdmin(spec, tplEmpty)
	if res != nil {
		t.Error("Expected nil result when template content is nil")
	}

	// 4. No-op (Nil Template)
	res = ResolveMultiAdmin(spec, nil)
	if res != nil {
		t.Error("Expected nil result for empty inputs")
	}
}

func parseQty(s string) resource.Quantity {
	return resource.MustParse(s)
}

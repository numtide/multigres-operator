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
func setupFixtures(t testing.TB) (
	*multigresv1alpha1.CoreTemplate,
	*multigresv1alpha1.CellTemplate,
	*multigresv1alpha1.ShardTemplate,
	string,
) {
	t.Helper()
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

	if got, want := r.Client, c; got != want {
		t.Errorf("Client mismatch: got %v, want %v", got, want)
	}
	if got, want := r.Namespace, "ns"; got != want {
		t.Errorf("Namespace mismatch: got %q, want %q", got, want)
	}
	if got, want := r.TemplateDefaults.CoreTemplate, "foo"; got != want {
		t.Errorf("Defaults mismatch: got %q, want %q", got, want)
	}
}

func TestResolver_ResolveTemplates(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	coreTpl, cellTpl, shardTpl, ns := setupFixtures(t)

	// Create custom templates to test explicit resolution
	customCore := coreTpl.DeepCopy()
	customCore.Name = "custom-core"

	customCell := cellTpl.DeepCopy()
	customCell.Name = "custom-cell"

	customShard := shardTpl.DeepCopy()
	customShard.Name = "custom-shard"

	tests := map[string]struct {
		existingObjects []client.Object
		defaults        multigresv1alpha1.TemplateDefaults
		reqName         string
		wantErr         bool
		errContains     string
		wantFound       bool
		wantResName     string
		resolveFunc     func(*Resolver, string) (client.Object, error)
	}{
		// ------------------------------------------------------------------
		// CoreTemplate Tests
		// ------------------------------------------------------------------
		"Core: Explicit Found": {
			existingObjects: []client.Object{customCore},
			reqName:         "custom-core",
			wantFound:       true,
			wantResName:     "custom-core",
			resolveFunc: func(r *Resolver, name string) (client.Object, error) {
				return r.ResolveCoreTemplate(t.Context(), name)
			},
		},
		"Core: Explicit Not Found (Error)": {
			existingObjects: []client.Object{}, // Cleared to simulate missing
			reqName:         "missing-core",
			wantErr:         true,
			errContains:     "referenced CoreTemplate 'missing-core' not found",
			resolveFunc: func(r *Resolver, name string) (client.Object, error) {
				return r.ResolveCoreTemplate(t.Context(), name)
			},
		},
		"Core: Default from Config Found": {
			existingObjects: []client.Object{customCore},
			defaults:        multigresv1alpha1.TemplateDefaults{CoreTemplate: "custom-core"},
			reqName:         "",
			wantFound:       true,
			wantResName:     "custom-core",
			resolveFunc: func(r *Resolver, name string) (client.Object, error) {
				return r.ResolveCoreTemplate(t.Context(), name)
			},
		},
		"Core: Default from Config Not Found (Error)": {
			existingObjects: []client.Object{},
			defaults:        multigresv1alpha1.TemplateDefaults{CoreTemplate: "missing"},
			reqName:         "",
			wantErr:         true,
			errContains:     "referenced CoreTemplate 'missing' not found",
			resolveFunc: func(r *Resolver, name string) (client.Object, error) {
				return r.ResolveCoreTemplate(t.Context(), name)
			},
		},
		"Core: Implicit Fallback Found": {
			// existingObjects defaults to standard set (containing "default" coreTpl)
			defaults:    multigresv1alpha1.TemplateDefaults{},
			reqName:     "",
			wantFound:   true,
			wantResName: "default",
			resolveFunc: func(r *Resolver, name string) (client.Object, error) {
				return r.ResolveCoreTemplate(t.Context(), name)
			},
		},
		"Core: Implicit Fallback Not Found (Safe Empty Return)": {
			existingObjects: []client.Object{},
			defaults:        multigresv1alpha1.TemplateDefaults{},
			reqName:         "",
			wantFound:       false,
			wantErr:         false,
			resolveFunc: func(r *Resolver, name string) (client.Object, error) {
				return r.ResolveCoreTemplate(t.Context(), name)
			},
		},

		// ------------------------------------------------------------------
		// CellTemplate Tests
		// ------------------------------------------------------------------
		"Cell: Explicit Found": {
			existingObjects: []client.Object{customCell},
			reqName:         "custom-cell",
			wantFound:       true,
			wantResName:     "custom-cell",
			resolveFunc: func(r *Resolver, name string) (client.Object, error) {
				return r.ResolveCellTemplate(t.Context(), name)
			},
		},
		"Cell: Explicit Not Found (Error)": {
			existingObjects: []client.Object{},
			reqName:         "missing-cell",
			wantErr:         true,
			errContains:     "referenced CellTemplate 'missing-cell' not found",
			resolveFunc: func(r *Resolver, name string) (client.Object, error) {
				return r.ResolveCellTemplate(t.Context(), name)
			},
		},
		"Cell: Implicit Fallback Found": {
			// existingObjects defaults to standard set (containing "default" cellTpl)
			defaults:    multigresv1alpha1.TemplateDefaults{},
			reqName:     "",
			wantFound:   true,
			wantResName: "default",
			resolveFunc: func(r *Resolver, name string) (client.Object, error) {
				return r.ResolveCellTemplate(t.Context(), name)
			},
		},
		"Cell: Implicit Fallback Not Found (Safe Empty Return)": {
			existingObjects: []client.Object{},
			defaults:        multigresv1alpha1.TemplateDefaults{},
			reqName:         "",
			wantFound:       false,
			wantErr:         false,
			resolveFunc: func(r *Resolver, name string) (client.Object, error) {
				return r.ResolveCellTemplate(t.Context(), name)
			},
		},

		// ------------------------------------------------------------------
		// ShardTemplate Tests
		// ------------------------------------------------------------------
		"Shard: Explicit Found": {
			existingObjects: []client.Object{customShard},
			reqName:         "custom-shard",
			wantFound:       true,
			wantResName:     "custom-shard",
			resolveFunc: func(r *Resolver, name string) (client.Object, error) {
				return r.ResolveShardTemplate(t.Context(), name)
			},
		},
		"Shard: Explicit Not Found (Error)": {
			existingObjects: []client.Object{},
			reqName:         "missing-shard",
			wantErr:         true,
			errContains:     "referenced ShardTemplate 'missing-shard' not found",
			resolveFunc: func(r *Resolver, name string) (client.Object, error) {
				return r.ResolveShardTemplate(t.Context(), name)
			},
		},
		"Shard: Implicit Fallback Found": {
			// existingObjects defaults to standard set (containing "default" shardTpl)
			defaults:    multigresv1alpha1.TemplateDefaults{},
			reqName:     "",
			wantFound:   true,
			wantResName: "default",
			resolveFunc: func(r *Resolver, name string) (client.Object, error) {
				return r.ResolveShardTemplate(t.Context(), name)
			},
		},
		"Shard: Implicit Fallback Not Found (Safe Empty Return)": {
			existingObjects: []client.Object{},
			defaults:        multigresv1alpha1.TemplateDefaults{},
			reqName:         "",
			wantFound:       false,
			wantErr:         false,
			resolveFunc: func(r *Resolver, name string) (client.Object, error) {
				return r.ResolveShardTemplate(t.Context(), name)
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Default existingObjects if nil
			objects := tc.existingObjects
			if objects == nil {
				objects = []client.Object{coreTpl, cellTpl, shardTpl}
			}

			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			r := NewResolver(c, ns, tc.defaults)

			res, err := tc.resolveFunc(r, tc.reqName)
			if tc.wantErr {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if tc.errContains != "" && err.Error() != tc.errContains {
					t.Errorf(
						"Error message mismatch: got %q, want substring %q",
						err.Error(),
						tc.errContains,
					)
				}
				return
			} else if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !tc.wantFound {
				if res == nil {
					t.Fatal(
						"Expected non-nil result structure even for not-found implicit fallback",
					)
				}
				if res.GetName() != "" {
					t.Errorf("Expected empty result, got object with name %q", res.GetName())
				}
				return
			}

			if got, want := res.GetName(), tc.wantResName; got != want {
				t.Errorf("Result name mismatch: got %q, want %q", got, want)
			}
		})
	}
}

// mockClient is a partial implementation of client.Client to force errors.
type mockClient struct {
	client.Client
	failGet bool
	err     error
}

func (m *mockClient) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	if m.failGet {
		return m.err
	}
	return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
}

func TestResolver_ClientErrors(t *testing.T) {
	t.Parallel()

	errSimulated := errors.New("simulated database connection error")
	mc := &mockClient{failGet: true, err: errSimulated}
	r := NewResolver(mc, "default", multigresv1alpha1.TemplateDefaults{})

	tests := map[string]struct {
		callFunc func() error
		wantMsg  string
	}{
		"ResolveCoreTemplate": {
			callFunc: func() error { _, err := r.ResolveCoreTemplate(t.Context(), "any"); return err },
			wantMsg:  "failed to get CoreTemplate: simulated database connection error",
		},
		"ResolveCellTemplate": {
			callFunc: func() error { _, err := r.ResolveCellTemplate(t.Context(), "any"); return err },
			wantMsg:  "failed to get CellTemplate: simulated database connection error",
		},
		"ResolveShardTemplate": {
			callFunc: func() error { _, err := r.ResolveShardTemplate(t.Context(), "any"); return err },
			wantMsg:  "failed to get ShardTemplate: simulated database connection error",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := tc.callFunc()
			if err == nil || err.Error() != tc.wantMsg {
				t.Errorf("Error mismatch: got %v, want %s", err, tc.wantMsg)
			}
		})
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
		"Full Merge With Resources and Affinity Overrides": {
			// Covers: Override resources/affinity present (Red lines in mergeStatelessSpec)
			tpl: &multigresv1alpha1.CellTemplate{
				Spec: multigresv1alpha1.CellTemplateSpec{
					MultiGateway: &multigresv1alpha1.StatelessSpec{
						Replicas:       ptr.To(int32(1)),
						PodAnnotations: map[string]string{"foo": "bar"},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: parseQty("100m")},
						},
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
					// Force override of Resources to trigger the if block
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: parseQty("200m")},
					},
					// Force override of Affinity to trigger the if block
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{},
					},
				},
			},
			wantGw: multigresv1alpha1.StatelessSpec{
				Replicas:       ptr.To(int32(2)),
				PodAnnotations: map[string]string{"foo": "bar", "baz": "qux"},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: parseQty("200m")},
				},
				Affinity: &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{},
				},
			},
			wantTopo: &multigresv1alpha1.LocalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{Image: "base"},
			},
		},
		"Template Only (Nil Overrides)": {
			// Covers the 'false' branch of 'if overrides != nil'
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
				MultiGateway: &multigresv1alpha1.StatelessSpec{}, // Empty struct
			},
			wantGw: multigresv1alpha1.StatelessSpec{
				Replicas:       ptr.To(int32(1)), // Should preserve base
				PodAnnotations: map[string]string{"foo": "bar"},
			},
		},
		"Map Init (Nil Base)": {
			// Covers: Loop where base map is nil (Red lines in mergeStatelessSpec)
			tpl: &multigresv1alpha1.CellTemplate{
				Spec: multigresv1alpha1.CellTemplateSpec{
					MultiGateway: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
					// PodAnnotations is nil here by default
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
			t.Parallel()
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
		"Full Merge with MultiOrch Overrides": {
			// Ensure resources/affinity are covered for MultiOrch too
			tpl: &multigresv1alpha1.ShardTemplate{
				Spec: multigresv1alpha1.ShardTemplateSpec{
					MultiOrch: &multigresv1alpha1.MultiOrchSpec{
						StatelessSpec: multigresv1alpha1.StatelessSpec{
							Replicas: ptr.To(int32(1)),
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: parseQty("1Gi"),
								},
							},
						},
						Cells: []multigresv1alpha1.CellName{"a"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"p1": {Type: "read"},
					},
				},
			},
			overrides: &multigresv1alpha1.ShardOverrides{
				MultiOrch: &multigresv1alpha1.MultiOrchSpec{
					StatelessSpec: multigresv1alpha1.StatelessSpec{
						// Override resources
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceMemory: parseQty("2Gi")},
						},
						// Override affinity
						Affinity: &corev1.Affinity{
							PodAntiAffinity: &corev1.PodAntiAffinity{},
						},
					},
					Cells: []multigresv1alpha1.CellName{"b"},
				},
				Pools: map[string]multigresv1alpha1.PoolSpec{
					"p1": {Type: "write"},
					"p2": {Type: "internal"},
				},
			},
			wantOrch: multigresv1alpha1.MultiOrchSpec{
				StatelessSpec: multigresv1alpha1.StatelessSpec{
					Replicas: ptr.To(int32(1)),
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceMemory: parseQty("2Gi")},
					},
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{},
					},
				},
				Cells: []multigresv1alpha1.CellName{"b"},
			},
			wantPools: map[string]multigresv1alpha1.PoolSpec{
				"p1": {Type: "write"},
				"p2": {Type: "internal"},
			},
		},
		"Template Only (Nil Overrides)": {
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
			t.Parallel()
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

	tests := map[string]struct {
		spec *multigresv1alpha1.GlobalTopoServerSpec
		tpl  *multigresv1alpha1.CoreTemplate
		want *multigresv1alpha1.GlobalTopoServerSpec
	}{
		"Inline Priority (Etcd)": {
			spec: &multigresv1alpha1.GlobalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{Image: "inline"},
			},
			tpl: nil,
			want: &multigresv1alpha1.GlobalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{Image: "inline"},
			},
		},
		"Inline Priority (External)": {
			spec: &multigresv1alpha1.GlobalTopoServerSpec{
				External: &multigresv1alpha1.ExternalTopoServerSpec{
					Endpoints: []multigresv1alpha1.EndpointUrl{"http://foo"},
				},
			},
			tpl: nil,
			want: &multigresv1alpha1.GlobalTopoServerSpec{
				External: &multigresv1alpha1.ExternalTopoServerSpec{
					Endpoints: []multigresv1alpha1.EndpointUrl{"http://foo"},
				},
			},
		},
		"Template Fallback": {
			spec: &multigresv1alpha1.GlobalTopoServerSpec{},
			tpl: &multigresv1alpha1.CoreTemplate{
				Spec: multigresv1alpha1.CoreTemplateSpec{
					GlobalTopoServer: &multigresv1alpha1.TopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{Image: "template"},
					},
				},
			},
			want: &multigresv1alpha1.GlobalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{Image: "template"},
			},
		},
		"Template Found but Nil Content": {
			spec: &multigresv1alpha1.GlobalTopoServerSpec{},
			tpl: &multigresv1alpha1.CoreTemplate{
				Spec: multigresv1alpha1.CoreTemplateSpec{
					GlobalTopoServer: nil,
				},
			},
			want: &multigresv1alpha1.GlobalTopoServerSpec{},
		},
		"No-op (Nil Template)": {
			spec: &multigresv1alpha1.GlobalTopoServerSpec{},
			tpl:  nil,
			want: &multigresv1alpha1.GlobalTopoServerSpec{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := ResolveGlobalTopo(tc.spec, tc.tpl)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ResolveGlobalTopo mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResolveMultiAdmin(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		spec *multigresv1alpha1.MultiAdminConfig
		tpl  *multigresv1alpha1.CoreTemplate
		want *multigresv1alpha1.StatelessSpec
	}{
		"Inline Priority": {
			spec: &multigresv1alpha1.MultiAdminConfig{
				Spec: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(5))},
			},
			tpl:  nil,
			want: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(5))},
		},
		"Template Fallback": {
			spec: &multigresv1alpha1.MultiAdminConfig{},
			tpl: &multigresv1alpha1.CoreTemplate{
				Spec: multigresv1alpha1.CoreTemplateSpec{
					MultiAdmin: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(3))},
				},
			},
			want: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(3))},
		},
		"Template Found but Nil Content": {
			spec: &multigresv1alpha1.MultiAdminConfig{},
			tpl: &multigresv1alpha1.CoreTemplate{
				Spec: multigresv1alpha1.CoreTemplateSpec{
					MultiAdmin: nil,
				},
			},
			want: nil,
		},
		"No-op (Nil Template)": {
			spec: &multigresv1alpha1.MultiAdminConfig{},
			tpl:  nil,
			want: nil,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := ResolveMultiAdmin(tc.spec, tc.tpl)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ResolveMultiAdmin mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func parseQty(s string) resource.Quantity {
	return resource.MustParse(s)
}

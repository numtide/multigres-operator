package resolver

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolver_ResolveShard(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_, _, shardTpl, ns := setupFixtures(t)

	tests := map[string]struct {
		config        *multigresv1alpha1.ShardConfig
		objects       []client.Object
		wantOrch      *multigresv1alpha1.MultiOrchSpec
		wantPools     map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec
		wantPVCPolicy *multigresv1alpha1.PVCDeletionPolicy
		wantErr       bool
		allCellNames  []multigresv1alpha1.CellName
	}{
		"Template Found": {
			config:  &multigresv1alpha1.ShardConfig{ShardTemplate: "default"},
			objects: []client.Object{shardTpl},
			wantOrch: &multigresv1alpha1.MultiOrchSpec{
				StatelessSpec: multigresv1alpha1.StatelessSpec{
					Replicas:  ptr.To(int32(1)),
					Resources: DefaultResourcesOrch(),
				},
			},
			wantPools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"default": {
					Type:            "readWrite",
					ReplicasPerCell: ptr.To(int32(1)),
					Storage: multigresv1alpha1.StorageSpec{
						Size: DefaultEtcdStorageSize,
					},
					Postgres: multigresv1alpha1.ContainerConfig{
						Resources: DefaultResourcesPostgres(),
					},
					Multipooler: multigresv1alpha1.ContainerConfig{
						Resources: DefaultResourcesPooler(),
					},
				},
			},
		},
		"Template Not Found": {
			config:  &multigresv1alpha1.ShardConfig{ShardTemplate: "missing"},
			wantErr: true,
		},
		"Inline Overrides": {
			config: &multigresv1alpha1.ShardConfig{
				Spec: &multigresv1alpha1.ShardInlineSpec{
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(5))},
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{"p": {}},
				},
			},
			wantOrch: &multigresv1alpha1.MultiOrchSpec{
				StatelessSpec: multigresv1alpha1.StatelessSpec{
					Replicas:  ptr.To(int32(5)),
					Resources: DefaultResourcesOrch(),
				},
			},
			// FIX: Updated to expect fully hydrated defaults for pool "p"
			wantPools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"p": {
					ReplicasPerCell: ptr.To(int32(1)),
					Storage: multigresv1alpha1.StorageSpec{
						Size: DefaultEtcdStorageSize, // "1Gi"
					},
					Postgres: multigresv1alpha1.ContainerConfig{
						Resources: DefaultResourcesPostgres(),
					},
					Multipooler: multigresv1alpha1.ContainerConfig{
						Resources: DefaultResourcesPooler(),
					},
				},
			},
		},
		"Dynamic Cell Injection": {
			config: &multigresv1alpha1.ShardConfig{
				Spec: &multigresv1alpha1.ShardInlineSpec{
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						// Empty Cells, should inherit allCellNames
						StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"p1": {Type: "read"}, // Empty Cells
					},
				},
			},
			allCellNames: []multigresv1alpha1.CellName{"zone-a", "zone-b"},
			wantOrch: &multigresv1alpha1.MultiOrchSpec{
				StatelessSpec: multigresv1alpha1.StatelessSpec{
					Replicas:  ptr.To(int32(1)),
					Resources: DefaultResourcesOrch(),
				},
				// Expect injected cells
				Cells: []multigresv1alpha1.CellName{"zone-a", "zone-b"},
			},
			wantPools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"p1": {
					Type:            "read",
					ReplicasPerCell: ptr.To(int32(1)),
					// Expect injected cells
					Cells: []multigresv1alpha1.CellName{"zone-a", "zone-b"},
					Storage: multigresv1alpha1.StorageSpec{
						Size: DefaultEtcdStorageSize,
					},
					Postgres: multigresv1alpha1.ContainerConfig{
						Resources: DefaultResourcesPostgres(),
					},
					Multipooler: multigresv1alpha1.ContainerConfig{
						Resources: DefaultResourcesPooler(),
					},
				},
			},
		},
		"PVC Policy Explicit": {
			config: &multigresv1alpha1.ShardConfig{
				Spec: &multigresv1alpha1.ShardInlineSpec{
					PVCDeletionPolicy: &multigresv1alpha1.PVCDeletionPolicy{
						WhenDeleted: multigresv1alpha1.RetainPVCRetentionPolicy,
					},
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{"p": {}},
				},
			},
			wantOrch: &multigresv1alpha1.MultiOrchSpec{
				StatelessSpec: multigresv1alpha1.StatelessSpec{
					Replicas:  ptr.To(int32(1)),
					Resources: DefaultResourcesOrch(),
				},
			},
			wantPools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"p": {
					ReplicasPerCell: ptr.To(int32(1)),
					Storage:         multigresv1alpha1.StorageSpec{Size: DefaultEtcdStorageSize},
					Postgres:        multigresv1alpha1.ContainerConfig{Resources: DefaultResourcesPostgres()},
					Multipooler:     multigresv1alpha1.ContainerConfig{Resources: DefaultResourcesPooler()},
				},
			},
			wantPVCPolicy: &multigresv1alpha1.PVCDeletionPolicy{
				WhenDeleted: multigresv1alpha1.RetainPVCRetentionPolicy,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objects...).Build()
			r := NewResolver(c, ns)

			orch, pools, pvcPolicy, err := r.ResolveShard(t.Context(), tc.config, tc.allCellNames)
			if tc.wantErr {
				if err == nil {
					t.Error("Expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if diff := cmp.Diff(tc.wantOrch, orch, cmpopts.IgnoreUnexported(resource.Quantity{}), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Orch Diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantPools, pools, cmpopts.IgnoreUnexported(resource.Quantity{}), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Pools Diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantPVCPolicy, pvcPolicy); diff != "" {
				t.Errorf("PVC Policy Diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResolver_ResolveShardTemplate(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	_, _, shardTpl, ns := setupFixtures(t)
	customShard := shardTpl.DeepCopy()
	customShard.Name = "custom-shard"

	tests := map[string]struct {
		existingObjects []client.Object
		defaults        multigresv1alpha1.TemplateDefaults
		reqName         multigresv1alpha1.TemplateRef
		wantErr         bool
		errContains     string
		wantFound       bool
		wantResName     string
	}{
		"Explicit Found": {
			existingObjects: []client.Object{customShard},
			reqName:         "custom-shard",
			wantFound:       true,
			wantResName:     "custom-shard",
		},
		"Explicit Not Found (Error)": {
			existingObjects: []client.Object{},
			reqName:         "missing-shard",
			wantErr:         true,
			errContains:     "referenced ShardTemplate 'missing-shard' not found",
		},
		"Implicit Fallback Found": {
			existingObjects: []client.Object{shardTpl},
			defaults:        multigresv1alpha1.TemplateDefaults{},
			reqName:         "",
			wantFound:       true,
			wantResName:     "default",
		},
		"Implicit Fallback Not Found (Safe Empty Return)": {
			existingObjects: []client.Object{},
			defaults:        multigresv1alpha1.TemplateDefaults{},
			reqName:         "",
			wantFound:       false,
			wantErr:         false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingObjects...).
				Build()
			r := NewResolver(c, ns)

			res, err := r.ResolveShardTemplate(t.Context(), tc.reqName)
			if tc.wantErr {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
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

func TestMergeShardConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		tpl       *multigresv1alpha1.ShardTemplate
		overrides *multigresv1alpha1.ShardOverrides
		inline    *multigresv1alpha1.ShardInlineSpec
		wantOrch  multigresv1alpha1.MultiOrchSpec
		wantPools map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec
	}{
		"Full Merge with MultiOrch Overrides": {
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"p1": {Type: "read"},
					},
				},
			},
			overrides: &multigresv1alpha1.ShardOverrides{
				MultiOrch: &multigresv1alpha1.MultiOrchSpec{
					StatelessSpec: multigresv1alpha1.StatelessSpec{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceMemory: parseQty("2Gi")},
						},
						Affinity: &corev1.Affinity{
							PodAntiAffinity: &corev1.PodAntiAffinity{},
						},
					},
					Cells: []multigresv1alpha1.CellName{"b"},
				},
				Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
			wantPools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
			wantPools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{},
		},
		"Pool Deep Merge": {
			tpl: &multigresv1alpha1.ShardTemplate{
				Spec: multigresv1alpha1.ShardTemplateSpec{
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"p1": {Type: "read"},
					},
				},
			},
			overrides: &multigresv1alpha1.ShardOverrides{
				Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
			wantPools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"p1": {Type: "read", ReplicasPerCell: ptr.To(int32(1))},
					},
				},
			},
			overrides: &multigresv1alpha1.ShardOverrides{
				Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
					"p1": {},
				},
			},
			wantOrch: multigresv1alpha1.MultiOrchSpec{},
			wantPools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
				Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
					"inline-pool": {Type: "read"},
				},
			},
			wantOrch: multigresv1alpha1.MultiOrchSpec{
				Cells: []multigresv1alpha1.CellName{"inline"},
			},
			wantPools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"inline-pool": {Type: "read"},
			},
		},
		"Inline Spec Overrides Existing Pool": {
			tpl: &multigresv1alpha1.ShardTemplate{
				Spec: multigresv1alpha1.ShardTemplateSpec{
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"existing": {Type: "read"},
					},
				},
			},
			inline: &multigresv1alpha1.ShardInlineSpec{
				Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
					"existing": {Type: "write"},
				},
			},
			wantOrch: multigresv1alpha1.MultiOrchSpec{},
			wantPools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"existing": {Type: "write"},
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
			wantPools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			orch, pools, _ := mergeShardConfig(tc.tpl, tc.overrides, tc.inline)

			if diff := cmp.Diff(tc.wantOrch, orch, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
				t.Errorf("Orch mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantPools, pools, cmpopts.IgnoreUnexported(resource.Quantity{}), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Pools mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResolver_ClientErrors_Shard(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	errSimulated := errors.New("simulated database connection error")
	mc := testutil.NewFakeClientWithFailures(
		fake.NewClientBuilder().WithScheme(scheme).Build(),
		&testutil.FailureConfig{
			OnGet: func(_ client.ObjectKey) error { return errSimulated },
		},
	)
	r := NewResolver(mc, "default")

	_, err := r.ResolveShardTemplate(t.Context(), "any")
	if err == nil ||
		err.Error() != "failed to get ShardTemplate: simulated database connection error" {
		t.Errorf("Error mismatch: got %v, want simulated error", err)
	}
}

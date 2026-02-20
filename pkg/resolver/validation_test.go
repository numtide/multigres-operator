package resolver

import (
	"strings"
	"testing"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestResolver_ValidateClusterIntegrity verifies checking template existence.
func TestResolver_ValidateClusterIntegrity(t *testing.T) {
	t.Parallel()
	scheme := setupScheme()

	// Setup Base Objects
	coreTpl := &multigresv1alpha1.CoreTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-core", Namespace: "default"},
	}
	cellTpl := &multigresv1alpha1.CellTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-cell", Namespace: "default"},
	}
	shardTpl := &multigresv1alpha1.ShardTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-shard", Namespace: "default"},
	}

	objs := []client.Object{coreTpl, cellTpl, shardTpl}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	tests := map[string]struct {
		cluster *multigresv1alpha1.MultigresCluster
		wantErr string
	}{
		"Valid": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "valid", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate:  "prod-core",
						CellTemplate:  "prod-cell",
						ShardTemplate: "prod-shard",
					},
					MultiAdmin: &multigresv1alpha1.MultiAdminConfig{TemplateRef: "prod-core"},
					GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
						TemplateRef: "prod-core",
					},
				},
			},
		},
		"Missing Core": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{CoreTemplate: "missing"},
				},
			},
			wantErr: "referenced CoreTemplate 'missing' not found",
		},
		"Missing Cell (Inline)": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Cells: []multigresv1alpha1.CellConfig{{Name: "c1", CellTemplate: "missing"}},
				},
			},
			wantErr: "referenced CellTemplate 'missing' not found",
		},
		"Missing MultiAdminWeb": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					MultiAdminWeb: &multigresv1alpha1.MultiAdminWebConfig{TemplateRef: "missing"},
				},
			},
			wantErr: "referenced CoreTemplate 'missing' not found",
		},
		"Missing MultiAdmin": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					MultiAdmin: &multigresv1alpha1.MultiAdminConfig{TemplateRef: "missing"},
				},
			},
			wantErr: "referenced CoreTemplate 'missing' not found",
		},
		"Missing GlobalTopoServer": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
						TemplateRef: "missing",
					},
				},
			},
			wantErr: "referenced CoreTemplate 'missing' not found",
		},
		"Missing CellTemplate Default": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{CellTemplate: "missing"},
				},
			},
			wantErr: "referenced CellTemplate 'missing' not found",
		},
		"Missing ShardTemplate Default": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{ShardTemplate: "missing"},
				},
			},
			wantErr: "referenced ShardTemplate 'missing' not found",
		},
		"Missing ShardTemplate Inline": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Databases: []multigresv1alpha1.DatabaseConfig{{
						TableGroups: []multigresv1alpha1.TableGroupConfig{{
							Shards: []multigresv1alpha1.ShardConfig{{
								Name:          "s0",
								ShardTemplate: "missing",
							}},
						}},
					}},
				},
			},
			wantErr: "referenced ShardTemplate 'missing' not found",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			r := NewResolver(fakeClient, "default")
			err := r.ValidateClusterIntegrity(t.Context(), tc.cluster)

			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("Expected nil error, got %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("Expected error containing '%s', got %v", tc.wantErr, err)
				}
			}
		})
	}
}

// TestResolver_ValidateClusterLogic verifies complex logical validations.
func TestResolver_ValidateClusterLogic(t *testing.T) {
	t.Parallel()
	scheme := setupScheme()

	// Setup Base Objects
	shardTpl := &multigresv1alpha1.ShardTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-shard", Namespace: "default"},
		Spec: multigresv1alpha1.ShardTemplateSpec{
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"default": {Type: "readWrite"},
			},
		},
	}

	defaultSC := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "standard",
			Annotations: map[string]string{"storageclass.kubernetes.io/is-default-class": "true"},
		},
		Provisioner: "k8s.io/fake",
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shardTpl, defaultSC).
		Build()

	noSCClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(shardTpl).Build()

	tests := map[string]struct {
		cluster        *multigresv1alpha1.MultigresCluster
		wantWarnings   []string
		wantErr        string
		customClient   client.Client
		clientFailures *testutil.FailureConfig
	}{
		"Valid Logic": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "valid", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Cells: []multigresv1alpha1.CellConfig{
						{Name: "zone-1", CellTemplate: "prod-cell"},
					},
					Databases: []multigresv1alpha1.DatabaseConfig{{
						TableGroups: []multigresv1alpha1.TableGroupConfig{{
							Shards: []multigresv1alpha1.ShardConfig{{
								Name:          "s0",
								ShardTemplate: "prod-shard",
							}},
						}},
					}},
				},
			},
		},
		"Orphan Override Warning": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "valid", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Cells: []multigresv1alpha1.CellConfig{
						{Name: "zone-1", CellTemplate: "prod-cell"},
					},
					Databases: []multigresv1alpha1.DatabaseConfig{{
						TableGroups: []multigresv1alpha1.TableGroupConfig{{
							Shards: []multigresv1alpha1.ShardConfig{{
								Name:          "s0",
								ShardTemplate: "prod-shard",
								Overrides: &multigresv1alpha1.ShardOverrides{
									Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
										"ghost": {Type: "read"},
									},
								},
							}},
						}},
					}},
				},
			},
			wantWarnings: []string{"does not exist in template 'prod-shard'"},
		},
		"Invalid Cell Reference (Orch)": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "valid", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Cells: []multigresv1alpha1.CellConfig{
						{Name: "zone-1", CellTemplate: "prod-cell"},
					},
					Databases: []multigresv1alpha1.DatabaseConfig{{
						TableGroups: []multigresv1alpha1.TableGroupConfig{{
							Shards: []multigresv1alpha1.ShardConfig{{
								Name:          "s0",
								ShardTemplate: "prod-shard",
								Overrides: &multigresv1alpha1.ShardOverrides{
									MultiOrch: &multigresv1alpha1.MultiOrchSpec{
										Cells: []multigresv1alpha1.CellName{"zone-missing"},
									},
								},
							}},
						}},
					}},
				},
			},
			wantErr: "assigned to non-existent cell 'zone-missing'",
		},
		"Invalid Cell Reference (Pool)": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "valid", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Cells: []multigresv1alpha1.CellConfig{
						{Name: "zone-1", CellTemplate: "prod-cell"},
					},
					Databases: []multigresv1alpha1.DatabaseConfig{{
						TableGroups: []multigresv1alpha1.TableGroupConfig{{
							Shards: []multigresv1alpha1.ShardConfig{{
								Name:          "s0",
								ShardTemplate: "prod-shard",
								Overrides: &multigresv1alpha1.ShardOverrides{
									Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
										"default": {
											Cells: []multigresv1alpha1.CellName{"zone-missing"},
										},
									},
								},
							}},
						}},
					}},
				},
			},
			wantErr: "pool 'default' in shard 's0' is assigned to non-existent cell 'zone-missing'",
		},
		"Empty Cells (Orphan Shard)": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "valid", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Cells: []multigresv1alpha1.CellConfig{}, // No cells
					Databases: []multigresv1alpha1.DatabaseConfig{{
						TableGroups: []multigresv1alpha1.TableGroupConfig{{
							Shards: []multigresv1alpha1.ShardConfig{{
								Name:          "s0",
								ShardTemplate: "prod-shard",
							}},
						}},
					}},
				},
			},
			wantErr: "matches NO cells",
		},
		"Empty Cells (Pool)": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "valid", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Cells: []multigresv1alpha1.CellConfig{}, // No cluster cells prevents defaulting
					Databases: []multigresv1alpha1.DatabaseConfig{{
						TableGroups: []multigresv1alpha1.TableGroupConfig{{
							Shards: []multigresv1alpha1.ShardConfig{{
								Name:          "s0",
								ShardTemplate: "prod-shard",
								Spec: &multigresv1alpha1.ShardInlineSpec{
									MultiOrch: multigresv1alpha1.MultiOrchSpec{
										Cells: []multigresv1alpha1.CellName{
											"ghost",
										}, // Bypass Check 1
									},
									Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
										"default": {
											Cells: []multigresv1alpha1.CellName{}, // Explicit empty
										},
									},
								},
							}},
						}},
					}},
				},
			},
			wantErr: "pool 'default' in shard 's0' matches NO cells",
		},
		"Resolution Error": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "valid", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Databases: []multigresv1alpha1.DatabaseConfig{{
						TableGroups: []multigresv1alpha1.TableGroupConfig{{
							Shards: []multigresv1alpha1.ShardConfig{{
								Name:          "s0",
								ShardTemplate: "missing-template",
							}},
						}},
					}},
				},
			},
			wantErr: "cannot resolve shard 's0'",
		},
		"Orphan Check Resolution Error": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "valid", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Databases: []multigresv1alpha1.DatabaseConfig{{
						TableGroups: []multigresv1alpha1.TableGroupConfig{{
							Shards: []multigresv1alpha1.ShardConfig{{
								Name:          "s0",
								ShardTemplate: "prod-shard",
								Overrides: &multigresv1alpha1.ShardOverrides{
									Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
										"ghost": {},
									},
								},
							}},
						}},
					}},
				},
			},
			clientFailures: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					return testutil.ErrInjected
				},
			},
			wantErr: "failed to resolve template for orphan check",
		},
		"No default SC and no explicit class rejects": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "no-sc", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{},
					},
					Cells: []multigresv1alpha1.CellConfig{
						{Name: "zone-1"},
					},
					Databases: []multigresv1alpha1.DatabaseConfig{{
						TableGroups: []multigresv1alpha1.TableGroupConfig{{
							Shards: []multigresv1alpha1.ShardConfig{{
								Name:          "s0",
								ShardTemplate: "prod-shard",
							}},
						}},
					}},
				},
			},
			// Use a client with no StorageClasses at all
			customClient: noSCClient,
			wantErr:      "no default StorageClass found",
		},
		"No default SC but explicit class set passes": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "explicit-sc", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{
							Storage: multigresv1alpha1.StorageSpec{Class: "my-sc"},
						},
					},
					Backup: &multigresv1alpha1.BackupConfig{
						Type: multigresv1alpha1.BackupTypeS3,
					},
					Cells: []multigresv1alpha1.CellConfig{
						{Name: "zone-1"},
					},
					Databases: []multigresv1alpha1.DatabaseConfig{{
						TableGroups: []multigresv1alpha1.TableGroupConfig{{
							Shards: []multigresv1alpha1.ShardConfig{{
								Name:          "s0",
								ShardTemplate: "prod-shard",
								Spec: &multigresv1alpha1.ShardInlineSpec{
									Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
										"default": {
											Type: "readWrite",
											Storage: multigresv1alpha1.StorageSpec{
												Class: "my-sc",
												Size:  "10Gi",
											},
										},
									},
								},
							}},
						}},
					}},
				},
			},
			customClient: noSCClient,
		},
		"Default SC exists with no explicit class passes": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "default-sc", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{},
					},
					Cells: []multigresv1alpha1.CellConfig{
						{Name: "zone-1"},
					},
					Databases: []multigresv1alpha1.DatabaseConfig{{
						TableGroups: []multigresv1alpha1.TableGroupConfig{{
							Shards: []multigresv1alpha1.ShardConfig{{
								Name:          "s0",
								ShardTemplate: "prod-shard",
							}},
						}},
					}},
				},
			},
			// Uses fakeClient which HAS a default StorageClass
		},
		"SC list error propagated": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "list-err", Namespace: "default"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{},
					},
					Cells: []multigresv1alpha1.CellConfig{
						{Name: "zone-1"},
					},
					Databases: []multigresv1alpha1.DatabaseConfig{{
						TableGroups: []multigresv1alpha1.TableGroupConfig{{
							Shards: []multigresv1alpha1.ShardConfig{{
								Name:          "s0",
								ShardTemplate: "prod-shard",
							}},
						}},
					}},
				},
			},
			clientFailures: &testutil.FailureConfig{
				OnList: func(_ client.ObjectList) error {
					return testutil.ErrInjected
				},
			},
			wantErr: "failed to check for default StorageClass",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var c client.Client = fakeClient
			if tc.customClient != nil {
				c = tc.customClient
			}
			if tc.clientFailures != nil {
				c = testutil.NewFakeClientWithFailures(c, tc.clientFailures)
			}
			r := NewResolver(c, "default")
			warnings, err := r.ValidateClusterLogic(t.Context(), tc.cluster)

			// Check Error
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("Expected nil error, got %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("Expected error containing '%s', got %v", tc.wantErr, err)
				}
			}

			// Check Warnings
			if len(tc.wantWarnings) > 0 {
				if len(warnings) == 0 {
					t.Errorf("Expected warnings containing %v, got none", tc.wantWarnings)
				}
				for _, want := range tc.wantWarnings {
					found := false
					for _, got := range warnings {
						if strings.Contains(got, want) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected warning containing '%s', got %v", want, warnings)
					}
				}
			}
		})
	}
}

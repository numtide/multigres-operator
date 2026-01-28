package handlers

import (
	"strings"
	"testing"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resolver"
	"github.com/numtide/multigres-operator/pkg/testutil"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// setupScheme creates a new scheme with all required types registered
func setupScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	return scheme
}

func TestMultigresClusterValidator(t *testing.T) {
	t.Parallel()

	scheme := setupScheme()

	baseMeta := metav1.ObjectMeta{Name: "cluster-1", Namespace: "default"}
	baseSpec := multigresv1alpha1.MultigresClusterSpec{
		TemplateDefaults: multigresv1alpha1.TemplateDefaults{
			CoreTemplate:  "prod-core",
			CellTemplate:  "prod-cell",
			ShardTemplate: "prod-shard",
		},
	}
	baseCluster := &multigresv1alpha1.MultigresCluster{ObjectMeta: baseMeta, Spec: baseSpec}

	tests := map[string]struct {
		object        *multigresv1alpha1.MultigresCluster
		operation     string // "Create", "Update", "Delete"
		existing      []client.Object
		failureConfig *testutil.FailureConfig
		wantAllowed   bool
		wantMessage   string
		wantWarnings  []string
	}{
		"Allowed: All templates exist (Create)": {
			object:      baseCluster.DeepCopy(),
			operation:   "Create",
			wantAllowed: true,
		},
		"Allowed: Update": {
			object:      baseCluster.DeepCopy(),
			operation:   "Update",
			wantAllowed: true,
		},
		"Allowed: Delete": {
			object:      baseCluster.DeepCopy(),
			operation:   "Delete",
			wantAllowed: true,
		},
		"Denied: Missing CoreTemplate": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.TemplateDefaults.CoreTemplate = "missing-core"
				return c
			}(),
			operation:   "Create",
			wantAllowed: false,
			wantMessage: "referenced CoreTemplate 'missing-core' not found",
		},
		"Denied: Missing CellTemplate": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.TemplateDefaults.CellTemplate = "missing-cell"
				return c
			}(),
			operation:   "Create",
			wantAllowed: false,
			wantMessage: "referenced CellTemplate 'missing-cell' not found",
		},
		"Denied: Missing ShardTemplate": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.TemplateDefaults.ShardTemplate = "missing-shard"
				return c
			}(),
			operation:   "Create",
			wantAllowed: false,
			wantMessage: "referenced ShardTemplate 'missing-shard' not found",
		},
		"Denied: Missing GlobalTopoServer Template": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.GlobalTopoServer = &multigresv1alpha1.GlobalTopoServerSpec{
					TemplateRef: "missing-topo",
				}
				return c
			}(),
			operation:   "Create",
			wantAllowed: false,
			wantMessage: "referenced CoreTemplate 'missing-topo' not found",
		},
		"Error: Client Error (CoreTemplate)": {
			object:    baseCluster.DeepCopy(),
			operation: "Create",
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error { return testutil.ErrInjected },
			},
			wantAllowed: false,
			wantMessage: "injected test error",
		},
		"Error: Client Error (CellTemplate)": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.TemplateDefaults.CellTemplate = "prod-cell"
				return c
			}(),
			operation: "Create",
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					// We can assume names are unique enough or check implicit knowledge of order
					if strings.Contains(key.Name, "cell") {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantAllowed: false,
			wantMessage: "injected test error",
		},
		"Error: Client Error (ShardTemplate)": {
			object:    baseCluster.DeepCopy(),
			operation: "Create",
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					if strings.Contains(key.Name, "shard") {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantAllowed: false,
			wantMessage: "injected test error",
		},
		"Error: Client Error (MultiAdmin)": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.MultiAdmin = &multigresv1alpha1.MultiAdminConfig{TemplateRef: "admin-core"}
				return c
			}(),
			operation: "Create",
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					if key.Name == "admin-core" {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantAllowed: false,
			wantMessage: "injected test error",
		},
		"Error: Client Error (Inline Cell)": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.Cells = []multigresv1alpha1.CellConfig{
					{Name: "c1", CellTemplate: "inline-cell"},
				}
				return c
			}(),
			operation: "Create",
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					if key.Name == "inline-cell" {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantAllowed: false,
			wantMessage: "injected test error",
		},
		"Error: Client Error (Inline Shard)": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.Databases = []multigresv1alpha1.DatabaseConfig{{
					TableGroups: []multigresv1alpha1.TableGroupConfig{{
						Shards: []multigresv1alpha1.ShardConfig{
							{Name: "s0", ShardTemplate: "inline-shard"},
						},
					}},
				}}
				return c
			}(),
			operation: "Create",
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					if key.Name == "inline-shard" {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantAllowed: false,
			wantMessage: "injected test error",
		},
		"Allowed: Missing Fallback Templates": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.TemplateDefaults = multigresv1alpha1.TemplateDefaults{
					CoreTemplate:  resolver.FallbackCoreTemplate,
					CellTemplate:  resolver.FallbackCellTemplate,
					ShardTemplate: resolver.FallbackShardTemplate,
				}
				return c
			}(),
			operation:   "Create",
			wantAllowed: true,
		},
		"Error: Missing CellTemplate (Core Valid)": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.TemplateDefaults.CellTemplate = "missing-cell"
				return c
			}(),
			operation:   "Create",
			wantAllowed: false,
			wantMessage: "referenced CellTemplate 'missing-cell' not found",
		},
		"Error: Missing ShardTemplate (Core/Cell Valid)": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.TemplateDefaults.ShardTemplate = "missing-shard"
				return c
			}(),
			operation:   "Create",
			wantAllowed: false,
			wantMessage: "referenced ShardTemplate 'missing-shard' not found",
		},
		"Allowed: Complex Cluster (All Valid)": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.MultiAdmin = &multigresv1alpha1.MultiAdminConfig{TemplateRef: "prod-core"}
				c.Spec.Cells = []multigresv1alpha1.CellConfig{
					{Name: "c1", CellTemplate: "prod-cell"},
				}
				c.Spec.Databases = []multigresv1alpha1.DatabaseConfig{{
					TableGroups: []multigresv1alpha1.TableGroupConfig{{
						Shards: []multigresv1alpha1.ShardConfig{
							{Name: "s0", ShardTemplate: "prod-shard"},
						},
					}},
				}}
				return c
			}(),
			operation:   "Create",
			wantAllowed: true,
		},
		"Allowed: Empty CoreTemplate": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.TemplateDefaults.CoreTemplate = ""
				return c
			}(),
			operation:   "Create",
			wantAllowed: true,
		},
		"Allowed: Orphan Override Warning": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.Cells = []multigresv1alpha1.CellConfig{
					{Name: "c1", CellTemplate: "prod-cell"},
				}
				// This cluster uses "prod-shard" which likely has "default" pool.
				// We override "typo-pool" which doesn't exist.
				c.Spec.Databases = []multigresv1alpha1.DatabaseConfig{{
					TableGroups: []multigresv1alpha1.TableGroupConfig{{
						Shards: []multigresv1alpha1.ShardConfig{
							{
								Name:          "s0",
								ShardTemplate: "prod-shard",
								Overrides: &multigresv1alpha1.ShardOverrides{
									Pools: map[string]multigresv1alpha1.PoolSpec{
										"typo-pool": {ReplicasPerCell: ptr.To(int32(3))},
									},
								},
							},
						},
					}},
				}}
				return c
			}(),
			operation:   "Create",
			wantAllowed: true,
			wantWarnings: []string{
				"Pool 'typo-pool' defined in overrides for shard 's0' does not exist in template 'prod-shard'",
			},
		},
		"Denied: Shard Assigned to Non-Existent Cell": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.Cells = []multigresv1alpha1.CellConfig{
					{Name: "zone-valid", CellTemplate: "prod-cell"},
				}
				c.Spec.Databases = []multigresv1alpha1.DatabaseConfig{{
					TableGroups: []multigresv1alpha1.TableGroupConfig{{
						Shards: []multigresv1alpha1.ShardConfig{{
							Name: "s0",
							Spec: &multigresv1alpha1.ShardInlineSpec{
								MultiOrch: multigresv1alpha1.MultiOrchSpec{
									Cells: []multigresv1alpha1.CellName{"zone-invalid"}, // Invalid!
								},
								Pools: map[string]multigresv1alpha1.PoolSpec{
									"p1": {
										Type:  "read",
										Cells: []multigresv1alpha1.CellName{"zone-invalid"},
									},
								},
							},
						}},
					}},
				}}
				return c
			}(),
			operation:   "Create",
			wantAllowed: false,
			wantMessage: "assigned to non-existent cell 'zone-invalid'",
		},
		"Denied: Shard Matches NO Cells": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.Cells = []multigresv1alpha1.CellConfig{} // Empty Cells to prevent defaulting
				c.Spec.Databases = []multigresv1alpha1.DatabaseConfig{{
					TableGroups: []multigresv1alpha1.TableGroupConfig{{
						Shards: []multigresv1alpha1.ShardConfig{{
							Name: "s0",
							Spec: &multigresv1alpha1.ShardInlineSpec{
								MultiOrch: multigresv1alpha1.MultiOrchSpec{
									Cells: []multigresv1alpha1.CellName{}, // Empty!
								},
								Pools: map[string]multigresv1alpha1.PoolSpec{
									"p1": {Type: "read"}, // Empty!
								},
							},
						}},
					}},
				}}
				return c
			}(),
			operation:   "Create",
			wantAllowed: false,
			wantMessage: "matches NO cells",
		},
		"Denied: Pool Assigned to Non-Existent Cell": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.Databases = []multigresv1alpha1.DatabaseConfig{{
					TableGroups: []multigresv1alpha1.TableGroupConfig{{
						Shards: []multigresv1alpha1.ShardConfig{{
							Name:          "s0",
							ShardTemplate: "prod-shard",
							Overrides: &multigresv1alpha1.ShardOverrides{
								Pools: map[string]multigresv1alpha1.PoolSpec{
									"default": {Cells: []multigresv1alpha1.CellName{"invalid"}},
								},
							},
						}},
					}},
				}}
				// Add a valid cell so Shard passes Check 1
				c.Spec.Cells = append(
					c.Spec.Cells,
					multigresv1alpha1.CellConfig{Name: "c1", CellTemplate: "prod-cell"},
				)
				return c
			}(),
			operation:   "Create",
			wantAllowed: false,
			wantMessage: "pool 'default' in shard 's0' is assigned to non-existent cell 'invalid'",
		},
		"Error: Transient Failure (Resolve Shard Template)": {
			// This tests the case where Validation passes (1st & 2nd Get), but orphan check fails (3rd Get)
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				// We use an override to trigger the lookup in validateLogic
				c.Spec.Databases = []multigresv1alpha1.DatabaseConfig{{
					TableGroups: []multigresv1alpha1.TableGroupConfig{{
						Shards: []multigresv1alpha1.ShardConfig{
							{
								Name:          "s0",
								ShardTemplate: "prod-shard",
								Overrides: &multigresv1alpha1.ShardOverrides{
									Pools: map[string]multigresv1alpha1.PoolSpec{"p": {}},
								},
							},
						},
					}},
				}}
				return c
			}(),
			operation: "Create",
			failureConfig: &testutil.FailureConfig{
				OnGet: func() func(key client.ObjectKey) error {
					count := 0
					return func(key client.ObjectKey) error {
						if key.Name == "prod-shard" {
							count++
							if count > 2 {
								// Fail on 3rd attempt (Orphan Check)
								return testutil.ErrInjected
							}
						}
						return nil
					}
				}(),
			},
			wantAllowed: false,
			wantMessage: "failed to resolve template for orphan check: failed to get ShardTemplate: injected test error",
		},
		"Denied: Empty Pool Cells (Orphaned Pool)": {
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.Cells = []multigresv1alpha1.CellConfig{} // No Cells
				c.Spec.Databases = []multigresv1alpha1.DatabaseConfig{{
					TableGroups: []multigresv1alpha1.TableGroupConfig{{
						Shards: []multigresv1alpha1.ShardConfig{{
							Name:          "s0",
							ShardTemplate: "prod-shard",
							Spec: &multigresv1alpha1.ShardInlineSpec{
								MultiOrch: multigresv1alpha1.MultiOrchSpec{
									// Pass Check 1
									Cells: []multigresv1alpha1.CellName{"ghost-cell"},
								},
								Pools: map[string]multigresv1alpha1.PoolSpec{
									"default": {
										Type:  "readWrite",
										Cells: []multigresv1alpha1.CellName{}, // Empty! Check 1b
									},
								},
							},
						}},
					}},
				}}
				return c
			}(),
			operation:   "Create",
			wantAllowed: false,
			wantMessage: "matches NO cells",
		},
		"Error: Resolve Shard Failure": {
			// Trigger a resolution error.
			object: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.Databases = []multigresv1alpha1.DatabaseConfig{{
					TableGroups: []multigresv1alpha1.TableGroupConfig{{
						Shards: []multigresv1alpha1.ShardConfig{
							{Name: "s0", ShardTemplate: "prod-shard"},
						},
					}},
				}}
				return c
			}(),
			operation: "Create",
			failureConfig: &testutil.FailureConfig{
				OnGet: func() func(key client.ObjectKey) error {
					count := 0
					return func(key client.ObjectKey) error {
						if key.Name == "prod-shard" {
							count++
							// 1. TemplateDefaults validation
							// 2. ShardTemplate validation
							// 3. Orphan Check (Skipped)
							// 4. ResolveShard -> ResolveShardTemplate
							if count > 2 {
								return testutil.ErrInjected
							}
						}
						return nil
					}
				}(),
			},
			wantAllowed: false,
			wantMessage: "validation failed: cannot resolve shard 's0': failed to get ShardTemplate: injected test error",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Default existing objects if nil
			existing := tc.existing
			if existing == nil {
				existing = []client.Object{
					&multigresv1alpha1.CoreTemplate{
						ObjectMeta: metav1.ObjectMeta{Name: "prod-core", Namespace: "default"},
					},
					&multigresv1alpha1.CellTemplate{
						ObjectMeta: metav1.ObjectMeta{Name: "prod-cell", Namespace: "default"},
					},
					&multigresv1alpha1.ShardTemplate{
						ObjectMeta: metav1.ObjectMeta{Name: "prod-shard", Namespace: "default"},
						Spec: multigresv1alpha1.ShardTemplateSpec{
							Pools: map[string]multigresv1alpha1.PoolSpec{
								"default": {Type: "readWrite"}, // Only "default" pool exists
							},
						},
					},
				}
			}

			var fakeClient client.Client
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing...).Build()
			if tc.failureConfig != nil {
				fakeClient = testutil.NewFakeClientWithFailures(fakeClient, tc.failureConfig)
			}
			validator := NewMultigresClusterValidator(fakeClient)

			var warnings admission.Warnings
			var err error
			switch tc.operation {
			case "Create":
				warnings, err = validator.ValidateCreate(t.Context(), tc.object)
			case "Update":
				warnings, err = validator.ValidateUpdate(t.Context(), tc.object, tc.object)
			case "Delete":
				warnings, err = validator.ValidateDelete(t.Context(), tc.object)
			}

			if tc.wantAllowed && err != nil {
				t.Fatalf("Expected allowed, got error: %v", err)
			}
			if !tc.wantAllowed {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if tc.wantMessage != "" && !strings.Contains(err.Error(), tc.wantMessage) {
					t.Errorf(
						"Expected error message containing '%s', got '%v'",
						tc.wantMessage,
						err,
					)
				}
			}

			// Check Warnings
			if len(tc.wantWarnings) > 0 {
				if len(warnings) == 0 {
					t.Errorf("Expected warnings containing %v, got none", tc.wantWarnings)
				} else {
					for _, want := range tc.wantWarnings {
						found := false
						for _, got := range warnings {
							if strings.Contains(got, want) {
								found = true
								break
							}
						}
						if !found {
							t.Errorf("Expected warning containing '%s', got warnings: %v", want, warnings)
						}
					}
				}
			}
		})
	}
}

// TrulyOnlyRuntimeObject is for negative testing of client.Object cast
type TrulyOnlyRuntimeObject struct{}

func (t *TrulyOnlyRuntimeObject) DeepCopyObject() runtime.Object {
	return t
}

func (t *TrulyOnlyRuntimeObject) GetObjectKind() schema.ObjectKind {
	return schema.EmptyObjectKind
}

func TestMultigresClusterValidator_WrongType(t *testing.T) {
	t.Parallel()
	validator := NewMultigresClusterValidator(fake.NewClientBuilder().Build())
	_, err := validator.ValidateCreate(t.Context(), &TrulyOnlyRuntimeObject{})
	if err == nil || !strings.Contains(err.Error(), "expected MultigresCluster") {
		t.Errorf("Expected wrong type error, got: %v", err)
	}
}

func TestTemplateValidator(t *testing.T) {
	t.Parallel()

	scheme := setupScheme()

	// Fixtures
	configUsingCore := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c-core", Namespace: "default"},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			TemplateDefaults: multigresv1alpha1.TemplateDefaults{CoreTemplate: "prod-core"},
		},
	}
	configUsingCoreAdmin := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c-admin", Namespace: "default"},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			MultiAdmin: &multigresv1alpha1.MultiAdminConfig{TemplateRef: "prod-core"},
		},
	}
	configUsingCell := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c-cell", Namespace: "default"},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			Cells: []multigresv1alpha1.CellConfig{{Name: "c1", CellTemplate: "prod-cell"}},
		},
	}
	configUsingCellDefault := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c-cell-def", Namespace: "default"},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			TemplateDefaults: multigresv1alpha1.TemplateDefaults{CellTemplate: "prod-cell"},
		},
	}
	configUsingShard := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c-shard", Namespace: "default"},
		Spec: multigresv1alpha1.MultigresClusterSpec{Databases: []multigresv1alpha1.DatabaseConfig{{
			TableGroups: []multigresv1alpha1.TableGroupConfig{{
				Shards: []multigresv1alpha1.ShardConfig{{Name: "s0", ShardTemplate: "prod-shard"}},
			}},
		}}},
	}
	configUsingShardDefault := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c-shard-def", Namespace: "default"},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			TemplateDefaults: multigresv1alpha1.TemplateDefaults{ShardTemplate: "prod-shard"},
		},
	}

	tests := map[string]struct {
		kind          string
		targetName    string
		existing      []client.Object
		failureConfig *testutil.FailureConfig
		wrongType     bool
		wantAllowed   bool
		wantMessage   string
	}{
		"Denied: Delete In-Use CoreTemplate (Defaults)": {
			kind:        "CoreTemplate",
			targetName:  "prod-core",
			existing:    []client.Object{configUsingCore},
			wantAllowed: false,
		},
		"Denied: Delete In-Use CoreTemplate (MultiAdmin)": {
			kind:        "CoreTemplate",
			targetName:  "prod-core",
			existing:    []client.Object{configUsingCoreAdmin},
			wantAllowed: false,
		},
		"Denied: Delete In-Use CoreTemplate (GlobalTopoServer)": {
			kind:       "CoreTemplate",
			targetName: "prod-core",
			existing: []client.Object{
				&multigresv1alpha1.MultigresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "c-topo", Namespace: "default"},
					Spec: multigresv1alpha1.MultigresClusterSpec{
						GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
							TemplateRef: "prod-core",
						},
					},
				},
			},
			wantAllowed: false,
		},
		"Denied: Delete In-Use CellTemplate (Inline)": {
			kind:        "CellTemplate",
			targetName:  "prod-cell",
			existing:    []client.Object{configUsingCell},
			wantAllowed: false,
		},
		"Denied: Delete In-Use CellTemplate (Defaults)": {
			kind:        "CellTemplate",
			targetName:  "prod-cell",
			existing:    []client.Object{configUsingCellDefault},
			wantAllowed: false,
		},
		"Denied: Delete In-Use ShardTemplate (Inline)": {
			kind:        "ShardTemplate",
			targetName:  "prod-shard",
			existing:    []client.Object{configUsingShard},
			wantAllowed: false,
		},
		"Denied: Delete In-Use ShardTemplate (Defaults)": {
			kind:        "ShardTemplate",
			targetName:  "prod-shard",
			existing:    []client.Object{configUsingShardDefault},
			wantAllowed: false,
		},
		"Allowed: Unused Template": {
			kind:        "CoreTemplate",
			targetName:  "unused",
			existing:    []client.Object{configUsingCore},
			wantAllowed: true,
		},
		"Error: Client Error": {
			kind: "CoreTemplate",
			failureConfig: &testutil.FailureConfig{
				OnList: func(list client.ObjectList) error { return testutil.ErrInjected },
			},
			wantAllowed: false,
		},
		"Error: Wrong Type Input": {
			kind:        "CoreTemplate",
			wrongType:   true,
			wantAllowed: false,
			wantMessage: "expected client.Object",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var fakeClient client.Client
			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existing...).
				Build()
			if tc.failureConfig != nil {
				fakeClient = testutil.NewFakeClientWithFailures(fakeClient, tc.failureConfig)
			}
			validator := NewTemplateValidator(fakeClient, tc.kind)

			var obj runtime.Object
			if tc.wrongType {
				obj = &TrulyOnlyRuntimeObject{}
			} else {
				meta := metav1.ObjectMeta{Name: tc.targetName}
				switch tc.kind {
				case "CoreTemplate":
					obj = &multigresv1alpha1.CoreTemplate{ObjectMeta: meta}
				case "CellTemplate":
					obj = &multigresv1alpha1.CellTemplate{ObjectMeta: meta}
				case "ShardTemplate":
					obj = &multigresv1alpha1.ShardTemplate{ObjectMeta: meta}
				default:
					obj = &multigresv1alpha1.CoreTemplate{ObjectMeta: meta}
				}
			}

			// Test all methods
			methods := []string{"Create", "Update", "Delete"}
			for _, method := range methods {
				var err error
				switch method {
				case "Create":
					_, err = validator.ValidateCreate(t.Context(), obj)
				case "Update":
					_, err = validator.ValidateUpdate(t.Context(), obj, obj)
				case "Delete":
					_, err = validator.ValidateDelete(t.Context(), obj)
				}
				if method != "Delete" {
					if err != nil {
						t.Errorf("%s: Expected nil error, got %v", method, err)
					}
					continue
				}

				// For Delete
				if tc.wantAllowed && err != nil {
					t.Fatalf("Delete: Expected allowed, got error: %v", err)
				}
				if !tc.wantAllowed {
					if err == nil {
						t.Fatal("Delete: Expected error, got nil")
					}
					if tc.wantMessage != "" && !strings.Contains(err.Error(), tc.wantMessage) {
						t.Errorf(
							"Delete: Expected error message containing '%s', got '%v'",
							tc.wantMessage,
							err,
						)
					}
				}
			}
		})
	}
}

func TestChildResourceValidator(t *testing.T) {
	t.Parallel()

	validator := NewChildResourceValidator("system:serviceaccount:default:multigres-operator")

	tests := map[string]struct {
		user        string
		noRequest   bool
		operation   string
		wantAllowed bool
		wantMessage string
	}{
		"Allowed: Operator (Create)": {
			user:        "system:serviceaccount:default:multigres-operator",
			operation:   "Create",
			wantAllowed: true,
		},
		"Denied: Random User (Create)": {
			user:        "alice",
			operation:   "Create",
			wantAllowed: false,
			wantMessage: "direct modification of",
		},
		"Allowed: Operator (Update)": {
			user:        "system:serviceaccount:default:multigres-operator",
			operation:   "Update",
			wantAllowed: true,
		},
		"Denied: Random User (Update)": {
			user:        "alice",
			operation:   "Update",
			wantAllowed: false,
		},
		"Allowed: Operator (Delete)": {
			user:        "system:serviceaccount:default:multigres-operator",
			operation:   "Delete",
			wantAllowed: true,
		},
		"Denied: Random User (Delete)": {
			user:        "alice",
			operation:   "Delete",
			wantAllowed: false,
		},
		"Error: No Admission Request": {
			noRequest:   true,
			operation:   "Create",
			wantAllowed: false,
			wantMessage: "could not get admission request",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Create context with admission request
			ctx := t.Context()
			if !tc.noRequest {
				req := admission.Request{
					AdmissionRequest: admissionv1.AdmissionRequest{
						UserInfo: authenticationv1.UserInfo{Username: tc.user},
					},
				}
				ctx = admission.NewContextWithRequest(ctx, req)
			}

			// We use Shard as the test object
			obj := &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{Name: "test-shard"},
				Spec: multigresv1alpha1.ShardSpec{
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"default": {
							ReplicasPerCell: ptr.To(int32(1)),
						},
					},
				},
			}

			var err error
			switch tc.operation {
			case "Create":
				_, err = validator.ValidateCreate(ctx, obj)
			case "Update":
				_, err = validator.ValidateUpdate(ctx, obj, obj)
			case "Delete":
				_, err = validator.ValidateDelete(ctx, obj)
			}

			if tc.wantAllowed && err != nil {
				t.Fatalf("Expected allowed, got error: %v", err)
			}
			if !tc.wantAllowed {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if tc.wantMessage != "" && !strings.Contains(err.Error(), tc.wantMessage) {
					t.Errorf(
						"Expected error message containing '%s', got '%v'",
						tc.wantMessage,
						err,
					)
				}
			}
		})
	}

	t.Run("Wrong Type", func(t *testing.T) {
		t.Parallel()
		_, err := validator.ValidateCreate(t.Context(), &TrulyOnlyRuntimeObject{})
		if err == nil {
			t.Error("Expected error for wrong type, got nil")
		}
	})
}

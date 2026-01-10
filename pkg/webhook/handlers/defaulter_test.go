package handlers

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resolver"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

func TestMultigresClusterDefaulter_Handle(t *testing.T) {
	t.Parallel()

	// Register scheme
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = multigresv1alpha1.AddToScheme(s)

	// Fixtures
	coreTemplate := &multigresv1alpha1.CoreTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "default-core", Namespace: "test-ns"},
		Spec: multigresv1alpha1.CoreTemplateSpec{
			MultiAdmin: &multigresv1alpha1.StatelessSpec{Replicas: ptr(int32(3))},
		},
	}
	cellTemplate := &multigresv1alpha1.CellTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "test-ns"},
		Spec:       multigresv1alpha1.CellTemplateSpec{},
	}
	shardTemplate := &multigresv1alpha1.ShardTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "test-ns"},
		Spec:       multigresv1alpha1.ShardTemplateSpec{},
	}

	tests := map[string]struct {
		cluster       *multigresv1alpha1.MultigresCluster
		existingObjs  []client.Object
		failureConfig *testutil.FailureConfig
		wantError     bool
		wantMessage   string // substring match on error message
		validateObj   func(t *testing.T, c *multigresv1alpha1.MultigresCluster)
	}{
		"Happy Path: System Catalog Injection & Resolution": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "test-ns"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate: "default-core",
					},
				},
			},
			existingObjs: []client.Object{coreTemplate, cellTemplate, shardTemplate},
			wantError:    false,
			validateObj: func(t *testing.T, c *multigresv1alpha1.MultigresCluster) {
				// 1. Verify System Catalog Injection (databases list)
				if len(c.Spec.Databases) == 0 {
					t.Error(
						"Expected spec.databases to be populated (System Catalog), but it was empty",
					)
				}

				// 2. Verify Template Resolution (MultiAdmin resolved from CoreTemplate)
				if c.Spec.MultiAdmin == nil || c.Spec.MultiAdmin.Spec == nil {
					t.Error("Expected spec.multiadmin to be resolved from template, but it was nil")
				} else if *c.Spec.MultiAdmin.Spec.Replicas != 3 {
					t.Errorf("Expected MultiAdmin replicas to be 3, got %v", c.Spec.MultiAdmin.Spec.Replicas)
				}
			},
		},
		"Error: Resolution Failed (Missing Template)": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "broken-cluster", Namespace: "test-ns"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate: "missing-template",
					},
				},
			},
			existingObjs: []client.Object{}, // No templates exist
			wantError:    true,
			wantMessage:  "failed to resolve globalTopoServer",
		},
		"Error: Resolution Failed (Client Error Injection)": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "fail-cluster", Namespace: "test-ns"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate: "default-core",
					},
				},
			},
			existingObjs: []client.Object{coreTemplate},
			failureConfig: &testutil.FailureConfig{
				OnGet: func(_ client.ObjectKey) error { return testutil.ErrInjected },
			},
			wantError:   true,
			wantMessage: "failed to resolve globalTopoServer",
		},
		"Error: Decode Failed (Bad Object)": {
			// This case is harder to simulate with strongly typed Default(obj) interface
			// unless we pass the wrong type.
			cluster:      nil, // Passing wrong type simulation
			existingObjs: []client.Object{},
			wantError:    true,
			wantMessage:  "expected MultigresCluster",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Setup Client
			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tc.existingObjs...).
				Build()

			var cl client.Client = fakeClient
			if tc.failureConfig != nil {
				cl = testutil.NewFakeClientWithFailures(fakeClient, tc.failureConfig)
			}

			// Setup Defaulter
			res := resolver.NewResolver(cl, "test-ns", multigresv1alpha1.TemplateDefaults{})
			defaulter := NewMultigresClusterDefaulter(res)

			// Prepare Object
			var obj runtime.Object
			if tc.cluster != nil {
				obj = tc.cluster.DeepCopy()
			} else {
				obj = &multigresv1alpha1.Cell{} // Wrong type
			}

			// Run
			err := defaulter.Default(context.Background(), obj)

			// Assertions
			if tc.wantError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				} else if tc.wantMessage != "" && !strings.Contains(err.Error(), tc.wantMessage) {
					t.Errorf("Error message mismatch. Want substring: '%s', Got: '%s'", tc.wantMessage, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
				if tc.validateObj != nil {
					// We know it's a cluster if we are in happy path
					tc.validateObj(t, obj.(*multigresv1alpha1.MultigresCluster))
				}
			}
		})
	}
}

// Helpers
func ptr[T any](v T) *T { return &v }

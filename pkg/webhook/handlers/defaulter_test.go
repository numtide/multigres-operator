package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"gomodules.xyz/jsonpatch/v2"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

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
	// FIX: Add default Cell/Shard templates because PopulateClusterDefaults will point to them
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
		wantAllowed   bool
		wantCode      int32
		wantMessage   string // substring match on error message
		validatePatch func(t *testing.T, patch []jsonpatch.JsonPatchOperation)
	}{
		"Happy Path: System Catalog Injection & Resolution": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "test-ns"},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate: "default-core",
						// Cell/Shard templates empty -> PopulateClusterDefaults sets them to "default"
					},
					// Empty Databases -> Should trigger System Catalog Injection
				},
			},
			existingObjs: []client.Object{coreTemplate, cellTemplate, shardTemplate},
			wantAllowed:  true,
			wantCode:     http.StatusOK,
			validatePatch: func(t *testing.T, ops []jsonpatch.JsonPatchOperation) {
				patchMap := mapPatches(ops)

				// 1. Verify System Catalog Injection (databases list)
				if _, ok := patchMap["/spec/databases"]; !ok {
					t.Error(
						"Expected patch to inject /spec/databases (System Catalog), but it was missing",
					)
				}

				// 2. Verify Template Resolution (MultiAdmin resolved from CoreTemplate)
				if _, ok := patchMap["/spec/multiadmin"]; !ok {
					t.Error(
						"Expected patch to resolve /spec/multiadmin from template, but it was missing",
					)
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
			wantAllowed:  false,
			wantCode:     http.StatusInternalServerError,
			// FIX: The error happens on GlobalTopo (first call), not MultiAdmin
			wantMessage: "failed to resolve globalTopoServer",
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
			wantAllowed: false,
			wantCode:    http.StatusInternalServerError,
			wantMessage: "failed to resolve globalTopoServer",
		},
		"Error: Decode Failed (Bad JSON)": {
			// Handled by setup logic below
			wantAllowed: false,
			wantCode:    http.StatusBadRequest,
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

			decoder := admission.NewDecoder(s)
			_ = defaulter.InjectDecoder(decoder)

			// Create Request
			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Create,
					Namespace: "test-ns",
				},
			}

			// Inject object into request if present
			if tc.cluster != nil {
				raw, _ := json.Marshal(tc.cluster)
				req.Object = runtime.RawExtension{Raw: raw}
			} else {
				// Simulate Decode Error
				req.Object = runtime.RawExtension{Raw: []byte("invalid-json")}
			}

			// Run
			resp := defaulter.Handle(context.Background(), req)

			// Assertions
			if resp.Allowed != tc.wantAllowed {
				t.Errorf("Allowed mismatch. Want: %v, Got: %v", tc.wantAllowed, resp.Allowed)
			}

			if resp.Result != nil {
				if resp.Result.Code != tc.wantCode {
					t.Errorf("Code mismatch. Want: %v, Got: %v", tc.wantCode, resp.Result.Code)
				}
				if tc.wantMessage != "" && !strings.Contains(resp.Result.Message, tc.wantMessage) {
					t.Errorf(
						"Message mismatch. Want substring: '%s', Got: '%s'",
						tc.wantMessage,
						resp.Result.Message,
					)
				}
			} else if tc.wantCode != http.StatusOK {
				t.Errorf("Expected error code %d but got nil Result (implies 200 OK)", tc.wantCode)
			}

			if tc.validatePatch != nil {
				if len(resp.Patches) == 0 {
					t.Error("Expected patches, got none")
				} else {
					tc.validatePatch(t, resp.Patches)
				}
			}
		})
	}
}

// Helpers
func ptr[T any](v T) *T { return &v }

func mapPatches(ops []jsonpatch.JsonPatchOperation) map[string]interface{} {
	m := make(map[string]interface{})
	for _, op := range ops {
		m[op.Path] = op.Value
	}
	return m
}

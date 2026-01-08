package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func TestMultigresClusterValidator(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = multigresv1alpha1.AddToScheme(s)

	validCluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "valid", Namespace: "default"},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			TemplateDefaults: multigresv1alpha1.TemplateDefaults{
				CoreTemplate: "existing-core",
			},
		},
	}

	invalidCluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "invalid", Namespace: "default"},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			TemplateDefaults: multigresv1alpha1.TemplateDefaults{
				CoreTemplate: "missing-core",
			},
		},
	}

	existingObjs := []client.Object{
		&multigresv1alpha1.CoreTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "existing-core", Namespace: "default"},
		},
	}

	tests := map[string]struct {
		object      *multigresv1alpha1.MultigresCluster
		wantAllowed bool
		wantMessage string
	}{
		"Allowed: All templates exist": {
			object:      validCluster,
			wantAllowed: true,
		},
		"Denied: Missing CoreTemplate": {
			object:      invalidCluster,
			wantAllowed: false,
			wantMessage: "referenced CoreTemplate 'missing-core' not found",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(existingObjs...).Build()
			validator := NewMultigresClusterValidator(fakeClient)

			// FIX: admission.NewDecoder returns only the decoder, no error
			decoder := admission.NewDecoder(s)
			_ = validator.InjectDecoder(decoder)

			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Create,
					Namespace: "default",
				},
			}
			raw, _ := json.Marshal(tc.object)
			req.Object = runtime.RawExtension{Raw: raw}

			resp := validator.Handle(context.Background(), req)

			if resp.Allowed != tc.wantAllowed {
				t.Errorf("Allowed mismatch. Want: %v, Got: %v", tc.wantAllowed, resp.Allowed)
			}
			if !tc.wantAllowed && !strings.Contains(resp.Result.Message, tc.wantMessage) {
				t.Errorf(
					"Message mismatch. Want: '%s', Got: '%s'",
					tc.wantMessage,
					resp.Result.Message,
				)
			}
		})
	}
}

func TestTemplateValidator_InUseProtection(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = multigresv1alpha1.AddToScheme(s)

	clusterUsingCore := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "user-cluster", Namespace: "default"},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			TemplateDefaults: multigresv1alpha1.TemplateDefaults{
				CoreTemplate: "prod-core",
			},
		},
	}

	tests := map[string]struct {
		kind        string
		targetName  string
		existing    []client.Object
		wantAllowed bool
		wantMessage string
	}{
		"Allowed: Delete Unused CoreTemplate": {
			kind:        "CoreTemplate",
			targetName:  "unused-core",
			existing:    []client.Object{clusterUsingCore},
			wantAllowed: true,
		},
		"Denied: Delete In-Use CoreTemplate (Defaults)": {
			kind:        "CoreTemplate",
			targetName:  "prod-core",
			existing:    []client.Object{clusterUsingCore},
			wantAllowed: false,
			wantMessage: "cannot delete CoreTemplate 'prod-core' because it is in use by MultigresCluster 'user-cluster'",
		},
		"Denied: Delete In-Use CellTemplate (Specific Ref)": {
			kind:       "CellTemplate",
			targetName: "prod-cell",
			existing: []client.Object{
				&multigresv1alpha1.MultigresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "cell-cluster", Namespace: "default"},
					Spec: multigresv1alpha1.MultigresClusterSpec{
						Cells: []multigresv1alpha1.CellConfig{
							{Name: "c1", CellTemplate: "prod-cell"},
						},
					},
				},
			},
			wantAllowed: false,
			wantMessage: "cannot delete CellTemplate 'prod-cell'",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(tc.existing...).Build()
			validator := NewTemplateValidator(fakeClient, tc.kind)

			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Delete,
					Name:      tc.targetName,
					Namespace: "default",
				},
			}

			resp := validator.Handle(context.Background(), req)

			if resp.Allowed != tc.wantAllowed {
				t.Errorf("Allowed mismatch. Want: %v, Got: %v", tc.wantAllowed, resp.Allowed)
			}
			if !tc.wantAllowed && !strings.Contains(resp.Result.Message, tc.wantMessage) {
				t.Errorf(
					"Message mismatch. Want: '%s', Got: '%s'",
					tc.wantMessage,
					resp.Result.Message,
				)
			}
		})
	}
}

func TestChildResourceValidator_Permissions(t *testing.T) {
	t.Parallel()

	validator := NewChildResourceValidator("system:serviceaccount:default:operator")

	tests := map[string]struct {
		user        string
		wantAllowed bool
	}{
		"Allowed: Operator": {
			user:        "system:serviceaccount:default:operator",
			wantAllowed: true,
		},
		"Denied: Random User": {
			user:        "minikube-user",
			wantAllowed: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Update,
					Kind:      metav1.GroupVersionKind{Group: "multigres.com", Kind: "Cell"},
					UserInfo:  authenticationv1.UserInfo{Username: tc.user},
				},
			}

			resp := validator.Handle(context.Background(), req)

			if resp.Allowed != tc.wantAllowed {
				t.Errorf("Allowed mismatch. Want: %v, Got: %v", tc.wantAllowed, resp.Allowed)
			}
			if !tc.wantAllowed {
				if !strings.Contains(
					resp.Result.Message,
					"Direct modification of Cell is prohibited",
				) {
					t.Errorf("Unexpected error message: %s", resp.Result.Message)
				}
			}
		})
	}
}

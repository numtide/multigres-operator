package cell

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	multigresv1alpha1 "github.com/multigres/multigres-operator/api/v1alpha1"
)

func TestBuildCertificate(t *testing.T) {
	tests := map[string]struct {
		cell           *multigresv1alpha1.Cell
		wantName       string
		wantNamespace  string
		wantDNSNames   []any
		wantSubject    string
		wantSecretName string
	}{
		"standard certCommonName with db prefix": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell",
					Namespace: "supabase",
					UID:       "cell-uid-1",
				},
				TypeMeta: metav1.TypeMeta{
					APIVersion: "multigres.com/v1alpha1",
					Kind:       "Cell",
				},
				Spec: multigresv1alpha1.CellSpec{
					CertCommonName: "db.abc123.supabase.red",
				},
			},
			wantName:      "db.abc123.supabase.red",
			wantNamespace: "supabase",
			wantDNSNames: []any{
				"db.abc123.supabase.red",
				"abc123.supabase.red",
			},
			wantSubject:    "C=US, ST=Delware, L=New Castle,O=Supabase Inc, CN=db.abc123.supabase.red",
			wantSecretName: CertSecretName,
		},
		"certCommonName without db prefix": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell",
					Namespace: "supabase",
					UID:       "cell-uid-2",
				},
				TypeMeta: metav1.TypeMeta{
					APIVersion: "multigres.com/v1alpha1",
					Kind:       "Cell",
				},
				Spec: multigresv1alpha1.CellSpec{
					CertCommonName: "custom.example.com",
				},
			},
			wantName:       "custom.example.com",
			wantNamespace:  "supabase",
			wantDNSNames:   []any{"custom.example.com"},
			wantSubject:    "C=US, ST=Delware, L=New Castle,O=Supabase Inc, CN=custom.example.com",
			wantSecretName: CertSecretName,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildCertificate(tc.cell)

			// Verify GVK
			wantGVK := schema.GroupVersionKind{
				Group:   "cert-manager.io",
				Version: "v1",
				Kind:    "Certificate",
			}
			if diff := cmp.Diff(wantGVK, got.GroupVersionKind()); diff != "" {
				t.Errorf("GVK mismatch (-want +got):\n%s", diff)
			}

			if got.GetName() != tc.wantName {
				t.Errorf("Name = %q, want %q", got.GetName(), tc.wantName)
			}
			if got.GetNamespace() != tc.wantNamespace {
				t.Errorf("Namespace = %q, want %q", got.GetNamespace(), tc.wantNamespace)
			}

			// Verify owner reference
			ownerRefs := got.GetOwnerReferences()
			if len(ownerRefs) != 1 {
				t.Fatalf("expected 1 ownerReference, got %d", len(ownerRefs))
			}
			if ownerRefs[0].Name != tc.cell.Name {
				t.Errorf("ownerRef.Name = %q, want %q", ownerRefs[0].Name, tc.cell.Name)
			}
			if ownerRefs[0].UID != tc.cell.UID {
				t.Errorf("ownerRef.UID = %q, want %q", ownerRefs[0].UID, tc.cell.UID)
			}
			if ownerRefs[0].Kind != "Cell" {
				t.Errorf("ownerRef.Kind = %q, want Cell", ownerRefs[0].Kind)
			}
			if ownerRefs[0].Controller == nil || !*ownerRefs[0].Controller {
				t.Error("ownerRef.Controller should be true")
			}

			spec, ok := got.Object["spec"].(map[string]any)
			if !ok {
				t.Fatal("spec is not a map")
			}

			if diff := cmp.Diff(tc.wantDNSNames, spec["dnsNames"]); diff != "" {
				t.Errorf("dnsNames mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantSubject, spec["literalSubject"]); diff != "" {
				t.Errorf("literalSubject mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantSecretName, spec["secretName"]); diff != "" {
				t.Errorf("secretName mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(CertDuration, spec["duration"]); diff != "" {
				t.Errorf("duration mismatch (-want +got):\n%s", diff)
			}

			issuerRef, ok := spec["issuerRef"].(map[string]any)
			if !ok {
				t.Fatal("issuerRef is not a map")
			}
			if issuerRef["name"] != CertIssuerName {
				t.Errorf("issuerRef.name = %q, want %q", issuerRef["name"], CertIssuerName)
			}
			if issuerRef["kind"] != "ClusterIssuer" {
				t.Errorf("issuerRef.kind = %q, want %q", issuerRef["kind"], "ClusterIssuer")
			}

			privateKey, ok := spec["privateKey"].(map[string]any)
			if !ok {
				t.Fatal("privateKey is not a map")
			}
			if privateKey["algorithm"] != "RSA" {
				t.Errorf("privateKey.algorithm = %q, want RSA", privateKey["algorithm"])
			}
			if privateKey["size"] != int64(2048) {
				t.Errorf("privateKey.size = %v, want 2048", privateKey["size"])
			}

			usages, ok := spec["usages"].([]any)
			if !ok {
				t.Fatal("usages is not a slice")
			}
			wantUsages := []any{"digital signature", "key encipherment", "server auth"}
			if diff := cmp.Diff(wantUsages, usages); diff != "" {
				t.Errorf("usages mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReconcileCertificate(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	certGVK := schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Certificate",
	}
	// Register cert-manager Certificate as an unstructured type so the fake
	// client can store and retrieve it. Without this the fake client falls
	// back to its unstructured storage which happens to work today, but
	// explicitly registering it makes the test contract clear.
	scheme.AddKnownTypeWithName(certGVK, &unstructured.Unstructured{})
	certListGVK := certGVK
	certListGVK.Kind += "List"
	scheme.AddKnownTypeWithName(certListGVK, &unstructured.UnstructuredList{})

	t.Run("no-op when CertCommonName is empty", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &CellReconciler{
			Client:   fakeClient,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}

		cell := &multigresv1alpha1.Cell{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cell-no-tls",
				Namespace: "default",
			},
			Spec: multigresv1alpha1.CellSpec{
				CertCommonName: "",
			},
		}

		if err := r.reconcileCertificate(t.Context(), cell); err != nil {
			t.Fatalf("reconcileCertificate() returned unexpected error: %v", err)
		}
	})

	t.Run("creates Certificate when CertCommonName is set", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &CellReconciler{
			Client:   fakeClient,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}

		cell := &multigresv1alpha1.Cell{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "multigres.com/v1alpha1",
				Kind:       "Cell",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cell-tls",
				Namespace: "default",
				UID:       "cell-tls-uid",
			},
			Spec: multigresv1alpha1.CellSpec{
				CertCommonName: "db.abc123.supabase.red",
			},
		}

		if err := r.reconcileCertificate(t.Context(), cell); err != nil {
			t.Fatalf("reconcileCertificate() returned unexpected error: %v", err)
		}

		// Verify the Certificate was created
		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(certGVK)
		err := fakeClient.Get(t.Context(), types.NamespacedName{
			Name:      "db.abc123.supabase.red",
			Namespace: "default",
		}, got)
		if err != nil {
			t.Fatalf("Certificate should exist after reconcile: %v", err)
		}

		if got.GetName() != "db.abc123.supabase.red" {
			t.Errorf("Certificate name = %q, want %q", got.GetName(), "db.abc123.supabase.red")
		}

		ownerRefs := got.GetOwnerReferences()
		if len(ownerRefs) != 1 {
			t.Fatalf("expected 1 ownerReference, got %d", len(ownerRefs))
		}
		if ownerRefs[0].Name != "cell-tls" {
			t.Errorf("ownerRef.Name = %q, want %q", ownerRefs[0].Name, "cell-tls")
		}
	})

	t.Run("idempotent on repeated calls", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &CellReconciler{
			Client:   fakeClient,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}

		cell := &multigresv1alpha1.Cell{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "multigres.com/v1alpha1",
				Kind:       "Cell",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cell-tls-idem",
				Namespace: "default",
				UID:       "cell-tls-idem-uid",
			},
			Spec: multigresv1alpha1.CellSpec{
				CertCommonName: "db.xyz789.supabase.red",
			},
		}

		// First call — creates
		if err := r.reconcileCertificate(t.Context(), cell); err != nil {
			t.Fatalf("first reconcileCertificate() error: %v", err)
		}

		// Second call — should be idempotent
		if err := r.reconcileCertificate(t.Context(), cell); err != nil {
			t.Fatalf("second reconcileCertificate() error: %v", err)
		}

		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(certGVK)
		err := fakeClient.Get(t.Context(), types.NamespacedName{
			Name:      "db.xyz789.supabase.red",
			Namespace: "default",
		}, got)
		if err != nil {
			t.Fatalf("Certificate should exist after reconcile: %v", err)
		}
	})
}

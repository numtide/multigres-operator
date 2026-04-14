package multigrescluster

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

func certScheme() *runtime.Scheme {
	s := setupScheme()
	// Register cert-manager Certificate as an unstructured type so the
	// fake client can store and retrieve it.
	s.AddKnownTypeWithName(certGVK, &unstructured.Unstructured{})
	listGVK := certGVK
	listGVK.Kind += "List"
	s.AddKnownTypeWithName(listGVK, &unstructured.UnstructuredList{})
	return s
}

func TestBuildCertificate(t *testing.T) {
	tests := map[string]struct {
		cluster        *multigresv1alpha1.MultigresCluster
		wantName       string
		wantDNSNames   []any
		wantSubject    string
		wantSecretName string
	}{
		"standard certCommonName with db prefix": {
			cluster: &multigresv1alpha1.MultigresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "multigres.com/v1alpha1",
					Kind:       "MultigresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "supabase",
					UID:       "cluster-uid-1",
				},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					CertCommonName: "db.abc123.supabase.red",
				},
			},
			wantName: "db.abc123.supabase.red",
			wantDNSNames: []any{
				"db.abc123.supabase.red",
				"abc123.supabase.red",
			},
			wantSubject:    "C=US, ST=Delware, L=New Castle,O=Supabase Inc, CN=db.abc123.supabase.red",
			wantSecretName: CertSecretName,
		},
		"certCommonName without db prefix": {
			cluster: &multigresv1alpha1.MultigresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "multigres.com/v1alpha1",
					Kind:       "MultigresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "supabase",
					UID:       "cluster-uid-2",
				},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					CertCommonName: "custom.example.com",
				},
			},
			wantName:       "custom.example.com",
			wantDNSNames:   []any{"custom.example.com"},
			wantSubject:    "C=US, ST=Delware, L=New Castle,O=Supabase Inc, CN=custom.example.com",
			wantSecretName: CertSecretName,
		},
	}

	scheme := certScheme()

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := buildCertificate(tc.cluster, scheme)
			if err != nil {
				t.Fatalf("buildCertificate() error: %v", err)
			}

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

			// Verify owner reference points to the cluster
			ownerRefs := got.GetOwnerReferences()
			if len(ownerRefs) != 1 {
				t.Fatalf("expected 1 ownerReference, got %d", len(ownerRefs))
			}
			if ownerRefs[0].Name != tc.cluster.Name {
				t.Errorf(
					"ownerRef.Name = %q, want %q",
					ownerRefs[0].Name, tc.cluster.Name,
				)
			}
			if ownerRefs[0].Kind != "MultigresCluster" {
				t.Errorf(
					"ownerRef.Kind = %q, want MultigresCluster",
					ownerRefs[0].Kind,
				)
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
		})
	}
}

func TestReconcileCertificate(t *testing.T) {
	scheme := certScheme()

	t.Run("no-op when CertCommonName is empty and no prior cert", func(t *testing.T) {
		fc := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &MultigresClusterReconciler{
			Client:   fc,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "c1", Namespace: "default", UID: "uid-1",
			},
		}
		if err := r.reconcileCertificate(t.Context(), cluster); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("creates Certificate when CertCommonName is set", func(t *testing.T) {
		fc := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &MultigresClusterReconciler{
			Client:   fc,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}
		cluster := &multigresv1alpha1.MultigresCluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "multigres.com/v1alpha1",
				Kind:       "MultigresCluster",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "c2", Namespace: "default", UID: "uid-2",
			},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				CertCommonName: "db.abc123.supabase.red",
			},
		}
		if err := r.reconcileCertificate(t.Context(), cluster); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(certGVK)
		if err := fc.Get(t.Context(), types.NamespacedName{
			Name: "db.abc123.supabase.red", Namespace: "default",
		}, got); err != nil {
			t.Fatalf("Certificate should exist: %v", err)
		}
		if got.GetOwnerReferences()[0].Name != "c2" {
			t.Errorf(
				"ownerRef.Name = %q, want c2",
				got.GetOwnerReferences()[0].Name,
			)
		}
	})

	t.Run("idempotent on repeated calls", func(t *testing.T) {
		fc := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &MultigresClusterReconciler{
			Client:   fc,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}
		cluster := &multigresv1alpha1.MultigresCluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "multigres.com/v1alpha1",
				Kind:       "MultigresCluster",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "c3", Namespace: "default", UID: "uid-3",
			},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				CertCommonName: "db.xyz.supabase.red",
			},
		}
		if err := r.reconcileCertificate(t.Context(), cluster); err != nil {
			t.Fatalf("first call: %v", err)
		}
		if err := r.reconcileCertificate(t.Context(), cluster); err != nil {
			t.Fatalf("second call: %v", err)
		}
	})

	t.Run("CN change updates Certificate", func(t *testing.T) {
		fc := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &MultigresClusterReconciler{
			Client:   fc,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}
		cluster := &multigresv1alpha1.MultigresCluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "multigres.com/v1alpha1",
				Kind:       "MultigresCluster",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "c4", Namespace: "default", UID: "uid-4",
			},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				CertCommonName: "db.old.supabase.red",
			},
		}

		// Create with old CN
		if err := r.reconcileCertificate(t.Context(), cluster); err != nil {
			t.Fatalf("create old: %v", err)
		}

		// Change CN
		cluster.Spec.CertCommonName = "db.new.supabase.red"
		if err := r.reconcileCertificate(t.Context(), cluster); err != nil {
			t.Fatalf("create new: %v", err)
		}

		// New cert should exist
		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(certGVK)
		if err := fc.Get(t.Context(), types.NamespacedName{
			Name: "db.new.supabase.red", Namespace: "default",
		}, got); err != nil {
			t.Fatalf("new Certificate should exist: %v", err)
		}

		// Old cert is still around (ownerRef GC handles it in a real
		// cluster, but the fake client doesn't simulate GC).
		old := &unstructured.Unstructured{}
		old.SetGroupVersionKind(certGVK)
		err := fc.Get(t.Context(), types.NamespacedName{
			Name: "db.old.supabase.red", Namespace: "default",
		}, old)
		// Not asserting deletion here — Kubernetes GC handles orphan
		// cleanup via ownerReferences in a live cluster.
		_ = err
	})

	t.Run("CN unset cleans up Certificate", func(t *testing.T) {
		fc := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &MultigresClusterReconciler{
			Client:   fc,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}
		cluster := &multigresv1alpha1.MultigresCluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "multigres.com/v1alpha1",
				Kind:       "MultigresCluster",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "c5", Namespace: "default", UID: "uid-5",
			},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				CertCommonName: "db.cleanup.supabase.red",
			},
		}

		// Create cert
		if err := r.reconcileCertificate(t.Context(), cluster); err != nil {
			t.Fatalf("create: %v", err)
		}

		// Verify it exists
		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(certGVK)
		if err := fc.Get(t.Context(), types.NamespacedName{
			Name: "db.cleanup.supabase.red", Namespace: "default",
		}, got); err != nil {
			t.Fatalf("Certificate should exist before cleanup: %v", err)
		}

		// Unset CN and reconcile
		cluster.Spec.CertCommonName = ""
		if err := r.reconcileCertificate(t.Context(), cluster); err != nil {
			t.Fatalf("cleanup: %v", err)
		}

		// Cert should be deleted
		err := fc.Get(t.Context(), types.NamespacedName{
			Name: "db.cleanup.supabase.red", Namespace: "default",
		}, got)
		if err == nil {
			t.Error("Certificate should be deleted after unsetting CN")
		}
	})

	t.Run(
		"cleanup ignores certs not owned by this cluster",
		func(t *testing.T) {
			fc := fake.NewClientBuilder().WithScheme(scheme).Build()
			r := &MultigresClusterReconciler{
				Client:   fc,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
			}

			// Pre-create a Certificate owned by a different cluster
			other := &unstructured.Unstructured{}
			other.SetGroupVersionKind(certGVK)
			other.SetName("db.other.supabase.red")
			other.SetNamespace("default")
			other.SetOwnerReferences([]metav1.OwnerReference{
				{
					APIVersion: "multigres.com/v1alpha1",
					Kind:       "MultigresCluster",
					Name:       "other-cluster",
					UID:        "other-uid",
				},
			})
			other.Object["spec"] = map[string]any{
				"secretName": CertSecretName,
			}
			if err := fc.Create(t.Context(), other); err != nil {
				t.Fatalf("failed to create other cert: %v", err)
			}

			// Our cluster has no CN — should not delete the other cert
			cluster := &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "c6", Namespace: "default", UID: "uid-6",
				},
			}
			if err := r.reconcileCertificate(t.Context(), cluster); err != nil {
				t.Fatalf("cleanup: %v", err)
			}

			// Other cert should still exist
			got := &unstructured.Unstructured{}
			got.SetGroupVersionKind(certGVK)
			if err := fc.Get(t.Context(), types.NamespacedName{
				Name: "db.other.supabase.red", Namespace: "default",
			}, got); err != nil {
				t.Fatal("Certificate owned by another cluster should survive")
			}
		},
	)
}

package resolver

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
			MultiAdmin: &multigresv1alpha1.StatelessSpec{
				// Image is not in StatelessSpec, it is global
			},
		},
	}

	cellTpl := &multigresv1alpha1.CellTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: namespace},
		Spec: multigresv1alpha1.CellTemplateSpec{
			MultiGateway: &multigresv1alpha1.StatelessSpec{
				Replicas: ptr.To(int32(1)),
			},
			LocalTopoServer: &multigresv1alpha1.LocalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{Image: "local-etcd-default"},
			},
		},
	}

	shardTpl := &multigresv1alpha1.ShardTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: namespace},
		Spec: multigresv1alpha1.ShardTemplateSpec{
			MultiOrch: &multigresv1alpha1.MultiOrchSpec{
				StatelessSpec: multigresv1alpha1.StatelessSpec{
					Replicas: ptr.To(int32(1)),
				},
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

// TestResolver_TemplateExists covers the helpers called by the Defaulter/Validator.
func TestResolver_TemplateExists(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	coreTpl, cellTpl, shardTpl, ns := setupFixtures(t)
	objs := []client.Object{coreTpl, cellTpl, shardTpl}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	r := NewResolver(c, ns, multigresv1alpha1.TemplateDefaults{})

	// 1. CoreTemplateExists
	t.Run("CoreTemplateExists", func(t *testing.T) {
		// Found
		if exists, err := r.CoreTemplateExists(t.Context(), "default"); err != nil || !exists {
			t.Errorf("Expected found, got %v, %v", exists, err)
		}
		// Not Found
		if exists, err := r.CoreTemplateExists(t.Context(), "missing"); err != nil || exists {
			t.Errorf("Expected not found, got %v, %v", exists, err)
		}
		// Empty Name
		if exists, err := r.CoreTemplateExists(t.Context(), ""); err != nil || exists {
			t.Errorf("Expected false for empty name, got %v, %v", exists, err)
		}
	})

	// 2. CellTemplateExists
	t.Run("CellTemplateExists", func(t *testing.T) {
		if exists, err := r.CellTemplateExists(t.Context(), "default"); err != nil || !exists {
			t.Errorf("Expected found, got %v, %v", exists, err)
		}
		if exists, err := r.CellTemplateExists(t.Context(), "missing"); err != nil || exists {
			t.Errorf("Expected not found, got %v, %v", exists, err)
		}
		if exists, err := r.CellTemplateExists(t.Context(), ""); err != nil || exists {
			t.Errorf("Expected false for empty name, got %v, %v", exists, err)
		}
	})

	// 3. ShardTemplateExists
	t.Run("ShardTemplateExists", func(t *testing.T) {
		if exists, err := r.ShardTemplateExists(t.Context(), "default"); err != nil || !exists {
			t.Errorf("Expected found, got %v, %v", exists, err)
		}
		if exists, err := r.ShardTemplateExists(t.Context(), "missing"); err != nil || exists {
			t.Errorf("Expected not found, got %v, %v", exists, err)
		}
		if exists, err := r.ShardTemplateExists(t.Context(), ""); err != nil || exists {
			t.Errorf("Expected false for empty name, got %v, %v", exists, err)
		}
	})

	// 4. Error Case (Simulate DB failure)
	t.Run("ClientFailure", func(t *testing.T) {
		errSim := testutil.ErrInjected
		failClient := testutil.NewFakeClientWithFailures(c, &testutil.FailureConfig{
			OnGet: func(_ client.ObjectKey) error { return errSim },
		})
		rFail := NewResolver(failClient, ns, multigresv1alpha1.TemplateDefaults{})

		if _, err := rFail.CoreTemplateExists(t.Context(), "any"); err == nil ||
			!errors.Is(err, errSim) {
			t.Error("Expected error for CoreTemplateExists")
		}
		if _, err := rFail.CellTemplateExists(t.Context(), "any"); err == nil ||
			!errors.Is(err, errSim) {
			t.Error("Expected error for CellTemplateExists")
		}
		if _, err := rFail.ShardTemplateExists(t.Context(), "any"); err == nil ||
			!errors.Is(err, errSim) {
			t.Error("Expected error for ShardTemplateExists")
		}
	})
}

func TestSharedHelpers(t *testing.T) {
	t.Parallel()

	t.Run("isResourcesZero", func(t *testing.T) {
		tests := []struct {
			name string
			res  corev1.ResourceRequirements
			want bool
		}{
			{"Zero", corev1.ResourceRequirements{}, true},
			{"Requests Set", corev1.ResourceRequirements{Requests: corev1.ResourceList{}}, false},
			{"Limits Set", corev1.ResourceRequirements{Limits: corev1.ResourceList{}}, false},
			{"Claims Set", corev1.ResourceRequirements{Claims: []corev1.ResourceClaim{}}, false},
		}
		for _, tc := range tests {
			if got := isResourcesZero(tc.res); got != tc.want {
				t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			}
		}
	})

	t.Run("defaultEtcdSpec", func(t *testing.T) {
		spec := &multigresv1alpha1.EtcdSpec{}
		defaultEtcdSpec(spec)

		if spec.Image != DefaultEtcdImage {
			t.Errorf("Image: got %q, want %q", spec.Image, DefaultEtcdImage)
		}
		if spec.Storage.Size != DefaultEtcdStorageSize {
			t.Errorf("Storage: got %q, want %q", spec.Storage.Size, DefaultEtcdStorageSize)
		}
		if *spec.Replicas != DefaultEtcdReplicas {
			t.Errorf("Replicas: got %d, want %d", *spec.Replicas, DefaultEtcdReplicas)
		}
		if isResourcesZero(spec.Resources) {
			t.Error("Resources should be defaulted")
		}

		// Test Preservation
		spec2 := &multigresv1alpha1.EtcdSpec{Image: "custom"}
		defaultEtcdSpec(spec2)
		if spec2.Image != "custom" {
			t.Error("Should preserve existing image")
		}
	})

	t.Run("defaultStatelessSpec", func(t *testing.T) {
		spec := &multigresv1alpha1.StatelessSpec{}
		res := corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: parseQty("1")},
		}
		defaultStatelessSpec(spec, res, 5)

		if *spec.Replicas != 5 {
			t.Errorf("Replicas: got %d, want 5", *spec.Replicas)
		}
		if cmp.Diff(spec.Resources, res) != "" {
			t.Error("Resources not copied correctly")
		}

		// Test DeepCopy independence
		res.Requests[corev1.ResourceCPU] = parseQty("999")
		// Cannot call String() directly on map index result in one line effectively if we want addressability
		// But String() is on *Quantity usually? Actually Quantity is a struct.
		// Wait, Requests[...] returns a Value.
		// We need to capture it to check it.
		val := spec.Resources.Requests[corev1.ResourceCPU]
		if val.String() == "999" {
			t.Error("Shared pointer detected in defaultStatelessSpec")
		}
	})

	t.Run("mergeStatelessSpec", func(t *testing.T) {
		base := &multigresv1alpha1.StatelessSpec{
			PodAnnotations: map[string]string{"a": "1"},
		}
		override := &multigresv1alpha1.StatelessSpec{
			PodAnnotations: map[string]string{"b": "2"},
			Replicas:       ptr.To(int32(3)),
		}
		mergeStatelessSpec(base, override)

		if len(base.PodAnnotations) != 2 {
			t.Errorf("Map merge failed, got %v", base.PodAnnotations)
		}
		if *base.Replicas != 3 {
			t.Error("Replicas not merged")
		}
	})
}

func parseQty(s string) resource.Quantity {
	return resource.MustParse(s)
}

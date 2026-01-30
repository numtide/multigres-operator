package multigrescluster

import (
	"testing"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

func TestBuildGlobalTopoServer(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
			UID:       "cluster-uid",
		},
	}

	t.Run("Etcd Enabled", func(t *testing.T) {
		spec := &multigresv1alpha1.GlobalTopoServerSpec{
			Etcd: &multigresv1alpha1.EtcdSpec{
				Image:    "etcd:latest",
				Replicas: ptr.To(int32(3)),
			},
		}

		got, err := BuildGlobalTopoServer(cluster, spec, scheme)
		if err != nil {
			t.Fatalf("BuildGlobalTopoServer() error = %v", err)
		}

		if got == nil {
			t.Fatal("Expected TopoServer, got nil")
		}
		if got.Name != "my-cluster-global-topo" {
			t.Errorf("Name = %v, want %v", got.Name, "my-cluster-global-topo")
		}
		if got.Spec.Etcd.Image != "etcd:latest" {
			t.Errorf("Image = %v, want %v", got.Spec.Etcd.Image, "etcd:latest")
		}
		// Verify OwnerReference
		if len(got.OwnerReferences) != 1 {
			t.Errorf("OwnerReferences count = %v, want 1", len(got.OwnerReferences))
		} else if got.OwnerReferences[0].Name != "my-cluster" {
			t.Errorf("OwnerReference Name = %v, want %v", got.OwnerReferences[0].Name, "my-cluster")
		}
	})

	t.Run("Etcd Disabled (External)", func(t *testing.T) {
		spec := &multigresv1alpha1.GlobalTopoServerSpec{
			Etcd: nil, // Simulating external mode where Etcd spec is nil
		}

		got, err := BuildGlobalTopoServer(cluster, spec, scheme)
		if err != nil {
			t.Fatalf("BuildGlobalTopoServer() error = %v", err)
		}
		if got != nil {
			t.Errorf("Expected nil when Etcd spec is nil, got %v", got)
		}
	})

	t.Run("ControllerRefError", func(t *testing.T) {
		emptyScheme := runtime.NewScheme()
		spec := &multigresv1alpha1.GlobalTopoServerSpec{
			Etcd: &multigresv1alpha1.EtcdSpec{Image: "img"},
		}
		_, err := BuildGlobalTopoServer(cluster, spec, emptyScheme)
		if err == nil {
			t.Error("Expected error due to missing scheme types, got nil")
		}
	})
}

func TestBuildMultiAdminDeployment(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
			UID:       "cluster-uid",
		},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			Images: multigresv1alpha1.ClusterImages{
				MultiAdmin: "multiadmin:latest",
			},
		},
	}

	spec := &multigresv1alpha1.StatelessSpec{
		Replicas:       ptr.To(int32(2)),
		PodLabels:      map[string]string{"custom": "label"},
		PodAnnotations: map[string]string{"anno": "tation"},
	}

	t.Run("Success", func(t *testing.T) {
		got, err := BuildMultiAdminDeployment(cluster, spec, scheme)
		if err != nil {
			t.Fatalf("BuildMultiAdminDeployment() error = %v", err)
		}

		if got.Name != "my-cluster-multiadmin" {
			t.Errorf("Name = %v, want %v", got.Name, "my-cluster-multiadmin")
		}
		if *got.Spec.Replicas != 2 {
			t.Errorf("Replicas = %v, want 2", *got.Spec.Replicas)
		}
		if got.Spec.Template.Labels["custom"] != "label" {
			t.Errorf("PodLabels missing custom label")
		}
		if got.Spec.Template.Annotations["anno"] != "tation" {
			t.Errorf("PodAnnotations missing annotation")
		}

		// Verify container image from cluster spec
		if len(got.Spec.Template.Spec.Containers) > 0 {
			if got.Spec.Template.Spec.Containers[0].Image != "multiadmin:latest" {
				t.Errorf(
					"Container Image = %v, want multiadmin:latest",
					got.Spec.Template.Spec.Containers[0].Image,
				)
			}
		}

		// Verify Selector does NOT contain mutable labels
		selector := got.Spec.Selector.MatchLabels
		if _, ok := selector["app.kubernetes.io/name"]; ok {
			t.Error("Selector should not contain app.kubernetes.io/name")
		}
		if _, ok := selector["app.kubernetes.io/managed-by"]; ok {
			t.Error("Selector should not contain app.kubernetes.io/managed-by")
		}
		if _, ok := selector["app.kubernetes.io/component"]; !ok {
			t.Error("Selector MUST contain app.kubernetes.io/component")
		}

		// Verify OwnerReference
		if len(got.OwnerReferences) != 1 {
			t.Errorf("OwnerReferences count = %v, want 1", len(got.OwnerReferences))
		}
	})

	t.Run("ControllerRefError", func(t *testing.T) {
		emptyScheme := runtime.NewScheme()
		_, err := BuildMultiAdminDeployment(cluster, spec, emptyScheme)
		if err == nil {
			t.Error("Expected error due to missing scheme types, got nil")
		}
	})
}

func TestBuildMultiAdminWebDeployment(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
			UID:       "cluster-uid",
		},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			Images: multigresv1alpha1.ClusterImages{
				MultiAdminWeb: "multiadmin-web:latest",
			},
		},
	}

	spec := &multigresv1alpha1.StatelessSpec{
		Replicas:       ptr.To(int32(2)),
		PodLabels:      map[string]string{"custom": "label"},
		PodAnnotations: map[string]string{"anno": "tation"},
	}

	t.Run("Success", func(t *testing.T) {
		got, err := BuildMultiAdminWebDeployment(cluster, spec, scheme)
		if err != nil {
			t.Fatalf("BuildMultiAdminWebDeployment() error = %v", err)
		}

		if got.Name != "my-cluster-multiadmin-web" {
			t.Errorf("Name = %v, want %v", got.Name, "my-cluster-multiadmin-web")
		}
		if *got.Spec.Replicas != 2 {
			t.Errorf("Replicas = %v, want 2", *got.Spec.Replicas)
		}
		if got.Spec.Template.Labels["custom"] != "label" {
			t.Errorf("PodLabels missing custom label")
		}
		if got.Spec.Template.Annotations["anno"] != "tation" {
			t.Errorf("PodAnnotations missing annotation")
		}

		// Verify container image from cluster spec
		if len(got.Spec.Template.Spec.Containers) > 0 {
			if got.Spec.Template.Spec.Containers[0].Image != "multiadmin-web:latest" {
				t.Errorf(
					"Container Image = %v, want multiadmin-web:latest",
					got.Spec.Template.Spec.Containers[0].Image,
				)
			}
		}

		// Verify Selector does NOT contain mutable labels
		selector := got.Spec.Selector.MatchLabels
		if _, ok := selector["app.kubernetes.io/name"]; ok {
			t.Error("Selector should not contain app.kubernetes.io/name")
		}
		if _, ok := selector["app.kubernetes.io/managed-by"]; ok {
			t.Error("Selector should not contain app.kubernetes.io/managed-by")
		}
		if _, ok := selector["app.kubernetes.io/component"]; !ok {
			t.Error("Selector MUST contain app.kubernetes.io/component")
		}

		// Verify OwnerReference
		if len(got.OwnerReferences) != 1 {
			t.Errorf("OwnerReferences count = %v, want 1", len(got.OwnerReferences))
		}
	})

	t.Run("ControllerRefError", func(t *testing.T) {
		emptyScheme := runtime.NewScheme()
		_, err := BuildMultiAdminWebDeployment(cluster, spec, emptyScheme)
		if err == nil {
			t.Error("Expected error due to missing scheme types, got nil")
		}
	})
}

func TestBuildMultiAdminWebService(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
			UID:       "cluster-uid",
		},
	}

	t.Run("Success", func(t *testing.T) {
		got, err := BuildMultiAdminWebService(cluster, scheme)
		if err != nil {
			t.Fatalf("BuildMultiAdminWebService() error = %v", err)
		}

		if got.Name != "my-cluster-multiadmin-web" {
			t.Errorf("Name = %v, want %v", got.Name, "my-cluster-multiadmin-web")
		}

		// Verify OwnerReference
		if len(got.OwnerReferences) != 1 {
			t.Errorf("OwnerReferences count = %v, want 1", len(got.OwnerReferences))
		}
	})

	t.Run("ControllerRefError", func(t *testing.T) {
		emptyScheme := runtime.NewScheme()
		_, err := BuildMultiAdminWebService(cluster, emptyScheme)
		if err == nil {
			t.Error("Expected error due to missing scheme types, got nil")
		}
	})
}

func TestBuildMultiAdminService(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
			UID:       "cluster-uid",
		},
	}

	t.Run("Success", func(t *testing.T) {
		got, err := BuildMultiAdminService(cluster, scheme)
		if err != nil {
			t.Fatalf("BuildMultiAdminService() error = %v", err)
		}

		if got.Name != "my-cluster-multiadmin" {
			t.Errorf("Name = %v, want %v", got.Name, "my-cluster-multiadmin")
		}

		// Verify OwnerReference
		if len(got.OwnerReferences) != 1 {
			t.Errorf("OwnerReferences count = %v, want 1", len(got.OwnerReferences))
		}
	})

	t.Run("ControllerRefError", func(t *testing.T) {
		emptyScheme := runtime.NewScheme()
		_, err := BuildMultiAdminService(cluster, emptyScheme)
		if err == nil {
			t.Error("Expected error due to missing scheme types, got nil")
		}
	})
}

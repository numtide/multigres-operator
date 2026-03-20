package multigrescluster

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
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

		// Verify env vars
		envVars := got.Spec.Template.Spec.Containers[0].Env
		wantEnv := map[string]string{
			"MULTIADMIN_API_URL": fmt.Sprintf("http://%s-multiadmin:18000", cluster.Name),
			"POSTGRES_HOST":      fmt.Sprintf("%s-multigateway", cluster.Name),
			"POSTGRES_PORT":      "15432",
			"POSTGRES_DATABASE":  "postgres",
			"POSTGRES_USER":      "postgres",
		}
		for wantName, wantValue := range wantEnv {
			found := false
			for _, ev := range envVars {
				if ev.Name == wantName {
					found = true
					if ev.Value != wantValue {
						t.Errorf("Env %s = %q, want %q", wantName, ev.Value, wantValue)
					}
					break
				}
			}
			if !found {
				t.Errorf("Missing env var %s", wantName)
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

func TestBuildMultiGatewayGlobalService(t *testing.T) {
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

	// Expected labels on every produced Service (standard + cluster label).
	wantLabels := map[string]string{
		"app.kubernetes.io/name":       "multigres",
		"app.kubernetes.io/instance":   "my-cluster",
		"app.kubernetes.io/component":  "multigateway",
		"app.kubernetes.io/part-of":    "multigres",
		"app.kubernetes.io/managed-by": "multigres-operator",
		"multigres.com/cluster":        "my-cluster",
	}

	wantPort := corev1.ServicePort{
		Name:       "postgres",
		Port:       15432,
		TargetPort: intstr.FromString("postgres"),
		Protocol:   corev1.ProtocolTCP,
	}

	tests := []struct {
		name            string
		extGw           *multigresv1alpha1.ExternalGatewayConfig
		wantType        corev1.ServiceType
		wantAnnotations map[string]string // nil means no gateway annotations expected
		wantExternalIPs []string
	}{
		{
			name:     "nil config → ClusterIP, no gateway annotations",
			extGw:    nil,
			wantType: corev1.ServiceTypeClusterIP,
		},
		{
			name:     "Enabled: false → ClusterIP, no gateway annotations",
			extGw:    &multigresv1alpha1.ExternalGatewayConfig{Enabled: false},
			wantType: corev1.ServiceTypeClusterIP,
		},
		{
			name:     "Enabled: true, no annotations → ClusterIP",
			extGw:    &multigresv1alpha1.ExternalGatewayConfig{Enabled: true},
			wantType: corev1.ServiceTypeClusterIP,
		},
		{
			name: "Enabled: true, with annotations → ClusterIP, annotations applied",
			extGw: &multigresv1alpha1.ExternalGatewayConfig{
				Enabled: true,
				Annotations: map[string]string{
					"service.beta.kubernetes.io/aws-load-balancer-scheme": "internet-facing",
					"service.beta.kubernetes.io/aws-load-balancer-type":   "external",
				},
			},
			wantType: corev1.ServiceTypeClusterIP,
			wantAnnotations: map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-scheme": "internet-facing",
				"service.beta.kubernetes.io/aws-load-balancer-type":   "external",
			},
		},
		{
			name: "Enabled: true, annotations with label-prefix keys → labels unchanged",
			extGw: &multigresv1alpha1.ExternalGatewayConfig{
				Enabled: true,
				Annotations: map[string]string{
					"app.kubernetes.io/custom-annotation": "should-not-overwrite-labels",
					"multigres.com/some-annotation":       "also-should-not-overwrite",
				},
			},
			wantType: corev1.ServiceTypeClusterIP,
			wantAnnotations: map[string]string{
				"app.kubernetes.io/custom-annotation": "should-not-overwrite-labels",
				"multigres.com/some-annotation":       "also-should-not-overwrite",
			},
		},
		{
			name: "Enabled: true, with external IPs",
			extGw: &multigresv1alpha1.ExternalGatewayConfig{
				Enabled:     true,
				ExternalIPs: []string{"2001:db8::10"},
			},
			wantType:        corev1.ServiceTypeClusterIP,
			wantExternalIPs: []string{"2001:db8::10"},
		},
		{
			name:     "Disabled after previously enabled → ClusterIP, no gateway annotations",
			extGw:    &multigresv1alpha1.ExternalGatewayConfig{Enabled: false},
			wantType: corev1.ServiceTypeClusterIP,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BuildMultiGatewayGlobalService(cluster, tc.extGw, scheme)
			require.NoError(t, err)

			// Name and namespace
			assert.Equal(t, "my-cluster-multigateway", got.Name)
			assert.Equal(t, "default", got.Namespace)

			// Service type
			assert.Equal(t, tc.wantType, got.Spec.Type)
			assert.Equal(t, tc.wantExternalIPs, got.Spec.ExternalIPs)

			// Port 15432 invariant
			require.Len(t, got.Spec.Ports, 1)
			assert.Equal(t, wantPort, got.Spec.Ports[0])

			// Labels preserved
			assert.Equal(t, wantLabels, got.Labels)

			// Annotations
			if tc.wantAnnotations != nil {
				for k, v := range tc.wantAnnotations {
					assert.Equal(t, v, got.Annotations[k], "annotation %s", k)
				}
			} else {
				// No gateway annotations expected; annotations should be nil or empty
				assert.Empty(t, got.Annotations)
			}

			// Selector: component + instance, no cell label
			assert.Equal(t, "multigateway", got.Spec.Selector["app.kubernetes.io/component"])
			assert.Equal(t, "my-cluster", got.Spec.Selector["app.kubernetes.io/instance"])
			assert.NotContains(t, got.Spec.Selector, "multigres.com/cell")

			// Owner reference
			require.Len(t, got.OwnerReferences, 1)
			assert.Equal(t, "my-cluster", got.OwnerReferences[0].Name)
		})
	}

	t.Run("Annotation removal on disable", func(t *testing.T) {
		// Build with annotations enabled
		enabledCfg := &multigresv1alpha1.ExternalGatewayConfig{
			Enabled: true,
			Annotations: map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-scheme": "internet-facing",
			},
		}
		enabled, err := BuildMultiGatewayGlobalService(cluster, enabledCfg, scheme)
		require.NoError(t, err)
		assert.Equal(t, corev1.ServiceTypeClusterIP, enabled.Spec.Type)
		assert.Equal(
			t,
			"internet-facing",
			enabled.Annotations["service.beta.kubernetes.io/aws-load-balancer-scheme"],
		)

		// Build with disabled config; previously-set gateway annotations absent
		disabledCfg := &multigresv1alpha1.ExternalGatewayConfig{Enabled: false}
		disabled, err := BuildMultiGatewayGlobalService(cluster, disabledCfg, scheme)
		require.NoError(t, err)
		assert.Equal(t, corev1.ServiceTypeClusterIP, disabled.Spec.Type)
		assert.Empty(t, disabled.Annotations)
	})

	t.Run("ControllerRefError", func(t *testing.T) {
		emptyScheme := runtime.NewScheme()
		_, err := BuildMultiGatewayGlobalService(cluster, nil, emptyScheme)
		assert.Error(t, err)
	})
}

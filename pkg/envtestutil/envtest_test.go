//go:build integration
// +build integration

package envtestutil_test

import (
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"github.com/numtide/multigres-operator/pkg/envtestutil"
)

func TestSetUpEnvTest(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		opts []envtestutil.EnvtestOption
	}{
		"default - with cleanup": {
			opts: nil,
		},
		"with kubeconfig": {
			opts: []envtestutil.EnvtestOption{envtestutil.WithKubeconfig()},
		},
		"with CRD path": {
			opts: []envtestutil.EnvtestOption{envtestutil.WithCRDPaths(
				filepath.Join("../../", "config", "crd", "bases"),
			)},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)

			cfg := envtestutil.SetUpEnvtest(t, tc.opts...)
			mgr := envtestutil.SetUpManager(t, cfg, scheme)

			if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
				t.Fatalf("Failed to set up health check, %v", err)
			}
			if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
				t.Fatalf("Failed to set up ready check, %v", err)
			}

			envtestutil.StartManager(t, mgr)
		})
	}
}

func TestSetUpClient(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	cfg := envtestutil.SetUpEnvtest(t)
	client := envtestutil.SetUpClient(t, cfg, scheme)

	if client == nil {
		t.Fatal("SetUpClient() returned nil")
	}

	// Verify client works by listing services
	svcList := &corev1.ServiceList{}
	if err := client.List(t.Context(), svcList); err != nil {
		t.Errorf("Client.List() failed: %v", err)
	}

	// Create and retrieve a service (tests direct API server access without cache)
	svc := &corev1.Service{
		ObjectMeta: envtestutil.Obj[corev1.Service]("test-svc", "default").ObjectMeta,
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
	}
	if err := client.Create(t.Context(), svc); err != nil {
		t.Fatalf("Client.Create() failed: %v", err)
	}

	retrieved := &corev1.Service{}
	objKey := types.NamespacedName{Name: "test-svc", Namespace: "default"}
	if err := client.Get(t.Context(), objKey, retrieved); err != nil {
		t.Errorf("Client.Get() failed: %v", err)
	}

	if retrieved.Name != "test-svc" {
		t.Errorf("Retrieved service Name = %s, want test-svc", retrieved.Name)
	}
}

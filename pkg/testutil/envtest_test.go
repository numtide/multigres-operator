//go:build integration
// +build integration

package testutil_test

import (
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"github.com/numtide/multigres-operator/pkg/testutil"
)

func TestSetUpEnvTest(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		opts []testutil.EnvtestOption
	}{
		"default - with cleanup": {
			opts: nil,
		},
		"with kubeconfig": {
			opts: []testutil.EnvtestOption{testutil.WithKubeconfig()},
		},
		"with CRD path": {
			opts: []testutil.EnvtestOption{testutil.WithCRDPaths(
				filepath.Join("../../", "config", "crd", "bases"),
			)},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)

			cfg := testutil.SetUpEnvtest(t, tc.opts...)
			mgr := testutil.SetUpManager(t, cfg, scheme)

			if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
				t.Fatalf("Failed to set up health check, %v", err)
			}
			if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
				t.Fatalf("Failed to set up ready check, %v", err)
			}

			testutil.StartManager(t, mgr)
		})
	}
}

func TestSetUpClient(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	cfg := testutil.SetUpEnvtest(t)
	client := testutil.SetUpClient(t, cfg, scheme)

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
		ObjectMeta: testutil.Obj[corev1.Service]("test-svc", "default").ObjectMeta,
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

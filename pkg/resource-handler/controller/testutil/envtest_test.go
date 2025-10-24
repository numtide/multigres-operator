//go:build integration
// +build integration

package testutil_test

import (
	"testing"

	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/testutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

func TestSetupEnvTest(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	cfg := testutil.SetUpEnvtest(t)
	mgr := testutil.SetUpManager(t, cfg, scheme)

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		t.Fatalf("Failed to set up health check, %v", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		t.Fatalf("Failed to set up ready check, %v", err)
	}

	testutil.StartManager(t, mgr)
}

func TestSetupEnvTestWithKubeconfig(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	cfg, shutdown := testutil.SetUpEnvtestWithKubeconfig(t)
	defer shutdown()
	mgr := testutil.SetUpManager(t, cfg, scheme)

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		t.Fatalf("Failed to set up health check, %v", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		t.Fatalf("Failed to set up ready check, %v", err)
	}

	testutil.StartManager(t, mgr)
}

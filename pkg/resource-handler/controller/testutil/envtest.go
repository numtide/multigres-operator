package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func SetUpEnvtest(t testing.TB) *rest.Config {
	t.Helper()

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join(
			// Go back up to repo root
			"../../../../",
			"config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("Setting up with envtest failed, %v", err)
	}
	t.Cleanup(func() {
		if err := testEnv.Stop(); err != nil {
			t.Fatalf("Failed to stop envtest, %v", err)
		}
	})

	return cfg
}

// SetUpEnvtestWithKubeconfig starts Kubernetes API server for testing, and
// keeps it running for further debugging.
func SetUpEnvtestWithKubeconfig(t testing.TB) (*rest.Config, func()) {
	t.Helper()

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join(
			// Go back up to repo root
			"../../../../",
			"config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("Setting up with envtest failed, %v", err)
	}
	// Purposely no clean-up to run so that the envtest setup can be debugged
	// further.

	// Write kubeconfig to file
	user, err := testEnv.ControlPlane.AddUser(envtest.User{
		Name:   "envtest-admin",
		Groups: []string{"system:masters"},
	}, nil)
	if err != nil {
		t.Fatalf("Failed to add user for testing, %v", err)
	}

	kubeconfig, err := user.KubeConfig()
	if err != nil {
		t.Fatalf("Failed to generate kubeconfig, %v", err)
	}

	kubeconfigPath := filepath.Join(os.TempDir(), "envtest-kubeconfig")
	if err := os.WriteFile(kubeconfigPath, kubeconfig, 0o644); err != nil {
		t.Fatalf("Failed to write kubeconfig to file, %v", err)
	}

	t.Cleanup(func() {
		fmt.Printf("Kubeconfig written to: %s\n", kubeconfigPath)
		fmt.Printf("Connect with: export KUBECONFIG=%s\n", kubeconfigPath)
	})

	return cfg, func() {
		if err := testEnv.Stop(); err != nil {
			t.Fatalf("Failed to stop envtest, %v", err)
		}
	}
}

// SetUpClient creates a direct Kubernetes client (non-cached).
//
// IMPORTANT: This creates a client that bypasses the manager's cache and reads
// directly from the API server. This is different from mgr.GetClient() which
// uses cached reads (same as controllers).
//
// When to use SetUpClient:
//
// 1. Testing cache synchronization:
//   - Verify what's actually in the API server vs what the cache sees
//   - Useful when debugging "why doesn't my controller see this resource?"
//
// 2. Strong consistency requirements:
//
//   - Need immediate reads after writes (no cache lag)
//
//   - Testing race conditions or timing-sensitive behavior
//
//     3. Comparing cached vs direct reads:
//     directClient := SetUpClient(t, cfg, scheme)
//     cachedClient := mgr.GetClient()
//     // Create resource
//     cachedClient.Create(ctx, obj)
//     // Direct read (guaranteed to see it)
//     directClient.Get(ctx, key, &actual)
//     // Cached read (might lag slightly)
//     cachedClient.Get(ctx, key, &fromCache)
//
// For most tests, use mgr.GetClient() instead - it tests what controllers actually see.
func SetUpClient(t testing.TB, cfg *rest.Config, scheme *runtime.Scheme) client.Client {
	t.Helper()

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("Failed to setup a Kubernetes client: %v", err)
	}

	return k8sClient
}

func SetUpManager(t testing.TB, cfg *rest.Config, scheme *runtime.Scheme) manager.Manager {
	t.Helper()

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:         scheme,
		LeaderElection: false,

		// WebhookServer:  webhook.NewServer(webhook.Options{
		// 	// Host:    webhookInstallOptions.LocalServingHost,
		// 	// Port:    webhookInstallOptions.LocalServingPort,
		// 	// CertDir: webhookInstallOptions.LocalServingCertDir,
		// }),
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	if err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	return mgr
}

func StartManager(t testing.TB, mgr manager.Manager) {
	t.Helper()

	// t.Context gets cancelled before the test cleanup function runs.
	ctx := t.Context()
	go func() {
		if err := mgr.Start(ctx); err != nil {
			t.Errorf("Manager failed: %v", err)
		}
	}()

	// Wait for cache to sync
	if !mgr.GetCache().WaitForCacheSync(ctx) {
		t.Fatal("Cache failed to sync")
	}
}

// SetUpEnvtestManager is a convenience function that combines SetUpEnvtest,
// SetUpManager, and StartManager into a single call.
//
// This is the recommended way to set up integration tests:
//
//	mgr := testutil.SetUpEnvtestManager(t, scheme)
//	c := mgr.GetClient()
//
//	// Setup your controller
//	reconciler := &YourReconciler{Client: c, Scheme: scheme}
//	reconciler.SetupWithManager(mgr)
//
// For more control, use the individual functions instead.
func SetUpEnvtestManager(t testing.TB, scheme *runtime.Scheme) manager.Manager {
	t.Helper()

	cfg := SetUpEnvtest(t)
	mgr := SetUpManager(t, cfg, scheme)
	StartManager(t, mgr)

	return mgr
}

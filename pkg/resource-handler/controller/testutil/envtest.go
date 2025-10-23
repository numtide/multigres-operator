package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func SetupEnvtest(t testing.TB) *rest.Config {
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
		testEnv.Stop()
	})

	return cfg
}

// SetupEnvtestWithKubeconfig starts Kubernetes API server for testing, and
// keeps it running for further debugging.
func SetupEnvtestWithKubeconfig(t testing.TB) *rest.Config {
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
	if err := os.WriteFile(kubeconfigPath, kubeconfig, 0644); err != nil {
		t.Fatalf("Failed to write kubeconfig to file, %v", err)
	}

	t.Cleanup(func() {
		fmt.Printf("Kubeconfig written to: %s\n", kubeconfigPath)
		fmt.Printf("Connect with: export KUBECONFIG=%s\n", kubeconfigPath)
	})

	return cfg
}

func SetUpClient(t testing.TB, cfg *rest.Config, scheme *runtime.Scheme) client.Client {
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("Failed to setup a Kubernetes client: %v", err)
	}

	return k8sClient
}

func SetUpClientSet(t testing.TB, cfg *rest.Config) *kubernetes.Clientset {
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		fmt.Printf("Failed to setup kubernetes clientset %v", err)
	}
	return clientset
}

func StartManager(t testing.TB, cfg *rest.Config, scheme *runtime.Scheme) manager.Manager {
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

	return mgr
}

package testutil

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const (
	defaultKindCluster = "multigres-operator-test-e2e"
	defaultKubectl     = "kubectl"
)

// KindOption is a functional option for configuring kind-based test setup.
type KindOption func(*kindConfig)

type kindConfig struct {
	clusterName string
	kubectl     string
	crdPaths    []string
}

func defaultKindConfig() *kindConfig {
	clusterName := os.Getenv("KIND_CLUSTER")
	if clusterName == "" {
		clusterName = defaultKindCluster
	}

	kubectl := os.Getenv("KUBECTL")
	if kubectl == "" {
		kubectl = defaultKubectl
	}

	return &kindConfig{
		clusterName: clusterName,
		kubectl:     kubectl,
	}
}

// WithKindCluster sets the kind cluster name to connect to.
func WithKindCluster(name string) KindOption {
	return func(cfg *kindConfig) {
		cfg.clusterName = name
	}
}

// WithKindKubectl sets the path to the kubectl binary.
func WithKindKubectl(path string) KindOption {
	return func(cfg *kindConfig) {
		cfg.kubectl = path
	}
}

// WithKindCRDPaths sets the CRD directory paths to install via kubectl apply.
func WithKindCRDPaths(paths ...string) KindOption {
	return func(cfg *kindConfig) {
		cfg.crdPaths = paths
	}
}

// SetUpKind connects to an existing kind cluster and returns a rest.Config and
// an isolated test namespace. The kind cluster must already exist (e.g. created
// by Makefile's setup-test-e2e target).
//
// CRDs are installed via 'kubectl apply --server-side' if paths are specified.
// A test namespace with a random suffix is created for isolation and
// automatically deleted when the test finishes.
func SetUpKind(t testing.TB, opts ...KindOption) (*rest.Config, string) {
	t.Helper()

	cfg := defaultKindConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	// Get kubeconfig from kind cluster
	restCfg := getKindRESTConfig(t, cfg.clusterName)

	// Install CRDs if paths specified
	if len(cfg.crdPaths) > 0 {
		installCRDs(t, cfg.kubectl, cfg.crdPaths)
	}

	// Create isolated test namespace
	ns := createTestNamespace(t, restCfg)

	return restCfg, ns
}

// SetUpKindManager connects to an existing kind cluster, creates a
// namespace-scoped manager, and starts it. This combines SetUpKind, manager
// creation, and StartManager into a single call.
//
// The manager's cache is scoped to the test namespace via
// cache.Options.DefaultNamespaces, so controllers only see resources in the
// test namespace.
func SetUpKindManager(
	t testing.TB,
	scheme *runtime.Scheme,
	opts ...KindOption,
) (manager.Manager, string) {
	t.Helper()

	restCfg, ns := SetUpKind(t, opts...)
	mgr := setUpKindManager(t, restCfg, scheme, ns)
	StartManager(t, mgr)

	return mgr, ns
}

func getKindRESTConfig(t testing.TB, clusterName string) *rest.Config {
	t.Helper()

	out, err := exec.Command("kind", "get", "kubeconfig", "--name", clusterName).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("Failed to get kubeconfig from kind cluster %q: %v\nstderr: %s", clusterName, err, exitErr.Stderr)
		}
		t.Fatalf("Failed to get kubeconfig from kind cluster %q: %v", clusterName, err)
	}

	restCfg, err := clientcmd.RESTConfigFromKubeConfig(out)
	if err != nil {
		t.Fatalf("Failed to parse kubeconfig: %v", err)
	}

	return restCfg
}

func installCRDs(t testing.TB, kubectl string, paths []string) {
	t.Helper()

	for _, path := range paths {
		out, err := exec.Command(kubectl, "apply", "--server-side", "-f", path).CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to install CRDs from %q: %v\noutput: %s", path, err, out)
		}
	}
}

func createTestNamespace(t testing.TB, restCfg *rest.Config) string {
	t.Helper()

	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		t.Fatalf("Failed to create kubernetes clientset: %v", err)
	}

	ns := fmt.Sprintf("e2e-test-%s", randomSuffix())
	_, err = clientset.CoreV1().Namespaces().Create(
		context.Background(),
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		t.Fatalf("Failed to create test namespace %q: %v", ns, err)
	}

	t.Cleanup(func() {
		err := clientset.CoreV1().Namespaces().Delete(
			context.Background(),
			ns,
			metav1.DeleteOptions{},
		)
		if err != nil {
			t.Errorf("Failed to delete test namespace %q: %v", ns, err)
		}
	})

	return ns
}

func randomSuffix() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[rand.IntN(len(chars))]
	}
	return string(b)
}

func setUpKindManager(t testing.TB, restCfg *rest.Config, scheme *runtime.Scheme, ns string) manager.Manager {
	t.Helper()

	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme:         scheme,
		LeaderElection: false,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				ns: {},
			},
		},
	})
	if err != nil {
		t.Fatalf("Failed to set up manager: %v", err)
	}

	return mgr
}

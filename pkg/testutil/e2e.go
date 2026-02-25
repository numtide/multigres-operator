// e2e.go provides reusable infrastructure for end-to-end tests that run the
// multigres-operator inside a Kind cluster. Tests are black-box: they apply
// CRs via the Kubernetes API and assert on the resulting resources and pod
// states.
//
// The Makefile builds the operator container image and loads it into Kind
// BEFORE `go test` runs. The Go code does NOT call `make container`.
//
// # Cluster cleanup
//
// By default, Kind clusters are destroyed after each test (pass or fail).
// Set E2E_KEEP_CLUSTERS to control this:
//
//	E2E_KEEP_CLUSTERS=never   (default) always destroy clusters
//	E2E_KEEP_CLUSTERS=on-failure        keep clusters from failed tests
//	E2E_KEEP_CLUSTERS=always            never destroy clusters
//
// When a cluster is kept, the test logs the cluster name and the command
// to destroy it manually:
//
//	kind delete cluster --name <cluster-name>
package testutil

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"sigs.k8s.io/e2e-framework/klient"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/pkg/features"
	"sigs.k8s.io/e2e-framework/pkg/types"
	"sigs.k8s.io/e2e-framework/support/kind"
)

// MultigresImages are the container images required by a MultigresCluster.
// These must be loaded into the Kind cluster before running tests.
var MultigresImages = []string{
	"ghcr.io/multigres/multigres:main",
	"ghcr.io/multigres/pgctld:main",
	"ghcr.io/multigres/multiadmin-web:main",
	"gcr.io/etcd-development/etcd:v3.6.7",
}

// ---------------------------------------------------------------------------
// TestCluster — per-test Kind cluster with e2e-framework Environment
// ---------------------------------------------------------------------------

// TestCluster wraps a Kind cluster and an e2e-framework Environment scoped to
// a single test function. Call NewTestCluster inside each TestXxx and the
// cluster is created automatically and destroyed via t.Cleanup.
type TestCluster struct {
	t           *testing.T
	clusterName string
	env         types.Environment
	cfg         *envconf.Config
	restCfg     *rest.Config
	clientset   *kubernetes.Clientset
}

// E2EOption configures NewTestCluster behaviour.
type E2EOption func(*e2eOpts)

type e2eOpts struct {
	parallel        bool
	kindConfigPath  string
	images          []string
	setupFuncs      []env.Func
	finishFuncs     []env.Func
	clusterWaitTime time.Duration
}

func defaultE2EOpts() *e2eOpts {
	return &e2eOpts{
		parallel:        true,
		clusterWaitTime: 5 * time.Minute,
	}
}

// WithSequential disables t.Parallel() for this test.
func WithSequential() E2EOption { return func(o *e2eOpts) { o.parallel = false } }

// WithE2EKindConfig passes a Kind YAML config (e.g. multi-node).
func WithE2EKindConfig(path string) E2EOption {
	return func(o *e2eOpts) { o.kindConfigPath = path }
}

// WithImage preloads a container image into the Kind cluster.
func WithImage(img string) E2EOption {
	return func(o *e2eOpts) { o.images = append(o.images, img) }
}

// WithSetup adds extra env.Func that runs after cluster creation.
func WithSetup(fn env.Func) E2EOption {
	return func(o *e2eOpts) { o.setupFuncs = append(o.setupFuncs, fn) }
}

// WithFinish adds extra env.Func that runs during cleanup (before cluster destroy).
func WithFinish(fn env.Func) E2EOption {
	return func(o *e2eOpts) { o.finishFuncs = append(o.finishFuncs, fn) }
}

// WithClusterWait sets the timeout for waiting for cluster readiness.
func WithClusterWait(d time.Duration) E2EOption {
	return func(o *e2eOpts) { o.clusterWaitTime = d }
}

// NewTestCluster creates a dedicated Kind cluster for a single test.
//
// Usage:
//
//	func TestMyFeature(t *testing.T) {
//	    tc := testutil.NewTestCluster(t,
//	        testutil.WithImage("my-operator:latest"),
//	    )
//	    feat := features.New("reconcile").
//	        Setup(preset.SetupFunc(tc)).
//	        Assess("something", ...).Feature()
//	    tc.Test(t, feat)
//	}
//
// The cluster is destroyed automatically via t.Cleanup.
func NewTestCluster(t *testing.T, opts ...E2EOption) *TestCluster {
	t.Helper()
	o := defaultE2EOpts()
	for _, fn := range opts {
		fn(o)
	}
	if o.parallel {
		t.Parallel()
	}

	clusterName := sanitizeE2EName(fmt.Sprintf("e2e-%s-%d",
		strings.ToLower(t.Name()), time.Now().UnixNano()%100000))

	// Build setup chain.
	setupFuncs := []env.Func{
		envfuncs.CreateCluster(kind.NewProvider(), clusterName),
	}
	for _, img := range o.images {
		setupFuncs = append(setupFuncs, envfuncs.LoadImageToCluster(clusterName, img))
	}
	setupFuncs = append(setupFuncs, o.setupFuncs...)

	// Build finish chain (log export only — destroy is conditional).
	finishFuncs := append(o.finishFuncs,
		envfuncs.ExportClusterLogs(clusterName, fmt.Sprintf("/tmp/e2e-logs/%s", clusterName)),
	)

	tc := &TestCluster{
		t:           t,
		clusterName: clusterName,
	}

	// Execute setup inline (no TestMain required).
	tc.runSetup(setupFuncs)
	t.Cleanup(func() {
		tc.runFinish(finishFuncs)
		tc.maybeDestroyCluster()
	})

	return tc
}

// runSetup executes the setup functions inline.
func (tc *TestCluster) runSetup(funcs []env.Func) {
	tc.t.Helper()
	ctx := context.Background()
	cfg := envconf.New()
	var err error
	for _, fn := range funcs {
		ctx, err = fn(ctx, cfg)
		if err != nil {
			tc.t.Fatalf("e2e: setup failed: %v", err)
		}
	}
	tc.cfg = cfg

	// Build REST config + clientset from the kubeconfig that envfuncs populated.
	kubeconfigFile := cfg.KubeconfigFile()
	if kubeconfigFile != "" {
		tc.cfg = envconf.NewWithKubeConfig(kubeconfigFile)
	}
	client, err := tc.cfg.NewClient()
	if err != nil {
		tc.t.Fatalf("e2e: failed to create klient: %v", err)
	}
	tc.cfg.WithClient(client)

	tc.restCfg = client.RESTConfig()
	tc.clientset, err = kubernetes.NewForConfig(tc.restCfg)
	if err != nil {
		tc.t.Fatalf("e2e: failed to create clientset: %v", err)
	}

	// Re-create the environment with the working config.
	tc.env = env.NewWithConfig(tc.cfg)
}

// runFinish executes finish funcs (log export etc, but not cluster destroy).
func (tc *TestCluster) runFinish(funcs []env.Func) {
	ctx := context.Background()
	for _, fn := range funcs {
		ctx, _ = fn(ctx, tc.cfg)
	}
}

// maybeDestroyCluster destroys the Kind cluster unless E2E_KEEP_CLUSTERS says
// to keep it. Called from t.Cleanup after runFinish.
func (tc *TestCluster) maybeDestroyCluster() {
	keep := os.Getenv("E2E_KEEP_CLUSTERS")
	action := clusterCleanupAction(keep, tc.t.Failed())

	switch action {
	case cleanupKeep:
		tc.t.Logf("e2e: keeping cluster %q (E2E_KEEP_CLUSTERS=%s)", tc.clusterName, keep)
		tc.t.Logf("e2e: inspect:  export KUBECONFIG=$(kind get kubeconfig --name %s)", tc.clusterName)
		tc.t.Logf("e2e: logs:     kubectl logs -n multigres-operator deploy/multigres-operator-controller-manager")
		tc.t.Logf("e2e: clean up: kind delete cluster --name %s", tc.clusterName)
	case cleanupDestroyWithHint:
		tc.t.Logf("e2e: cluster %q will be destroyed. To keep failed clusters for debugging, run with E2E_KEEP_CLUSTERS=on-failure", tc.clusterName)
		tc.destroyCluster()
	case cleanupDestroy:
		tc.destroyCluster()
	}
}

func (tc *TestCluster) destroyCluster() {
	ctx := context.Background()
	destroyFn := envfuncs.DestroyCluster(tc.clusterName)
	if _, err := destroyFn(ctx, tc.cfg); err != nil {
		tc.t.Logf("e2e: failed to destroy cluster %q: %v", tc.clusterName, err)
	}
}

type cleanupDecision int

const (
	cleanupDestroy         cleanupDecision = iota // destroy silently
	cleanupDestroyWithHint                        // destroy but hint about on-failure
	cleanupKeep                                   // keep the cluster
)

// clusterCleanupAction decides what to do with the cluster based on the
// E2E_KEEP_CLUSTERS value and whether the test failed. This is a pure
// function for testability.
func clusterCleanupAction(keepPolicy string, failed bool) cleanupDecision {
	switch keepPolicy {
	case "always":
		return cleanupKeep
	case "on-failure":
		if failed {
			return cleanupKeep
		}
		return cleanupDestroy
	default:
		if failed {
			return cleanupDestroyWithHint
		}
		return cleanupDestroy
	}
}

// ClusterName returns the name of the Kind cluster.
func (tc *TestCluster) ClusterName() string { return tc.clusterName }

// Env returns the underlying e2e-framework Environment.
func (tc *TestCluster) Env() types.Environment { return tc.env }

// Config returns the envconf.Config with the cluster's kubeconfig.
func (tc *TestCluster) Config() *envconf.Config { return tc.cfg }

// RESTConfig returns the *rest.Config for the Kind cluster.
func (tc *TestCluster) RESTConfig() *rest.Config { return tc.restCfg }

// Clientset returns a typed Kubernetes client for the cluster.
func (tc *TestCluster) Clientset() *kubernetes.Clientset { return tc.clientset }

// KlientClient returns the e2e-framework's klient.Client.
func (tc *TestCluster) KlientClient() klient.Client { return tc.cfg.Client() }

// KubeconfigFile returns the path to the cluster's kubeconfig file.
func (tc *TestCluster) KubeconfigFile() string { return tc.cfg.KubeconfigFile() }

// Test runs features against this cluster's environment.
func (tc *TestCluster) Test(t *testing.T, feat ...types.Feature) {
	t.Helper()
	tc.env.Test(t, feat...)
}

// TestInParallel runs features in parallel against this cluster.
func (tc *TestCluster) TestInParallel(t *testing.T, feat ...types.Feature) {
	t.Helper()
	tc.env.TestInParallel(t, feat...)
}

// ---------------------------------------------------------------------------
// Operator deployment
// ---------------------------------------------------------------------------

// DeployOperator installs CRDs and deploys the multigres-operator into the
// Kind cluster using kustomize. It copies the config/ directory to a temp
// location to avoid dirtying the git working tree.
//
// The kubeconfigFile and clusterName are used to target kubectl at the right
// cluster. operatorImg is the fully-qualified operator image reference.
func DeployOperator(t *testing.T, kubeconfigFile, clusterName, operatorImg string) {
	t.Helper()

	repoRoot := repoRoot(t)

	crdPath := os.Getenv("CRD_PATH")
	if crdPath == "" {
		crdPath = filepath.Join(repoRoot, "config", "crd")
	}

	overlayRelPath := os.Getenv("KUSTOMIZE_OVERLAY")
	if overlayRelPath == "" {
		overlayRelPath = "config/no-webhook"
	}
	// Make relative to repo root so it works with the temp copy.
	if strings.HasPrefix(overlayRelPath, repoRoot) {
		overlayRelPath = strings.TrimPrefix(overlayRelPath, repoRoot+"/")
	}

	// 1. Install CRDs.
	t.Log("e2e: installing CRDs...")
	kustomizePipe(t, kubeconfigFile, crdPath)

	// 2. Copy config/ to temp dir to avoid dirtying git.
	tmpDir := t.TempDir()
	cpCmd := exec.Command("cp", "-r", filepath.Join(repoRoot, "config"), tmpDir)
	if out, err := cpCmd.CombinedOutput(); err != nil {
		t.Fatalf("e2e: copy config: %v\n%s", err, out)
	}

	// 3. Set the operator image in the temp copy.
	t.Logf("e2e: setting operator image to %s", operatorImg)
	managerDir := filepath.Join(tmpDir, "config", "manager")
	setImg := exec.Command("kustomize", "edit", "set", "image",
		fmt.Sprintf("controller=%s", operatorImg))
	setImg.Dir = managerDir
	if out, err := setImg.CombinedOutput(); err != nil {
		t.Fatalf("e2e: kustomize edit set image: %v\n%s", err, out)
	}

	// 4. Deploy operator via kustomize.
	t.Log("e2e: deploying operator...")
	kustomizePipe(t, kubeconfigFile, filepath.Join(tmpDir, overlayRelPath))

	t.Log("e2e: operator deployment applied")
}

// kustomizePipe runs `kustomize build <dir> | kubectl apply --server-side -f -`.
func kustomizePipe(t *testing.T, kubeconfigFile, dir string) {
	t.Helper()

	kBuild := exec.Command("kustomize", "build", dir)
	kApply := exec.Command("kubectl",
		"--kubeconfig", kubeconfigFile,
		"apply", "--server-side", "-f", "-")

	var errBuf strings.Builder
	pipe, err := kBuild.StdoutPipe()
	if err != nil {
		t.Fatalf("e2e: pipe: %v", err)
	}
	kApply.Stdin = pipe
	kApply.Stdout = os.Stdout
	kApply.Stderr = &errBuf

	if err := kBuild.Start(); err != nil {
		t.Fatalf("e2e: kustomize build start: %v", err)
	}
	if err := kApply.Start(); err != nil {
		t.Fatalf("e2e: kubectl apply start: %v", err)
	}
	if err := kBuild.Wait(); err != nil {
		t.Fatalf("e2e: kustomize build %s: %v", dir, err)
	}
	if err := kApply.Wait(); err != nil {
		t.Fatalf("e2e: kubectl apply: %v\n%s", err, errBuf.String())
	}
}

// repoRoot returns the git repository root.
func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("e2e: git rev-parse --show-toplevel: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// ---------------------------------------------------------------------------
// OperatorPreset — captures the deploy-operator + wait pattern
// ---------------------------------------------------------------------------

// OperatorPreset captures the common "deploy operator + wait for ready"
// pattern so test files don't repeat the same Setup step.
type OperatorPreset struct {
	// Image is the fully-qualified operator container image reference.
	Image string
	// Namespace where the operator Deployment lives.
	Namespace string
	// DeploymentName is the name of the operator Deployment.
	DeploymentName string
	// ReadyTimeout is how long to wait for the Deployment to become ready.
	ReadyTimeout time.Duration
}

// DefaultOperatorPreset returns a preset configured for the standard
// multigres-operator deployment. The image comes from the OPERATOR_IMG
// environment variable which is set by the Makefile.
func DefaultOperatorPreset() OperatorPreset {
	img := os.Getenv("OPERATOR_IMG")
	if img == "" {
		img = "ghcr.io/numtide/multigres-operator:dev"
	}
	return OperatorPreset{
		Image:          img,
		Namespace:      "multigres-operator",
		DeploymentName: "multigres-operator-controller-manager",
		ReadyTimeout:   3 * time.Minute,
	}
}

// SetupFunc returns a features.Func that deploys the operator and waits for
// the Deployment to become ready. Use in Feature.Setup().
func (p OperatorPreset) SetupFunc(tc *TestCluster) features.Func {
	return func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		t.Helper()
		DeployOperator(t, cfg.KubeconfigFile(), tc.ClusterName(), p.Image)
		WaitForDeploymentReady(t, tc.Clientset(), p.Namespace, p.DeploymentName, p.ReadyTimeout)
		return ctx
	}
}

// ---------------------------------------------------------------------------
// Wait helpers
// ---------------------------------------------------------------------------

// WaitForDeploymentReady waits until a Deployment has all replicas available.
func WaitForDeploymentReady(
	t *testing.T,
	clientset *kubernetes.Clientset,
	namespace, name string,
	timeout time.Duration,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	t.Logf("e2e: waiting for deployment %s/%s to be ready (timeout %s)", namespace, name, timeout)

	err := wait.PollUntilContextCancel(ctx, 3*time.Second, true, func(ctx context.Context) (bool, error) {
		dep, err := clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		if dep.Spec.Replicas == nil {
			return false, nil
		}
		return dep.Status.AvailableReplicas == *dep.Spec.Replicas, nil
	})
	if err != nil {
		// Dump deployment status for debugging.
		dep, getErr := clientset.AppsV1().Deployments(namespace).Get(
			context.Background(), name, metav1.GetOptions{})
		if getErr == nil {
			t.Logf("e2e: deployment %s/%s status: %+v", namespace, name, dep.Status)
		}
		dumpPodLogs(t, clientset, namespace, name)
		t.Fatalf("e2e: deployment %s/%s not ready: %v", namespace, name, err)
	}
	t.Logf("e2e: deployment %s/%s is ready", namespace, name)
}

// dumpPodLogs fetches logs from pods belonging to a deployment for debugging.
func dumpPodLogs(t *testing.T, clientset *kubernetes.Clientset, namespace, deploymentName string) {
	t.Helper()
	ctx := context.Background()

	dep, err := clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return
	}

	sel := selectorFromDeployment(dep)
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: sel,
	})
	if err != nil || len(pods.Items) == 0 {
		return
	}

	for _, pod := range pods.Items {
		for _, c := range pod.Spec.Containers {
			logs, err := clientset.CoreV1().Pods(namespace).
				GetLogs(pod.Name, &corev1.PodLogOptions{Container: c.Name, TailLines: int64Ptr(50)}).
				Do(ctx).Raw()
			if err == nil {
				t.Logf("e2e: logs %s/%s/%s:\n%s", namespace, pod.Name, c.Name, string(logs))
			}
		}
	}
}

func selectorFromDeployment(dep *appsv1.Deployment) string {
	if dep.Spec.Selector == nil {
		return ""
	}
	return labels.Set(dep.Spec.Selector.MatchLabels).AsSelector().String()
}

func int64Ptr(i int64) *int64 { return &i }

// WaitForAllPodsReady waits until every pod in the given namespace is Ready or
// Succeeded. Timeout 8 minutes, poll every 5 seconds.
func WaitForAllPodsReady(t *testing.T, clientset *kubernetes.Clientset, namespace string) {
	t.Helper()
	WaitForAllPodsReadyWithTimeout(t, clientset, namespace, 8*time.Minute)
}

// WaitForAllPodsReadyWithTimeout waits until every pod in the given namespace
// is Ready or Succeeded, with a configurable timeout.
func WaitForAllPodsReadyWithTimeout(
	t *testing.T,
	clientset *kubernetes.Clientset,
	namespace string,
	timeout time.Duration,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	t.Logf("e2e: waiting for all pods in %s to be ready (timeout %s)", namespace, timeout)

	err := wait.PollUntilContextCancel(ctx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, nil
		}
		if len(pods.Items) == 0 {
			return false, nil // no pods yet
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodSucceeded {
				continue
			}
			if !isPodReady(&pod) {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		// Log pod statuses for debugging.
		pods, listErr := clientset.CoreV1().Pods(namespace).List(
			context.Background(), metav1.ListOptions{})
		if listErr == nil {
			for _, pod := range pods.Items {
				t.Logf("e2e: pod %s/%s phase=%s ready=%v",
					namespace, pod.Name, pod.Status.Phase, isPodReady(&pod))
			}
		}
		t.Fatalf("e2e: not all pods in %s ready: %v", namespace, err)
	}
	t.Logf("e2e: all pods in %s are ready", namespace)
}

func isPodReady(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Port-forward helpers
// ---------------------------------------------------------------------------

// PortForwardResult holds the local port and a stop function.
type PortForwardResult struct {
	LocalPort uint16
	StopCh    chan struct{}
}

// Stop tears down the port-forward tunnel.
func (pf *PortForwardResult) Stop() {
	close(pf.StopCh)
}

// PortForwardService resolves a Service to one of its backing Pods, then
// creates a port-forward tunnel. The tunnel is torn down via t.Cleanup.
func (tc *TestCluster) PortForwardService(
	t *testing.T,
	namespace, serviceName string,
	remotePort int,
) *PortForwardResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	svc, err := tc.clientset.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("e2e: get service %s/%s: %v", namespace, serviceName, err)
	}

	podName := tc.waitForReadyPod(t, ctx, namespace, svc.Spec.Selector)
	return tc.PortForwardPod(t, namespace, podName, remotePort)
}

// PortForwardPod creates a port-forward tunnel to a specific pod.
func (tc *TestCluster) PortForwardPod(
	t *testing.T,
	namespace, podName string,
	remotePort int,
) *PortForwardResult {
	t.Helper()

	transport, upgrader, err := spdy.RoundTripperFor(tc.restCfg)
	if err != nil {
		t.Fatalf("e2e: spdy round tripper: %v", err)
	}

	url := tc.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("portforward").
		URL()

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, url)

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})

	ports := []string{fmt.Sprintf("0:%d", remotePort)}
	fw, err := portforward.New(dialer, ports, stopCh, readyCh, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("e2e: portforward.New: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- fw.ForwardPorts()
	}()

	select {
	case <-readyCh:
	case err := <-errCh:
		t.Fatalf("e2e: port-forward failed: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("e2e: port-forward timed out")
	}

	forwardedPorts, err := fw.GetPorts()
	if err != nil || len(forwardedPorts) == 0 {
		t.Fatalf("e2e: get forwarded ports: %v", err)
	}

	result := &PortForwardResult{
		LocalPort: forwardedPorts[0].Local,
		StopCh:    stopCh,
	}
	t.Cleanup(func() { result.Stop() })

	t.Logf("e2e: port-forward %s/%s :%d -> localhost:%d",
		namespace, podName, remotePort, result.LocalPort)

	return result
}

// waitForReadyPod polls until a Ready pod matching the selector exists.
func (tc *TestCluster) waitForReadyPod(
	t *testing.T,
	ctx context.Context,
	namespace string,
	selector map[string]string,
) string {
	t.Helper()
	labelSel := labels.Set(selector).AsSelector().String()
	var podName string

	err := wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		pods, err := tc.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSel,
		})
		if err != nil {
			return false, nil
		}
		for i := range pods.Items {
			if isPodReady(&pods.Items[i]) {
				podName = pods.Items[i].Name
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("e2e: timed out waiting for ready pod (selector=%s): %v", labelSel, err)
	}
	return podName
}

// ---------------------------------------------------------------------------
// psql helper
// ---------------------------------------------------------------------------

// PsqlViaKubectl executes a psql query via kubectl exec and returns the output.
// This is useful for verifying database connectivity through multigateway pods.
func PsqlViaKubectl(
	t *testing.T,
	kubeconfigFile, namespace, podName, container, host string,
	port int,
	query string,
) string {
	t.Helper()

	args := []string{
		"--kubeconfig", kubeconfigFile,
		"exec", "-n", namespace, podName,
	}
	if container != "" {
		args = append(args, "-c", container)
	}
	args = append(args, "--",
		"psql",
		"-h", host,
		"-p", fmt.Sprintf("%d", port),
		"-U", "postgres",
		"-d", "postgres",
		"-t", "-A",
		"-c", query,
	)

	cmd := exec.Command("kubectl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("e2e: psql via kubectl failed: %v\noutput: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func sanitizeE2EName(name string) string {
	r := strings.NewReplacer("/", "-", "_", "-", " ", "-")
	s := r.Replace(strings.ToLower(name))
	if len(s) > 50 {
		s = s[:50]
	}
	return strings.Trim(s, "-")
}

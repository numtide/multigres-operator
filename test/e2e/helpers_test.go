//go:build e2e

package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// multigresImages are the upstream container images needed by operator workloads.
var multigresImages = []string{
	"ghcr.io/multigres/multigres:main",
	"ghcr.io/multigres/pgctld:main",
	"ghcr.io/multigres/multiadmin-web:main",
	"gcr.io/etcd-development/etcd:v3.6.7",
}

// testCluster holds references to a kind cluster set up for e2e testing.
// The operator is deployed as a real Deployment inside the cluster, so all
// controllers (including data-handler) run in-cluster with full network access.
type testCluster struct {
	name      string // kind cluster name
	client    client.Client
	clientset *kubernetes.Clientset
	namespace string // isolated test namespace
}

// newScheme creates a runtime.Scheme with all types needed by the operator.
func newScheme(t testing.TB) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = admissionregistrationv1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	return scheme
}

// setUpCluster creates a kind cluster, builds and deploys the operator,
// loads upstream images, and returns a testCluster ready for use.
//
// The operator runs as a real Deployment inside the cluster with all
// controllers (cluster-handler, resource-handler, data-handler). This is
// a true end-to-end test — no in-process controllers, no hacks.
func setUpCluster(t *testing.T) *testCluster {
	t.Helper()

	clusterName := fmt.Sprintf("e2e-%s", randSuffix())
	t.Logf("Creating kind cluster %q...", clusterName)

	// Create kind cluster
	run(t, "kind", "create", "cluster", "--name", clusterName)
	t.Cleanup(func() {
		t.Logf("Deleting kind cluster %q...", clusterName)
		out, err := exec.Command("kind", "delete", "cluster", "--name", clusterName).CombinedOutput()
		if err != nil {
			t.Errorf("Failed to delete kind cluster %q: %v\n%s", clusterName, err, out)
		}
	})

	// Build operator image
	t.Log("Building operator image...")
	runInRepo(t, "make", "container")

	// Load operator image into kind
	operatorImg := operatorImage(t)
	t.Logf("Loading operator image %s into kind...", operatorImg)
	run(t, "kind", "load", "docker-image", operatorImg, "--name", clusterName)

	// Load upstream multigres images into kind
	for _, img := range multigresImages {
		t.Logf("Loading image %s...", img)
		// Pull if not present locally
		if _, err := exec.Command("docker", "image", "inspect", img).Output(); err != nil {
			run(t, "docker", "pull", img)
		}
		run(t, "kind", "load", "docker-image", img, "--name", clusterName)
	}

	// Get kubeconfig
	kubeconfigData := runOutput(t, "kind", "get", "kubeconfig", "--name", clusterName)
	restCfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfigData))
	if err != nil {
		t.Fatalf("Failed to parse kubeconfig: %v", err)
	}

	// Deploy operator (no-webhook) via kustomize
	t.Log("Deploying operator into cluster...")
	deployOperator(t, clusterName, operatorImg)

	// Wait for operator pod to be ready
	t.Log("Waiting for operator to be ready...")
	waitForOperatorReady(t, clusterName)

	// Create controller-runtime client
	scheme := newScheme(t)
	c, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Create clientset for namespace management
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		t.Fatalf("Failed to create clientset: %v", err)
	}

	// Create isolated test namespace
	ns := fmt.Sprintf("e2e-test-%s", randSuffix())
	_, err = clientset.CoreV1().Namespaces().Create(
		context.Background(),
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		t.Fatalf("Failed to create namespace %q: %v", ns, err)
	}
	t.Cleanup(func() {
		_ = clientset.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
	})

	return &testCluster{
		name:      clusterName,
		client:    c,
		clientset: clientset,
		namespace: ns,
	}
}

// operatorImage returns the operator container image tag from the Makefile.
func operatorImage(t *testing.T) string {
	t.Helper()
	out := runInRepoOutput(t, "make", "--no-print-directory", "-s", "print-img")
	return strings.TrimSpace(out)
}

// deployOperator deploys the operator into the kind cluster using kustomize.
func deployOperator(t *testing.T, clusterName, img string) {
	t.Helper()

	kubeconfig := kindKubeconfig(t, clusterName)

	// Install CRDs
	kustomizeOut := runInRepoOutput(t, "kustomize", "build", "config/crd")
	cmd := exec.Command("kubectl", "--kubeconfig", kubeconfig, "apply", "--server-side", "-f", "-")
	cmd.Stdin = strings.NewReader(kustomizeOut)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to install CRDs: %v\n%s", err, out)
	}

	// Set the image and deploy using no-webhook config
	repoDir := repoRoot(t)

	// Run kustomize edit set image in a temp copy to avoid git dirty
	tmpDir := t.TempDir()
	run(t, "cp", "-r", repoDir+"/config", tmpDir+"/config")
	cmd = exec.Command("kustomize", "edit", "set", "image", "controller="+img)
	cmd.Dir = tmpDir + "/config/manager"
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to set image: %v\n%s", err, out)
	}

	cmd = exec.Command("kustomize", "build", tmpDir+"/config/no-webhook")
	manifestOut, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("kustomize build failed: %v\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("kustomize build failed: %v", err)
	}

	cmd = exec.Command("kubectl", "--kubeconfig", kubeconfig, "apply", "--server-side", "-f", "-")
	cmd.Stdin = strings.NewReader(string(manifestOut))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to deploy operator: %v\n%s", err, out)
	}
}

// waitForOperatorReady waits until the operator Deployment has at least 1 ready replica.
func waitForOperatorReady(t *testing.T, clusterName string) {
	t.Helper()
	kubeconfig := kindKubeconfig(t, clusterName)
	pollUntil(t, 3*time.Minute, 5*time.Second, "operator ready", func() (bool, string) {
		out, err := exec.Command("kubectl", "--kubeconfig", kubeconfig,
			"get", "deployment", "-n", "multigres-operator",
			"multigres-operator-controller-manager",
			"-o", "jsonpath={.status.readyReplicas}",
		).Output()
		if err != nil {
			return false, fmt.Sprintf("kubectl error: %v", err)
		}
		if strings.TrimSpace(string(out)) == "1" {
			return true, ""
		}
		return false, fmt.Sprintf("readyReplicas=%s", string(out))
	})
}

// kindKubeconfig writes the kubeconfig for a kind cluster to a temp file and
// returns its path.
func kindKubeconfig(t *testing.T, clusterName string) string {
	t.Helper()
	data := runOutput(t, "kind", "get", "kubeconfig", "--name", clusterName)
	f, err := os.CreateTemp("", "kubeconfig-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp kubeconfig: %v", err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	if _, err := f.WriteString(data); err != nil {
		t.Fatalf("Failed to write kubeconfig: %v", err)
	}
	f.Close()
	return f.Name()
}

// repoRoot returns the absolute path to the repo root.
func repoRoot(t *testing.T) string {
	t.Helper()
	out := runOutput(t, "git", "rev-parse", "--show-toplevel")
	return strings.TrimSpace(out)
}

// --------------------------------------------------------------------------
// Poll and wait helpers
// --------------------------------------------------------------------------

// pollUntil repeatedly calls check until it returns true or timeout is exceeded.
func pollUntil(t *testing.T, timeout, interval time.Duration, desc string, check func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastMsg string
	for time.Now().Before(deadline) {
		ok, msg := check()
		if ok {
			return
		}
		lastMsg = msg
		time.Sleep(interval)
	}
	t.Fatalf("timed out waiting for %s: %s", desc, lastMsg)
}

// waitForMinCount waits until at least minCount items exist for a resource kind.
func waitForMinCount[T client.ObjectList](
	t *testing.T, ctx context.Context, c client.Client, ns string,
	factory func() T, countFn func(T) int, minCount int, desc string,
) {
	t.Helper()
	pollUntil(t, 2*time.Minute, 2*time.Second, desc, func() (bool, string) {
		list := factory()
		if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
			return false, fmt.Sprintf("list error: %v", err)
		}
		n := countFn(list)
		if n >= minCount {
			return true, ""
		}
		return false, fmt.Sprintf("got %d, want >= %d", n, minCount)
	})
}

// waitForDeploymentWithContainer waits for a Deployment containing a container
// with the given name to appear in the namespace.
func waitForDeploymentWithContainer(t *testing.T, ctx context.Context, c client.Client, ns, containerName string) *appsv1.Deployment {
	t.Helper()
	var found *appsv1.Deployment
	pollUntil(t, 2*time.Minute, 2*time.Second, fmt.Sprintf("Deployment with container %q", containerName), func() (bool, string) {
		list := &appsv1.DeploymentList{}
		if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
			return false, fmt.Sprintf("list error: %v", err)
		}
		for i := range list.Items {
			for _, cont := range list.Items[i].Spec.Template.Spec.Containers {
				if cont.Name == containerName {
					found = &list.Items[i]
					return true, ""
				}
			}
		}
		return false, fmt.Sprintf("no Deployment with container %q among %d deployments", containerName, len(list.Items))
	})
	return found
}

// waitForStatefulSetWithContainer waits for a StatefulSet containing a container
// with the given name to appear in the namespace.
func waitForStatefulSetWithContainer(t *testing.T, ctx context.Context, c client.Client, ns, containerName string) *appsv1.StatefulSet {
	t.Helper()
	var found *appsv1.StatefulSet
	pollUntil(t, 2*time.Minute, 2*time.Second, fmt.Sprintf("StatefulSet with container %q", containerName), func() (bool, string) {
		list := &appsv1.StatefulSetList{}
		if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
			return false, fmt.Sprintf("list error: %v", err)
		}
		for i := range list.Items {
			for _, cont := range list.Items[i].Spec.Template.Spec.Containers {
				if cont.Name == containerName {
					found = &list.Items[i]
					return true, ""
				}
			}
		}
		return false, fmt.Sprintf("no StatefulSet with container %q among %d statefulsets", containerName, len(list.Items))
	})
	return found
}

// waitForServiceWithPort waits for a Service with the given port name and number.
func waitForServiceWithPort(t *testing.T, ctx context.Context, c client.Client, ns, portName string, port int32) *corev1.Service {
	t.Helper()
	var found *corev1.Service
	pollUntil(t, 2*time.Minute, 2*time.Second, fmt.Sprintf("Service with port %s:%d", portName, port), func() (bool, string) {
		list := &corev1.ServiceList{}
		if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
			return false, fmt.Sprintf("list error: %v", err)
		}
		for i := range list.Items {
			for _, p := range list.Items[i].Spec.Ports {
				if p.Name == portName && p.Port == port {
					found = &list.Items[i]
					return true, ""
				}
			}
		}
		return false, fmt.Sprintf("no Service with port %s:%d among %d services", portName, port, len(list.Items))
	})
	return found
}

// waitForAllPodsReady waits until all pods in the namespace are Ready.
func waitForAllPodsReady(t *testing.T, ctx context.Context, c client.Client, ns string) {
	t.Helper()
	pollUntil(t, 8*time.Minute, 5*time.Second, "all pods ready", func() (bool, string) {
		pods := &corev1.PodList{}
		if err := c.List(ctx, pods, client.InNamespace(ns)); err != nil {
			return false, fmt.Sprintf("list error: %v", err)
		}
		if len(pods.Items) == 0 {
			return false, "no pods found"
		}
		var notReady []string
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodSucceeded {
				continue
			}
			ready := false
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
					ready = true
					break
				}
			}
			if !ready {
				notReady = append(notReady, fmt.Sprintf("%s(%s)", pod.Name, pod.Status.Phase))
			}
		}
		if len(notReady) == 0 {
			return true, ""
		}
		return false, fmt.Sprintf("not ready: %s", strings.Join(notReady, ", "))
	})
}

// waitForQueryServing polls until SELECT 1 succeeds through the multigateway.
// Uses kubectl exec into a pod with psql (pgctld container is based on postgres).
func waitForQueryServing(t *testing.T, tc *testCluster, gatewaySvc string) {
	t.Helper()
	kubeconfig := kindKubeconfig(t, tc.name)
	pollUntil(t, 3*time.Minute, 5*time.Second, "query serving via multigateway", func() (bool, string) {
		// Find a running pod with postgres container (has psql)
		pods := &corev1.PodList{}
		if err := tc.client.List(context.Background(), pods, client.InNamespace(tc.namespace)); err != nil {
			return false, fmt.Sprintf("list error: %v", err)
		}
		var targetPod string
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning {
				continue
			}
			for _, cont := range pod.Spec.Containers {
				if cont.Name == "postgres" {
					// Check container is actually ready
					for _, cs := range pod.Status.ContainerStatuses {
						if cs.Name == "postgres" && cs.Ready {
							targetPod = pod.Name
						}
					}
				}
			}
			if targetPod != "" {
				break
			}
		}
		if targetPod == "" {
			return false, "no running postgres pod with ready container"
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		out, err := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfig,
			"exec", "-n", tc.namespace, targetPod, "-c", "postgres",
			"--",
			"psql",
			"-h", gatewaySvc,
			"-p", "15432",
			"-U", "postgres",
			"-d", "postgres",
			"-t", "-A",
			"-c", "SELECT 1",
		).CombinedOutput()
		if err != nil {
			return false, fmt.Sprintf("psql error: %v (output: %s)", err, strings.TrimSpace(string(out)))
		}
		result := strings.TrimSpace(string(out))
		if result == "1" {
			return true, ""
		}
		return false, fmt.Sprintf("unexpected result: %q", result)
	})
}

// findGatewayServiceName finds the multigateway Service name.
func findGatewayServiceName(t *testing.T, ctx context.Context, c client.Client, ns string) string {
	t.Helper()
	var svcName string
	pollUntil(t, 60*time.Second, 2*time.Second, "multigateway Service", func() (bool, string) {
		svcs := &corev1.ServiceList{}
		if err := c.List(ctx, svcs, client.InNamespace(ns)); err != nil {
			return false, fmt.Sprintf("list error: %v", err)
		}
		for _, svc := range svcs.Items {
			for _, port := range svc.Spec.Ports {
				if port.Name == "postgres" && port.Port == 15432 {
					svcName = svc.Name
					return true, ""
				}
			}
		}
		return false, "no Service with postgres:15432"
	})
	return svcName
}

// --------------------------------------------------------------------------
// Exec helpers
// --------------------------------------------------------------------------

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Command %s %v failed: %v\n%s", name, args, err, out)
	}
}

func runOutput(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("Command %s %v failed: %v\nstderr: %s", name, args, err, exitErr.Stderr)
		}
		t.Fatalf("Command %s %v failed: %v", name, args, err)
	}
	return strings.TrimSpace(string(out))
}

func runInRepo(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Command %s %v (in repo) failed: %v\n%s", name, args, err, out)
	}
}

func runInRepoOutput(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("Command %s %v (in repo) failed: %v\nstderr: %s", name, args, err, exitErr.Stderr)
		}
		t.Fatalf("Command %s %v (in repo) failed: %v", name, args, err)
	}
	return strings.TrimSpace(string(out))
}

func randSuffix() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[rand.IntN(len(chars))]
	}
	return string(b)
}

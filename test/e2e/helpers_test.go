//go:build e2e

package e2e_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

// newClusterWithOperator creates a kind cluster with all multigres images loaded
// and the operator deployed. Returns testutil.TestCluster and a features.Func
// for Feature.Setup().
func newClusterWithOperator(t *testing.T) (*testutil.TestCluster, features.Func) {
	t.Helper()

	images := make([]testutil.E2EOption, 0, len(testutil.MultigresImages)+1)
	operatorImg := testutil.DefaultOperatorPreset().Image
	images = append(images, testutil.WithImage(operatorImg))
	for _, img := range testutil.MultigresImages {
		images = append(images, testutil.WithImage(img))
	}

	tc := testutil.NewTestCluster(t, images...)
	preset := testutil.DefaultOperatorPreset()
	return tc, preset.SetupFunc(tc)
}

// createNamespace creates a namespace and returns its name. Registers cleanup.
func createNamespace(t *testing.T, tc *testutil.TestCluster) string {
	t.Helper()
	ns := envconf.RandomName("e2e-test", 16)
	_, err := tc.Clientset().CoreV1().Namespaces().Create(
		context.Background(),
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}},
		metav1.CreateOptions{},
	)
	if err != nil {
		t.Fatalf("create namespace: %v", err)
	}
	t.Cleanup(func() {
		_ = tc.Clientset().CoreV1().Namespaces().Delete(
			context.Background(), ns, metav1.DeleteOptions{})
	})
	return ns
}

// newCRClient creates a controller-runtime client with the multigres scheme registered.
func newCRClient(t *testing.T, tc *testutil.TestCluster) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	c, err := client.New(tc.RESTConfig(), client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("new cr client: %v", err)
	}
	return c
}

// findGatewayServiceName finds the multigateway Service with port 15432 named "postgres".
func findGatewayServiceName(t *testing.T, tc *testutil.TestCluster, ns string) string {
	t.Helper()
	var svcName string
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		svcs, err := tc.Clientset().CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, nil
		}
		for _, svc := range svcs.Items {
			for _, port := range svc.Spec.Ports {
				if port.Name == "postgres" && port.Port == 15432 {
					svcName = svc.Name
					return true, nil
				}
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("timed out finding multigateway service with postgres:15432: %v", err)
	}
	return svcName
}

// waitForQueryServing polls until SELECT 1 succeeds through a multigateway service.
func waitForQueryServing(t *testing.T, tc *testutil.TestCluster, ns, gatewaySvc string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	err := wait.PollUntilContextCancel(ctx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		// Find a running pod with a ready "postgres" container.
		pods, err := tc.Clientset().CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, nil
		}
		var targetPod string
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning {
				continue
			}
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.Name == "postgres" && cs.Ready {
					targetPod = pod.Name
					break
				}
			}
			if targetPod != "" {
				break
			}
		}
		if targetPod == "" {
			return false, nil
		}

		result := testutil.PsqlViaKubectl(
			t,
			tc.KubeconfigFile(), ns, targetPod, "postgres",
			gatewaySvc, 15432,
			"SELECT 1",
		)
		return strings.TrimSpace(result) == "1", nil
	})
	if err != nil {
		t.Fatalf("timed out waiting for query serving via multigateway: %v", err)
	}
}

// waitForQueryServingSafe is like waitForQueryServing but doesn't fatalf on psql failure.
// It's used inside poll loops where psql errors are expected transiently.
func waitForQueryServingSafe(t *testing.T, tc *testutil.TestCluster, ns, gatewaySvc string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	err := wait.PollUntilContextCancel(ctx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		pods, err := tc.Clientset().CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, nil
		}
		var targetPod string
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning {
				continue
			}
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.Name == "postgres" && cs.Ready {
					targetPod = pod.Name
					break
				}
			}
			if targetPod != "" {
				break
			}
		}
		if targetPod == "" {
			return false, nil
		}

		// Use kubectl exec directly to avoid t.Fatalf from PsqlViaKubectl.
		args := []string{
			"--kubeconfig", tc.KubeconfigFile(),
			"exec", "-n", ns, targetPod, "-c", "postgres",
			"--",
			"psql",
			"-h", gatewaySvc,
			"-p", "15432",
			"-U", "postgres",
			"-d", "postgres",
			"-t", "-A",
			"-c", "SELECT 1",
		}
		execCtx, execCancel := context.WithTimeout(ctx, 10*time.Second)
		defer execCancel()
		out, err := exec.CommandContext(execCtx, "kubectl", args...).CombinedOutput()
		if err != nil {
			return false, nil
		}
		return strings.TrimSpace(string(out)) == "1", nil
	})
	if err != nil {
		t.Fatalf("timed out waiting for query serving via multigateway: %v", err)
	}
}

// waitForDeploymentWithContainer waits for a Deployment containing a container
// with the given name to appear in the namespace.
func waitForDeploymentWithContainer(t *testing.T, c client.Client, ns, containerName string) *appsv1.Deployment {
	t.Helper()
	var found *appsv1.Deployment
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	err := wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		list := &appsv1.DeploymentList{}
		if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
			return false, nil
		}
		for i := range list.Items {
			for _, cont := range list.Items[i].Spec.Template.Spec.Containers {
				if cont.Name == containerName {
					found = &list.Items[i]
					return true, nil
				}
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("timed out waiting for Deployment with container %q: %v", containerName, err)
	}
	return found
}

// waitForStatefulSetWithContainer waits for a StatefulSet containing a container
// with the given name to appear in the namespace.
func waitForStatefulSetWithContainer(t *testing.T, c client.Client, ns, containerName string) *appsv1.StatefulSet {
	t.Helper()
	var found *appsv1.StatefulSet
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	err := wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		list := &appsv1.StatefulSetList{}
		if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
			return false, nil
		}
		for i := range list.Items {
			for _, cont := range list.Items[i].Spec.Template.Spec.Containers {
				if cont.Name == containerName {
					found = &list.Items[i]
					return true, nil
				}
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("timed out waiting for StatefulSet with container %q: %v", containerName, err)
	}
	return found
}

// waitForPodWithContainer waits for at least one Pod containing a container
// with the given name to appear in the namespace. Used for resources that
// create individual Pods rather than StatefulSets (e.g., postgres pool pods).
func waitForPodWithContainer(t *testing.T, c client.Client, ns, containerName string) *corev1.Pod {
	t.Helper()
	var found *corev1.Pod
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	err := wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		list := &corev1.PodList{}
		if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
			return false, nil
		}
		for i := range list.Items {
			for _, cont := range list.Items[i].Spec.Containers {
				if cont.Name == containerName {
					found = &list.Items[i]
					return true, nil
				}
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("timed out waiting for Pod with container %q: %v", containerName, err)
	}
	return found
}

func waitForServiceWithPort(t *testing.T, c client.Client, ns, portName string, port int32) *corev1.Service {
	t.Helper()
	var found *corev1.Service
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		list := &corev1.ServiceList{}
		if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
			return false, nil
		}
		for i := range list.Items {
			for _, p := range list.Items[i].Spec.Ports {
				if p.Name == portName && p.Port == port {
					found = &list.Items[i]
					return true, nil
				}
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("timed out waiting for Service with port %s:%d: %v", portName, port, err)
	}
	return found
}

// waitForCRDCount waits until at least minCount items of a CR type exist in the namespace.
func waitForCRDCount[T client.ObjectList](
	t *testing.T, c client.Client, ns string,
	list T, countFn func(T) int, minCount int, desc string,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
			return false, nil
		}
		return countFn(list) >= minCount, nil
	})
	if err != nil {
		t.Fatalf("timed out waiting for %s (want >= %d): %v", desc, minCount, err)
	}
}

// waitForEmpty waits until a list resource has zero items in the namespace.
func waitForEmpty[T client.ObjectList](
	t *testing.T, c client.Client, ns string,
	list T, countFn func(T) int, desc string, timeout time.Duration,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := wait.PollUntilContextCancel(ctx, 3*time.Second, true, func(ctx context.Context) (bool, error) {
		if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
			return false, nil
		}
		return countFn(list) == 0, nil
	})
	if err != nil {
		t.Fatalf("timed out waiting for %s to be empty: %v (count=%d)", desc, err, countFn(list))
	}
}

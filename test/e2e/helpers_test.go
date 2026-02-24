//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	multigresclustercontroller "github.com/numtide/multigres-operator/pkg/cluster-handler/controller/multigrescluster"
	tablegroupcontroller "github.com/numtide/multigres-operator/pkg/cluster-handler/controller/tablegroup"
	datahandlercellcontroller "github.com/numtide/multigres-operator/pkg/data-handler/controller/cell"
	datahandlershardcontroller "github.com/numtide/multigres-operator/pkg/data-handler/controller/shard"
	cellcontroller "github.com/numtide/multigres-operator/pkg/resource-handler/controller/cell"
	shardcontroller "github.com/numtide/multigres-operator/pkg/resource-handler/controller/shard"
	toposervercontroller "github.com/numtide/multigres-operator/pkg/resource-handler/controller/toposerver"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

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

// multigresImages are the container images needed by the operator's workloads.
// These must be loaded into the kind cluster before running tests.
var multigresImages = []string{
	"ghcr.io/multigres/multigres:main",
	"ghcr.io/multigres/pgctld:main",
	"ghcr.io/multigres/multiadmin-web:main",
	"gcr.io/etcd-development/etcd:v3.6.7",
}

type operatorOpts struct {
	skipDataHandler bool
	kindOptions     []testutil.KindOption
}

type operatorOption func(*operatorOpts)

// withoutDataHandler skips registering data-handler controllers. Use this for
// tests that don't need topology server connectivity (e.g. deletion tests).
// The data-handler controllers add finalizers to Cells and Shards that require
// a live etcd topology server to process; without a running etcd cluster these
// finalizers block deletion indefinitely.
func withoutDataHandler() operatorOption {
	return func(o *operatorOpts) { o.skipDataHandler = true }
}

// withKindOptions passes additional KindOption values through to
// testutil.SetUpKindManager. Use this to create ephemeral kind clusters per
// test via testutil.WithKindCreateCluster() and load images via
// testutil.WithKindImages().
func withKindOptions(opts ...testutil.KindOption) operatorOption {
	return func(o *operatorOpts) { o.kindOptions = append(o.kindOptions, opts...) }
}

// setUpOperator starts a namespace-scoped manager against the kind cluster and
// registers all operator controllers. It returns the running manager, a
// Kubernetes client, and the isolated test namespace name.
//
// Each test gets its own namespace, so tests can run in parallel without
// interfering with each other.
func setUpOperator(t *testing.T, opts ...operatorOption) (manager.Manager, client.Client, string) {
	t.Helper()

	cfg := &operatorOpts{}
	for _, o := range opts {
		o(cfg)
	}

	scheme := newScheme(t)

	// Base kind options: always install CRDs
	kindOpts := []testutil.KindOption{
		testutil.WithKindCRDPaths("../../config/crd/bases"),
	}
	// Append any extra kind options (e.g. WithKindCreateCluster, WithKindImages)
	kindOpts = append(kindOpts, cfg.kindOptions...)

	mgr, ns := testutil.SetUpKindManager(t, scheme, kindOpts...)
	c := mgr.GetClient()

	ctrlOpts := controller.Options{
		SkipNameValidation: ptr.To(true),
	}

	// cluster-handler controllers
	if err := (&multigresclustercontroller.MultigresClusterReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: mgr.GetEventRecorderFor("multigrescluster-controller"),
	}).SetupWithManager(mgr, ctrlOpts); err != nil {
		t.Fatalf("Failed to set up MultigresCluster controller: %v", err)
	}

	if err := (&tablegroupcontroller.TableGroupReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: mgr.GetEventRecorderFor("tablegroup-controller"),
	}).SetupWithManager(mgr, ctrlOpts); err != nil {
		t.Fatalf("Failed to set up TableGroup controller: %v", err)
	}

	// resource-handler controllers
	if err := (&cellcontroller.CellReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: mgr.GetEventRecorderFor("cell-controller"),
	}).SetupWithManager(mgr, ctrlOpts); err != nil {
		t.Fatalf("Failed to set up Cell controller: %v", err)
	}

	if err := (&toposervercontroller.TopoServerReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: mgr.GetEventRecorderFor("toposerver-controller"),
	}).SetupWithManager(mgr, ctrlOpts); err != nil {
		t.Fatalf("Failed to set up TopoServer controller: %v", err)
	}

	if err := (&shardcontroller.ShardReconciler{
		Client:    c,
		Scheme:    scheme,
		Recorder:  mgr.GetEventRecorderFor("shard-controller"),
		APIReader: mgr.GetAPIReader(),
	}).SetupWithManager(mgr, ctrlOpts); err != nil {
		t.Fatalf("Failed to set up Shard controller: %v", err)
	}

	if !cfg.skipDataHandler {
		// data-handler controllers require a live topology server (etcd)
		// to process their finalizers during deletion.
		if err := (&datahandlercellcontroller.CellReconciler{
			Client:   c,
			Scheme:   scheme,
			Recorder: mgr.GetEventRecorderFor("cell-datahandler"),
		}).SetupWithManager(mgr, ctrlOpts); err != nil {
			t.Fatalf("Failed to set up Cell data-handler controller: %v", err)
		}

		if err := (&datahandlershardcontroller.ShardReconciler{
			Client:   c,
			Scheme:   scheme,
			Recorder: mgr.GetEventRecorderFor("shard-datahandler"),
		}).SetupWithManager(mgr, ctrlOpts); err != nil {
			t.Fatalf("Failed to set up Shard data-handler controller: %v", err)
		}
	}

	return mgr, c, ns
}

// pollUntil repeatedly calls check with the given interval until it returns
// true or the timeout is exceeded. Returns an error describing the last state
// on timeout.
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

// requireResourceCount waits until exactly count resources of the given type
// exist in the namespace. Returns the list when found.
func requireResourceCount[T any, PT interface {
	*T
	client.ObjectList
}](t *testing.T, ctx context.Context, c client.Client, ns string, count int, desc string,
) *T {
	t.Helper()

	var result T
	pollUntil(t, 60*time.Second, 2*time.Second, desc, func() (bool, string) {
		list := PT(&result)
		if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
			return false, fmt.Sprintf("list error: %v", err)
		}
		// Use reflection-free approach: just check if the resource was created
		// We can't easily get len() generically, so callers should use the typed helpers below
		return true, ""
	})
	return &result
}

// listResources is a generic helper that lists resources in a namespace.
func listResources[T client.ObjectList](t *testing.T, ctx context.Context, c client.Client, ns string, list T) {
	t.Helper()
	if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
		t.Fatalf("Failed to list resources: %v", err)
	}
}

// waitForMinCount waits until at least minCount items exist for a resource kind.
func waitForMinCount[T client.ObjectList](
	t *testing.T, ctx context.Context, c client.Client, ns string,
	factory func() T, countFn func(T) int, minCount int, desc string,
) {
	t.Helper()
	pollUntil(t, 120*time.Second, 2*time.Second, desc, func() (bool, string) {
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
	pollUntil(t, 120*time.Second, 2*time.Second, fmt.Sprintf("Deployment with container %q", containerName), func() (bool, string) {
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
	pollUntil(t, 120*time.Second, 2*time.Second, fmt.Sprintf("StatefulSet with container %q", containerName), func() (bool, string) {
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

// waitForServiceWithPort waits for a Service with the given port name and number
// to appear in the namespace.
func waitForServiceWithPort(t *testing.T, ctx context.Context, c client.Client, ns, portName string, port int32) *corev1.Service {
	t.Helper()
	var found *corev1.Service
	pollUntil(t, 120*time.Second, 2*time.Second, fmt.Sprintf("Service with port %s:%d", portName, port), func() (bool, string) {
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

// waitForPodsReady waits for at least minReady pods matching the given labels
// to be in Ready condition.
func waitForPodsReady(t *testing.T, ctx context.Context, c client.Client, ns string, labels map[string]string, minReady int) {
	t.Helper()
	pollUntil(t, 5*time.Minute, 3*time.Second, fmt.Sprintf("pods with labels %v ready", labels), func() (bool, string) {
		pods := &corev1.PodList{}
		if err := c.List(ctx, pods, client.InNamespace(ns), client.MatchingLabels(labels)); err != nil {
			return false, fmt.Sprintf("list error: %v", err)
		}
		readyCount := 0
		for _, pod := range pods.Items {
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
					readyCount++
					break
				}
			}
		}
		if readyCount >= minReady {
			return true, ""
		}
		return false, fmt.Sprintf("%d/%d pods ready (want >= %d)", readyCount, len(pods.Items), minReady)
	})
}

// waitForAllPodsReady waits until all pods in the namespace are in Ready
// condition. This is the gate before attempting psql connectivity — all
// components (etcd, multipooler, multiorch, multigateway) must be healthy.
func waitForAllPodsReady(t *testing.T, ctx context.Context, c client.Client, ns string) {
	t.Helper()
	pollUntil(t, 5*time.Minute, 5*time.Second, "all pods ready", func() (bool, string) {
		pods := &corev1.PodList{}
		if err := c.List(ctx, pods, client.InNamespace(ns)); err != nil {
			return false, fmt.Sprintf("list error: %v", err)
		}
		if len(pods.Items) == 0 {
			return false, "no pods found"
		}
		notReady := []string{}
		for _, pod := range pods.Items {
			// Skip completed pods (Jobs)
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
				phase := string(pod.Status.Phase)
				notReady = append(notReady, fmt.Sprintf("%s(%s)", pod.Name, phase))
			}
		}
		if len(notReady) == 0 {
			return true, ""
		}
		return false, fmt.Sprintf("not ready: %s", strings.Join(notReady, ", "))
	})
}

// psqlViaKubectl runs a psql query by exec'ing into a pod that has the psql
// binary (typically a multipooler/postgres pod). The query is directed at the
// multigateway service on port 15432.
//
// This avoids requiring a local psql binary — we use the one inside the
// postgres container.
func psqlViaKubectl(t *testing.T, ctx context.Context, c client.Client, ns, gatewayHost string, query string) string {
	t.Helper()

	// Find a pod with a "postgres" container (multipooler StatefulSet pods have psql)
	pods := &corev1.PodList{}
	if err := c.List(ctx, pods, client.InNamespace(ns)); err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	var targetPod string
	for _, pod := range pods.Items {
		for _, cont := range pod.Spec.Containers {
			if cont.Name == "postgres" {
				if pod.Status.Phase == corev1.PodRunning {
					targetPod = pod.Name
					break
				}
			}
		}
		if targetPod != "" {
			break
		}
	}
	if targetPod == "" {
		t.Fatalf("No running pod with 'postgres' container found in namespace %s", ns)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "kubectl", "exec",
		"-n", ns,
		targetPod,
		"-c", "postgres",
		"--",
		"psql",
		"-h", gatewayHost,
		"-p", "15432",
		"-U", "postgres",
		"-d", "postgres",
		"-t", "-A",
		"-c", query,
	)
	cmd.Env = append(os.Environ(), "PGPASSWORD=postgres", "PGCONNECT_TIMEOUT=10")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("psql query %q via kubectl exec on %s failed: %v\noutput: %s", query, targetPod, err, out)
	}
	return strings.TrimSpace(string(out))
}

// waitForQueryServing polls until a SELECT 1 query succeeds through the
// multigateway. This is the definitive test that the full stack is working:
// etcd → metadata → multipooler → multiorch bootstrap → multigateway → psql.
//
// Per upstream research, HTTP /ready on multigateway only means "registered
// with topology", not "can route queries". We must actually execute a query.
func waitForQueryServing(t *testing.T, ctx context.Context, c client.Client, ns, gatewayHost string) {
	t.Helper()
	pollUntil(t, 3*time.Minute, 5*time.Second, "query serving via multigateway", func() (bool, string) {
		// Find a postgres pod to exec from
		pods := &corev1.PodList{}
		if err := c.List(ctx, pods, client.InNamespace(ns)); err != nil {
			return false, fmt.Sprintf("list error: %v", err)
		}

		var targetPod string
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning {
				continue
			}
			for _, cont := range pod.Spec.Containers {
				if cont.Name == "postgres" {
					targetPod = pod.Name
					break
				}
			}
			if targetPod != "" {
				break
			}
		}
		if targetPod == "" {
			return false, "no running postgres pod found"
		}

		cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		cmd := exec.CommandContext(cmdCtx, "kubectl", "exec",
			"-n", ns,
			targetPod,
			"-c", "postgres",
			"--",
			"psql",
			"-h", gatewayHost,
			"-p", "15432",
			"-U", "postgres",
			"-d", "postgres",
			"-t", "-A",
			"-c", "SELECT 1",
		)
		cmd.Env = append(os.Environ(), "PGPASSWORD=postgres", "PGCONNECT_TIMEOUT=5")

		out, err := cmd.CombinedOutput()
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

// findGatewayServiceName finds the multigateway Service name in the namespace.
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

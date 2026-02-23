//go:build e2e

package e2e_test

import (
	"testing"

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
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	return scheme
}

// setUpOperator starts a namespace-scoped manager against the kind cluster and
// registers all operator controllers. It returns the running manager and a
// Kubernetes client scoped to the test namespace.
func setUpOperator(t *testing.T) (manager.Manager, client.Client) {
	t.Helper()

	scheme := newScheme(t)
	mgr, _ := testutil.SetUpKindManager(t, scheme,
		testutil.WithKindCRDPaths("../../config/crd/bases"),
	)
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

	// data-handler controllers
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

	return mgr, c
}

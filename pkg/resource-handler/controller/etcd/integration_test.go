//go:build integration
// +build integration

package etcd_test

import (
	"testing"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/testutil"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	etcdcontroller "github.com/numtide/multigres-operator/pkg/resource-handler/controller/etcd"
)

func TestSetupWithManager(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	cfg := testutil.SetupEnvtestWithKubeconfig(t)
	mgr := testutil.StartManager(t, cfg, scheme)

	if err := (&etcdcontroller.EtcdReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		t.Fatalf("Failed to create controller, %v", err)
	}
}

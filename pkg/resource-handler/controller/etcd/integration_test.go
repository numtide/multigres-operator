//go:build integration
// +build integration

package etcd_test

import (
	"testing"
	"time"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/testutil"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	etcdcontroller "github.com/numtide/multigres-operator/pkg/resource-handler/controller/etcd"
)

func TestSetupWithManager(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	cfg := testutil.SetUpEnvtest(t)
	mgr := testutil.SetUpManager(t, cfg, scheme)
	testutil.StartManager(t, mgr)

	if err := (&etcdcontroller.EtcdReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		t.Fatalf("Failed to create controller, %v", err)
	}
}

func TestEtcdReconciliation(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := map[string]struct {
		etcd            *multigresv1alpha1.Etcd
		existingObjects []client.Object
		failureConfig   *testutil.FailureConfig
		// TODO: If wantErr is false but failureConfig is set, assertions may fail
		// due to failure injection. This should be addressed when we need to test
		// partial failures that don't prevent reconciliation success.
		wantErr     bool
		wantRequeue bool
		assertFunc  func(t *testing.T, c client.Client, etcd *multigresv1alpha1.Etcd)
	}{
		"something": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
		},
		"another test": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			cfg := testutil.SetUpEnvtest(t)
			mgr := testutil.SetUpManager(t, cfg, scheme)
			testutil.StartManager(t, mgr)

			// For getting cluster resources
			watcher := testutil.NewResourceWatcher(t, ctx, mgr,
				testutil.WithExtraResource(&multigresv1alpha1.Etcd{}),
			)

			etcdReconciler := &etcdcontroller.EtcdReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}
			if err := etcdReconciler.SetupWithManager(mgr, controller.Options{
				// Needed for the parallel test runs
				SkipNameValidation: ptr.To(true),
			}); err != nil {
				t.Fatalf("Failed to create controller, %v", err)
			}

			client := mgr.GetClient()
			if err := client.Create(ctx, tc.etcd); err != nil {
				t.Fatalf("Failed to create the initial item, %v", err)
			}

			// // Verify events captured the reconciliation
			// if len(events) == 0 {
			// 	t.Fatal("No events captured")
			// }
			time.Sleep(3 * time.Second)

			allEvents := watcher.Events()
			t.Logf("Total events collected: %d", len(allEvents))

			stsEvents := watcher.ForKind("StatefulSet")
			sts := stsEvents[0].Object.(*appsv1.StatefulSet)
			t.Fatalf("%#v", sts)

			// NOTE: this is how I can get the list of object from API server
			// l := corev1.ServiceList{}
			// if err := mgr.GetCache().List(ctx, &l); err != nil {
			// 	t.Errorf("some err: %v", err)
			// }
			// t.Errorf("list data %#v", l)

			data := multigresv1alpha1.Etcd{}
			if err := client.Get(ctx, types.NamespacedName{
				Name:      "test-etcd",
				Namespace: "default",
			}, &data); err != nil {
				t.Errorf("failed to get, %v", err)
			}
			// Testing only
			t.Errorf("data: %v", data)
		})
	}

}

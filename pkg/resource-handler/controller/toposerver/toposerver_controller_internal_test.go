package toposerver

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

// TestReconcileStatefulSet_InvalidScheme tests the error path when BuildStatefulSet fails.
// This should never happen in production - scheme is properly set up in main.go.
// Test exists for coverage of defensive error handling.
func TestReconcileStatefulSet_InvalidScheme(t *testing.T) {
	// Empty scheme without TopoServer type registered
	invalidScheme := runtime.NewScheme()

	toposerver := &multigresv1alpha1.TopoServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-toposerver",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.TopoServerSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &TopoServerReconciler{
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcileStatefulSet(context.Background(), toposerver)
	if err == nil {
		t.Error("reconcileStatefulSet() should error with invalid scheme")
	}
}

// TestReconcileHeadlessService_InvalidScheme tests the error path when BuildHeadlessService fails.
func TestReconcileHeadlessService_InvalidScheme(t *testing.T) {
	invalidScheme := runtime.NewScheme()

	toposerver := &multigresv1alpha1.TopoServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-toposerver",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.TopoServerSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &TopoServerReconciler{
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcileHeadlessService(context.Background(), toposerver)
	if err == nil {
		t.Error("reconcileHeadlessService() should error with invalid scheme")
	}
}

// TestReconcileClientService_InvalidScheme tests the error path when BuildClientService fails.
func TestReconcileClientService_InvalidScheme(t *testing.T) {
	invalidScheme := runtime.NewScheme()

	toposerver := &multigresv1alpha1.TopoServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-toposerver",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.TopoServerSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &TopoServerReconciler{
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcileClientService(context.Background(), toposerver)
	if err == nil {
		t.Error("reconcileClientService() should error with invalid scheme")
	}
}

// TestUpdateStatus_StatefulSetNotFound tests the NotFound path in updateStatus.
func TestUpdateStatus_StatefulSetNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme) // Need StatefulSet type registered for Get to work

	toposerver := &multigresv1alpha1.TopoServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-toposerver",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.TopoServerSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(toposerver).
		WithStatusSubresource(&multigresv1alpha1.TopoServer{}).
		Build()

	reconciler := &TopoServerReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// Call updateStatus when StatefulSet doesn't exist yet
	err := reconciler.updateStatus(context.Background(), toposerver)
	if err != nil {
		t.Errorf("updateStatus() should not error when StatefulSet not found, got: %v", err)
	}
}

// TestReconcileClientService_PatchError tests error path on Patch client Service.
func TestReconcileClientService_PatchError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	toposerver := &multigresv1alpha1.TopoServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-toposerver",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.TopoServerSpec{},
	}

	// Create client with failure injection
	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(toposerver).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnPatch: func(obj client.Object) error {
			if obj.GetName() == "test-toposerver" {
				return testutil.ErrNetworkTimeout
			}
			return nil
		},
	})

	reconciler := &TopoServerReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.reconcileClientService(context.Background(), toposerver)
	if err == nil {
		t.Error("reconcileClientService() should error on Patch failure")
	}
}

// TestUpdateStatus_GetError tests error path on Get StatefulSet (not NotFound).
func TestUpdateStatus_GetError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	toposerver := &multigresv1alpha1.TopoServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-toposerver",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.TopoServerSpec{},
	}

	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(toposerver).
		WithStatusSubresource(&multigresv1alpha1.TopoServer{}).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnGet: testutil.FailOnKeyName("test-toposerver", testutil.ErrNetworkTimeout),
	})

	reconciler := &TopoServerReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.updateStatus(context.Background(), toposerver)
	if err == nil {
		t.Error("updateStatus() should error on Get failure")
	}
}

// TestSetupWithManager tests the manager setup function.
func TestSetupWithManager(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// dummy config
	cfg := &rest.Config{Host: "http://localhost:8080"}

	createMgr := func() ctrl.Manager {
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:  scheme,
			Metrics: metricsserver.Options{BindAddress: "0"},
		})
		if err != nil {
			t.Fatalf("Failed to create manager: %v", err)
		}
		return mgr
	}

	t.Run("default options", func(t *testing.T) {
		mgr := createMgr()
		r := &TopoServerReconciler{Client: mgr.GetClient(), Scheme: scheme}
		if err := r.SetupWithManager(mgr); err != nil {
			t.Errorf("SetupWithManager() error = %v", err)
		}
	})

	t.Run("with options", func(t *testing.T) {
		mgr := createMgr()
		r := &TopoServerReconciler{Client: mgr.GetClient(), Scheme: scheme}
		if err := r.SetupWithManager(mgr, controller.Options{
			MaxConcurrentReconciles: 1,
			SkipNameValidation:      ptr.To(true),
		}); err != nil {
			t.Errorf("SetupWithManager() with opts error = %v", err)
		}
	})
}

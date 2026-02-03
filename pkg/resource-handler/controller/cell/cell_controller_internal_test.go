package cell

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

// TestReconcileMultiGatewayDeployment_InvalidScheme tests the error path when BuildMultiGatewayDeployment fails.
// This should never happen in production - scheme is properly set up in main.go.
// Test exists for coverage of defensive error handling.
func TestReconcileMultiGatewayDeployment_InvalidScheme(t *testing.T) {
	// Empty scheme without Cell type registered
	invalidScheme := runtime.NewScheme()

	cell := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cell",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.CellSpec{
			Name: "zone1",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &CellReconciler{
		Client:   fakeClient,
		Scheme:   invalidScheme,
		Recorder: record.NewFakeRecorder(100),
	}

	err := reconciler.reconcileMultiGatewayDeployment(context.Background(), cell)
	if err == nil {
		t.Error("reconcileMultiGatewayDeployment() should error with invalid scheme")
	}
}

// TestReconcileMultiGatewayService_InvalidScheme tests the error path when BuildMultiGatewayService fails.
func TestReconcileMultiGatewayService_InvalidScheme(t *testing.T) {
	invalidScheme := runtime.NewScheme()

	cell := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cell",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.CellSpec{
			Name: "zone1",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &CellReconciler{
		Client:   fakeClient,
		Scheme:   invalidScheme,
		Recorder: record.NewFakeRecorder(100),
	}

	err := reconciler.reconcileMultiGatewayService(context.Background(), cell)
	if err == nil {
		t.Error("reconcileMultiGatewayService() should error with invalid scheme")
	}
}

// TestUpdateStatus_MultiGatewayDeploymentNotFound tests the NotFound path in updateStatus.
func TestUpdateStatus_MultiGatewayDeploymentNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme) // Need Deployment type registered for Get to work

	cell := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cell",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.CellSpec{
			Name: "zone1",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cell).
		WithStatusSubresource(&multigresv1alpha1.Cell{}).
		Build()

	reconciler := &CellReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(100),
	}

	// Call updateStatus when MultiGateway Deployment doesn't exist yet
	err := reconciler.updateStatus(context.Background(), cell)
	if err != nil {
		t.Errorf(
			"updateStatus() should not error when MultiGateway Deployment not found, got: %v",
			err,
		)
	}
}

// TestReconcileMultiGatewayDeployment_PatchError tests error path on Patch MultiGateway Deployment.
func TestReconcileMultiGatewayDeployment_PatchError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	cell := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cell",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.CellSpec{
			Name: "zone1",
		},
	}

	// Create client with failure injection
	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cell).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnPatch: func(obj client.Object) error {
			if strings.Contains(obj.GetName(), "multigateway") {
				return testutil.ErrNetworkTimeout
			}
			return nil
		},
	})

	reconciler := &CellReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(100),
	}

	err := reconciler.reconcileMultiGatewayDeployment(context.Background(), cell)
	if err == nil {
		t.Error("reconcileMultiGatewayDeployment() should error on Patch failure")
	}
}

// TestReconcileMultiGatewayService_PatchError tests error path on Patch MultiGateway Service.
func TestReconcileMultiGatewayService_PatchError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	cell := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cell",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.CellSpec{
			Name: "zone1",
		},
	}

	// Create client with failure injection
	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cell).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnPatch: func(obj client.Object) error {
			if strings.Contains(obj.GetName(), "multigateway") {
				return testutil.ErrNetworkTimeout
			}
			return nil
		},
	})

	reconciler := &CellReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(100),
	}

	err := reconciler.reconcileMultiGatewayService(context.Background(), cell)
	if err == nil {
		t.Error("reconcileMultiGatewayService() should error on Patch failure")
	}
}

// TestUpdateStatus_GetError tests error path on Get MultiGateway Deployment (not NotFound).
func TestUpdateStatus_GetError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	cell := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cell",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.CellSpec{
			Name: "zone1",
		},
	}

	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cell).
		WithStatusSubresource(&multigresv1alpha1.Cell{}).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnGet: func(key client.ObjectKey) error {
			if strings.Contains(key.Name, "multigateway") {
				return testutil.ErrNetworkTimeout
			}
			return nil
		},
	})

	reconciler := &CellReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(100),
	}

	err := reconciler.updateStatus(context.Background(), cell)
	if err == nil {
		t.Error("updateStatus() should error on Get failure")
	}
}

// TestSetConditions_ZeroReplicas tests setConditions when deployments have zero replicas.
func TestSetConditions_ZeroReplicas(t *testing.T) {
	reconciler := &CellReconciler{
		Recorder: record.NewFakeRecorder(100),
	}

	cell := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-cell",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: multigresv1alpha1.CellSpec{
			Name: "zone1",
		},
	}

	mgDeploy := &appsv1.Deployment{
		Status: appsv1.DeploymentStatus{
			Replicas:      0,
			ReadyReplicas: 0,
		},
	}

	reconciler.setConditions(cell, mgDeploy)
	conditions := cell.Status.Conditions

	if len(conditions) != 2 {
		t.Fatalf("setConditions() should set 2 conditions, got %d", len(conditions))
	}

	availCond := conditions[0]
	if availCond.Type != "Available" {
		t.Errorf("Condition type = %s, want Available", availCond.Type)
	}
	if availCond.Status != metav1.ConditionFalse {
		t.Errorf("Condition status = %s, want False (zero replicas)", availCond.Status)
	}
	if availCond.Reason != "MultiGatewayUnavailable" {
		t.Errorf("Condition reason = %s, want MultiGatewayUnavailable", availCond.Reason)
	}

	readyCond := conditions[1]
	if readyCond.Type != "Ready" {
		t.Errorf("Condition type = %s, want Ready", readyCond.Type)
	}
	if readyCond.Status != metav1.ConditionFalse {
		t.Errorf("Condition status = %s, want False", readyCond.Status)
	}
	if readyCond.Reason != "MultiGatewayNotReady" {
		t.Errorf("Condition reason = %s, want MultiGatewayNotReady", readyCond.Reason)
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
		r := &CellReconciler{
			Client:   mgr.GetClient(),
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(100),
		}
		if err := r.SetupWithManager(mgr); err != nil {
			t.Errorf("SetupWithManager() error = %v", err)
		}
	})

	t.Run("with options", func(t *testing.T) {
		mgr := createMgr()
		r := &CellReconciler{
			Client:   mgr.GetClient(),
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(100),
		}
		if err := r.SetupWithManager(mgr, controller.Options{
			MaxConcurrentReconciles: 1,
			SkipNameValidation:      ptr.To(true),
		}); err != nil {
			t.Errorf("SetupWithManager() with opts error = %v", err)
		}
	})
}

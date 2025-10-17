package multigateway

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/testutil"
)

// TestReconcileDeployment_InvalidScheme tests the error path when BuildDeployment fails.
// This should never happen in production - scheme is properly set up in main.go.
// Test exists for coverage of defensive error handling.
func TestReconcileDeployment_InvalidScheme(t *testing.T) {
	// Empty scheme without Etcd type registered
	invalidScheme := runtime.NewScheme()

	mg := &multigresv1alpha1.MultiGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-multigateway",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.MultiGatewaySpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &MultiGatewayReconciler{
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcileDeployment(context.Background(), mg)
	if err == nil {
		t.Error("reconcileDeployment() should error with invalid scheme")
	}
}

// TestReconcileService_InvalidScheme tests the error path when BuildClientService fails.
func TestReconcileService_InvalidScheme(t *testing.T) {
	invalidScheme := runtime.NewScheme()

	mg := &multigresv1alpha1.MultiGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-multigateway",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.MultiGatewaySpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &MultiGatewayReconciler{
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcileService(context.Background(), mg)
	if err == nil {
		t.Error("reconcileService() should error with invalid scheme")
	}
}

// TestUpdateStatus_DeploymentNotFound tests the NotFound path in updateStatus.
func TestUpdateStatus_DeploymentNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme) // Need Deployment type registered for Get to work

	mg := &multigresv1alpha1.MultiGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-multigateway",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.MultiGatewaySpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(mg).
		WithStatusSubresource(&multigresv1alpha1.MultiGateway{}).
		Build()

	reconciler := &MultiGatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// Call updateStatus when Deployment doesn't exist yet
	err := reconciler.updateStatus(context.Background(), mg)
	if err != nil {
		t.Errorf("updateStatus() should not error when Deployment not found, got: %v", err)
	}
}

// TestHandleDeletion_NoFinalizer tests early return when no finalizer is present.
func TestHandleDeletion_NoFinalizer(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	mg := &multigresv1alpha1.MultiGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-multigateway",
			Namespace:  "default",
			Finalizers: []string{}, // No finalizer
		},
		Spec: multigresv1alpha1.MultiGatewaySpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(mg).
		Build()

	reconciler := &MultiGatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	result, err := reconciler.handleDeletion(context.Background(), mg)
	if err != nil {
		t.Errorf("handleDeletion() should not error when no finalizer, got: %v", err)
	}
	if result.RequeueAfter > 0 {
		t.Error("handleDeletion() should not requeue when no finalizer")
	}
}

// TestReconcileService_GetError tests error path on Get client Service (not NotFound).
func TestReconcileService_GetError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	mg := &multigresv1alpha1.MultiGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-multigateway",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.MultiGatewaySpec{},
	}

	// Create client with failure injection
	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(mg).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnGet: testutil.FailOnKeyName("test-multigateway", testutil.ErrNetworkTimeout),
	})

	reconciler := &MultiGatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.reconcileService(context.Background(), mg)
	if err == nil {
		t.Error("reconcileService() should error on Get failure")
	}
}

// TestUpdateStatus_GetError tests error path on Get StatefulSet (not NotFound).
func TestUpdateStatus_GetError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	mg := &multigresv1alpha1.MultiGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-multigateway",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.MultiGatewaySpec{},
	}

	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(mg).
		WithStatusSubresource(&multigresv1alpha1.MultiGateway{}).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnGet: testutil.FailOnKeyName("test-multigateway", testutil.ErrNetworkTimeout),
	})

	reconciler := &MultiGatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.updateStatus(context.Background(), mg)
	if err == nil {
		t.Error("updateStatus() should error on Get failure")
	}
}

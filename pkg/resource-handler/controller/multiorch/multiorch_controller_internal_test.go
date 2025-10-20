package multiorch

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
	// Empty scheme without MultiOrch type registered
	invalidScheme := runtime.NewScheme()

	multiorch := &multigresv1alpha1.MultiOrch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-multiorch",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.MultiOrchSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &MultiOrchReconciler{
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcileDeployment(context.Background(), multiorch)
	if err == nil {
		t.Error("reconcileDeployment() should error with invalid scheme")
	}
}

// TestReconcileService_InvalidScheme tests the error path when BuildService fails.
func TestReconcileService_InvalidScheme(t *testing.T) {
	invalidScheme := runtime.NewScheme()

	multiorch := &multigresv1alpha1.MultiOrch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-multiorch",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.MultiOrchSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &MultiOrchReconciler{
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcileService(context.Background(), multiorch)
	if err == nil {
		t.Error("reconcileService() should error with invalid scheme")
	}
}

// TestUpdateStatus_DeploymentNotFound tests the NotFound path in updateStatus.
func TestUpdateStatus_DeploymentNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme) // Need Deployment type registered for Get to work

	multiorch := &multigresv1alpha1.MultiOrch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-multiorch",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.MultiOrchSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(multiorch).
		WithStatusSubresource(&multigresv1alpha1.MultiOrch{}).
		Build()

	reconciler := &MultiOrchReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// Call updateStatus when Deployment doesn't exist yet
	err := reconciler.updateStatus(context.Background(), multiorch)
	if err != nil {
		t.Errorf("updateStatus() should not error when Deployment not found, got: %v", err)
	}
}

// TestHandleDeletion_NoFinalizer tests early return when no finalizer is present.
func TestHandleDeletion_NoFinalizer(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	multiorch := &multigresv1alpha1.MultiOrch{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-multiorch",
			Namespace:  "default",
			Finalizers: []string{}, // No finalizer
		},
		Spec: multigresv1alpha1.MultiOrchSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(multiorch).
		Build()

	reconciler := &MultiOrchReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	result, err := reconciler.handleDeletion(context.Background(), multiorch)
	if err != nil {
		t.Errorf("handleDeletion() should not error when no finalizer, got: %v", err)
	}
	if result.RequeueAfter > 0 {
		t.Error("handleDeletion() should not requeue when no finalizer")
	}
}

// TestReconcileService_GetError tests error path on Get Service (not NotFound).
func TestReconcileService_GetError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	multiorch := &multigresv1alpha1.MultiOrch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-multiorch",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.MultiOrchSpec{},
	}

	// Create client with failure injection
	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(multiorch).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnGet: testutil.FailOnKeyName("test-multiorch", testutil.ErrNetworkTimeout),
	})

	reconciler := &MultiOrchReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.reconcileService(context.Background(), multiorch)
	if err == nil {
		t.Error("reconcileService() should error on Get failure")
	}
}

// TestUpdateStatus_GetError tests error path on Get Deployment (not NotFound).
func TestUpdateStatus_GetError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	multiorch := &multigresv1alpha1.MultiOrch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-multiorch",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.MultiOrchSpec{},
	}

	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(multiorch).
		WithStatusSubresource(&multigresv1alpha1.MultiOrch{}).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnGet: testutil.FailOnKeyName("test-multiorch", testutil.ErrNetworkTimeout),
	})

	reconciler := &MultiOrchReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.updateStatus(context.Background(), multiorch)
	if err == nil {
		t.Error("updateStatus() should error on Get failure")
	}
}

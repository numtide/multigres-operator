package multipooler

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

// TestReconcileStatefulSet_InvalidScheme tests the error path when BuildStatefulSet fails.
// This should never happen in production - scheme is properly set up in main.go.
// Test exists for coverage of defensive error handling.
func TestReconcileStatefulSet_InvalidScheme(t *testing.T) {
	// Empty scheme without MultiPooler type registered
	invalidScheme := runtime.NewScheme()

	multipooler := &multigresv1alpha1.MultiPooler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-multipooler",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.MultiPoolerSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &MultiPoolerReconciler{
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcileStatefulSet(context.Background(), multipooler)
	if err == nil {
		t.Error("reconcileStatefulSet() should error with invalid scheme")
	}
}

// TestReconcileHeadlessService_InvalidScheme tests the error path when BuildHeadlessService fails.
func TestReconcileHeadlessService_InvalidScheme(t *testing.T) {
	invalidScheme := runtime.NewScheme()

	multipooler := &multigresv1alpha1.MultiPooler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-multipooler",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.MultiPoolerSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &MultiPoolerReconciler{
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcileHeadlessService(context.Background(), multipooler)
	if err == nil {
		t.Error("reconcileHeadlessService() should error with invalid scheme")
	}
}

// TestUpdateStatus_StatefulSetNotFound tests the NotFound path in updateStatus.
func TestUpdateStatus_StatefulSetNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme) // Need StatefulSet type registered for Get to work

	multipooler := &multigresv1alpha1.MultiPooler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-multipooler",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.MultiPoolerSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(multipooler).
		WithStatusSubresource(&multigresv1alpha1.MultiPooler{}).
		Build()

	reconciler := &MultiPoolerReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// Call updateStatus when StatefulSet doesn't exist yet
	err := reconciler.updateStatus(context.Background(), multipooler)
	if err != nil {
		t.Errorf("updateStatus() should not error when StatefulSet not found, got: %v", err)
	}
}

// TestHandleDeletion_NoFinalizer tests early return when no finalizer is present.
func TestHandleDeletion_NoFinalizer(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	multipooler := &multigresv1alpha1.MultiPooler{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-multipooler",
			Namespace:  "default",
			Finalizers: []string{}, // No finalizer
		},
		Spec: multigresv1alpha1.MultiPoolerSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(multipooler).
		Build()

	reconciler := &MultiPoolerReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	result, err := reconciler.handleDeletion(context.Background(), multipooler)
	if err != nil {
		t.Errorf("handleDeletion() should not error when no finalizer, got: %v", err)
	}
	if result.Requeue {
		t.Error("handleDeletion() should not requeue when no finalizer")
	}
}

// TestReconcileHeadlessService_GetError tests error path on Get headless Service (not NotFound).
func TestReconcileHeadlessService_GetError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	multipooler := &multigresv1alpha1.MultiPooler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-multipooler",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.MultiPoolerSpec{},
	}

	// Create client with failure injection
	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(multipooler).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnGet: testutil.FailOnKeyName("test-multipooler-headless", testutil.ErrNetworkTimeout),
	})

	reconciler := &MultiPoolerReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.reconcileHeadlessService(context.Background(), multipooler)
	if err == nil {
		t.Error("reconcileHeadlessService() should error on Get failure")
	}
}

// TestUpdateStatus_GetError tests error path on Get StatefulSet (not NotFound).
func TestUpdateStatus_GetError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	multipooler := &multigresv1alpha1.MultiPooler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-multipooler",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.MultiPoolerSpec{},
	}

	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(multipooler).
		WithStatusSubresource(&multigresv1alpha1.MultiPooler{}).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnGet: testutil.FailOnKeyName("test-multipooler", testutil.ErrNetworkTimeout),
	})

	reconciler := &MultiPoolerReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.updateStatus(context.Background(), multipooler)
	if err == nil {
		t.Error("updateStatus() should error on Get failure")
	}
}

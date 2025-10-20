package etcd

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
	// Empty scheme without Etcd type registered
	invalidScheme := runtime.NewScheme()

	etcd := &multigresv1alpha1.Etcd{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-etcd",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.EtcdSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &EtcdReconciler{
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcileStatefulSet(context.Background(), etcd)
	if err == nil {
		t.Error("reconcileStatefulSet() should error with invalid scheme")
	}
}

// TestReconcileHeadlessService_InvalidScheme tests the error path when BuildHeadlessService fails.
func TestReconcileHeadlessService_InvalidScheme(t *testing.T) {
	invalidScheme := runtime.NewScheme()

	etcd := &multigresv1alpha1.Etcd{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-etcd",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.EtcdSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &EtcdReconciler{
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcileHeadlessService(context.Background(), etcd)
	if err == nil {
		t.Error("reconcileHeadlessService() should error with invalid scheme")
	}
}

// TestReconcileClientService_InvalidScheme tests the error path when BuildClientService fails.
func TestReconcileClientService_InvalidScheme(t *testing.T) {
	invalidScheme := runtime.NewScheme()

	etcd := &multigresv1alpha1.Etcd{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-etcd",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.EtcdSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &EtcdReconciler{
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcileClientService(context.Background(), etcd)
	if err == nil {
		t.Error("reconcileClientService() should error with invalid scheme")
	}
}

// TestUpdateStatus_StatefulSetNotFound tests the NotFound path in updateStatus.
func TestUpdateStatus_StatefulSetNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme) // Need StatefulSet type registered for Get to work

	etcd := &multigresv1alpha1.Etcd{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-etcd",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.EtcdSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(etcd).
		WithStatusSubresource(&multigresv1alpha1.Etcd{}).
		Build()

	reconciler := &EtcdReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// Call updateStatus when StatefulSet doesn't exist yet
	err := reconciler.updateStatus(context.Background(), etcd)
	if err != nil {
		t.Errorf("updateStatus() should not error when StatefulSet not found, got: %v", err)
	}
}

// TestHandleDeletion_NoFinalizer tests early return when no finalizer is present.
func TestHandleDeletion_NoFinalizer(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	etcd := &multigresv1alpha1.Etcd{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-etcd",
			Namespace:  "default",
			Finalizers: []string{}, // No finalizer
		},
		Spec: multigresv1alpha1.EtcdSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(etcd).
		Build()

	reconciler := &EtcdReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	result, err := reconciler.handleDeletion(context.Background(), etcd)
	if err != nil {
		t.Errorf("handleDeletion() should not error when no finalizer, got: %v", err)
	}
	if result.RequeueAfter > 0 {
		t.Error("handleDeletion() should not requeue when no finalizer")
	}
}

// TestReconcileClientService_GetError tests error path on Get client Service (not NotFound).
func TestReconcileClientService_GetError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	etcd := &multigresv1alpha1.Etcd{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-etcd",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.EtcdSpec{},
	}

	// Create client with failure injection
	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(etcd).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnGet: testutil.FailOnKeyName("test-etcd", testutil.ErrNetworkTimeout),
	})

	reconciler := &EtcdReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.reconcileClientService(context.Background(), etcd)
	if err == nil {
		t.Error("reconcileClientService() should error on Get failure")
	}
}

// TestUpdateStatus_GetError tests error path on Get StatefulSet (not NotFound).
func TestUpdateStatus_GetError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	etcd := &multigresv1alpha1.Etcd{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-etcd",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.EtcdSpec{},
	}

	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(etcd).
		WithStatusSubresource(&multigresv1alpha1.Etcd{}).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnGet: testutil.FailOnKeyName("test-etcd", testutil.ErrNetworkTimeout),
	})

	reconciler := &EtcdReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.updateStatus(context.Background(), etcd)
	if err == nil {
		t.Error("updateStatus() should error on Get failure")
	}
}

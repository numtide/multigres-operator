package cell

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
		Client: fakeClient,
		Scheme: invalidScheme,
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
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcileMultiGatewayService(context.Background(), cell)
	if err == nil {
		t.Error("reconcileMultiGatewayService() should error with invalid scheme")
	}
}

// TestReconcileMultiOrchDeployment_InvalidScheme tests the error path when BuildMultiOrchDeployment fails.
func TestReconcileMultiOrchDeployment_InvalidScheme(t *testing.T) {
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
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcileMultiOrchDeployment(context.Background(), cell)
	if err == nil {
		t.Error("reconcileMultiOrchDeployment() should error with invalid scheme")
	}
}

// TestReconcileMultiOrchService_InvalidScheme tests the error path when BuildMultiOrchService fails.
func TestReconcileMultiOrchService_InvalidScheme(t *testing.T) {
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
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcileMultiOrchService(context.Background(), cell)
	if err == nil {
		t.Error("reconcileMultiOrchService() should error with invalid scheme")
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
		Client: fakeClient,
		Scheme: scheme,
	}

	// Call updateStatus when MultiGateway Deployment doesn't exist yet
	err := reconciler.updateStatus(context.Background(), cell)
	if err != nil {
		t.Errorf("updateStatus() should not error when MultiGateway Deployment not found, got: %v", err)
	}
}

// TestUpdateStatus_MultiOrchDeploymentNotFound tests when MultiOrch Deployment is not found.
func TestUpdateStatus_MultiOrchDeploymentNotFound(t *testing.T) {
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

	// Create MultiGateway Deployment but not MultiOrch
	mgDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cell-multigateway",
			Namespace: "default",
		},
		Status: appsv1.DeploymentStatus{
			Replicas:      2,
			ReadyReplicas: 2,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cell, mgDeploy).
		WithStatusSubresource(&multigresv1alpha1.Cell{}).
		Build()

	reconciler := &CellReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// Call updateStatus when MultiOrch Deployment doesn't exist
	err := reconciler.updateStatus(context.Background(), cell)
	if err != nil {
		t.Errorf("updateStatus() should not error when MultiOrch Deployment not found, got: %v", err)
	}
}

// TestHandleDeletion_NoFinalizer tests early return when no finalizer is present.
func TestHandleDeletion_NoFinalizer(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	cell := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-cell",
			Namespace:  "default",
			Finalizers: []string{}, // No finalizer
		},
		Spec: multigresv1alpha1.CellSpec{
			Name: "zone1",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cell).
		Build()

	reconciler := &CellReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	result, err := reconciler.handleDeletion(context.Background(), cell)
	if err != nil {
		t.Errorf("handleDeletion() should not error when no finalizer, got: %v", err)
	}
	if result.RequeueAfter > 0 {
		t.Error("handleDeletion() should not requeue when no finalizer")
	}
}

// TestReconcileMultiGatewayDeployment_GetError tests error path on Get MultiGateway Deployment (not NotFound).
func TestReconcileMultiGatewayDeployment_GetError(t *testing.T) {
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
		OnGet: testutil.FailOnKeyName("test-cell-multigateway", testutil.ErrNetworkTimeout),
	})

	reconciler := &CellReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.reconcileMultiGatewayDeployment(context.Background(), cell)
	if err == nil {
		t.Error("reconcileMultiGatewayDeployment() should error on Get failure")
	}
}

// TestReconcileMultiGatewayService_GetError tests error path on Get MultiGateway Service (not NotFound).
func TestReconcileMultiGatewayService_GetError(t *testing.T) {
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
		OnGet: testutil.FailOnKeyName("test-cell-multigateway", testutil.ErrNetworkTimeout),
	})

	reconciler := &CellReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.reconcileMultiGatewayService(context.Background(), cell)
	if err == nil {
		t.Error("reconcileMultiGatewayService() should error on Get failure")
	}
}

// TestReconcileMultiOrchDeployment_GetError tests error path on Get MultiOrch Deployment (not NotFound).
func TestReconcileMultiOrchDeployment_GetError(t *testing.T) {
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
		OnGet: testutil.FailOnKeyName("test-cell-multiorch", testutil.ErrNetworkTimeout),
	})

	reconciler := &CellReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.reconcileMultiOrchDeployment(context.Background(), cell)
	if err == nil {
		t.Error("reconcileMultiOrchDeployment() should error on Get failure")
	}
}

// TestReconcileMultiOrchService_GetError tests error path on Get MultiOrch Service (not NotFound).
func TestReconcileMultiOrchService_GetError(t *testing.T) {
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
		OnGet: testutil.FailOnKeyName("test-cell-multiorch", testutil.ErrNetworkTimeout),
	})

	reconciler := &CellReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.reconcileMultiOrchService(context.Background(), cell)
	if err == nil {
		t.Error("reconcileMultiOrchService() should error on Get failure")
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
		OnGet: testutil.FailOnKeyName("test-cell-multigateway", testutil.ErrNetworkTimeout),
	})

	reconciler := &CellReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.updateStatus(context.Background(), cell)
	if err == nil {
		t.Error("updateStatus() should error on Get failure")
	}
}

// TestUpdateStatus_GetMultiOrchError tests error path on Get MultiOrch Deployment (not NotFound).
func TestUpdateStatus_GetMultiOrchError(t *testing.T) {
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

	// Create MultiGateway Deployment so we get past that Get
	mgDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cell-multigateway",
			Namespace: "default",
		},
		Status: appsv1.DeploymentStatus{
			Replicas:      2,
			ReadyReplicas: 2,
		},
	}

	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cell, mgDeploy).
		WithStatusSubresource(&multigresv1alpha1.Cell{}).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnGet: testutil.FailOnKeyName("test-cell-multiorch", testutil.ErrNetworkTimeout),
	})

	reconciler := &CellReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.updateStatus(context.Background(), cell)
	if err == nil {
		t.Error("updateStatus() should error on Get MultiOrch failure")
	}
}

// TestBuildConditions_NilMultiOrchDeployment tests buildConditions when MultiOrch deployment is nil.
func TestBuildConditions_NilMultiOrchDeployment(t *testing.T) {
	reconciler := &CellReconciler{}

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
			Replicas:      2,
			ReadyReplicas: 2,
		},
	}

	// Pass nil for MultiOrch deployment
	conditions := reconciler.buildConditions(cell, mgDeploy, nil)

	if len(conditions) == 0 {
		t.Fatal("buildConditions() should return conditions")
	}

	readyCondition := conditions[0]
	if readyCondition.Type != "Ready" {
		t.Errorf("Condition type = %s, want Ready", readyCondition.Type)
	}
	if readyCondition.Status != metav1.ConditionFalse {
		t.Errorf("Condition status = %s, want False (MultiOrch is nil)", readyCondition.Status)
	}
	if readyCondition.Reason != "ComponentsNotReady" {
		t.Errorf("Condition reason = %s, want ComponentsNotReady", readyCondition.Reason)
	}
}

// TestBuildConditions_ZeroReplicas tests buildConditions when deployments have zero replicas.
func TestBuildConditions_ZeroReplicas(t *testing.T) {
	reconciler := &CellReconciler{}

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

	moDeploy := &appsv1.Deployment{
		Status: appsv1.DeploymentStatus{
			Replicas:      0,
			ReadyReplicas: 0,
		},
	}

	conditions := reconciler.buildConditions(cell, mgDeploy, moDeploy)

	if len(conditions) == 0 {
		t.Fatal("buildConditions() should return conditions")
	}

	readyCondition := conditions[0]
	if readyCondition.Type != "Ready" {
		t.Errorf("Condition type = %s, want Ready", readyCondition.Type)
	}
	if readyCondition.Status != metav1.ConditionFalse {
		t.Errorf("Condition status = %s, want False (zero replicas)", readyCondition.Status)
	}
	if readyCondition.Reason != "ComponentsNotReady" {
		t.Errorf("Condition reason = %s, want ComponentsNotReady", readyCondition.Reason)
	}
}

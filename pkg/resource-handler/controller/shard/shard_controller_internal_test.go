package shard

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

func TestBuildConditions(t *testing.T) {
	tests := map[string]struct {
		generation int64
		totalPods  int32
		readyPods  int32
		want       []metav1.Condition
	}{
		"all pods ready": {
			generation: 5,
			totalPods:  3,
			readyPods:  3,
			want: []metav1.Condition{
				{
					Type:               "Available",
					Status:             metav1.ConditionTrue,
					Reason:             "AllPodsReady",
					Message:            "All 3 pods are ready",
					ObservedGeneration: 5,
				},
			},
		},
		"partial pods ready": {
			generation: 10,
			totalPods:  5,
			readyPods:  2,
			want: []metav1.Condition{
				{
					Type:               "Available",
					Status:             metav1.ConditionFalse,
					Reason:             "NotAllPodsReady",
					Message:            "2/5 pods ready",
					ObservedGeneration: 10,
				},
			},
		},
		"no pods": {
			generation: 1,
			totalPods:  0,
			readyPods:  0,
			want: []metav1.Condition{
				{
					Type:               "Available",
					Status:             metav1.ConditionFalse,
					Reason:             "NotAllPodsReady",
					Message:            "0/0 pods ready",
					ObservedGeneration: 1,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			shard := &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{Generation: tc.generation},
			}
			r := &ShardReconciler{}
			got := r.buildConditions(shard, tc.totalPods, tc.readyPods)

			// Use go-cmp for exact match, ignoring LastTransitionTime
			opts := cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")
			if diff := cmp.Diff(tc.want, got, opts); diff != "" {
				t.Errorf("buildConditions() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestBuildMultiOrchContainer_WithImage tests buildMultiOrchContainer with custom image.
// This tests the image override path that was missing coverage.
func TestBuildMultiOrchContainer_WithImage(t *testing.T) {
	customImage := "custom/multiorch:v1.2.3"
	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.ShardSpec{
			MultiOrch: multigresv1alpha1.MultiOrchSpec{
				Image: customImage,
			},
		},
	}

	container := buildMultiOrchContainer(shard)

	if container.Image != customImage {
		t.Errorf("buildMultiOrchContainer() image = %s, want %s", container.Image, customImage)
	}
	if container.Name != "multiorch" {
		t.Errorf("buildMultiOrchContainer() name = %s, want multiorch", container.Name)
	}
}

// TestReconcileMultiOrchDeployment_InvalidScheme tests the error path when BuildMultiOrchDeployment fails.
// This should never happen in production - scheme is properly set up in main.go.
// Test exists for coverage of defensive error handling.
func TestReconcileMultiOrchDeployment_InvalidScheme(t *testing.T) {
	// Empty scheme without Shard type registered
	invalidScheme := runtime.NewScheme()

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.ShardSpec{
			MultiOrch: multigresv1alpha1.MultiOrchSpec{
				Cells: []string{"cell1"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &ShardReconciler{
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcileMultiOrchDeployment(context.Background(), shard)
	if err == nil {
		t.Error("reconcileMultiOrchDeployment() should error with invalid scheme")
	}
}

// TestReconcileMultiOrchService_InvalidScheme tests the error path when BuildMultiOrchService fails.
func TestReconcileMultiOrchService_InvalidScheme(t *testing.T) {
	invalidScheme := runtime.NewScheme()

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &ShardReconciler{
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcileMultiOrchService(context.Background(), shard)
	if err == nil {
		t.Error("reconcileMultiOrchService() should error with invalid scheme")
	}
}

// TestReconcilePoolStatefulSet_InvalidScheme tests the error path when BuildPoolStatefulSet fails.
func TestReconcilePoolStatefulSet_InvalidScheme(t *testing.T) {
	invalidScheme := runtime.NewScheme()

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
		},
	}

	poolName := "pool1"
	poolSpec := multigresv1alpha1.ShardPoolSpec{
		Cell:       "cell1",
		Database:   "db1",
		TableGroup: "tg1",
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &ShardReconciler{
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcilePoolStatefulSet(context.Background(), shard, poolName, poolSpec)
	if err == nil {
		t.Error("reconcilePoolStatefulSet() should error with invalid scheme")
	}
}

// TestReconcilePoolHeadlessService_InvalidScheme tests the error path when BuildPoolHeadlessService fails.
func TestReconcilePoolHeadlessService_InvalidScheme(t *testing.T) {
	invalidScheme := runtime.NewScheme()

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
		},
	}

	poolName := "pool1"
	poolSpec := multigresv1alpha1.ShardPoolSpec{
		Cell:       "cell1",
		Database:   "db1",
		TableGroup: "tg1",
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(invalidScheme).
		Build()

	reconciler := &ShardReconciler{
		Client: fakeClient,
		Scheme: invalidScheme,
	}

	err := reconciler.reconcilePoolHeadlessService(context.Background(), shard, poolName, poolSpec)
	if err == nil {
		t.Error("reconcilePoolHeadlessService() should error with invalid scheme")
	}
}

// TestUpdateStatus_PoolStatefulSetNotFound tests the NotFound path in updateStatus.
func TestUpdateStatus_PoolStatefulSetNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme) // Need StatefulSet type registered for Get to work

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.ShardSpec{
			Pools: map[string]multigresv1alpha1.ShardPoolSpec{
				"pool1": {
					Cell:       "cell1",
					Database:   "db1",
					TableGroup: "tg1",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	reconciler := &ShardReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// Call updateStatus when pool StatefulSet doesn't exist yet
	err := reconciler.updateStatus(context.Background(), shard)
	if err != nil {
		t.Errorf("updateStatus() should not error when pool StatefulSet not found, got: %v", err)
	}
}

// TestHandleDeletion_NoFinalizer tests early return when no finalizer is present.
func TestHandleDeletion_NoFinalizer(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-shard",
			Namespace:  "default",
			Finalizers: []string{}, // No finalizer
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		Build()

	reconciler := &ShardReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	result, err := reconciler.handleDeletion(context.Background(), shard)
	if err != nil {
		t.Errorf("handleDeletion() should not error when no finalizer, got: %v", err)
	}
	if result.RequeueAfter > 0 {
		t.Error("handleDeletion() should not requeue when no finalizer")
	}
}

// TestReconcileMultiOrchDeployment_GetError tests error path on Get MultiOrch Deployment (not NotFound).
func TestReconcileMultiOrchDeployment_GetError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.ShardSpec{
			MultiOrch: multigresv1alpha1.MultiOrchSpec{
				Cells: []string{"cell1"},
			},
		},
	}

	// Create client with failure injection
	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnGet: testutil.FailOnKeyName("test-shard-multiorch", testutil.ErrNetworkTimeout),
	})

	reconciler := &ShardReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.reconcileMultiOrchDeployment(context.Background(), shard)
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

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
		},
	}

	// Create client with failure injection
	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnGet: testutil.FailOnKeyName("test-shard-multiorch", testutil.ErrNetworkTimeout),
	})

	reconciler := &ShardReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.reconcileMultiOrchService(context.Background(), shard)
	if err == nil {
		t.Error("reconcileMultiOrchService() should error on Get failure")
	}
}

// TestReconcilePoolStatefulSet_GetError tests error path on Get pool StatefulSet (not NotFound).
func TestReconcilePoolStatefulSet_GetError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
		},
	}

	poolName := "pool1"
	poolSpec := multigresv1alpha1.ShardPoolSpec{
		Cell:       "cell1",
		Database:   "db1",
		TableGroup: "tg1",
	}

	// Create client with failure injection
	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnGet: testutil.FailOnKeyName("test-shard-pool-pool1", testutil.ErrNetworkTimeout),
	})

	reconciler := &ShardReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.reconcilePoolStatefulSet(context.Background(), shard, poolName, poolSpec)
	if err == nil {
		t.Error("reconcilePoolStatefulSet() should error on Get failure")
	}
}

// TestReconcilePoolHeadlessService_GetError tests error path on Get pool headless Service (not NotFound).
func TestReconcilePoolHeadlessService_GetError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
		},
	}

	poolName := "pool1"
	poolSpec := multigresv1alpha1.ShardPoolSpec{
		Cell:       "cell1",
		Database:   "db1",
		TableGroup: "tg1",
	}

	// Create client with failure injection
	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnGet: testutil.FailOnKeyName("test-shard-pool-pool1-headless", testutil.ErrNetworkTimeout),
	})

	reconciler := &ShardReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.reconcilePoolHeadlessService(context.Background(), shard, poolName, poolSpec)
	if err == nil {
		t.Error("reconcilePoolHeadlessService() should error on Get failure")
	}
}

// TestUpdateStatus_GetError tests error path on Get pool StatefulSet (not NotFound).
func TestUpdateStatus_GetError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.ShardSpec{
			Pools: map[string]multigresv1alpha1.ShardPoolSpec{
				"pool1": {
					Cell:       "cell1",
					Database:   "db1",
					TableGroup: "tg1",
				},
			},
		},
	}

	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnGet: testutil.FailOnKeyName("test-shard-pool-pool1", testutil.ErrNetworkTimeout),
	})

	reconciler := &ShardReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.updateStatus(context.Background(), shard)
	if err == nil {
		t.Error("updateStatus() should error on Get failure")
	}
}

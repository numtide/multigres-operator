package shard

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
	"github.com/numtide/multigres-operator/pkg/util/name"
)

// Helper functions moved from shard_controller_test_util_test.go

func buildHashedPoolName(shard *multigresv1alpha1.Shard, poolName, cellName string) string {
	clusterName := shard.Labels["multigres.com/cluster"]
	return name.JoinWithConstraints(
		name.StatefulSetConstraints,
		clusterName,
		string(shard.Spec.DatabaseName),
		string(shard.Spec.TableGroupName),
		string(shard.Spec.ShardName),
		"pool",
		poolName,
		cellName,
	)
}

func buildHashedPoolHeadlessServiceName(
	shard *multigresv1alpha1.Shard,
	poolName, cellName string,
) string {
	clusterName := shard.Labels["multigres.com/cluster"]
	return name.JoinWithConstraints(
		name.ServiceConstraints,
		clusterName,
		string(shard.Spec.DatabaseName),
		string(shard.Spec.TableGroupName),
		string(shard.Spec.ShardName),
		"pool",
		poolName,
		cellName,
		"headless",
	)
}

func buildHashedBackupPVCName(shard *multigresv1alpha1.Shard, poolName, cellName string) string {
	clusterName := shard.Labels["multigres.com/cluster"]
	return name.JoinWithConstraints(
		name.ServiceConstraints,
		"backup-data",
		clusterName,
		string(shard.Spec.DatabaseName),
		string(shard.Spec.TableGroupName),
		string(shard.Spec.ShardName),
		"pool",
		poolName,
		cellName,
	)
}

func buildHashedMultiOrchName(shard *multigresv1alpha1.Shard, cellName string) string {
	clusterName := shard.Labels["multigres.com/cluster"]
	return name.JoinWithConstraints(
		name.ServiceConstraints,
		clusterName,
		string(shard.Spec.DatabaseName),
		string(shard.Spec.TableGroupName),
		string(shard.Spec.ShardName),
		"multiorch",
		cellName,
	)
}

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
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				Address:        "global-topo:2379",
				RootPath:       "/multigres/global",
				Implementation: "etcd2",
			},
			Images: multigresv1alpha1.ShardImages{
				MultiOrch: multigresv1alpha1.ImageRef(customImage),
			},
		},
	}

	container := buildMultiOrchContainer(shard, "zone1")

	if container.Image != customImage {
		t.Errorf("buildMultiOrchContainer() image = %s, want %s", container.Image, customImage)
	}
	if container.Name != "multiorch" {
		t.Errorf("buildMultiOrchContainer() name = %s, want multiorch", container.Name)
	}
}

// TestReconcile_InvalidScheme tests the error path when Build* functions fail due to invalid scheme.
// This should never happen in production - scheme is properly set up in main.go.
// Test exists for coverage of defensive error handling.
func TestReconcile_InvalidScheme(t *testing.T) {
	tests := map[string]struct {
		setupShard    func() *multigresv1alpha1.Shard
		reconcileFunc func(*ShardReconciler, context.Context, *multigresv1alpha1.Shard) error
	}{
		"MultiOrchDeployment": {
			setupShard: func() *multigresv1alpha1.Shard {
				return &multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard",
						Namespace: "default",
					},
					Spec: multigresv1alpha1.ShardSpec{
						MultiOrch: multigresv1alpha1.MultiOrchSpec{
							Cells: []multigresv1alpha1.CellName{"cell1"},
						},
					},
				}
			},
			reconcileFunc: func(r *ShardReconciler, ctx context.Context, shard *multigresv1alpha1.Shard) error {
				return r.reconcileMultiOrchDeployment(ctx, shard, "cell1")
			},
		},
		"MultiOrchService": {
			setupShard: func() *multigresv1alpha1.Shard {
				return &multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard",
						Namespace: "default",
					},
				}
			},
			reconcileFunc: func(r *ShardReconciler, ctx context.Context, shard *multigresv1alpha1.Shard) error {
				return r.reconcileMultiOrchService(ctx, shard, "cell1")
			},
		},
		"PoolStatefulSet": {
			setupShard: func() *multigresv1alpha1.Shard {
				return &multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard",
						Namespace: "default",
					},
				}
			},
			reconcileFunc: func(r *ShardReconciler, ctx context.Context, shard *multigresv1alpha1.Shard) error {
				poolSpec := multigresv1alpha1.PoolSpec{
					Cells: []multigresv1alpha1.CellName{"cell1"},
				}
				return r.reconcilePoolStatefulSet(ctx, shard, "pool1", "", poolSpec)
			},
		},
		"PoolHeadlessService": {
			setupShard: func() *multigresv1alpha1.Shard {
				return &multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard",
						Namespace: "default",
					},
				}
			},
			reconcileFunc: func(r *ShardReconciler, ctx context.Context, shard *multigresv1alpha1.Shard) error {
				poolSpec := multigresv1alpha1.PoolSpec{
					Cells: []multigresv1alpha1.CellName{"cell1"},
				}
				return r.reconcilePoolHeadlessService(ctx, shard, "pool1", "", poolSpec)
			},
		},
		"PoolBackupPVC": {
			setupShard: func() *multigresv1alpha1.Shard {
				return &multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard",
						Namespace: "default",
					},
				}
			},
			reconcileFunc: func(r *ShardReconciler, ctx context.Context, shard *multigresv1alpha1.Shard) error {
				poolSpec := multigresv1alpha1.PoolSpec{
					Cells: []multigresv1alpha1.CellName{"cell1"},
				}
				return r.reconcilePoolBackupPVC(ctx, shard, "pool1", "cell1", poolSpec)
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Empty scheme without Shard type registered
			invalidScheme := runtime.NewScheme()

			shard := tc.setupShard()

			fakeClient := fake.NewClientBuilder().
				WithScheme(invalidScheme).
				Build()

			reconciler := &ShardReconciler{
				Client: fakeClient,
				Scheme: invalidScheme,
			}

			err := tc.reconcileFunc(reconciler, context.Background(), shard)
			if err == nil {
				t.Errorf("reconcile function should error with invalid scheme")
			}
		})
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
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool1": {
					Cells: []multigresv1alpha1.CellName{"cell1"},
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

// TestReconcile_GetError tests error path on Get operations (not NotFound, but network errors).
func TestReconcile_GetError(t *testing.T) {
	tests := map[string]struct {
		setupShard    func() *multigresv1alpha1.Shard
		getFailKey    func(*multigresv1alpha1.Shard) string
		reconcileFunc func(*ShardReconciler, context.Context, *multigresv1alpha1.Shard) error
	}{
		"MultiOrchDeployment": {
			setupShard: func() *multigresv1alpha1.Shard {
				return &multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard",
						Namespace: "default",
					},
					Spec: multigresv1alpha1.ShardSpec{
						DatabaseName:   "testdb",
						TableGroupName: "default",
						MultiOrch: multigresv1alpha1.MultiOrchSpec{
							Cells: []multigresv1alpha1.CellName{"cell1"},
						},
					},
				}
			},
			getFailKey: func(s *multigresv1alpha1.Shard) string {
				return buildHashedMultiOrchName(s, "cell1")
			},
			reconcileFunc: func(r *ShardReconciler, ctx context.Context, shard *multigresv1alpha1.Shard) error {
				return r.reconcileMultiOrchDeployment(ctx, shard, "cell1")
			},
		},
		"MultiOrchService": {
			setupShard: func() *multigresv1alpha1.Shard {
				return &multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard",
						Namespace: "default",
					},
					Spec: multigresv1alpha1.ShardSpec{
						DatabaseName:   "testdb",
						TableGroupName: "default",
					},
				}
			},
			getFailKey: func(s *multigresv1alpha1.Shard) string {
				return buildHashedMultiOrchName(s, "cell1")
			},
			reconcileFunc: func(r *ShardReconciler, ctx context.Context, shard *multigresv1alpha1.Shard) error {
				return r.reconcileMultiOrchService(ctx, shard, "cell1")
			},
		},
		"PoolStatefulSet": {
			setupShard: func() *multigresv1alpha1.Shard {
				return &multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard",
						Namespace: "default",
					},
					Spec: multigresv1alpha1.ShardSpec{
						DatabaseName:   "testdb",
						TableGroupName: "default",
					},
				}
			},
			getFailKey: func(s *multigresv1alpha1.Shard) string {
				return buildHashedPoolName(s, "pool1", "cell1")
			},
			reconcileFunc: func(r *ShardReconciler, ctx context.Context, shard *multigresv1alpha1.Shard) error {
				poolSpec := multigresv1alpha1.PoolSpec{
					Cells: []multigresv1alpha1.CellName{"cell1"},
				}
				return r.reconcilePoolStatefulSet(ctx, shard, "pool1", "cell1", poolSpec)
			},
		},
		"PoolHeadlessService": {
			setupShard: func() *multigresv1alpha1.Shard {
				return &multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard",
						Namespace: "default",
					},
					Spec: multigresv1alpha1.ShardSpec{
						DatabaseName:   "testdb",
						TableGroupName: "default",
					},
				}
			},
			getFailKey: func(s *multigresv1alpha1.Shard) string {
				return buildPoolHeadlessServiceName(s, "pool1", "cell1")
			},
			reconcileFunc: func(r *ShardReconciler, ctx context.Context, shard *multigresv1alpha1.Shard) error {
				poolSpec := multigresv1alpha1.PoolSpec{
					Cells: []multigresv1alpha1.CellName{"cell1"},
				}
				return r.reconcilePoolHeadlessService(ctx, shard, "pool1", "cell1", poolSpec)
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = multigresv1alpha1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			shard := tc.setupShard()
			failKey := tc.getFailKey(shard)

			// Create client with failure injection
			baseClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(shard).
				Build()

			fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName(failKey, testutil.ErrNetworkTimeout),
			})

			reconciler := &ShardReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			err := tc.reconcileFunc(reconciler, context.Background(), shard)
			if err == nil {
				t.Errorf("reconcile function should error on Get failure")
			}
		})
	}
}

// TestUpdateStatus_MultiOrch tests updateStatus with different MultiOrch deployment scenarios.
func TestUpdateStatus_MultiOrch(t *testing.T) {
	tests := map[string]struct {
		setupObjects    []client.Object
		expectError     bool
		expectOrchReady bool
		setupClient     func(*testing.T, *runtime.Scheme, *multigresv1alpha1.Shard) client.Client
		customShard     *multigresv1alpha1.Shard // Optional: override default shard
	}{
		"GetError": {
			expectError: true,
			setupClient: func(t *testing.T, scheme *runtime.Scheme, shard *multigresv1alpha1.Shard) client.Client {
				baseClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(shard).
					WithStatusSubresource(&multigresv1alpha1.Shard{}).
					Build()
				return testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
					OnGet: testutil.FailOnKeyName(
						buildHashedMultiOrchName(shard, "zone1"),
						testutil.ErrNetworkTimeout,
					),
				})
			},
		},
		"NotReady": {
			expectError:     false,
			expectOrchReady: false,
			setupObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch-zone1",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(3)),
					},
					Status: appsv1.DeploymentStatus{
						ReadyReplicas: 1, // Not all ready
					},
				},
			},
		},
		"NilReplicas": {
			expectError:     false,
			expectOrchReady: false,
			setupObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch-zone1",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: nil, // Nil replicas
					},
				},
			},
		},
		"NotFound": {
			expectError:     false,
			expectOrchReady: false,
			setupObjects:    []client.Object{}, // No MultiOrch Deployment - will get NotFound
		},
		"NoCellsInMultiOrchOrPools": {
			expectError:     false,
			expectOrchReady: false,
			setupObjects:    []client.Object{},
			customShard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{}, // Empty
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{}, // Empty
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = multigresv1alpha1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			// Use custom shard if provided, otherwise use default
			shard := tc.customShard
			if shard == nil {
				shard = &multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard",
						Namespace: "default",
					},
					Spec: multigresv1alpha1.ShardSpec{
						MultiOrch: multigresv1alpha1.MultiOrchSpec{
							Cells: []multigresv1alpha1.CellName{"zone1"},
						},
						Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{},
					},
				}
			}

			var fakeClient client.Client
			if tc.setupClient != nil {
				fakeClient = tc.setupClient(t, scheme, shard)
			} else {
				objects := append([]client.Object{shard}, tc.setupObjects...)
				fakeClient = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(objects...).
					WithStatusSubresource(&multigresv1alpha1.Shard{}).
					Build()
			}

			reconciler := &ShardReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			err := reconciler.updateStatus(context.Background(), shard)
			if tc.expectError && err == nil {
				t.Error("updateStatus() should error but didn't")
			}
			if !tc.expectError && err != nil {
				t.Errorf("updateStatus() unexpected error: %v", err)
			}

			// For non-error cases, verify OrchReady status
			if !tc.expectError {
				updatedShard := &multigresv1alpha1.Shard{}
				if err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(shard), updatedShard); err != nil {
					t.Fatalf("Failed to get shard: %v", err)
				}

				if updatedShard.Status.OrchReady != tc.expectOrchReady {
					t.Errorf(
						"OrchReady = %v, want %v",
						updatedShard.Status.OrchReady,
						tc.expectOrchReady,
					)
				}
			}
		})
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
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool1": {
					Cells: []multigresv1alpha1.CellName{"cell1"},
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
		OnGet: testutil.FailOnKeyName(
			buildHashedPoolName(shard, "pool1", "cell1"),
			testutil.ErrNetworkTimeout,
		),
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

package shard

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	ctrl "sigs.k8s.io/controller-runtime"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
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

func buildHashedBackupPVCName(shard *multigresv1alpha1.Shard, cellName string) string {
	clusterName := shard.Labels["multigres.com/cluster"]
	return name.JoinWithConstraints(
		name.ServiceConstraints,
		"backup-data",
		clusterName,
		string(shard.Spec.DatabaseName),
		string(shard.Spec.TableGroupName),
		string(shard.Spec.ShardName),
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

func TestGetPoolCells(t *testing.T) {
	tests := map[string]struct {
		shard *multigresv1alpha1.Shard
		want  []multigresv1alpha1.CellName
	}{
		"explicit multiorch and pools in different cells": {
			shard: &multigresv1alpha1.Shard{
				Spec: multigresv1alpha1.ShardSpec{
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone-a"},
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"pool1": {Cells: []multigresv1alpha1.CellName{"zone-b"}},
					},
				},
			},
			want: []multigresv1alpha1.CellName{"zone-b"},
		},
		"only multiorch": {
			shard: &multigresv1alpha1.Shard{
				Spec: multigresv1alpha1.ShardSpec{
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone-a"},
					},
				},
			},
			want: []multigresv1alpha1.CellName{},
		},
		"only pools": {
			shard: &multigresv1alpha1.Shard{
				Spec: multigresv1alpha1.ShardSpec{
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"pool1": {Cells: []multigresv1alpha1.CellName{"zone-b"}},
						"pool2": {Cells: []multigresv1alpha1.CellName{"zone-c"}},
					},
				},
			},
			want: []multigresv1alpha1.CellName{"zone-b", "zone-c"},
		},
		"overlapping cells": {
			shard: &multigresv1alpha1.Shard{
				Spec: multigresv1alpha1.ShardSpec{
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone-a", "zone-b"},
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"pool1": {Cells: []multigresv1alpha1.CellName{"zone-b", "zone-c"}},
					},
				},
			},
			want: []multigresv1alpha1.CellName{"zone-b", "zone-c"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := getPoolCells(tc.shard)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("getPoolCells() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetConditions(t *testing.T) {
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
			r := &ShardReconciler{
				Recorder: record.NewFakeRecorder(100),
			}
			r.setConditions(shard, tc.totalPods, tc.readyPods)
			got := shard.Status.Conditions

			// Use go-cmp for exact match, ignoring LastTransitionTime
			opts := cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")
			if diff := cmp.Diff(tc.want, got, opts); diff != "" {
				t.Errorf("setConditions() mismatch (-want +got):\n%s", diff)
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
				Implementation: "etcd",
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
		"PoolPDB": {
			setupShard: func() *multigresv1alpha1.Shard {
				return &multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard",
						Namespace: "default",
					},
				}
			},
			reconcileFunc: func(r *ShardReconciler, ctx context.Context, shard *multigresv1alpha1.Shard) error {
				return r.reconcilePoolPDB(ctx, shard, "pool1", "cell1")
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
		"SharedBackupPVC": {
			setupShard: func() *multigresv1alpha1.Shard {
				return &multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard",
						Namespace: "default",
					},
					Spec: multigresv1alpha1.ShardSpec{
						Backup: &multigresv1alpha1.BackupConfig{
							Type:       multigresv1alpha1.BackupTypeFilesystem,
							Filesystem: &multigresv1alpha1.FilesystemBackupConfig{},
						},
					},
				}
			},
			reconcileFunc: func(r *ShardReconciler, ctx context.Context, shard *multigresv1alpha1.Shard) error {
				return r.reconcileSharedBackupPVC(ctx, shard, "cell1")
			},
		},
		"PostgresPasswordSecret": {
			setupShard: func() *multigresv1alpha1.Shard {
				return &multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard",
						Namespace: "default",
					},
				}
			},
			reconcileFunc: func(r *ShardReconciler, ctx context.Context, shard *multigresv1alpha1.Shard) error {
				return r.reconcilePostgresPasswordSecret(ctx, shard)
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
				Client:    fakeClient,
				Scheme:    invalidScheme,
				Recorder:  record.NewFakeRecorder(100),
				APIReader: fakeClient,
			}

			err := tc.reconcileFunc(reconciler, context.Background(), shard)
			if err == nil {
				t.Errorf("reconcile function should error with invalid scheme")
			}
		})
	}
}

// TestUpdateStatus_PoolPodsNotFound tests the NotFound path in updateStatus.
func TestUpdateStatus_PoolPodsNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = policyv1.AddToScheme(scheme)

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
		Client:    fakeClient,
		Scheme:    scheme,
		Recorder:  record.NewFakeRecorder(100),
		APIReader: fakeClient,
	}

	// Call updateStatus when pool Pods don't exist yet
	err := reconciler.updateStatus(context.Background(), shard)
	if err != nil {
		t.Errorf("updateStatus() should not error when pool Pods not found, got: %v", err)
	}
}

// TestReconcile_PatchError tests error path on Patch operations.
func TestReconcile_PatchError(t *testing.T) {
	tests := map[string]struct {
		setupShard    func() *multigresv1alpha1.Shard
		getFailObj    func(*multigresv1alpha1.Shard) string
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
			getFailObj: func(s *multigresv1alpha1.Shard) string {
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
			getFailObj: func(s *multigresv1alpha1.Shard) string {
				return buildHashedMultiOrchName(s, "cell1")
			},
			reconcileFunc: func(r *ShardReconciler, ctx context.Context, shard *multigresv1alpha1.Shard) error {
				return r.reconcileMultiOrchService(ctx, shard, "cell1")
			},
		},
		"PoolPDB": {
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
			getFailObj: func(s *multigresv1alpha1.Shard) string {
				// The PDB name formula is from BuildPoolPodDisruptionBudget
				clusterName := s.Labels["multigres.com/cluster"]
				return name.JoinWithConstraints(
					name.DefaultConstraints,
					clusterName,
					string(s.Spec.DatabaseName),
					string(s.Spec.TableGroupName),
					string(s.Spec.ShardName),
					"pool",
					"pool1",
					"cell1",
					"pdb",
				)
			},
			reconcileFunc: func(r *ShardReconciler, ctx context.Context, shard *multigresv1alpha1.Shard) error {
				return r.reconcilePoolPDB(ctx, shard, "pool1", "cell1")
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
			getFailObj: func(s *multigresv1alpha1.Shard) string {
				return buildHashedPoolHeadlessServiceName(s, "pool1", "cell1")
			},
			reconcileFunc: func(r *ShardReconciler, ctx context.Context, shard *multigresv1alpha1.Shard) error {
				poolSpec := multigresv1alpha1.PoolSpec{
					Cells: []multigresv1alpha1.CellName{"cell1"},
				}
				return r.reconcilePoolHeadlessService(ctx, shard, "pool1", "cell1", poolSpec)
			},
		},
		"PostgresPasswordSecret": {
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
			getFailObj: func(s *multigresv1alpha1.Shard) string {
				return PostgresPasswordSecretName(s.Name)
			},
			reconcileFunc: func(r *ShardReconciler, ctx context.Context, shard *multigresv1alpha1.Shard) error {
				return r.reconcilePostgresPasswordSecret(ctx, shard)
			},
		},
		"PgHbaConfigMap": {
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
			getFailObj: func(s *multigresv1alpha1.Shard) string {
				return PgHbaConfigMapName(s.Name)
			},
			reconcileFunc: func(r *ShardReconciler, ctx context.Context, shard *multigresv1alpha1.Shard) error {
				return r.reconcilePgHbaConfigMap(ctx, shard)
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = multigresv1alpha1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = policyv1.AddToScheme(scheme)

			shard := tc.setupShard()
			failObj := tc.getFailObj(shard)

			// Create client with failure injection
			baseClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(shard).
				Build()

			fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
				OnPatch: func(obj client.Object) error {
					if obj.GetName() == failObj {
						return testutil.ErrNetworkTimeout
					}
					return nil
				},
			})

			reconciler := &ShardReconciler{
				Client:    fakeClient,
				Scheme:    scheme,
				Recorder:  record.NewFakeRecorder(100),
				APIReader: fakeClient,
			}

			err := tc.reconcileFunc(reconciler, context.Background(), shard)
			if err == nil {
				t.Errorf("reconcile function should error on Patch failure")
			}
		})
	}
}

// TestReconcile_PostgresSecretError verifies the error path in Reconcile when
// reconcilePostgresPasswordSecret fails (lines 81-92 of shard_controller.go).
func TestReconcile_PostgresSecretError(t *testing.T) {
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
			DatabaseName:   "testdb",
			TableGroupName: "default",
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool1": {
					ReplicasPerCell: ptr.To(int32(1)),
					Cells:           []multigresv1alpha1.CellName{"cell1"},
				},
			},
		},
	}

	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		Build()

	// Fail on the second Patch (first is pg_hba ConfigMap, second is postgres-password Secret).
	var patchCount atomic.Int32
	fakeClient := testutil.NewFakeClientWithFailures(baseClient, &testutil.FailureConfig{
		OnPatch: func(obj client.Object) error {
			if patchCount.Add(1) == 2 {
				return testutil.ErrNetworkTimeout
			}
			return nil
		},
	})

	reconciler := &ShardReconciler{
		Client:    fakeClient,
		Scheme:    scheme,
		Recorder:  record.NewFakeRecorder(100),
		APIReader: fakeClient,
	}

	req := ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(shard),
	}

	_, err := reconciler.Reconcile(t.Context(), req)
	if err == nil {
		t.Fatal("Reconcile should return an error when reconcilePostgresPasswordSecret fails")
	}
	if !strings.Contains(err.Error(), "failed to apply postgres password Secret") {
		t.Errorf("unexpected error: %v", err)
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
				Client:    fakeClient,
				Scheme:    scheme,
				Recorder:  record.NewFakeRecorder(100),
				APIReader: fakeClient,
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
				if err := fakeClient.Get(
					context.Background(),
					client.ObjectKeyFromObject(shard),
					updatedShard,
				); err != nil {
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

// TestUpdateStatus_ListError tests error path on List pool Pods.
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
		OnList: func(list client.ObjectList) error {
			if _, ok := list.(*corev1.PodList); ok {
				return testutil.ErrNetworkTimeout
			}
			return nil
		},
	})

	reconciler := &ShardReconciler{
		Client:    fakeClient,
		Scheme:    scheme,
		Recorder:  record.NewFakeRecorder(100),
		APIReader: fakeClient,
	}

	err := reconciler.updateStatus(context.Background(), shard)
	if err == nil {
		t.Error("updateStatus() should error on Get failure")
	}
}

// statusPatchCapture wraps a client.Client to snapshot the state of the
// patch object and options before delegating to the real Status().Patch().
// Snapshotting before the call is necessary because the fake client's SSA
// implementation may mutate the object in place during the merge.
type statusPatchCapture struct {
	client.Client
	podRolesWasNil       bool
	lastBackupTimeWasNil bool
	lastBackupTypeEmpty  bool
	capturedOpts         []client.SubResourcePatchOption
}

func (c *statusPatchCapture) Status() client.StatusWriter {
	return &capturingStatusWriter{
		StatusWriter: c.Client.Status(),
		capture:      c,
	}
}

type capturingStatusWriter struct {
	client.StatusWriter
	capture *statusPatchCapture
}

func (w *capturingStatusWriter) Patch(
	ctx context.Context,
	obj client.Object,
	patch client.Patch,
	opts ...client.SubResourcePatchOption,
) error {
	w.capture.capturedOpts = opts
	if s, ok := obj.(*multigresv1alpha1.Shard); ok {
		w.capture.podRolesWasNil = s.Status.PodRoles == nil
		w.capture.lastBackupTimeWasNil = s.Status.LastBackupTime == nil
		w.capture.lastBackupTypeEmpty = s.Status.LastBackupType == ""
	}
	return w.StatusWriter.Patch(ctx, obj, patch, opts...)
}

// TestUpdateStatus_NilsDataHandlerFields verifies that updateStatus clears
// data-handler-owned fields (PodRoles, LastBackupTime, LastBackupType) from
// the SSA patch object so the resource-handler never overwrites them.
func TestUpdateStatus_NilsDataHandlerFields(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = policyv1.AddToScheme(scheme)

	now := metav1.Now()

	tests := map[string]struct {
		preStatus multigresv1alpha1.ShardStatus
	}{
		"PodRoles populated": {
			preStatus: multigresv1alpha1.ShardStatus{
				PodRoles: map[string]string{
					"pod-0": "PRIMARY",
					"pod-1": "REPLICA",
				},
			},
		},
		"LastBackupTime and LastBackupType populated": {
			preStatus: multigresv1alpha1.ShardStatus{
				LastBackupTime: &now,
				LastBackupType: "full",
			},
		},
		"all data-handler fields populated": {
			preStatus: multigresv1alpha1.ShardStatus{
				PodRoles: map[string]string{
					"pod-0": "PRIMARY",
				},
				LastBackupTime: &now,
				LastBackupType: "incr",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
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
				Status: tc.preStatus,
			}

			baseClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(shard).
				WithStatusSubresource(&multigresv1alpha1.Shard{}).
				Build()

			capture := &statusPatchCapture{Client: baseClient}

			reconciler := &ShardReconciler{
				Client:    capture,
				Scheme:    scheme,
				Recorder:  record.NewFakeRecorder(100),
				APIReader: baseClient,
			}

			err := reconciler.updateStatus(context.Background(), shard)
			if err != nil {
				t.Fatalf("updateStatus() unexpected error: %v", err)
			}

			if !capture.podRolesWasNil {
				t.Error("PodRoles should be nil in SSA patch object")
			}
			if !capture.lastBackupTimeWasNil {
				t.Error("LastBackupTime should be nil in SSA patch object")
			}
			if !capture.lastBackupTypeEmpty {
				t.Error("LastBackupType should be empty in SSA patch object")
			}
		})
	}
}

// TestUpdateStatus_FieldOwner verifies that the SSA status patch uses
// "multigres-resource-handler" as the field owner, not "multigres-operator".
func TestUpdateStatus_FieldOwner(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = policyv1.AddToScheme(scheme)

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

	capture := &statusPatchCapture{Client: baseClient}

	reconciler := &ShardReconciler{
		Client:    capture,
		Scheme:    scheme,
		Recorder:  record.NewFakeRecorder(100),
		APIReader: baseClient,
	}

	err := reconciler.updateStatus(context.Background(), shard)
	if err != nil {
		t.Fatalf("updateStatus() unexpected error: %v", err)
	}

	// Verify the field owner is "multigres-resource-handler"
	foundFieldOwner := false
	for _, opt := range capture.capturedOpts {
		if fo, ok := opt.(client.FieldOwner); ok {
			if string(fo) != "multigres-resource-handler" {
				t.Errorf("field owner = %q, want %q", string(fo), "multigres-resource-handler")
			}
			foundFieldOwner = true
		}
	}
	if !foundFieldOwner {
		t.Error("no FieldOwner option found in Status().Patch() call")
	}
}

// TestHandleScaleDown_ConcurrentDrainPrevention verifies that handleScaleDown
// respects the inProgress flag: when any pod already has a drain annotation
// (DrainStateRequested, DrainStateDraining, or DrainStateAcknowledged), no new
// drains are initiated for either DRAINED replacement or extra-pod scale-down.
func TestHandleScaleDown_ConcurrentDrainPrevention(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	poolName := "main"
	cellName := "z1"

	baseShard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "postgres",
			TableGroupName: "default",
			ShardName:      "0-inf",
		},
	}

	podName0 := BuildPoolPodName(baseShard, poolName, cellName, 0)
	podName1 := BuildPoolPodName(baseShard, poolName, cellName, 1)
	podName2 := BuildPoolPodName(baseShard, poolName, cellName, 2)

	makePod := func(podName string, annotations map[string]string) *corev1.Pod {
		if annotations == nil {
			annotations = map[string]string{}
		}
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        podName,
				Namespace:   "default",
				Annotations: annotations,
				Finalizers:  []string{PoolPodFinalizer},
				Labels: map[string]string{
					metadata.LabelMultigresCell: cellName,
				},
			},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		}
	}

	tests := map[string]struct {
		replicas       int32
		pods           []*corev1.Pod
		podRoles       map[string]string
		actionTaken    bool
		wantAction     bool
		wantInProgress bool
		wantNoDrains   bool
		wantDrainedPod string
	}{
		"drain in progress (DrainStateRequested) blocks DRAINED replacement": {
			replicas: 2,
			pods: []*corev1.Pod{
				makePod(podName0, map[string]string{
					metadata.AnnotationDrainState: metadata.DrainStateRequested,
				}),
				makePod(podName1, nil),
			},
			podRoles: map[string]string{
				podName1: "DRAINED",
			},
			wantAction:     false,
			wantInProgress: true,
			wantNoDrains:   true,
		},
		"drain in progress (DrainStateDraining) blocks DRAINED replacement": {
			replicas: 2,
			pods: []*corev1.Pod{
				makePod(podName0, map[string]string{
					metadata.AnnotationDrainState: metadata.DrainStateDraining,
				}),
				makePod(podName1, nil),
			},
			podRoles: map[string]string{
				podName1: "DRAINED",
			},
			wantAction:     false,
			wantInProgress: true,
			wantNoDrains:   true,
		},
		"drain in progress (DrainStateAcknowledged) blocks DRAINED replacement": {
			replicas: 2,
			pods: []*corev1.Pod{
				makePod(podName0, map[string]string{
					metadata.AnnotationDrainState: metadata.DrainStateAcknowledged,
				}),
				makePod(podName1, nil),
			},
			podRoles: map[string]string{
				podName1: "DRAINED",
			},
			wantAction:     false,
			wantInProgress: true,
			wantNoDrains:   true,
		},
		"drain in progress (DrainStateRequested) blocks extra pod drain": {
			replicas: 1,
			pods: []*corev1.Pod{
				makePod(podName0, map[string]string{
					metadata.AnnotationDrainState: metadata.DrainStateRequested,
				}),
				makePod(podName1, nil),
			},
			wantAction:     false,
			wantInProgress: true,
			wantNoDrains:   true,
		},
		"drain in progress (DrainStateDraining) blocks extra pod drain": {
			replicas: 1,
			pods: []*corev1.Pod{
				makePod(podName0, map[string]string{
					metadata.AnnotationDrainState: metadata.DrainStateDraining,
				}),
				makePod(podName1, nil),
			},
			wantAction:     false,
			wantInProgress: true,
			wantNoDrains:   true,
		},
		"no drain in progress allows DRAINED pod replacement": {
			replicas: 2,
			pods: []*corev1.Pod{
				makePod(podName0, nil),
				makePod(podName1, nil),
			},
			podRoles: map[string]string{
				podName1: "DRAINED",
			},
			wantAction:     true,
			wantInProgress: false,
			wantDrainedPod: podName1,
		},
		"no drain in progress allows extra pod drain": {
			replicas: 1,
			pods: []*corev1.Pod{
				makePod(podName0, nil),
				makePod(podName1, nil),
			},
			wantAction:     true,
			wantInProgress: false,
			wantDrainedPod: podName1,
		},
		"actionTaken from earlier phase blocks DRAINED replacement": {
			replicas: 2,
			pods: []*corev1.Pod{
				makePod(podName0, nil),
				makePod(podName1, nil),
			},
			podRoles: map[string]string{
				podName1: "DRAINED",
			},
			actionTaken:    true,
			wantAction:     true,
			wantInProgress: false,
			wantNoDrains:   true,
		},
		"actionTaken from earlier phase blocks extra pod drain": {
			replicas: 1,
			pods: []*corev1.Pod{
				makePod(podName0, nil),
				makePod(podName1, nil),
			},
			actionTaken:    true,
			wantAction:     true,
			wantInProgress: false,
			wantNoDrains:   true,
		},
		"pod with DeletionTimestamp sets inProgress and blocks DRAINED replacement": {
			replicas: 2,
			pods: []*corev1.Pod{
				func() *corev1.Pod {
					p := makePod(podName0, nil)
					now := metav1.Now()
					p.DeletionTimestamp = &now
					return p
				}(),
				makePod(podName1, nil),
			},
			podRoles: map[string]string{
				podName1: "DRAINED",
			},
			wantAction:     false,
			wantInProgress: true,
			wantNoDrains:   true,
		},
		"multiple DRAINED pods with drain in progress drains none": {
			replicas: 3,
			pods: []*corev1.Pod{
				makePod(podName0, map[string]string{
					metadata.AnnotationDrainState: metadata.DrainStateRequested,
				}),
				makePod(podName1, nil),
				makePod(podName2, nil),
			},
			podRoles: map[string]string{
				podName1: "DRAINED",
				podName2: "DRAINED",
			},
			wantAction:     false,
			wantInProgress: true,
			wantNoDrains:   true,
		},
		"ready-for-deletion pod is cleaned up even when other drain in progress": {
			replicas: 2,
			pods: []*corev1.Pod{
				makePod(podName0, map[string]string{
					metadata.AnnotationDrainState: metadata.DrainStateDraining,
				}),
				makePod(podName1, map[string]string{
					metadata.AnnotationDrainState: metadata.DrainStateReadyForDeletion,
				}),
			},
			wantAction:     true,
			wantInProgress: true,
			wantNoDrains:   true,
		},
	}

	for testName, tc := range tests {
		t.Run(testName, func(t *testing.T) {
			shard := baseShard.DeepCopy()
			shard.Status.PodRoles = tc.podRoles

			objects := make([]client.Object, 0, len(tc.pods)+1)
			objects = append(objects, shard)
			for _, p := range tc.pods {
				objects = append(objects, p.DeepCopy())
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			reconciler := &ShardReconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(100),
			}

			existingPods := make(map[string]*corev1.Pod, len(tc.pods))
			for _, p := range tc.pods {
				existingPods[p.Name] = p
			}

			poolSpec := multigresv1alpha1.PoolSpec{}

			gotAction, gotInProgress, err := reconciler.handleScaleDown(
				context.Background(),
				shard,
				poolName,
				poolSpec,
				existingPods,
				tc.replicas,
				tc.actionTaken,
			)
			if err != nil {
				t.Fatalf("handleScaleDown() unexpected error: %v", err)
			}

			if gotAction != tc.wantAction {
				t.Errorf("actionTaken = %v, want %v", gotAction, tc.wantAction)
			}
			if gotInProgress != tc.wantInProgress {
				t.Errorf("inProgress = %v, want %v", gotInProgress, tc.wantInProgress)
			}

			for _, p := range tc.pods {
				updated := &corev1.Pod{}
				err := fakeClient.Get(
					context.Background(),
					client.ObjectKeyFromObject(p),
					updated,
				)
				if err != nil {
					// Pod may have been deleted by cleanupDrainedPod (ready-for-deletion flow)
					if errors.IsNotFound(err) {
						continue
					}
					t.Fatalf("failed to get pod %s: %v", p.Name, err)
				}

				drainState := updated.Annotations[metadata.AnnotationDrainState]
				originalState := p.Annotations[metadata.AnnotationDrainState]

				if tc.wantNoDrains && drainState != originalState {
					t.Errorf(
						"pod %s: drain state changed from %q to %q, expected no new drains",
						p.Name, originalState, drainState,
					)
				}

				if tc.wantDrainedPod == p.Name && drainState != metadata.DrainStateRequested {
					t.Errorf(
						"pod %s: drain state = %q, want %q",
						p.Name, drainState, metadata.DrainStateRequested,
					)
				}
			}
		})
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
		r := &ShardReconciler{
			Client:    mgr.GetClient(),
			Scheme:    scheme,
			Recorder:  record.NewFakeRecorder(100),
			APIReader: mgr.GetClient(),
		}
		if err := r.SetupWithManager(mgr); err != nil {
			t.Errorf("SetupWithManager() error = %v", err)
		}
	})

	t.Run("with options", func(t *testing.T) {
		mgr := createMgr()
		r := &ShardReconciler{
			Client:    mgr.GetClient(),
			Scheme:    scheme,
			Recorder:  record.NewFakeRecorder(100),
			APIReader: mgr.GetClient(),
		}
		if err := r.SetupWithManager(mgr, controller.Options{
			MaxConcurrentReconciles: 1,
			SkipNameValidation:      ptr.To(true),
		}); err != nil {
			t.Errorf("SetupWithManager() with opts error = %v", err)
		}
	})
}

// TestCleanupDrainedPod_PVCDeletion verifies that cleanupDrainedPod handles
// PVC deletion correctly for DRAINED replacement pods (idx < replicas),
// scale-down pods (idx >= replicas), and rolling-update pods under different
// PVC deletion policies.
func TestCleanupDrainedPod_PVCDeletion(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	baseShard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "testdb",
			TableGroupName: "default",
			ShardName:      "shard0",
		},
	}

	poolName := "primary"
	cellName := "zone1"
	replicas := int32(3)

	podName0 := BuildPoolPodName(baseShard, poolName, cellName, 0)
	pvcName0 := BuildPoolDataPVCName(baseShard, poolName, cellName, 0)
	podName1 := BuildPoolPodName(baseShard, poolName, cellName, 1)
	pvcName1 := BuildPoolDataPVCName(baseShard, poolName, cellName, 1)
	podName5 := BuildPoolPodName(baseShard, poolName, cellName, 5)
	pvcName5 := BuildPoolDataPVCName(baseShard, poolName, cellName, 5)

	makePod := func(n string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:       n,
				Namespace:  "default",
				Finalizers: []string{PoolPodFinalizer},
				Labels: map[string]string{
					metadata.LabelMultigresCell: cellName,
				},
				Annotations: map[string]string{
					metadata.AnnotationDrainState: metadata.DrainStateReadyForDeletion,
				},
			},
		}
	}
	makePVC := func(n string) *corev1.PersistentVolumeClaim {
		return &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      n,
				Namespace: "default",
			},
		}
	}

	deletePolicy := &multigresv1alpha1.PVCDeletionPolicy{
		WhenScaled: multigresv1alpha1.DeletePVCRetentionPolicy,
	}
	retainPolicy := &multigresv1alpha1.PVCDeletionPolicy{
		WhenScaled: multigresv1alpha1.RetainPVCRetentionPolicy,
	}

	tests := map[string]struct {
		podName  string
		pvcName  string
		podRoles map[string]string
		policy   *multigresv1alpha1.PVCDeletionPolicy
		wantPVC  bool
	}{
		"DRAINED replacement (idx<replicas) with Delete policy deletes PVC": {
			podName:  podName0,
			pvcName:  pvcName0,
			podRoles: map[string]string{podName0: "DRAINED"},
			policy:   deletePolicy,
			wantPVC:  false,
		},
		"DRAINED replacement (idx<replicas) with Retain policy keeps PVC": {
			podName:  podName1,
			pvcName:  pvcName1,
			podRoles: map[string]string{podName1: "DRAINED"},
			policy:   retainPolicy,
			wantPVC:  true,
		},
		"non-DRAINED pod (idx<replicas) with Delete policy keeps PVC": {
			podName:  podName0,
			pvcName:  pvcName0,
			podRoles: map[string]string{podName0: "REPLICA"},
			policy:   deletePolicy,
			wantPVC:  true,
		},
		"scale-down (idx>=replicas) with Delete policy deletes PVC": {
			podName:  podName5,
			pvcName:  pvcName5,
			podRoles: map[string]string{},
			policy:   deletePolicy,
			wantPVC:  false,
		},
	}

	for tn, tc := range tests {
		t.Run(tn, func(t *testing.T) {
			shard := baseShard.DeepCopy()
			shard.Status.PodRoles = tc.podRoles

			pod := makePod(tc.podName)
			pvc := makePVC(tc.pvcName)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(shard, pod, pvc).
				Build()

			reconciler := &ShardReconciler{
				Client:    fakeClient,
				Scheme:    scheme,
				Recorder:  record.NewFakeRecorder(100),
				APIReader: fakeClient,
			}

			poolSpec := multigresv1alpha1.PoolSpec{
				PVCDeletionPolicy: tc.policy,
			}

			err := reconciler.cleanupDrainedPod(
				context.Background(), shard, pod, poolName, poolSpec, replicas,
			)
			if err != nil {
				t.Fatalf("cleanupDrainedPod() returned unexpected error: %v", err)
			}

			pvcAfter := &corev1.PersistentVolumeClaim{}
			getErr := fakeClient.Get(
				context.Background(),
				client.ObjectKey{Namespace: "default", Name: tc.pvcName},
				pvcAfter,
			)
			pvcExists := getErr == nil
			if pvcExists != tc.wantPVC {
				t.Errorf("PVC %s exists = %v, want %v (err=%v)", tc.pvcName, pvcExists, tc.wantPVC, getErr)
			}

			podAfter := &corev1.Pod{}
			if err := fakeClient.Get(
				context.Background(),
				client.ObjectKey{Namespace: "default", Name: tc.podName},
				podAfter,
			); err != nil {
				t.Fatalf("failed to get pod after cleanup: %v", err)
			}
			for _, f := range podAfter.Finalizers {
				if f == PoolPodFinalizer {
					t.Errorf("finalizer %q should have been removed from pod", PoolPodFinalizer)
				}
			}
		})
	}
}

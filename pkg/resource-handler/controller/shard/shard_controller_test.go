package shard_test

import (
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/shard"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

func TestShardReconciler_Reconcile(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := map[string]struct {
		shard           *multigresv1alpha1.Shard
		existingObjects []client.Object
		failureConfig   *testutil.FailureConfig
		wantErr         bool
		assertFunc      func(t *testing.T, c client.Client, shard *multigresv1alpha1.Shard)
	}{
		////----------------------------------------
		///   Success
		//------------------------------------------
		"create all resources for new Shard with single pool": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells:           []multigresv1alpha1.CellName{"zone1"},
							Type:            "replica",
							ReplicasPerCell: ptr.To(int32(1)),
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			assertFunc: func(t *testing.T, c client.Client, shard *multigresv1alpha1.Shard) {
				// Verify MultiOrch Deployment was created (with cell suffix)
				moDeploy := &appsv1.Deployment{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-shard-multiorch-zone1", Namespace: "default"},
					moDeploy); err != nil {
					t.Errorf("MultiOrch Deployment should exist: %v", err)
				}

				// Verify MultiOrch Service was created (with cell suffix)
				moSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-shard-multiorch-zone1", Namespace: "default"},
					moSvc); err != nil {
					t.Errorf("MultiOrch Service should exist: %v", err)
				}

				// Verify Pool StatefulSet was created (with cell suffix)
				poolSts := &appsv1.StatefulSet{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-shard-pool-primary-zone1", Namespace: "default"},
					poolSts); err != nil {
					t.Errorf("Pool StatefulSet should exist: %v", err)
				}

				// Verify Pool headless Service was created (with cell suffix)
				poolSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-shard-pool-primary-zone1-headless", Namespace: "default"},
					poolSvc); err != nil {
					t.Errorf("Pool headless Service should exist: %v", err)
				}

				// Verify finalizer was added
				updatedShard := &multigresv1alpha1.Shard{}
				if err := c.Get(t.Context(), types.NamespacedName{Name: "test-shard", Namespace: "default"}, updatedShard); err != nil {
					t.Fatalf("Failed to get Shard: %v", err)
				}
				if !slices.Contains(updatedShard.Finalizers, "shard.multigres.com/finalizer") {
					t.Errorf("Finalizer should be added")
				}
			},
		},
		"create resources for Shard with multiple pools": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-pool-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"replica": {
							Cells:           []multigresv1alpha1.CellName{"zone1"},
							Type:            "replica",
							ReplicasPerCell: ptr.To(int32(2)),
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
						"readOnly": {
							Cells:           []multigresv1alpha1.CellName{"zone1"},
							Type:            "readOnly",
							ReplicasPerCell: ptr.To(int32(3)),
							Storage: multigresv1alpha1.StorageSpec{
								Size: "5Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			assertFunc: func(t *testing.T, c client.Client, shard *multigresv1alpha1.Shard) {
				// Verify replica pool StatefulSet
				replicaSts := &appsv1.StatefulSet{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "multi-pool-shard-pool-replica-zone1", Namespace: "default"},
					replicaSts); err != nil {
					t.Errorf("Replica pool StatefulSet should exist: %v", err)
				} else if *replicaSts.Spec.Replicas != 2 {
					t.Errorf("Replica pool replicas = %d, want 2", *replicaSts.Spec.Replicas)
				}

				// Verify readOnly pool StatefulSet
				readOnlySts := &appsv1.StatefulSet{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "multi-pool-shard-pool-readOnly-zone1", Namespace: "default"},
					readOnlySts); err != nil {
					t.Errorf("ReadOnly pool StatefulSet should exist: %v", err)
				} else if *readOnlySts.Spec.Replicas != 3 {
					t.Errorf("ReadOnly pool replicas = %d, want 3", *readOnlySts.Spec.Replicas)
				}

				// Verify both headless services
				replicaSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "multi-pool-shard-pool-replica-zone1-headless", Namespace: "default"},
					replicaSvc); err != nil {
					t.Errorf("Replica pool headless Service should exist: %v", err)
				}

				readOnlySvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "multi-pool-shard-pool-readOnly-zone1-headless", Namespace: "default"},
					readOnlySvc); err != nil {
					t.Errorf("ReadOnly pool headless Service should exist: %v", err)
				}
			},
		},
		"MultiOrch infers cells from pools when not specified": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "inferred-cells-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{}, // Empty - will infer from pools
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells:           []multigresv1alpha1.CellName{"zone1", "zone2"},
							Type:            "replica",
							ReplicasPerCell: ptr.To(int32(1)),
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			assertFunc: func(t *testing.T, c client.Client, shard *multigresv1alpha1.Shard) {
				// MultiOrch should be deployed to both zone1 and zone2
				mo1 := &appsv1.Deployment{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "inferred-cells-shard-multiorch-zone1", Namespace: "default"},
					mo1); err != nil {
					t.Errorf("MultiOrch Deployment for zone1 should exist: %v", err)
				}

				mo2 := &appsv1.Deployment{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "inferred-cells-shard-multiorch-zone2", Namespace: "default"},
					mo2); err != nil {
					t.Errorf("MultiOrch Deployment for zone2 should exist: %v", err)
				}
			},
		},
		"error when MultiOrch and pools have no cells specified": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-cells-anywhere",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{}, // Empty
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells:           []multigresv1alpha1.CellName{}, // Also empty - should error
							Type:            "replica",
							ReplicasPerCell: ptr.To(int32(1)),
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			wantErr:         true,
		},
		"error when pool has no cells specified": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-cell-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells:           []multigresv1alpha1.CellName{}, // Empty cells - should error
							Type:            "replica",
							ReplicasPerCell: ptr.To(int32(1)),
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			wantErr:         true,
		},
		"create resources for Shard with multi-cell pool": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-cell-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1", "zone2"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells:           []multigresv1alpha1.CellName{"zone1", "zone2"},
							Type:            "replica",
							ReplicasPerCell: ptr.To(int32(2)),
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			assertFunc: func(t *testing.T, c client.Client, shard *multigresv1alpha1.Shard) {
				// Verify StatefulSet for zone1
				sts1 := &appsv1.StatefulSet{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "multi-cell-shard-pool-primary-zone1", Namespace: "default"},
					sts1); err != nil {
					t.Fatalf("StatefulSet for zone1 should exist: %v", err)
				}
				if *sts1.Spec.Replicas != 2 {
					t.Errorf("Zone1 replicas = %d, want 2", *sts1.Spec.Replicas)
				}

				// Verify StatefulSet for zone2
				sts2 := &appsv1.StatefulSet{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "multi-cell-shard-pool-primary-zone2", Namespace: "default"},
					sts2); err != nil {
					t.Fatalf("StatefulSet for zone2 should exist: %v", err)
				}
				if *sts2.Spec.Replicas != 2 {
					t.Errorf("Zone2 replicas = %d, want 2", *sts2.Spec.Replicas)
				}

				// Verify headless Services for both cells
				svc1 := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "multi-cell-shard-pool-primary-zone1-headless", Namespace: "default"},
					svc1); err != nil {
					t.Fatalf("Headless Service for zone1 should exist: %v", err)
				}

				svc2 := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "multi-cell-shard-pool-primary-zone2-headless", Namespace: "default"},
					svc2); err != nil {
					t.Fatalf("Headless Service for zone2 should exist: %v", err)
				}
			},
		},
		"update existing resources": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "existing-shard",
					Namespace:  "default",
					Finalizers: []string{"shard.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Images: multigresv1alpha1.ShardImages{
						MultiPooler: "custom/multipooler:v1.0.0",
						Postgres:    "postgres:16",
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells:           []multigresv1alpha1.CellName{"zone1"},
							Type:            "replica",
							ReplicasPerCell: ptr.To(int32(5)),
							Storage: multigresv1alpha1.StorageSpec{
								Size: "20Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-shard-multiorch",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(2)),
					},
					Status: appsv1.DeploymentStatus{
						Replicas:      2,
						ReadyReplicas: 2,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-shard-multiorch",
						Namespace: "default",
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-shard-pool-primary-zone1",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: ptr.To(int32(2)), // will be updated to 5
					},
					Status: appsv1.StatefulSetStatus{
						Replicas:      2,
						ReadyReplicas: 2,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-shard-pool-primary-zone1-headless",
						Namespace: "default",
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, shard *multigresv1alpha1.Shard) {
				poolSts := &appsv1.StatefulSet{}
				err := c.Get(t.Context(), types.NamespacedName{
					Name:      "existing-shard-pool-primary-zone1",
					Namespace: "default",
				}, poolSts)
				if err != nil {
					t.Fatalf("Failed to get Pool StatefulSet: %v", err)
				}

				if *poolSts.Spec.Replicas != 5 {
					t.Errorf("Pool StatefulSet replicas = %d, want 5", *poolSts.Spec.Replicas)
				}
			},
		},
		"deletion with finalizer": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-shard-deletion",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					Finalizers:        []string{"shard.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone1"},
							Type:  "replica",
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-shard-deletion",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{"shard.multigres.com/finalizer"},
					},
					Spec: multigresv1alpha1.ShardSpec{
						MultiOrch: multigresv1alpha1.MultiOrchSpec{
							Cells: []multigresv1alpha1.CellName{"zone1"},
						},
						Pools: map[string]multigresv1alpha1.PoolSpec{
							"primary": {
								Cells: []multigresv1alpha1.CellName{"zone1"},
								Type:  "replica",
								Storage: multigresv1alpha1.StorageSpec{
									Size: "10Gi",
								},
							},
						},
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, shard *multigresv1alpha1.Shard) {
				updatedShard := &multigresv1alpha1.Shard{}
				err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-shard-deletion", Namespace: "default"},
					updatedShard)
				if err == nil {
					t.Errorf(
						"Shard object should be deleted but still exists (finalizers: %v)",
						updatedShard.Finalizers,
					)
				}
			},
		},
		"all replicas ready status": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-shard-ready",
					Namespace:  "default",
					Finalizers: []string{"shard.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells:           []multigresv1alpha1.CellName{"zone1"},
							Type:            "replica",
							ReplicasPerCell: ptr.To(int32(3)),
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-ready-multiorch-zone1",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(1)),
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-ready-multiorch-zone1",
						Namespace: "default",
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-ready-pool-primary-zone1",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: ptr.To(int32(3)),
					},
					Status: appsv1.StatefulSetStatus{
						Replicas:      3,
						ReadyReplicas: 3,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-ready-pool-primary-zone1-headless",
						Namespace: "default",
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, shard *multigresv1alpha1.Shard) {
				updatedShard := &multigresv1alpha1.Shard{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-shard-ready", Namespace: "default"},
					updatedShard); err != nil {
					t.Fatalf("Failed to get Shard: %v", err)
				}

				if len(updatedShard.Status.Conditions) == 0 {
					t.Error("Status.Conditions should not be empty")
				} else {
					availableCondition := updatedShard.Status.Conditions[0]
					if availableCondition.Type != "Available" {
						t.Errorf("Condition type = %s, want Available", availableCondition.Type)
					}
					if availableCondition.Status != metav1.ConditionTrue {
						t.Errorf("Condition status = %s, want True", availableCondition.Status)
					}
					if availableCondition.Reason != "AllPodsReady" {
						t.Errorf("Condition reason = %s, want AllPodsReady", availableCondition.Reason)
					}
				}

				// Status no longer tracks TotalPods/ReadyPods - uses PoolsReady boolean instead
				if !updatedShard.Status.PoolsReady {
					t.Error("PoolsReady should be true when all pools are ready")
				}
			},
		},
		"not ready status - partial replicas": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-shard-partial",
					Namespace:  "default",
					Finalizers: []string{"shard.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells:           []multigresv1alpha1.CellName{"zone1"},
							Type:            "replica",
							ReplicasPerCell: ptr.To(int32(5)),
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-partial-multiorch",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-partial-multiorch",
						Namespace: "default",
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-partial-pool-primary",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: ptr.To(int32(5)),
					},
					Status: appsv1.StatefulSetStatus{
						Replicas:      5,
						ReadyReplicas: 3, // only 3 out of 5 ready
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-partial-pool-primary-headless",
						Namespace: "default",
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, shard *multigresv1alpha1.Shard) {
				updatedShard := &multigresv1alpha1.Shard{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-shard-partial", Namespace: "default"},
					updatedShard); err != nil {
					t.Fatalf("Failed to get Shard: %v", err)
				}

				if len(updatedShard.Status.Conditions) == 0 {
					t.Fatal("Status.Conditions should not be empty")
				}

				availableCondition := updatedShard.Status.Conditions[0]
				if availableCondition.Type != "Available" {
					t.Errorf("Condition type = %s, want Available", availableCondition.Type)
				}
				if availableCondition.Status != metav1.ConditionFalse {
					t.Errorf("Condition status = %s, want False", availableCondition.Status)
				}
				if availableCondition.Reason != "NotAllPodsReady" {
					t.Errorf(
						"Condition reason = %s, want NotAllPodsReady",
						availableCondition.Reason,
					)
				}

				// Status no longer tracks TotalPods/ReadyPods - partial ready means PoolsReady=false
				if updatedShard.Status.PoolsReady {
					t.Error("PoolsReady should be false when not all pools are ready")
				}
			},
		},
		"status with multiple pools": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-shard-multi",
					Namespace:  "default",
					Finalizers: []string{"shard.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"replica": {
							Cells:           []multigresv1alpha1.CellName{"zone1"},
							Type:            "replica",
							ReplicasPerCell: ptr.To(int32(2)),
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
						"readOnly": {
							Cells:           []multigresv1alpha1.CellName{"zone1"},
							Type:            "readOnly",
							ReplicasPerCell: ptr.To(int32(3)),
							Storage: multigresv1alpha1.StorageSpec{
								Size: "5Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multi-multiorch-zone1",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multi-multiorch-zone1",
						Namespace: "default",
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multi-pool-replica-zone1",
						Namespace: "default",
					},
					Status: appsv1.StatefulSetStatus{
						Replicas:      2,
						ReadyReplicas: 2,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multi-pool-replica-zone1-headless",
						Namespace: "default",
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multi-pool-readOnly-zone1",
						Namespace: "default",
					},
					Status: appsv1.StatefulSetStatus{
						Replicas:      3,
						ReadyReplicas: 3,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multi-pool-readOnly-zone1-headless",
						Namespace: "default",
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, shard *multigresv1alpha1.Shard) {
				updatedShard := &multigresv1alpha1.Shard{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-shard-multi", Namespace: "default"},
					updatedShard); err != nil {
					t.Fatalf("Failed to get Shard: %v", err)
				}

				// Total should be 2 + 3 = 5
				// Status no longer tracks TotalPods/ReadyPods - uses PoolsReady boolean instead
				if !updatedShard.Status.PoolsReady {
					t.Error("PoolsReady should be true when all pools are ready")
				}
			},
		},
		////----------------------------------------
		///   Error
		//------------------------------------------
		"error on status update": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone1"},
							Type:  "replica",
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnStatusUpdate: testutil.FailOnObjectName("test-shard", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error on MultiOrch Deployment create": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone1"},
							Type:  "replica",
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: func(obj client.Object) error {
					if deploy, ok := obj.(*appsv1.Deployment); ok &&
						deploy.Name == "test-shard-multiorch-zone1" {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on MultiOrch Deployment Update": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-shard",
					Namespace:  "default",
					Finalizers: []string{"shard.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone1"},
							Type:  "replica",
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch-zone1",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(2)),
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: func(obj client.Object) error {
					if deploy, ok := obj.(*appsv1.Deployment); ok &&
						deploy.Name == "test-shard-multiorch-zone1" {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Get MultiOrch Deployment (network error)": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-shard",
					Namespace:  "default",
					Finalizers: []string{"shard.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone1"},
							Type:  "replica",
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					if key.Name == "test-shard-multiorch-zone1" {
						return testutil.ErrNetworkTimeout
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on MultiOrch Service create": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone1"},
							Type:  "replica",
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok &&
						svc.Name == "test-shard-multiorch-zone1" {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on MultiOrch Service Update": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-shard",
					Namespace:  "default",
					Finalizers: []string{"shard.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone1"},
							Type:  "replica",
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch-zone1",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch-zone1",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok &&
						svc.Name == "test-shard-multiorch-zone1" {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Get MultiOrch Service (network error)": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-shard-svc",
					Namespace:  "default",
					Finalizers: []string{"shard.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone1"},
							Type:  "replica",
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-svc-multiorch-zone1",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnNamespacedKeyName(
					"test-shard-svc-multiorch-zone1",
					"default",
					testutil.ErrNetworkTimeout,
				),
			},
			wantErr: true,
		},
		"error on Pool StatefulSet create": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone1"},
							Type:  "replica",
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: func(obj client.Object) error {
					if sts, ok := obj.(*appsv1.StatefulSet); ok &&
						sts.Name == "test-shard-pool-primary-zone1" {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Pool StatefulSet Update": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-shard",
					Namespace:  "default",
					Finalizers: []string{"shard.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells:           []multigresv1alpha1.CellName{"zone1"},
							Type:            "replica",
							ReplicasPerCell: ptr.To(int32(5)),
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch-zone1",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch-zone1",
						Namespace: "default",
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-pool-primary-zone1",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: ptr.To(int32(2)),
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: func(obj client.Object) error {
					if sts, ok := obj.(*appsv1.StatefulSet); ok &&
						sts.Name == "test-shard-pool-primary-zone1" {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Get Pool StatefulSet (network error)": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-shard",
					Namespace:  "default",
					Finalizers: []string{"shard.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone1"},
							Type:  "replica",
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch-zone1",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch-zone1",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					if key.Name == "test-shard-pool-primary-zone1" {
						return testutil.ErrNetworkTimeout
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Pool Service create": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone1"},
							Type:  "replica",
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok &&
						svc.Name == "test-shard-pool-primary-zone1-headless" {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Pool Service Update": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-shard",
					Namespace:  "default",
					Finalizers: []string{"shard.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone1"},
							Type:  "replica",
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch-zone1",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch-zone1",
						Namespace: "default",
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-pool-primary-zone1",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-pool-primary-zone1-headless",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok &&
						svc.Name == "test-shard-pool-primary-zone1-headless" {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Get Pool Service (network error)": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-shard",
					Namespace:  "default",
					Finalizers: []string{"shard.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone1"},
							Type:  "replica",
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch-zone1",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch-zone1",
						Namespace: "default",
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-pool-primary-zone1",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					if key.Name == "test-shard-pool-primary-zone1-headless" &&
						key.Namespace == "default" {
						return testutil.ErrNetworkTimeout
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on finalizer Update": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone1"},
							Type:  "replica",
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-shard", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"deletion error on finalizer removal": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-shard-del",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					Finalizers:        []string{"shard.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone1"},
							Type:  "replica",
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-shard-del",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{"shard.multigres.com/finalizer"},
					},
					Spec: multigresv1alpha1.ShardSpec{
						MultiOrch: multigresv1alpha1.MultiOrchSpec{
							Cells: []multigresv1alpha1.CellName{"zone1"},
						},
						Pools: map[string]multigresv1alpha1.PoolSpec{
							"primary": {
								Cells: []multigresv1alpha1.CellName{"zone1"},
								Type:  "replica",
								Storage: multigresv1alpha1.StorageSpec{
									Size: "10Gi",
								},
							},
						},
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-shard-del", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error on Get Shard (network error)": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone1"},
							Type:  "replica",
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName("test-shard", testutil.ErrNetworkTimeout),
			},
			wantErr: true,
		},
		"error on Get Pool StatefulSet in updateStatus (network error)": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-shard-status",
					Namespace:  "default",
					Finalizers: []string{"shard.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone1"},
							Type:  "replica",
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-status-multiorch",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-status-multiorch",
						Namespace: "default",
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-status-pool-primary",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-status-pool-primary-headless",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				// Fail Pool StatefulSet Get after successful reconciliation calls
				// Get calls: 1=Shard, 2=MultiOrchDeploy, 3=MultiOrchSvc, 4=PoolSts, 5=PoolSvc, 6=PoolSts(status)
				OnGet: testutil.FailKeyAfterNCalls(5, testutil.ErrNetworkTimeout),
			},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Create base fake client
			baseClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingObjects...).
				WithStatusSubresource(&multigresv1alpha1.Shard{}).
				Build()

			fakeClient := client.Client(baseClient)
			// Wrap with failure injection if configured
			if tc.failureConfig != nil {
				fakeClient = testutil.NewFakeClientWithFailures(baseClient, tc.failureConfig)
			}

			reconciler := &shard.ShardReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			// Create the Shard resource if not in existing objects
			shardInExisting := false
			for _, obj := range tc.existingObjects {
				if shard, ok := obj.(*multigresv1alpha1.Shard); ok && shard.Name == tc.shard.Name {
					shardInExisting = true
					break
				}
			}
			if !shardInExisting {
				err := fakeClient.Create(t.Context(), tc.shard)
				if err != nil {
					t.Fatalf("Failed to create Shard: %v", err)
				}
			}

			// Reconcile
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tc.shard.Name,
					Namespace: tc.shard.Namespace,
				},
			}

			result, err := reconciler.Reconcile(t.Context(), req)
			if (err != nil) != tc.wantErr {
				t.Errorf("Reconcile() error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			if tc.wantErr {
				return
			}

			// NOTE: Check for requeue delay when we need to support such setup.
			_ = result

			// Run custom assertions if provided
			if tc.assertFunc != nil {
				tc.assertFunc(t, fakeClient, tc.shard)
			}
		})
	}
}

func TestShardReconciler_ReconcileNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	reconciler := &shard.ShardReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// Reconcile non-existent resource
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "nonexistent-shard",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(t.Context(), req)
	if err != nil {
		t.Errorf("Reconcile() should not error on NotFound, got: %v", err)
	}
	if result.RequeueAfter > 0 {
		t.Errorf("Reconcile() should not requeue on NotFound")
	}
}

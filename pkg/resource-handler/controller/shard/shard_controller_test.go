package shard

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"

	"github.com/numtide/multigres-operator/pkg/testutil"
)

type reconcileTestCase struct {
	shard            *multigresv1alpha1.Shard
	existingObjects  []client.Object
	failureConfig    *testutil.FailureConfig
	reconcilerScheme *runtime.Scheme
	wantErr          bool
	assertFunc       func(t *testing.T, c client.Client, shard *multigresv1alpha1.Shard)
}

func TestShardReconciler_Reconcile(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := map[string]reconcileTestCase{
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
				hashedMoName := buildHashedMultiOrchName(shard, "zone1")
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: hashedMoName, Namespace: "default"},
					moDeploy); err != nil {
					t.Errorf("MultiOrch Deployment should exist: %v", err)
				}

				// Verify MultiOrch Service was created (with cell suffix)
				moSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: hashedMoName, Namespace: "default"},
					moSvc); err != nil {
					t.Errorf("MultiOrch Service should exist: %v", err)
				}

				// Verify Pool StatefulSet was created (with cell suffix)
				hashPoolName := buildHashedPoolName(shard, "primary", "zone1")
				poolSts := &appsv1.StatefulSet{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: hashPoolName, Namespace: "default"},
					poolSts); err != nil {
					t.Errorf("Pool StatefulSet should exist: %v", err)
				}

				// Verify Pool headless Service was created (with cell suffix)
				hashedHeadless := buildHashedPoolHeadlessServiceName(shard, "primary", "zone1")
				poolSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: hashedHeadless, Namespace: "default"},
					poolSvc); err != nil {
					t.Errorf("Pool headless Service should exist: %v", err)
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
				hashReplica := buildHashedPoolName(shard, "replica", "zone1")
				replicaSts := &appsv1.StatefulSet{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: hashReplica, Namespace: "default"},
					replicaSts); err != nil {
					t.Errorf("Replica pool StatefulSet should exist: %v", err)
				} else if *replicaSts.Spec.Replicas != 2 {
					t.Errorf("Replica pool replicas = %d, want 2", *replicaSts.Spec.Replicas)
				}

				// Verify readOnly pool StatefulSet
				hashReadOnly := buildHashedPoolName(shard, "readOnly", "zone1")
				readOnlySts := &appsv1.StatefulSet{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: hashReadOnly, Namespace: "default"},
					readOnlySts); err != nil {
					t.Errorf("ReadOnly pool StatefulSet should exist: %v", err)
				} else if *readOnlySts.Spec.Replicas != 3 {
					t.Errorf("ReadOnly pool replicas = %d, want 3", *readOnlySts.Spec.Replicas)
				}

				// Verify both headless services
				hashReplicaHeadless := buildHashedPoolHeadlessServiceName(shard, "replica", "zone1")
				replicaSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: hashReplicaHeadless, Namespace: "default"},
					replicaSvc); err != nil {
					t.Errorf("Replica pool headless Service should exist: %v", err)
				}

				hashReadOnlyHeadless := buildHashedPoolHeadlessServiceName(
					shard,
					"readOnly",
					"zone1",
				)
				readOnlySvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: hashReadOnlyHeadless, Namespace: "default"},
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
				hashedMo1 := buildHashedMultiOrchName(shard, "zone1")
				mo1 := &appsv1.Deployment{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: hashedMo1, Namespace: "default"},
					mo1); err != nil {
					t.Errorf("MultiOrch Deployment for zone1 should exist: %v", err)
				}

				hashedMo2 := buildHashedMultiOrchName(shard, "zone2")
				mo2 := &appsv1.Deployment{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: hashedMo2, Namespace: "default"},
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
				hashZone1 := buildHashedPoolName(shard, "primary", "zone1")
				sts1 := &appsv1.StatefulSet{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: hashZone1, Namespace: "default"},
					sts1); err != nil {
					t.Fatalf("StatefulSet for zone1 should exist: %v", err)
				}
				if *sts1.Spec.Replicas != 2 {
					t.Errorf("Zone1 replicas = %d, want 2", *sts1.Spec.Replicas)
				}

				// Verify StatefulSet for zone2
				hashZone2 := buildHashedPoolName(shard, "primary", "zone2")
				sts2 := &appsv1.StatefulSet{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: hashZone2, Namespace: "default"},
					sts2); err != nil {
					t.Fatalf("StatefulSet for zone2 should exist: %v", err)
				}
				if *sts2.Spec.Replicas != 2 {
					t.Errorf("Zone2 replicas = %d, want 2", *sts2.Spec.Replicas)
				}

				// Verify headless Services for both cells
				hashSvc1 := buildHashedPoolHeadlessServiceName(shard, "primary", "zone1")
				svc1 := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: hashSvc1, Namespace: "default"},
					svc1); err != nil {
					t.Fatalf("Headless Service for zone1 should exist: %v", err)
				}

				hashSvc2 := buildHashedPoolHeadlessServiceName(shard, "primary", "zone2")
				svc2 := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: hashSvc2, Namespace: "default"},
					svc2); err != nil {
					t.Fatalf("Headless Service for zone2 should exist: %v", err)
				}
			},
		},
		"update existing resources": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing-shard",
					Namespace: "default",
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg-hba-template",
						Namespace: "default",
					},
				},
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup-data-existing-shard-pool-primary-zone1",
						Namespace: "default",
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, shard *multigresv1alpha1.Shard) {
				hashPool := buildHashedPoolName(shard, "primary", "zone1")
				poolSts := &appsv1.StatefulSet{}
				err := c.Get(t.Context(), types.NamespacedName{
					Name:      hashPool,
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

		"deletion - early exit": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-shard-deletion",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
			existingObjects: []client.Object{
				&multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-shard-deletion",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{"testing"},
					},
					Spec: multigresv1alpha1.ShardSpec{
						DatabaseName:   "testdb",
						TableGroupName: "default",
						MultiOrch: multigresv1alpha1.MultiOrchSpec{
							Cells: []multigresv1alpha1.CellName{"zone1"},
						},
						Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
			},
			assertFunc: func(t *testing.T, c client.Client, shard *multigresv1alpha1.Shard) {
				// Verify MultiOrch Deployment was NOT created
				moDeploy := &appsv1.Deployment{}
				hashedMoName := buildHashedMultiOrchName(shard, "zone1")
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: hashedMoName, Namespace: "default"},
					moDeploy); err == nil {
					t.Errorf("MultiOrch Deployment should NOT exist")
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
				OnStatusPatch: testutil.FailOnObjectName("test-shard", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error on MultiOrch Deployment patch": {
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
				OnPatch: func(obj client.Object) error {
					if deploy, ok := obj.(*appsv1.Deployment); ok &&
						strings.Contains(
							deploy.Name,
							"multiorch",
						) && strings.Contains(deploy.Name, "zone1") {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},

		"error on MultiOrch Service patch": {
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
				OnPatch: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok &&
						strings.Contains(
							svc.Name,
							"multiorch",
						) && strings.Contains(svc.Name, "zone1") {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},

		"error on Pool StatefulSet patch": {
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
				OnPatch: func(obj client.Object) error {
					if sts, ok := obj.(*appsv1.StatefulSet); ok &&
						strings.Contains(
							sts.Name,
							"pool",
						) && strings.Contains(sts.Name, "primary") && strings.Contains(sts.Name, "zone1") {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},

		"error on Pool Service patch": {
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
				OnPatch: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok &&
						strings.Contains(
							svc.Name,
							"pool",
						) && strings.Contains(svc.Name, "primary") &&
						strings.Contains(
							svc.Name,
							"zone1",
						) && strings.Contains(svc.Name, "headless") {
						return testutil.ErrPermissionError
					}
					return nil
				},
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
		"error on pg_hba ConfigMap patch": {
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
				OnPatch: func(obj client.Object) error {
					if cm, ok := obj.(*corev1.ConfigMap); ok &&
						strings.Contains(cm.Name, "pg-hba") {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Pool Backup PVC patch": {
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
				OnPatch: func(obj client.Object) error {
					if pvc, ok := obj.(*corev1.PersistentVolumeClaim); ok &&
						strings.Contains(pvc.Name, "backup-data") {
						return testutil.ErrPermissionError
					}
					return nil
				},
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
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
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
						Name:      "test-shard-status-multiorch-zone1",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-status-multiorch-zone1",
						Namespace: "default",
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-status-pool-primary-zone1",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-status-pool-primary-zone1-headless",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				// Fail Pool StatefulSet Get in updateStatus
				// With SSA, the only Get calls are:
				// 1. Shard (at start of Reconcile)
				// 2. Pool StatefulSet (in updateStatus)
				// So we want to fail the 2nd Get call.
				OnGet: testutil.FailKeyAfterNCalls(1, testutil.ErrNetworkTimeout),
			},
			wantErr: true,
		},
		"error on build PgHba ConfigMap (empty scheme)": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "build-err-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
				},
			},
			reconcilerScheme: runtime.NewScheme(), // Empty scheme
			wantErr:          true,
		},
		"error on Get PgHba ConfigMap (network error)": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard-pghba-get-err",
					Namespace: "default",
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					if key.Name == "pg-hba-template" {
						return testutil.ErrNetworkTimeout
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on build MultiOrch Deployment (scheme missing Shard)": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "build-mo-err-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1"},
					},
				},
			},
			reconcilerScheme: runtime.NewScheme(),
			wantErr:          true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Patch existing objects names to use hashed names
			for i, obj := range tc.existingObjects {
				name := obj.GetName()
				replaced := false

				// Check MultiOrch
				for _, cell := range tc.shard.Spec.MultiOrch.Cells {
					if strings.Contains(name, "multiorch") && strings.Contains(name, string(cell)) {
						hashed := buildHashedMultiOrchName(tc.shard, string(cell))
						obj.SetName(hashed)
						// Update labels/selectors if applicable
						if deploy, ok := obj.(*appsv1.Deployment); ok {
							if deploy.Labels != nil {
								deploy.Labels["app.kubernetes.io/instance"] = hashed
							}
							if deploy.Spec.Selector != nil {
								deploy.Spec.Selector.MatchLabels["app.kubernetes.io/instance"] = hashed
							}
						}
						// Service selector update? Service selector uses labels.
						// We don't update Service contents here usually, just name.
						replaced = true
						break
					}
				}
				if replaced {
					tc.existingObjects[i] = obj
					continue
				}

				// Check Pools
				for poolName, poolSpec := range tc.shard.Spec.Pools {
					for _, cell := range poolSpec.Cells {
						if strings.Contains(name, "pool") &&
							strings.Contains(name, string(poolName)) &&
							strings.Contains(name, string(cell)) {
							// Determine if headless svc or backup pvc
							if strings.Contains(name, "headless") {
								hashed := buildHashedPoolHeadlessServiceName(
									tc.shard,
									string(poolName),
									string(cell),
								)
								obj.SetName(hashed)
							} else if strings.Contains(name, "backup-data") {
								hashed := buildHashedBackupPVCName(
									tc.shard,
									string(poolName),
									string(cell),
								)
								obj.SetName(hashed)
							} else {
								// StatefulSet or Service (if not headless - wait, pool service IS headless)
								// Wait, is there a non-headless service for pool?
								// pool_service.go creates headless. Use buildHashedPoolHeadlessServiceName.
								// Check if obj kind is Service.
								// But "multiorch" handled above. "pool" here.
								// Pool creates StatefulSet and Headless Service.

								// What if test creates a generic service?
								// The tests create: "test-shard-pool-primary-zone1-headless".

								// What about "pool-primary-zone1" (StatefulSet)?
								hashed := buildHashedPoolName(
									tc.shard,
									string(poolName),
									string(cell),
								)
								if !strings.Contains(name, "headless") &&
									!strings.Contains(name, "backup-data") {
									obj.SetName(hashed)
									if sts, ok := obj.(*appsv1.StatefulSet); ok {
										if sts.Labels != nil {
											sts.Labels["app.kubernetes.io/instance"] = hashed
										}
										if sts.Spec.Selector != nil {
											sts.Spec.Selector.MatchLabels["app.kubernetes.io/instance"] = hashed
										}
										// ServiceName in STS must match Headless Service Name!
										// We need to update sts.Spec.ServiceName to hashed headless name.
										headlessName := buildHashedPoolHeadlessServiceName(
											tc.shard,
											string(poolName),
											string(cell),
										)
										sts.Spec.ServiceName = headlessName
									}
								}
							}
							replaced = true
							break
						}
					}
					if replaced {
						break
					}
				}
				tc.existingObjects[i] = obj
			}

			// Create base fake client
			baseClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingObjects...).
				WithStatusSubresource(&multigresv1alpha1.Shard{}).
				WithStatusSubresource(&appsv1.StatefulSet{}).
				WithStatusSubresource(&appsv1.Deployment{}).
				Build()

			fakeClient := client.Client(baseClient)

			// Wrap with failure injection if configured
			if tc.failureConfig != nil {
				fakeClient = testutil.NewFakeClientWithFailures(baseClient, tc.failureConfig)
			}

			reconciler := &ShardReconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(1000),
			}
			if tc.reconcilerScheme != nil {
				reconciler.Scheme = tc.reconcilerScheme
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

			// Check headers
			for _, obj := range tc.existingObjects {
				if sts, ok := obj.(*appsv1.StatefulSet); ok {
					t.Logf(
						"PRERECONCILE STS: %s, Status.Replicas: %d, Status.ReadyReplicas: %d",
						sts.Name,
						sts.Status.Replicas,
						sts.Status.ReadyReplicas,
					)
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

	reconciler := &ShardReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
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

func TestShardReconciler_UpdateStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = multigresv1alpha1.AddToScheme(scheme)

	t.Run("all_replicas_ready_status", func(t *testing.T) {
		shard := &multigresv1alpha1.Shard{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-shard-ready",
				Namespace: "default",
				Labels: map[string]string{
					"multigres.com/cluster": "test-cluster",
				},
			},
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName:   "testdb",
				TableGroupName: "default",
				MultiOrch: multigresv1alpha1.MultiOrchSpec{
					Cells: []multigresv1alpha1.CellName{"zone1"},
				},
				Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
					"primary": {
						Cells: []multigresv1alpha1.CellName{"zone1"},
						Type:  "readWrite",
						Storage: multigresv1alpha1.StorageSpec{
							Size: "10Gi",
						},
						ReplicasPerCell: ptr.To(int32(3)),
					},
				},
			},
		}

		stsName := buildHashedPoolName(shard, "primary", "zone1")
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      stsName,
				Namespace: "default",
				Labels: map[string]string{
					"multigres.com/shard": "test-shard-ready",
				},
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: ptr.To(int32(3)),
			},
			Status: appsv1.StatefulSetStatus{
				Replicas:      3,
				ReadyReplicas: 3,
			},
		}

		moName := buildHashedMultiOrchName(shard, "zone1")
		mo := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moName,
				Namespace: "default",
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To(int32(1)),
			},
			Status: appsv1.DeploymentStatus{
				Replicas:      1,
				ReadyReplicas: 1,
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(shard, sts, mo).
			WithStatusSubresource(shard, sts, mo).
			Build()

		r := &ShardReconciler{
			Client:   fakeClient,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(100),
		}

		if err := r.updateStatus(context.Background(), shard); err != nil {
			t.Fatalf("updateStatus failed: %v", err)
		}

		updatedShard := &multigresv1alpha1.Shard{}
		if err := fakeClient.Get(
			context.Background(),
			client.ObjectKeyFromObject(shard),
			updatedShard,
		); err != nil {
			t.Fatalf("Failed to get Shard: %v", err)
		}

		foundTrue := false
		for _, cond := range updatedShard.Status.Conditions {
			if cond.Type == "Available" {
				if cond.Status != metav1.ConditionTrue {
					t.Errorf("Condition status = %s, want %s", cond.Status, metav1.ConditionTrue)
				}
				if cond.Reason != "AllPodsReady" {
					t.Errorf("Condition reason = %s, want %s", cond.Reason, "AllPodsReady")
				}
				foundTrue = true
			}
		}
		if !foundTrue {
			t.Errorf("Condition %s not found", "Available")
		}
		if !updatedShard.Status.PoolsReady {
			t.Error("PoolsReady should be true when all pools are ready")
		}
		if updatedShard.Status.Phase != multigresv1alpha1.PhaseHealthy {
			t.Errorf(
				"Expected Phase to be %s, got %s",
				multigresv1alpha1.PhaseHealthy,
				updatedShard.Status.Phase,
			)
		}
	})

	t.Run("status_with_multiple_pools", func(t *testing.T) {
		shard := &multigresv1alpha1.Shard{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-shard-multi",
				Namespace: "default",
			},
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName:   "testdb",
				TableGroupName: "default",
				MultiOrch: multigresv1alpha1.MultiOrchSpec{
					Cells: []multigresv1alpha1.CellName{"zone1"},
				},
				Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
					"replica": {
						Cells:           []multigresv1alpha1.CellName{"zone1"},
						Type:            "replica",
						ReplicasPerCell: ptr.To(int32(2)),
					},
					"readOnly": {
						Cells:           []multigresv1alpha1.CellName{"zone1"},
						Type:            "readOnly",
						ReplicasPerCell: ptr.To(int32(3)),
					},
				},
			},
		}

		sts1Name := buildHashedPoolName(shard, "replica", "zone1")
		sts1 := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: sts1Name, Namespace: "default"},
			Status:     appsv1.StatefulSetStatus{Replicas: 2, ReadyReplicas: 2},
		}
		sts2Name := buildHashedPoolName(shard, "readOnly", "zone1")
		sts2 := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: sts2Name, Namespace: "default"},
			Status:     appsv1.StatefulSetStatus{Replicas: 3, ReadyReplicas: 3},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(shard, sts1, sts2).
			WithStatusSubresource(shard, sts1, sts2).
			Build()

		r := &ShardReconciler{
			Client:   fakeClient,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(100),
		}

		if err := r.updateStatus(context.Background(), shard); err != nil {
			t.Fatalf("updateStatus failed: %v", err)
		}

		updatedShard := &multigresv1alpha1.Shard{}
		if err := fakeClient.Get(
			context.Background(),
			client.ObjectKeyFromObject(shard),
			updatedShard,
		); err != nil {
			t.Fatalf("Failed to get Shard: %v", err)
		}

		if !updatedShard.Status.PoolsReady {
			t.Error("PoolsReady should be true when all pools are ready")
		}
	})
}

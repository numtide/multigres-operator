package shard_test

import (
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/shard"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/testutil"
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							Replicas:   ptr.To(int32(1)),
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			assertFunc: func(t *testing.T, c client.Client, shard *multigresv1alpha1.Shard) {
				// Verify MultiOrch Deployment was created
				moDeploy := &appsv1.Deployment{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-shard-multiorch", Namespace: "default"},
					moDeploy); err != nil {
					t.Errorf("MultiOrch Deployment should exist: %v", err)
				}

				// Verify MultiOrch Service was created
				moSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-shard-multiorch", Namespace: "default"},
					moSvc); err != nil {
					t.Errorf("MultiOrch Service should exist: %v", err)
				}

				// Verify Pool StatefulSet was created
				poolSts := &appsv1.StatefulSet{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-shard-pool-primary", Namespace: "default"},
					poolSts); err != nil {
					t.Errorf("Pool StatefulSet should exist: %v", err)
				}

				// Verify Pool headless Service was created
				poolSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-shard-pool-primary-headless", Namespace: "default"},
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"replica": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							Replicas:   ptr.To(int32(2)),
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
							},
						},
						"readOnly": {
							Cell:       "zone1",
							Type:       "readOnly",
							Database:   "testdb",
							TableGroup: "default",
							Replicas:   ptr.To(int32(3)),
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("5Gi"),
									},
								},
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
					types.NamespacedName{Name: "multi-pool-shard-pool-replica", Namespace: "default"},
					replicaSts); err != nil {
					t.Errorf("Replica pool StatefulSet should exist: %v", err)
				} else if *replicaSts.Spec.Replicas != 2 {
					t.Errorf("Replica pool replicas = %d, want 2", *replicaSts.Spec.Replicas)
				}

				// Verify readOnly pool StatefulSet
				readOnlySts := &appsv1.StatefulSet{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "multi-pool-shard-pool-readOnly", Namespace: "default"},
					readOnlySts); err != nil {
					t.Errorf("ReadOnly pool StatefulSet should exist: %v", err)
				} else if *readOnlySts.Spec.Replicas != 3 {
					t.Errorf("ReadOnly pool replicas = %d, want 3", *readOnlySts.Spec.Replicas)
				}

				// Verify both headless services
				replicaSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "multi-pool-shard-pool-replica-headless", Namespace: "default"},
					replicaSvc); err != nil {
					t.Errorf("Replica pool headless Service should exist: %v", err)
				}

				readOnlySvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "multi-pool-shard-pool-readOnly-headless", Namespace: "default"},
					readOnlySvc); err != nil {
					t.Errorf("ReadOnly pool headless Service should exist: %v", err)
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Images: multigresv1alpha1.ShardImagesSpec{
						MultiPooler: "custom/multipooler:v1.0.0",
						Postgres:    "postgres:16",
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							Replicas:   ptr.To(int32(5)),
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("20Gi"),
									},
								},
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
						Name:      "existing-shard-pool-primary",
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
						Name:      "existing-shard-pool-primary-headless",
						Namespace: "default",
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, shard *multigresv1alpha1.Shard) {
				poolSts := &appsv1.StatefulSet{}
				err := c.Get(t.Context(), types.NamespacedName{
					Name:      "existing-shard-pool-primary",
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
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
							Cells: []string{"zone1"},
						},
						Pools: map[string]multigresv1alpha1.ShardPoolSpec{
							"primary": {
								Cell:       "zone1",
								Type:       "replica",
								Database:   "testdb",
								TableGroup: "default",
								DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
									AccessModes: []corev1.PersistentVolumeAccessMode{
										corev1.ReadWriteOnce,
									},
									Resources: corev1.VolumeResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceStorage: resource.MustParse("10Gi"),
										},
									},
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							Replicas:   ptr.To(int32(3)),
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-ready-multiorch",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(2)),
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-ready-multiorch",
						Namespace: "default",
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-ready-pool-primary",
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
						Name:      "test-shard-ready-pool-primary-headless",
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

				if updatedShard.Status.TotalPods != 3 {
					t.Errorf("TotalPods = %d, want 3", updatedShard.Status.TotalPods)
				}
				if updatedShard.Status.ReadyPods != 3 {
					t.Errorf("ReadyPods = %d, want 3", updatedShard.Status.ReadyPods)
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							Replicas:   ptr.To(int32(5)),
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
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

				if updatedShard.Status.TotalPods != 5 {
					t.Errorf("TotalPods = %d, want 5", updatedShard.Status.TotalPods)
				}
				if updatedShard.Status.ReadyPods != 3 {
					t.Errorf("ReadyPods = %d, want 3", updatedShard.Status.ReadyPods)
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"replica": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							Replicas:   ptr.To(int32(2)),
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
							},
						},
						"readOnly": {
							Cell:       "zone1",
							Type:       "readOnly",
							Database:   "testdb",
							TableGroup: "default",
							Replicas:   ptr.To(int32(3)),
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("5Gi"),
									},
								},
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multi-multiorch",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multi-multiorch",
						Namespace: "default",
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multi-pool-replica",
						Namespace: "default",
					},
					Status: appsv1.StatefulSetStatus{
						Replicas:      2,
						ReadyReplicas: 2,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multi-pool-replica-headless",
						Namespace: "default",
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multi-pool-readOnly",
						Namespace: "default",
					},
					Status: appsv1.StatefulSetStatus{
						Replicas:      3,
						ReadyReplicas: 3,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multi-pool-readOnly-headless",
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
				if updatedShard.Status.TotalPods != 5 {
					t.Errorf("TotalPods = %d, want 5", updatedShard.Status.TotalPods)
				}
				if updatedShard.Status.ReadyPods != 5 {
					t.Errorf("ReadyPods = %d, want 5", updatedShard.Status.ReadyPods)
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: func(obj client.Object) error {
					if deploy, ok := obj.(*appsv1.Deployment); ok &&
						deploy.Name == "test-shard-multiorch" {
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch",
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
						deploy.Name == "test-shard-multiorch" {
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					if key.Name == "test-shard-multiorch" {
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok && svc.Name == "test-shard-multiorch" {
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok && svc.Name == "test-shard-multiorch" {
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-svc-multiorch",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnNamespacedKeyName(
					"test-shard-svc-multiorch",
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: func(obj client.Object) error {
					if sts, ok := obj.(*appsv1.StatefulSet); ok &&
						sts.Name == "test-shard-pool-primary" {
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							Replicas:   ptr.To(int32(5)),
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch",
						Namespace: "default",
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-pool-primary",
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
						sts.Name == "test-shard-pool-primary" {
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					if key.Name == "test-shard-pool-primary" {
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
							},
						},
					},
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok &&
						svc.Name == "test-shard-pool-primary-headless" {
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch",
						Namespace: "default",
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-pool-primary",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-pool-primary-headless",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok &&
						svc.Name == "test-shard-pool-primary-headless" {
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch",
						Namespace: "default",
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-pool-primary",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					if key.Name == "test-shard-pool-primary-headless" &&
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
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
							Cells: []string{"zone1"},
						},
						Pools: map[string]multigresv1alpha1.ShardPoolSpec{
							"primary": {
								Cell:       "zone1",
								Type:       "replica",
								Database:   "testdb",
								TableGroup: "default",
								DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
									AccessModes: []corev1.PersistentVolumeAccessMode{
										corev1.ReadWriteOnce,
									},
									Resources: corev1.VolumeResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceStorage: resource.MustParse("10Gi"),
										},
									},
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1"},
					},
					Pools: map[string]multigresv1alpha1.ShardPoolSpec{
						"primary": {
							Cell:       "zone1",
							Type:       "replica",
							Database:   "testdb",
							TableGroup: "default",
							DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
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

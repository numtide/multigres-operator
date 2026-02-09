package shard_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/multigres/multigres/go/common/topoclient"
	"github.com/multigres/multigres/go/common/topoclient/memorytopo"
	"github.com/multigres/multigres/go/pb/clustermetadata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/data-handler/controller/shard"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

const (
	finalizerName = "shard.data-handler.multigres.com/finalizer"
)

func TestReconcile(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	tests := map[string]struct {
		shard               *multigresv1alpha1.Shard
		existingObjects     []client.Object
		skipAddShard        bool
		customTopoStoreFunc func(*multigresv1alpha1.Shard) (topoclient.Store, error)
		failureConfig       *testutil.FailureConfig
		topoSetup           func(t *testing.T, store topoclient.Store)
		wantErr             bool
		assertFunc          func(t *testing.T, c client.Client, shardObj *multigresv1alpha1.Shard, store topoclient.Store)
	}{
		"adds finalizer on first reconcile": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "postgres",
					TableGroupName: "default",
					ShardName:      "0",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:  "localhost:2379",
						RootPath: "/test",
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone-a"},
						},
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, shardObj *multigresv1alpha1.Shard, store topoclient.Store) {
				updatedShard := &multigresv1alpha1.Shard{}
				if err := c.Get(context.Background(), types.NamespacedName{
					Name:      shardObj.Name,
					Namespace: shardObj.Namespace,
				}, updatedShard); err != nil {
					t.Fatalf("Failed to get Shard: %v", err)
				}
				if !slices.Contains(updatedShard.Finalizers, finalizerName) {
					t.Error("Finalizer should be added")
				}
			},
		},
		"registers database in topology": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-shard",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "postgres",
					TableGroupName: "default",
					ShardName:      "0",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:  "localhost:2379",
						RootPath: "/test",
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone-a", "zone-b"},
						},
						"replica": {
							Cells: []multigresv1alpha1.CellName{"zone-c"},
						},
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, shardObj *multigresv1alpha1.Shard, store topoclient.Store) {
				gotDB, err := store.GetDatabase(context.Background(), "postgres")
				if err != nil {
					t.Errorf("Database should be in topology: %v", err)
					return
				}
				wantDB := &clustermetadata.Database{
					Name: "postgres",
					BackupLocation: &clustermetadata.BackupLocation{
						Location: &clustermetadata.BackupLocation_Filesystem{
							Filesystem: &clustermetadata.FilesystemBackup{
								Path: "/backups",
							},
						},
					},
					DurabilityPolicy: "ANY_2",
					Cells:            []string{"zone-a", "zone-b", "zone-c"},
				}
				opts := cmpopts.IgnoreUnexported(
					clustermetadata.Database{},
					clustermetadata.BackupLocation{},
					clustermetadata.BackupLocation_Filesystem{},
					clustermetadata.FilesystemBackup{},
				)
				if diff := cmp.Diff(wantDB, gotDB, opts); diff != "" {
					t.Errorf("Database in topology mismatch (-want +got):\n%s", diff)
				}
			},
		},
		"idempotent - database already exists": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-shard",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "postgres",
					TableGroupName: "default",
					ShardName:      "0",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:  "localhost:2379",
						RootPath: "/test",
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone-a"},
						},
					},
				},
			},
			topoSetup: func(t *testing.T, store topoclient.Store) {
				if err := store.CreateDatabase(context.Background(), "postgres", &clustermetadata.Database{
					Name: "postgres",
				}); err != nil {
					t.Fatalf("Failed to setup topology: %v", err)
				}
			},
			assertFunc: func(t *testing.T, c client.Client, shardObj *multigresv1alpha1.Shard, store topoclient.Store) {
				_, err := store.GetDatabase(context.Background(), "postgres")
				if err != nil {
					t.Errorf("Database should still be in topology: %v", err)
				}
			},
		},
		"handles deletion with finalizer": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-shard",
					Namespace:         "default",
					Finalizers:        []string{finalizerName},
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "postgres",
					TableGroupName: "default",
					ShardName:      "0",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:  "localhost:2379",
						RootPath: "/test",
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone-a"},
						},
					},
				},
			},
			topoSetup: func(t *testing.T, store topoclient.Store) {
				if err := store.CreateDatabase(context.Background(), "postgres", &clustermetadata.Database{
					Name: "postgres",
				}); err != nil {
					t.Fatalf("Failed to setup topology: %v", err)
				}
				// Verify database exists before deletion
				_, err := store.GetDatabase(context.Background(), "postgres")
				if err != nil {
					t.Fatalf("Database should exist after creation: %v", err)
				}
			},
			assertFunc: func(t *testing.T, c client.Client, shardObj *multigresv1alpha1.Shard, store topoclient.Store) {
				_, err := store.GetDatabase(context.Background(), "postgres")
				if err == nil {
					t.Error("Database should be removed from topology")
				}
			},
		},
		"handles deletion when database not in topology": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-shard",
					Namespace:         "default",
					Finalizers:        []string{finalizerName},
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "postgres",
					TableGroupName: "default",
					ShardName:      "0",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:  "localhost:2379",
						RootPath: "/test",
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone-a"},
						},
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, shardObj *multigresv1alpha1.Shard, store topoclient.Store) {
				_, err := store.GetDatabase(context.Background(), "postgres")
				if err == nil {
					t.Error("Database should not be in topology")
				}
			},
		},
		"handles deletion with other finalizers": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-shard",
					Namespace:         "default",
					Finalizers:        []string{"other-finalizer"},
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "postgres",
					TableGroupName: "default",
					ShardName:      "0",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:  "localhost:2379",
						RootPath: "/test",
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone-a"},
						},
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, shardObj *multigresv1alpha1.Shard, store topoclient.Store) {
				updatedShard := &multigresv1alpha1.Shard{}
				if err := c.Get(context.Background(), types.NamespacedName{
					Name:      shardObj.Name,
					Namespace: shardObj.Namespace,
				}, updatedShard); err != nil {
					t.Fatalf("Failed to get Shard: %v", err)
				}
				if slices.Contains(updatedShard.Finalizers, finalizerName) {
					t.Error("Our finalizer should not be present")
				}
			},
		},
		"handles non-existent shard": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{Name: "test-shard", Namespace: "default"},
			},
			skipAddShard: true,
		},
		"error on Get shard": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{Name: "test-shard", Namespace: "default"},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName("test-shard", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error on Update when adding finalizer": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{Name: "test-shard", Namespace: "default"},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "postgres",
					TableGroupName: "default",
					ShardName:      "0",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:  "localhost:2379",
						RootPath: "/test",
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone-a"},
						},
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-shard", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error on Update when removing finalizer": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-shard",
					Namespace:         "default",
					Finalizers:        []string{finalizerName},
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "postgres",
					TableGroupName: "default",
					ShardName:      "0",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:  "localhost:2379",
						RootPath: "/test",
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone-a"},
						},
					},
				},
			},
			topoSetup: func(t *testing.T, store topoclient.Store) {
				if err := store.CreateDatabase(context.Background(), "postgres", &clustermetadata.Database{
					Name: "postgres",
				}); err != nil {
					t.Fatalf("Failed to setup topology: %v", err)
				}
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-shard", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error creating topology store during registration": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-shard",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "postgres",
					TableGroupName: "default",
					ShardName:      "0",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:  "localhost:2379",
						RootPath: "/test",
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone-a"},
						},
					},
				},
			},
			customTopoStoreFunc: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
				return nil, testutil.ErrInjected
			},
			wantErr: true,
		},
		"error creating topology store during deletion": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-shard",
					Namespace:         "default",
					Finalizers:        []string{finalizerName},
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "postgres",
					TableGroupName: "default",
					ShardName:      "0",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:  "localhost:2379",
						RootPath: "/test",
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells: []multigresv1alpha1.CellName{"zone-a"},
						},
					},
				},
			},
			customTopoStoreFunc: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
				return nil, testutil.ErrInjected
			},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store, factory := memorytopo.NewServerAndFactory(context.Background())
			// TODO: handle store.Close() error properly
			defer func() { _ = store.Close() }()

			if tc.topoSetup != nil {
				tc.topoSetup(t, store)
			}

			var objs []client.Object
			if tc.existingObjects != nil {
				objs = tc.existingObjects
			} else if !tc.skipAddShard {
				objs = append(objs, tc.shard)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				WithStatusSubresource(&multigresv1alpha1.Shard{}).
				Build()

			c := client.Client(fakeClient)
			if tc.failureConfig != nil {
				c = testutil.NewFakeClientWithFailures(fakeClient, tc.failureConfig)
			}

			reconciler := &shard.ShardReconciler{
				Client: c,
				Scheme: scheme,
			}

			// Use custom topo store function if provided, otherwise default to memory topo
			if tc.customTopoStoreFunc != nil {
				reconciler.SetCreateTopoStore(tc.customTopoStoreFunc)
			} else {
				reconciler.SetCreateTopoStore(func(shardObj *multigresv1alpha1.Shard) (topoclient.Store, error) {
					return topoclient.NewWithFactory(factory, shardObj.Spec.GlobalTopoServer.RootPath, nil, nil), nil
				})
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tc.shard.Name,
					Namespace: tc.shard.Namespace,
				},
			}

			result, err := reconciler.Reconcile(context.Background(), req)

			if (err != nil) != tc.wantErr {
				t.Errorf("Reconcile() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if tc.wantErr {
				return
			}

			if result.RequeueAfter > 0 {
				t.Errorf(
					"Reconcile() should not set RequeueAfter, controller-runtime watch will trigger re-reconciliation, got %v",
					result.RequeueAfter,
				)
			}

			if tc.assertFunc != nil {
				tc.assertFunc(t, c, tc.shard, store)
			}
		})
	}
}

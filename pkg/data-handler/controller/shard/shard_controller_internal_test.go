package shard

import (
	"context"
	"errors"
	"testing"

	"github.com/multigres/multigres/go/common/topoclient"
	_ "github.com/multigres/multigres/go/common/topoclient/etcdtopo"
	"github.com/multigres/multigres/go/common/topoclient/memorytopo"
	"github.com/multigres/multigres/go/pb/clustermetadata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// TestDefaultCreateTopoStore tests error paths in defaultCreateTopoStore
func TestDefaultCreateTopoStore(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		shard   *multigresv1alpha1.Shard
		wantErr bool
	}{
		"creates store for etcd": {
			shard: &multigresv1alpha1.Shard{
				Spec: multigresv1alpha1.ShardSpec{
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:  "localhost:2379",
						RootPath: "/test",
					},
				},
			},
			wantErr: false,
		},

	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store, err := defaultCreateTopoStore(tc.shard)
			if (err != nil) != tc.wantErr {
				t.Errorf("defaultCreateTopoStore() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if !tc.wantErr && store != nil {
				store.Close()
			}
		})
	}
}

// TestDefaultCreateTopoStore_InvalidImplementation tests the error path when OpenServer fails
func TestDefaultCreateTopoStore_InvalidImplementation(t *testing.T) {
	shard := &multigresv1alpha1.Shard{
		Spec: multigresv1alpha1.ShardSpec{
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				Address:        "localhost:2379",
				RootPath:       "/test",
				Implementation: "invalid-implementation-that-does-not-exist",
			},
		},
	}

	store, err := defaultCreateTopoStore(shard)
	if err == nil {
		if store != nil {
			store.Close()
		}
		t.Error("defaultCreateTopoStore() should error with invalid implementation")
	}
}

// TestGetTopoStoreUsesCustom tests that getTopoStore uses custom factory when set
func TestGetTopoStoreUsesCustom(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		Spec: multigresv1alpha1.ShardSpec{
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				Address:  "localhost:2379",
				RootPath: "/test",
			},
		},
	}

	reconciler := &ShardReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme: scheme,
	}

	customCalled := false
	_, factory := memorytopo.NewServerAndFactory(context.Background())
	reconciler.SetCreateTopoStore(func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
		customCalled = true
		return topoclient.NewWithFactory(factory, "/test", nil, nil), nil
	})

	store, err := reconciler.getTopoStore(shard)
	if err != nil {
		t.Fatalf("getTopoStore() error = %v", err)
	}
	defer store.Close()

	if !customCalled {
		t.Error("Custom factory should have been called")
	}
}

// TestGetTopoStoreUsesDefault tests that getTopoStore falls back to default
func TestGetTopoStoreUsesDefault(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		Spec: multigresv1alpha1.ShardSpec{
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				Address:  "localhost:2379",
				RootPath: "/test",
			},
		},
	}

	reconciler := &ShardReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme: scheme,
	}

	store, err := reconciler.getTopoStore(shard)
	if err != nil {
		t.Fatalf("getTopoStore() error = %v", err)
	}
	if store != nil {
		defer store.Close()
	}
}

// TestUnregisterDatabaseSuccessPath tests successful deletion to ensure logger line is hit
func TestUnregisterDatabaseSuccessPath(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
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
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		Build()

	reconciler := &ShardReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// Setup memory topo with database
	_, factory := memorytopo.NewServerAndFactory(context.Background())

	// Create database in topology
	setupStore := topoclient.NewWithFactory(factory, "/test", nil, nil)
	err := setupStore.CreateDatabase(context.Background(), "postgres", &clustermetadata.Database{
		Name: "postgres",
	})
	setupStore.Close()
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Override factory
	reconciler.SetCreateTopoStore(func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
		return topoclient.NewWithFactory(factory, "/test", nil, nil), nil
	})

	// Call unregisterDatabaseFromTopology directly
	err = reconciler.unregisterDatabaseFromTopology(context.Background(), shard)
	if err != nil {
		t.Errorf("unregisterDatabaseFromTopology() error = %v", err)
	}

	// Verify database was deleted
	verifyStore := topoclient.NewWithFactory(factory, "/test", nil, nil)
	_, err = verifyStore.GetDatabase(context.Background(), "postgres")
	verifyStore.Close()
	if err == nil {
		t.Error("Database should be deleted from topology")
	}
}

// TestSetupWithManager tests the manager setup function.
func TestSetupWithManager(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

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
			Client: mgr.GetClient(),
			Scheme: scheme,
		}
		if err := r.SetupWithManager(mgr); err != nil {
			t.Errorf("SetupWithManager() error = %v", err)
		}
	})

	t.Run("with options", func(t *testing.T) {
		mgr := createMgr()
		r := &ShardReconciler{
			Client: mgr.GetClient(),
			Scheme: scheme,
		}
		if err := r.SetupWithManager(mgr, controller.Options{
			MaxConcurrentReconciles: 1,
			SkipNameValidation:      ptr.To(true),
		}); err != nil {
			t.Errorf("SetupWithManager() with opts error = %v", err)
		}
	})
}

// errorTopoStore is a mock topology store that returns errors for testing
type errorTopoStore struct {
	topoclient.Store
	createDBError error
	deleteDBError error
}

func (s *errorTopoStore) CreateDatabase(ctx context.Context, database string, db *clustermetadata.Database) error {
	if s.createDBError != nil {
		return s.createDBError
	}
	return nil
}

func (s *errorTopoStore) DeleteDatabase(ctx context.Context, database string, force bool) error {
	if s.deleteDBError != nil {
		return s.deleteDBError
	}
	return nil
}

func (s *errorTopoStore) Close() error {
	return nil
}

// TestReconcile_ErrorRegisteringDatabase tests error path when CreateDatabase fails with non-NodeExists error
func TestReconcile_ErrorRegisteringDatabase(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-shard",
			Namespace:  "default",
			Finalizers: []string{"shard.data-handler.multigres.com/finalizer"},
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

	// Set custom factory that returns a store that fails CreateDatabase
	reconciler.SetCreateTopoStore(func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
		return &errorTopoStore{createDBError: errors.New("database creation failed")}, nil
	})

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      shard.Name,
			Namespace: shard.Namespace,
		},
	}

	_, err := reconciler.Reconcile(context.Background(), req)
	if err == nil {
		t.Error("Reconcile() should error when database creation fails")
	}
}

// TestReconcile_ErrorDeletingDatabase tests error path when DeleteDatabase fails with non-NoNode error
func TestReconcile_ErrorDeletingDatabase(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-shard",
			Namespace:         "default",
			Finalizers:        []string{"shard.data-handler.multigres.com/finalizer"},
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

	// Set custom factory that returns a store that fails DeleteDatabase
	reconciler.SetCreateTopoStore(func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
		return &errorTopoStore{deleteDBError: errors.New("database deletion failed")}, nil
	})

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      shard.Name,
			Namespace: shard.Namespace,
		},
	}

	_, err := reconciler.Reconcile(context.Background(), req)
	if err == nil {
		t.Error("Reconcile() should error when database deletion fails")
	}
}


package shard

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/multigres/multigres/go/common/topoclient"
	_ "github.com/multigres/multigres/go/common/topoclient/etcdtopo"
	"github.com/multigres/multigres/go/common/topoclient/memorytopo"
	"github.com/multigres/multigres/go/pb/clustermetadata"
	consensusdatapb "github.com/multigres/multigres/go/pb/consensusdata"
	"github.com/multigres/multigres/go/pb/multipoolermanagerdata"
	multipoolermanagerdatapb "github.com/multigres/multigres/go/pb/multipoolermanagerdata"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/data-handler/backuphealth"
	"github.com/numtide/multigres-operator/pkg/data-handler/drain"
	"github.com/numtide/multigres-operator/pkg/testutil"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
	"github.com/numtide/multigres-operator/pkg/util/status"
)

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
	// TODO: handle store.Close() error properly
	defer func() { _ = store.Close() }()

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
		// TODO: handle store.Close() error properly
		defer func() { _ = store.Close() }()
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
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
	}

	// Setup memory topo with database
	_, factory := memorytopo.NewServerAndFactory(context.Background())

	// Create database in topology
	setupStore := topoclient.NewWithFactory(factory, "/test", nil, nil)
	err := setupStore.CreateDatabase(context.Background(), "postgres", &clustermetadata.Database{
		Name: "postgres",
	})
	_ = setupStore.Close() // TODO: handle setupStore.Close() error properly
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
	_ = verifyStore.Close() // TODO: handle verifyStore.Close() error properly
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

func (s *errorTopoStore) CreateDatabase(
	ctx context.Context,
	database string,
	db *clustermetadata.Database,
) error {
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
			Name:              "test-shard",
			Namespace:         "default",
			Finalizers:        []string{"multigres.com/shard-data-protection"},
			CreationTimestamp: metav1.Time{Time: time.Now().Add(-10 * time.Minute)},
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
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
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
			Finalizers:        []string{"multigres.com/shard-data-protection"},
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
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
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

func TestIsConditionTrue(t *testing.T) {
	t.Parallel()

	conditions := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue},
		{Type: "BackupHealthy", Status: metav1.ConditionFalse},
	}

	tests := map[string]struct {
		condType string
		want     bool
	}{
		"true condition":    {condType: "Ready", want: true},
		"false condition":   {condType: "BackupHealthy", want: false},
		"missing condition": {condType: "Missing", want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := status.IsConditionTrue(conditions, tc.condType); got != tc.want {
				t.Errorf("IsConditionTrue(%q) = %v, want %v", tc.condType, got, tc.want)
			}
		})
	}
}

func TestSetCondition(t *testing.T) {
	t.Parallel()

	t.Run("adds new condition", func(t *testing.T) {
		t.Parallel()
		var conditions []metav1.Condition
		cond := metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionTrue,
			Reason:  "AllGood",
			Message: "everything is fine",
		}
		status.SetCondition(&conditions, cond)
		if len(conditions) != 1 {
			t.Fatalf("expected 1 condition, got %d", len(conditions))
		}
		if conditions[0].Status != metav1.ConditionTrue {
			t.Errorf("expected True, got %s", conditions[0].Status)
		}
	})

	t.Run("updates existing condition with different status", func(t *testing.T) {
		t.Parallel()
		originalTime := metav1.Now()
		conditions := []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "AllGood",
				LastTransitionTime: originalTime,
			},
		}
		newCond := metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "NotReady",
			LastTransitionTime: metav1.Now(),
		}
		status.SetCondition(&conditions, newCond)
		if len(conditions) != 1 {
			t.Fatalf("expected 1 condition, got %d", len(conditions))
		}
		if conditions[0].Status != metav1.ConditionFalse {
			t.Errorf("expected False, got %s", conditions[0].Status)
		}
		if conditions[0].Reason != "NotReady" {
			t.Errorf("expected reason NotReady, got %s", conditions[0].Reason)
		}
	})

	t.Run(
		"updates existing condition with same status preserves transition time",
		func(t *testing.T) {
			t.Parallel()
			originalTime := metav1.Now()
			conditions := []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "AllGood",
					LastTransitionTime: originalTime,
				},
			}
			newCond := metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionTrue,
				Reason:  "StillGood",
				Message: "updated message",
			}
			status.SetCondition(&conditions, newCond)
			if len(conditions) != 1 {
				t.Fatalf("expected 1 condition, got %d", len(conditions))
			}
			if conditions[0].Reason != "StillGood" {
				t.Errorf("expected reason StillGood, got %s", conditions[0].Reason)
			}
			if !conditions[0].LastTransitionTime.Equal(&originalTime) {
				t.Error("expected LastTransitionTime to be preserved when status unchanged")
			}
		},
	)
}

func TestSetRPCClient(t *testing.T) {
	t.Parallel()
	r := &ShardReconciler{}
	mock := &mockRPCClient{}
	r.SetRPCClient(mock)
	if r.rpcClient != mock {
		t.Error("expected rpcClient to be set")
	}
}

func TestPodToShard(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	r := &ShardReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme: scheme,
	}

	t.Run("returns request for shard owner", func(t *testing.T) {
		t.Parallel()
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "multigres.com/v1alpha1",
						Kind:       "Shard",
						Name:       "my-shard",
					},
				},
			},
		}
		requests := r.podToShard(context.Background(), pod)
		if len(requests) != 1 {
			t.Fatalf("expected 1 request, got %d", len(requests))
		}
		if requests[0].Name != "my-shard" {
			t.Errorf("expected name my-shard, got %s", requests[0].Name)
		}
		if requests[0].Namespace != "default" {
			t.Errorf("expected namespace default, got %s", requests[0].Namespace)
		}
	})

	t.Run("returns nil for non-shard owner", func(t *testing.T) {
		t.Parallel()
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "apps/v1",
						Kind:       "StatefulSet",
						Name:       "my-sts",
					},
				},
			},
		}
		requests := r.podToShard(context.Background(), pod)
		if len(requests) != 0 {
			t.Errorf("expected 0 requests, got %d", len(requests))
		}
	})

	t.Run("returns nil for no owner", func(t *testing.T) {
		t.Parallel()
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
			},
		}
		requests := r.podToShard(context.Background(), pod)
		if len(requests) != 0 {
			t.Errorf("expected 0 requests, got %d", len(requests))
		}
	})
}

func TestEvaluateBackupHealth(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	t.Run("returns nil when no primary found", func(t *testing.T) {
		t.Parallel()
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")

		shard := &multigresv1alpha1.Shard{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-shard",
				Namespace: "default",
				Labels:    map[string]string{"multigres.com/cluster": "test"},
			},
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName:   "db",
				TableGroupName: "tg",
				ShardName:      "0",
				GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
					RootPath: "/test",
				},
				Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
					"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
				},
			},
		}

		r := &ShardReconciler{
			Client:   fake.NewClientBuilder().WithScheme(scheme).Build(),
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
			createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
				return topoclient.NewWithFactory(
					factory,
					"",
					[]string{""},
					topoclient.NewDefaultTopoConfig(),
				), nil
			},
			rpcClient: &mockRPCClient{},
		}

		result, err := r.evaluateBackupHealth(context.Background(), shard)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Error("expected nil result when no primary found")
		}
	})

	t.Run("returns error when topo store fails", func(t *testing.T) {
		t.Parallel()
		shard := &multigresv1alpha1.Shard{
			Spec: multigresv1alpha1.ShardSpec{
				Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
					"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
				},
			},
		}

		r := &ShardReconciler{
			Client:   fake.NewClientBuilder().WithScheme(scheme).Build(),
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
			createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
				return nil, errors.New("topo store error")
			},
			rpcClient: &mockRPCClient{},
		}

		_, err := r.evaluateBackupHealth(context.Background(), shard)
		if err == nil {
			t.Error("expected error when topo store fails")
		}
	})

	t.Run("returns backups from primary", func(t *testing.T) {
		t.Parallel()
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
		store := topoclient.NewWithFactory(
			factory,
			"",
			[]string{""},
			topoclient.NewDefaultTopoConfig(),
		)

		ctx := context.Background()
		_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
			Id:         &clustermetadata.ID{Cell: "cell1", Name: "primary-pod"},
			Hostname:   "primary-pod",
			Type:       clustermetadata.PoolerType_PRIMARY,
			Database:   "db",
			TableGroup: "tg",
			Shard:      "0",
		}, false)
		_ = store.Close()

		recentID := time.Now().Add(-1 * time.Hour).Format("20060102-150405")
		rpc := &mockRPCClientWithBackups{
			backups: []*multipoolermanagerdata.BackupMetadata{
				{
					BackupId: recentID,
					Status:   multipoolermanagerdata.BackupMetadata_COMPLETE,
					Type:     "full",
				},
			},
		}

		shard := &multigresv1alpha1.Shard{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-shard",
				Namespace: "default",
				Labels:    map[string]string{"multigres.com/cluster": "test"},
			},
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName:   "db",
				TableGroupName: "tg",
				ShardName:      "0",
				Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
					"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
				},
			},
		}

		r := &ShardReconciler{
			Client:   fake.NewClientBuilder().WithScheme(scheme).Build(),
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
			createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
				return topoclient.NewWithFactory(
					factory,
					"",
					[]string{""},
					topoclient.NewDefaultTopoConfig(),
				), nil
			},
			rpcClient: rpc,
		}

		result, err := r.evaluateBackupHealth(ctx, shard)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if !result.Healthy {
			t.Errorf("expected healthy backup, got message: %s", result.Message)
		}
	})

	t.Run("returns error when getBackups fails", func(t *testing.T) {
		t.Parallel()
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
		store := topoclient.NewWithFactory(
			factory,
			"",
			[]string{""},
			topoclient.NewDefaultTopoConfig(),
		)

		ctx := context.Background()
		_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
			Id:         &clustermetadata.ID{Cell: "cell1", Name: "primary-pod"},
			Hostname:   "primary-pod",
			Type:       clustermetadata.PoolerType_PRIMARY,
			Database:   "db",
			TableGroup: "tg",
			Shard:      "0",
		}, false)
		_ = store.Close()

		rpc := &mockRPCClientWithBackups{
			err: errors.New("rpc error"),
		}

		shard := &multigresv1alpha1.Shard{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-shard",
				Namespace: "default",
				Labels:    map[string]string{"multigres.com/cluster": "test"},
			},
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName:   "db",
				TableGroupName: "tg",
				ShardName:      "0",
				Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
					"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
				},
			},
		}

		r := &ShardReconciler{
			Client:   fake.NewClientBuilder().WithScheme(scheme).Build(),
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
			createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
				return topoclient.NewWithFactory(
					factory,
					"",
					[]string{""},
					topoclient.NewDefaultTopoConfig(),
				), nil
			},
			rpcClient: rpc,
		}

		_, err := r.evaluateBackupHealth(ctx, shard)
		if err == nil {
			t.Error("expected error when getBackups fails")
		}
	})
}

// mockRPCClient is a no-op implementation of rpcclient.MultiPoolerClient for tests.
type mockRPCClient struct {
	promoteCalled       bool
	updateStandbyCalled bool
}

func (m *mockRPCClient) BeginTerm(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *consensusdatapb.BeginTermRequest,
) (*consensusdatapb.BeginTermResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) ConsensusStatus(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *consensusdatapb.StatusRequest,
) (*consensusdatapb.StatusResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) GetLeadershipView(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *consensusdatapb.LeadershipViewRequest,
) (*consensusdatapb.LeadershipViewResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) CanReachPrimary(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *consensusdatapb.CanReachPrimaryRequest,
) (*consensusdatapb.CanReachPrimaryResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) InitializeEmptyPrimary(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.InitializeEmptyPrimaryRequest,
) (*multipoolermanagerdatapb.InitializeEmptyPrimaryResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) State(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.StateRequest,
) (*multipoolermanagerdatapb.StateResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) WaitForLSN(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.WaitForLSNRequest,
) (*multipoolermanagerdatapb.WaitForLSNResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) SetPrimaryConnInfo(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.SetPrimaryConnInfoRequest,
) (*multipoolermanagerdatapb.SetPrimaryConnInfoResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) StartReplication(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.StartReplicationRequest,
) (*multipoolermanagerdatapb.StartReplicationResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) StopReplication(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.StopReplicationRequest,
) (*multipoolermanagerdatapb.StopReplicationResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) StandbyReplicationStatus(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.StandbyReplicationStatusRequest,
) (*multipoolermanagerdatapb.StandbyReplicationStatusResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) Status(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.StatusRequest,
) (*multipoolermanagerdatapb.StatusResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) ResetReplication(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.ResetReplicationRequest,
) (*multipoolermanagerdatapb.ResetReplicationResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) StopReplicationAndGetStatus(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.StopReplicationAndGetStatusRequest,
) (*multipoolermanagerdatapb.StopReplicationAndGetStatusResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) ConfigureSynchronousReplication(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.ConfigureSynchronousReplicationRequest,
) (*multipoolermanagerdatapb.ConfigureSynchronousReplicationResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) PrimaryStatus(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.PrimaryStatusRequest,
) (*multipoolermanagerdatapb.PrimaryStatusResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) PrimaryPosition(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.PrimaryPositionRequest,
) (*multipoolermanagerdatapb.PrimaryPositionResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) GetFollowers(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.GetFollowersRequest,
) (*multipoolermanagerdatapb.GetFollowersResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) EmergencyDemote(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.EmergencyDemoteRequest,
) (*multipoolermanagerdatapb.EmergencyDemoteResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) UndoDemote(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.UndoDemoteRequest,
) (*multipoolermanagerdatapb.UndoDemoteResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) DemoteStalePrimary(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.DemoteStalePrimaryRequest,
) (*multipoolermanagerdatapb.DemoteStalePrimaryResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) ChangeType(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.ChangeTypeRequest,
) (*multipoolermanagerdatapb.ChangeTypeResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) GetDurabilityPolicy(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.GetDurabilityPolicyRequest,
) (*multipoolermanagerdatapb.GetDurabilityPolicyResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) CreateDurabilityPolicy(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.CreateDurabilityPolicyRequest,
) (*multipoolermanagerdatapb.CreateDurabilityPolicyResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) Backup(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.BackupRequest,
) (*multipoolermanagerdatapb.BackupResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) RestoreFromBackup(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.RestoreFromBackupRequest,
) (*multipoolermanagerdatapb.RestoreFromBackupResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) GetBackups(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.GetBackupsRequest,
) (*multipoolermanagerdatapb.GetBackupsResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) GetBackupByJobId(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.GetBackupByJobIdRequest,
) (*multipoolermanagerdatapb.GetBackupByJobIdResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) RewindToSource(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.RewindToSourceRequest,
) (*multipoolermanagerdatapb.RewindToSourceResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) SetMonitor(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.SetMonitorRequest,
) (*multipoolermanagerdatapb.SetMonitorResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) Close()                                          {}
func (m *mockRPCClient) CloseTablet(pooler *clustermetadata.MultiPooler) {}

func (m *mockRPCClient) Promote(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.PromoteRequest,
) (*multipoolermanagerdatapb.PromoteResponse, error) {
	m.promoteCalled = true
	return &multipoolermanagerdatapb.PromoteResponse{}, nil
}

func (m *mockRPCClient) UpdateSynchronousStandbyList(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.UpdateSynchronousStandbyListRequest,
) (*multipoolermanagerdatapb.UpdateSynchronousStandbyListResponse, error) {
	m.updateStandbyCalled = true
	return &multipoolermanagerdatapb.UpdateSynchronousStandbyListResponse{}, nil
}

// mockRPCClientWithBackups extends mockRPCClient but overrides GetBackups to return custom data.
type mockRPCClientWithBackups struct {
	mockRPCClient
	backups []*multipoolermanagerdata.BackupMetadata
	err     error
}

func (m *mockRPCClientWithBackups) GetBackups(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdata.GetBackupsRequest,
) (*multipoolermanagerdata.GetBackupsResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &multipoolermanagerdata.GetBackupsResponse{Backups: m.backups}, nil
}

func TestReconcile_PodRolesAndDrainIntegration(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-shard",
			Namespace:  "default",
			Finalizers: []string{finalizerName},
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				RootPath: "/test",
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	// Create pods that belong to this shard
	primaryPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "primary-pod",
			Namespace: "default",
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
				metadata.LabelMultigresShard:   "0",
				metadata.LabelMultigresCell:    "cell1",
			},
		},
	}

	replicaPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "replica-pod",
			Namespace: "default",
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
				metadata.LabelMultigresShard:   "0",
				metadata.LabelMultigresCell:    "cell1",
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard, primaryPod, replicaPod).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
	store := topoclient.NewWithFactory(factory, "", []string{""}, topoclient.NewDefaultTopoConfig())

	ctx := context.Background()
	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:         &clustermetadata.ID{Cell: "cell1", Name: "primary-pod"},
		Hostname:   "primary-pod",
		Type:       clustermetadata.PoolerType_PRIMARY,
		Database:   "db",
		TableGroup: "tg",
		Shard:      "0",
	}, false)
	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:         &clustermetadata.ID{Cell: "cell1", Name: "replica-pod"},
		Hostname:   "replica-pod",
		Type:       clustermetadata.PoolerType_REPLICA,
		Database:   "db",
		TableGroup: "tg",
		Shard:      "0",
	}, false)
	_ = store.Close()

	r := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
			return topoclient.NewWithFactory(
				factory,
				"",
				[]string{""},
				topoclient.NewDefaultTopoConfig(),
			), nil
		},
	}

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-shard", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("unexpected requeue: %v", result.RequeueAfter)
	}

	// Verify pod roles were updated
	updatedShard := &multigresv1alpha1.Shard{}
	_ = c.Get(ctx, types.NamespacedName{Name: "test-shard", Namespace: "default"}, updatedShard)
	if updatedShard.Status.PodRoles == nil {
		t.Fatal("expected PodRoles to be set")
	}
	if updatedShard.Status.PodRoles["primary-pod"] != "PRIMARY" {
		t.Errorf(
			"expected primary-pod=PRIMARY, got %s",
			updatedShard.Status.PodRoles["primary-pod"],
		)
	}
	if updatedShard.Status.PodRoles["replica-pod"] != "REPLICA" {
		t.Errorf(
			"expected replica-pod=REPLICA, got %s",
			updatedShard.Status.PodRoles["replica-pod"],
		)
	}
}

func TestReconcile_BackupHealth(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-shard",
			Namespace:  "default",
			Finalizers: []string{finalizerName},
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				RootPath: "/test",
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
	store := topoclient.NewWithFactory(factory, "", []string{""}, topoclient.NewDefaultTopoConfig())

	ctx := context.Background()
	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:         &clustermetadata.ID{Cell: "cell1", Name: "primary-pod"},
		Hostname:   "primary-pod",
		Type:       clustermetadata.PoolerType_PRIMARY,
		Database:   "db",
		TableGroup: "tg",
		Shard:      "0",
	}, false)
	_ = store.Close()

	recentID := time.Now().Add(-1 * time.Hour).Format("20060102-150405")
	rpc := &mockRPCClientWithBackups{
		backups: []*multipoolermanagerdata.BackupMetadata{
			{
				BackupId: recentID,
				Status:   multipoolermanagerdata.BackupMetadata_COMPLETE,
				Type:     "full",
			},
		},
	}

	r := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
			return topoclient.NewWithFactory(
				factory,
				"",
				[]string{""},
				topoclient.NewDefaultTopoConfig(),
			), nil
		},
		rpcClient: rpc,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-shard", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("unexpected requeue: %v", result.RequeueAfter)
	}

	updatedShard := &multigresv1alpha1.Shard{}
	_ = c.Get(ctx, types.NamespacedName{Name: "test-shard", Namespace: "default"}, updatedShard)
	if updatedShard.Status.LastBackupType != "full" {
		t.Errorf("expected LastBackupType=full, got %s", updatedShard.Status.LastBackupType)
	}
	if updatedShard.Status.LastBackupTime == nil {
		t.Error("expected LastBackupTime to be set")
	}
}

func TestReconcile_BackupHealthError(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-shard",
			Namespace:  "default",
			Finalizers: []string{finalizerName},
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				RootPath: "/test",
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
	store := topoclient.NewWithFactory(factory, "", []string{""}, topoclient.NewDefaultTopoConfig())

	ctx := context.Background()
	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:         &clustermetadata.ID{Cell: "cell1", Name: "primary-pod"},
		Hostname:   "primary-pod",
		Type:       clustermetadata.PoolerType_PRIMARY,
		Database:   "db",
		TableGroup: "tg",
		Shard:      "0",
	}, false)
	_ = store.Close()

	rpc := &mockRPCClientWithBackups{
		err: errors.New("backup check failed"),
	}

	r := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
			return topoclient.NewWithFactory(
				factory,
				"",
				[]string{""},
				topoclient.NewDefaultTopoConfig(),
			), nil
		},
		rpcClient: rpc,
	}

	// Backup health error should NOT fail reconcile
	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-shard", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("reconcile should not fail on backup health error, got: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("unexpected requeue: %v", result.RequeueAfter)
	}
}

func TestReconcile_TopoStoreFailureDuringDrain(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-shard",
			Namespace:  "default",
			Finalizers: []string{finalizerName},
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				RootPath: "/test",
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	// First call to getTopoStore (for registerDatabaseInTopology) succeeds
	// but the second call (for drain) uses the same factory which will succeed.
	// We test the error path where getTopoStore fails during the drain section.
	callCount := 0
	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
	r := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
			callCount++
			if callCount >= 2 {
				return nil, errors.New("topo store error on drain")
			}
			return topoclient.NewWithFactory(
				factory,
				"",
				[]string{""},
				topoclient.NewDefaultTopoConfig(),
			), nil
		},
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-shard", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should requeue after 5s when topo store fails during drain
	if result.RequeueAfter != 5*time.Second {
		t.Errorf("expected RequeueAfter=5s, got %v", result.RequeueAfter)
	}
}

func TestReconcile_ConflictOnFinalizerAdd(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				RootPath: "/test",
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	conflictErr := apierrors.NewConflict(
		multigresv1alpha1.GroupVersion.WithResource("shards").GroupResource(),
		"test-shard",
		errors.New("conflict"),
	)

	c := testutil.NewFakeClientWithFailures(fakeClient, &testutil.FailureConfig{
		OnUpdate: testutil.FailOnObjectName("test-shard", conflictErr),
	})

	r := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-shard", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("expected no error on conflict, got: %v", err)
	}
	if result.Requeue == false {
		t.Error("expected Requeue=true on conflict")
	}
}

func TestReconcile_ConflictOnFinalizerRemove(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-shard",
			Namespace:         "default",
			Finalizers:        []string{finalizerName},
			DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				RootPath: "/test",
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	conflictErr := apierrors.NewConflict(
		multigresv1alpha1.GroupVersion.WithResource("shards").GroupResource(),
		"test-shard",
		errors.New("conflict"),
	)

	// First update (from handleDeletion finalizer removal) should fail with conflict
	c := testutil.NewFakeClientWithFailures(fakeClient, &testutil.FailureConfig{
		OnUpdate: testutil.FailOnObjectName("test-shard", conflictErr),
	})

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")

	r := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
			return topoclient.NewWithFactory(
				factory,
				"",
				[]string{""},
				topoclient.NewDefaultTopoConfig(),
			), nil
		},
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-shard", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("expected no error on conflict, got: %v", err)
	}
	if result.Requeue == false {
		t.Error("expected Requeue=true on conflict")
	}
}

func TestReconcile_DrainRequeue(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-shard",
			Namespace:  "default",
			Finalizers: []string{finalizerName},
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				RootPath: "/test",
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	// Pod with drain state should trigger drain state machine
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "draining-pod",
			Namespace: "default",
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
				metadata.LabelMultigresShard:   "0",
				metadata.LabelMultigresCell:    "cell1",
			},
			Annotations: map[string]string{
				metadata.AnnotationDrainState: metadata.DrainStateRequested,
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard, pod).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
	store := topoclient.NewWithFactory(factory, "", []string{""}, topoclient.NewDefaultTopoConfig())

	ctx := context.Background()
	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:         &clustermetadata.ID{Cell: "cell1", Name: "draining-pod"},
		Hostname:   "draining-pod",
		Type:       clustermetadata.PoolerType_REPLICA,
		Database:   "db",
		TableGroup: "tg",
		Shard:      "0",
	}, false)
	_ = store.Close()

	r := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
			return topoclient.NewWithFactory(
				factory,
				"",
				[]string{""},
				topoclient.NewDefaultTopoConfig(),
			), nil
		},
	}

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-shard", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 2*time.Second {
		t.Errorf("expected RequeueAfter=2s for drain requeue, got %v", result.RequeueAfter)
	}
}

func TestReconcile_ListPodsError(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-shard",
			Namespace:  "default",
			Finalizers: []string{finalizerName},
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				RootPath: "/test",
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	c := testutil.NewFakeClientWithFailures(fakeClient, &testutil.FailureConfig{
		OnList: func(list client.ObjectList) error {
			if _, ok := list.(*corev1.PodList); ok {
				return testutil.ErrInjected
			}
			return nil
		},
	})

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")

	r := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
			return topoclient.NewWithFactory(
				factory,
				"",
				[]string{""},
				topoclient.NewDefaultTopoConfig(),
			), nil
		},
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-shard", Namespace: "default"},
	})
	if err == nil {
		t.Error("expected error when listing pods fails")
	}
}

func TestReconcile_BackupStaleTransition(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-shard",
			Namespace:  "default",
			Finalizers: []string{finalizerName},
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				RootPath: "/test",
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
		Status: multigresv1alpha1.ShardStatus{
			Conditions: []metav1.Condition{
				{
					Type:   backuphealth.ConditionBackupHealthy,
					Status: metav1.ConditionTrue,
					Reason: "BackupRecent",
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
	store := topoclient.NewWithFactory(factory, "", []string{""}, topoclient.NewDefaultTopoConfig())

	ctx := context.Background()
	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:         &clustermetadata.ID{Cell: "cell1", Name: "primary-pod"},
		Hostname:   "primary-pod",
		Type:       clustermetadata.PoolerType_PRIMARY,
		Database:   "db",
		TableGroup: "tg",
		Shard:      "0",
	}, false)
	_ = store.Close()

	// Return a stale backup (48h old)
	staleID := time.Now().Add(-48 * time.Hour).Format("20060102-150405")
	rpc := &mockRPCClientWithBackups{
		backups: []*multipoolermanagerdata.BackupMetadata{
			{
				BackupId: staleID,
				Status:   multipoolermanagerdata.BackupMetadata_COMPLETE,
				Type:     "full",
			},
		},
	}

	r := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
			return topoclient.NewWithFactory(
				factory,
				"",
				[]string{""},
				topoclient.NewDefaultTopoConfig(),
			), nil
		},
		rpcClient: rpc,
	}

	_, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-shard", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updatedShard := &multigresv1alpha1.Shard{}
	_ = c.Get(ctx, types.NamespacedName{Name: "test-shard", Namespace: "default"}, updatedShard)
	if status.IsConditionTrue(updatedShard.Status.Conditions, backuphealth.ConditionBackupHealthy) {
		t.Error("expected backup condition to be False for stale backup")
	}
}

func TestReconcile_DRAINEDRoleInPodRoles(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-shard",
			Namespace:  "default",
			Finalizers: []string{finalizerName},
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				RootPath: "/test",
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
	store := topoclient.NewWithFactory(factory, "", []string{""}, topoclient.NewDefaultTopoConfig())

	ctx := context.Background()
	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:         &clustermetadata.ID{Cell: "cell1", Name: "drained-pod"},
		Hostname:   "drained-pod",
		Type:       clustermetadata.PoolerType_DRAINED,
		Database:   "db",
		TableGroup: "tg",
		Shard:      "0",
	}, false)
	_ = store.Close()

	r := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
			return topoclient.NewWithFactory(
				factory,
				"",
				[]string{""},
				topoclient.NewDefaultTopoConfig(),
			), nil
		},
	}

	_, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-shard", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updatedShard := &multigresv1alpha1.Shard{}
	_ = c.Get(ctx, types.NamespacedName{Name: "test-shard", Namespace: "default"}, updatedShard)
	if updatedShard.Status.PodRoles["drained-pod"] != "DRAINED" {
		t.Errorf(
			"expected drained-pod=DRAINED, got %s",
			updatedShard.Status.PodRoles["drained-pod"],
		)
	}
}

func TestReconcile_PodRolesPruning(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-shard",
			Namespace:  "default",
			Finalizers: []string{finalizerName},
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				RootPath: "/test",
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
		Status: multigresv1alpha1.ShardStatus{
			PodRoles: map[string]string{
				"stale-pod": "REPLICA",
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")

	r := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
			return topoclient.NewWithFactory(
				factory,
				"",
				[]string{""},
				topoclient.NewDefaultTopoConfig(),
			), nil
		},
	}

	ctx := context.Background()
	_, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-shard", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updatedShard := &multigresv1alpha1.Shard{}
	_ = c.Get(ctx, types.NamespacedName{Name: "test-shard", Namespace: "default"}, updatedShard)
	if _, exists := updatedShard.Status.PodRoles["stale-pod"]; exists {
		t.Error("expected stale-pod to be pruned from PodRoles")
	}
}

func TestReconcile_EmptyHostnameFallback(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-shard",
			Namespace:  "default",
			Finalizers: []string{finalizerName},
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				RootPath: "/test",
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
	store := topoclient.NewWithFactory(factory, "", []string{""}, topoclient.NewDefaultTopoConfig())

	ctx := context.Background()
	// Register a pooler with NO hostname — forces the empty-hostname fallback
	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:         &clustermetadata.ID{Cell: "cell1", Name: "pod-no-host"},
		Type:       clustermetadata.PoolerType_REPLICA,
		Database:   "db",
		TableGroup: "tg",
		Shard:      "0",
	}, false)
	_ = store.Close()

	r := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
			return topoclient.NewWithFactory(
				factory,
				"",
				[]string{""},
				topoclient.NewDefaultTopoConfig(),
			), nil
		},
	}

	_, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-shard", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updatedShard := &multigresv1alpha1.Shard{}
	_ = c.Get(ctx, types.NamespacedName{Name: "test-shard", Namespace: "default"}, updatedShard)
	if len(updatedShard.Status.PodRoles) == 0 {
		t.Error("expected PodRoles to have an entry for the empty-hostname fallback")
	}
}

func TestReconcile_DrainErrorContinues(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-shard",
			Namespace:  "default",
			Finalizers: []string{finalizerName},
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				RootPath: "/test",
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	// Pod with drain state that will cause an error (non-UNAVAILABLE topo error)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "draining-pod",
			Namespace: "default",
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
				metadata.LabelMultigresShard:   "0",
				metadata.LabelMultigresCell:    "nonexistent-cell",
			},
			Annotations: map[string]string{
				metadata.AnnotationDrainState: metadata.DrainStateRequested,
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard, pod).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")

	r := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
			return topoclient.NewWithFactory(
				factory,
				"",
				[]string{""},
				topoclient.NewDefaultTopoConfig(),
			), nil
		},
	}

	// The drain error should be logged but not fail the reconcile
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-shard", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReconcile_UpdateDatabaseFieldsError(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-shard",
			Namespace:         "default",
			Finalizers:        []string{finalizerName},
			CreationTimestamp: metav1.Time{Time: time.Now().Add(-10 * time.Minute)},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				RootPath: "/test",
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	// Use errorTopoStoreWithUpdateFail that returns NodeExists on create but fails on UpdateDatabaseFields
	r := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
			return &errorTopoStoreWithUpdateFail{}, nil
		},
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-shard", Namespace: "default"},
	})
	if err == nil {
		t.Error("expected error when UpdateDatabaseFields fails")
	}
}

// errorTopoStoreWithUpdateFail returns NodeExists on CreateDatabase
// and errors on UpdateDatabaseFields.
type errorTopoStoreWithUpdateFail struct {
	topoclient.Store
}

func (s *errorTopoStoreWithUpdateFail) CreateDatabase(
	ctx context.Context,
	database string,
	db *clustermetadata.Database,
) error {
	return topoclient.TopoError{Code: topoclient.NodeExists, Message: "already exists"}
}

func (s *errorTopoStoreWithUpdateFail) UpdateDatabaseFields(
	ctx context.Context,
	database string,
	update func(*clustermetadata.Database) error,
) error {
	return errors.New("update failed")
}

func (s *errorTopoStoreWithUpdateFail) Close() error { return nil }

func TestReconcile_TopoQueryFailurePreventsRolePruning(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-shard",
			Namespace:  "default",
			Finalizers: []string{finalizerName},
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				RootPath: "/test",
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				// Use a nonexistent cell so GetMultiPoolersByCell fails
				"pool": {Cells: []multigresv1alpha1.CellName{"nonexistent-cell"}},
			},
		},
		Status: multigresv1alpha1.ShardStatus{
			PodRoles: map[string]string{
				"old-pod": "REPLICA",
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")

	r := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
			return topoclient.NewWithFactory(
				factory,
				"",
				[]string{""},
				topoclient.NewDefaultTopoConfig(),
			), nil
		},
	}

	ctx := context.Background()
	_, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-shard", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// When topo query fails, old entries should NOT be pruned
	updatedShard := &multigresv1alpha1.Shard{}
	_ = c.Get(ctx, types.NamespacedName{Name: "test-shard", Namespace: "default"}, updatedShard)
	if _, exists := updatedShard.Status.PodRoles["old-pod"]; !exists {
		t.Error("expected old-pod to be preserved when topo query fails")
	}
}

func TestReconcile_BackupNilResult(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-shard",
			Namespace:  "default",
			Finalizers: []string{finalizerName},
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				RootPath: "/test",
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	// No primary registered → evaluateBackupHealth returns nil
	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")

	rpc := &mockRPCClient{}

	r := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
			return topoclient.NewWithFactory(
				factory,
				"",
				[]string{""},
				topoclient.NewDefaultTopoConfig(),
			), nil
		},
		rpcClient: rpc,
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-shard", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReconcile_StatusPatchError(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-shard",
			Namespace:  "default",
			Finalizers: []string{finalizerName},
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				RootPath: "/test",
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	c := testutil.NewFakeClientWithFailures(fakeClient, &testutil.FailureConfig{
		OnStatusPatch: testutil.FailOnObjectName("test-shard", testutil.ErrInjected),
	})

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
	store := topoclient.NewWithFactory(factory, "", []string{""}, topoclient.NewDefaultTopoConfig())

	ctx := context.Background()
	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:         &clustermetadata.ID{Cell: "cell1", Name: "primary-pod"},
		Hostname:   "primary-pod",
		Type:       clustermetadata.PoolerType_PRIMARY,
		Database:   "db",
		TableGroup: "tg",
		Shard:      "0",
	}, false)
	_ = store.Close()

	recentID := time.Now().Add(-1 * time.Hour).Format("20060102-150405")
	rpc := &mockRPCClientWithBackups{
		backups: []*multipoolermanagerdata.BackupMetadata{
			{
				BackupId: recentID,
				Status:   multipoolermanagerdata.BackupMetadata_COMPLETE,
				Type:     "full",
			},
		},
	}

	r := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
			return topoclient.NewWithFactory(
				factory,
				"",
				[]string{""},
				topoclient.NewDefaultTopoConfig(),
			), nil
		},
		rpcClient: rpc,
	}

	_, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-shard", Namespace: "default"},
	})
	if err == nil {
		t.Error("expected error when status patch fails for backup health")
	}
}

// errorGetPoolersStore returns an error for GetMultiPoolersByCell.
type errorGetPoolersStore struct {
	topoclient.Store
}

func (s *errorGetPoolersStore) GetMultiPoolersByCell(
	ctx context.Context,
	cell string,
	opts *topoclient.GetMultiPoolersByCellOptions,
) ([]*topoclient.MultiPoolerInfo, error) {
	return nil, errors.New("topo error")
}

func (s *errorGetPoolersStore) Close() error { return nil }

func TestDrain_ForceUnregisterError(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shardObj := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-shard", Namespace: "default",
			Labels: map[string]string{metadata.LabelMultigresCluster: "test"},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName: "test-db", TableGroupName: "test-tg", ShardName: "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{RootPath: "/test"},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool1": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	// Pod stuck draining > 5 minutes, forces unregistration which fails
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod-0", Namespace: "default",
			Labels: map[string]string{metadata.LabelMultigresCell: "cell1"},
			Annotations: map[string]string{
				metadata.AnnotationDrainState: metadata.DrainStateRequested,
				metadata.AnnotationDrainRequestedAt: time.Now().
					Add(-10 * time.Minute).
					Format(time.RFC3339),
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(shardObj, pod).Build()

	store := &errorGetPoolersStore{}

	_, err := drain.ExecuteDrainStateMachine(
		context.Background(),
		c,
		nil,
		record.NewFakeRecorder(10),
		store,
		shardObj,
		pod,
	)
	if err == nil {
		t.Error("expected error when forceUnregister fails during stuck drain timeout")
	}
}

func TestDrain_AcknowledgedForceUnregisterError(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shardObj := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-shard", Namespace: "default",
			Labels: map[string]string{metadata.LabelMultigresCluster: "test"},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName: "test-db", TableGroupName: "test-tg", ShardName: "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{RootPath: "/test"},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool1": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-0",
			Namespace: "default",
			Labels:    map[string]string{metadata.LabelMultigresCell: "cell1"},
			Annotations: map[string]string{
				metadata.AnnotationDrainState: metadata.DrainStateAcknowledged,
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(shardObj, pod).Build()

	// Use an error store that has a pooler matching our pod but fails on unregister
	store := &errorUnregisterStore{
		podName: "test-pod-0",
	}

	_, err := drain.ExecuteDrainStateMachine(
		context.Background(),
		c,
		nil,
		record.NewFakeRecorder(10),
		store,
		shardObj,
		pod,
	)
	if err == nil {
		t.Error("expected error when forceUnregister fails during Acknowledged state")
	}
}

// errorUnregisterStore returns poolers but fails on unregistration.
type errorUnregisterStore struct {
	topoclient.Store
	podName string
}

func (s *errorUnregisterStore) GetMultiPoolersByCell(
	ctx context.Context,
	cell string,
	opts *topoclient.GetMultiPoolersByCellOptions,
) ([]*topoclient.MultiPoolerInfo, error) {
	return []*topoclient.MultiPoolerInfo{
		{
			MultiPooler: &clustermetadata.MultiPooler{
				Id:       &clustermetadata.ID{Cell: cell, Name: s.podName},
				Hostname: s.podName,
				Type:     clustermetadata.PoolerType_REPLICA,
			},
		},
	}, nil
}

func (s *errorUnregisterStore) UnregisterMultiPooler(
	ctx context.Context,
	id *clustermetadata.ID,
) error {
	return errors.New("unregister failed")
}

func (s *errorUnregisterStore) Close() error { return nil }

func TestDrain_InvalidState(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shardObj := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-shard", Namespace: "default",
			Labels: map[string]string{metadata.LabelMultigresCluster: "test"},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName: "test-db", TableGroupName: "test-tg", ShardName: "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{RootPath: "/test"},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool1": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod-0", Namespace: "default",
			Labels:      map[string]string{metadata.LabelMultigresCell: "cell1"},
			Annotations: map[string]string{metadata.AnnotationDrainState: "some-invalid-state"},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(shardObj, pod).Build()

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
	store := topoclient.NewWithFactory(factory, "", []string{""}, topoclient.NewDefaultTopoConfig())
	defer func() { _ = store.Close() }()

	requeue, err := drain.ExecuteDrainStateMachine(
		context.Background(),
		c,
		nil,
		record.NewFakeRecorder(10),
		store,
		shardObj,
		pod,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requeue {
		t.Error("expected no requeue for invalid state")
	}
}

func TestDrain_Draining_FindPrimaryError(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shardObj := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-shard", Namespace: "default",
			Labels: map[string]string{metadata.LabelMultigresCluster: "test-cluster"},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName: "test-db", TableGroupName: "test-tg", ShardName: "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{RootPath: "/test"},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool1": {Cells: []multigresv1alpha1.CellName{"cell1", "cell2"}},
			},
		},
	}

	replicaPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "replica-pod-0",
			Namespace: "default",
			Labels:    map[string]string{metadata.LabelMultigresCell: "cell1"},
			Annotations: map[string]string{
				metadata.AnnotationDrainState: metadata.DrainStateDraining,
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(shardObj, replicaPod).Build()
	rpcMock := &mockRPCClient{}

	// cell1 works (has our replica), cell2 errors with non-UNAVAILABLE
	store := &multiCellStore{
		cells: map[string][]*topoclient.MultiPoolerInfo{
			"cell1": {
				{
					MultiPooler: &clustermetadata.MultiPooler{
						Id:       &clustermetadata.ID{Cell: "cell1", Name: "replica-pod-0"},
						Hostname: "replica-pod-0",
						Type:     clustermetadata.PoolerType_REPLICA,
					},
				},
			},
		},
		errorCells: map[string]error{
			"cell2": errors.New("permission denied"),
		},
	}

	requeue, err := drain.ExecuteDrainStateMachine(
		context.Background(),
		c,
		rpcMock,
		record.NewFakeRecorder(10),
		store,
		shardObj,
		replicaPod,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !requeue {
		t.Error("expected requeue when findPrimaryPooler errors during Draining")
	}

	// State should remain Draining
	_ = c.Get(context.Background(), client.ObjectKeyFromObject(replicaPod), replicaPod)
	if replicaPod.Annotations[metadata.AnnotationDrainState] != metadata.DrainStateDraining {
		t.Errorf("expected state to remain Draining, got %s",
			replicaPod.Annotations[metadata.AnnotationDrainState])
	}
}

// multiCellStore is a mock store that returns different results per cell.
type multiCellStore struct {
	topoclient.Store
	cells      map[string][]*topoclient.MultiPoolerInfo
	errorCells map[string]error
}

func (s *multiCellStore) GetMultiPoolersByCell(
	ctx context.Context,
	cell string,
	opts *topoclient.GetMultiPoolersByCellOptions,
) ([]*topoclient.MultiPoolerInfo, error) {
	if err, ok := s.errorCells[cell]; ok {
		return nil, err
	}
	return s.cells[cell], nil
}

func (s *multiCellStore) Close() error { return nil }

func TestEvaluateBackupHealth_FindPrimaryError(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test"},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	r := &ShardReconciler{
		Client:   fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		createTopoStore: func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
			return &errorGetPoolersStore{}, nil
		},
		rpcClient: &mockRPCClient{},
	}

	_, err := r.evaluateBackupHealth(context.Background(), shard)
	if err == nil {
		t.Error("expected error when findPrimaryPooler fails")
	}
}

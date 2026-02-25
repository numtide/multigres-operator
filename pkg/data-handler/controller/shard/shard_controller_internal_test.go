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
	"github.com/multigres/multigres/go/pb/multipoolermanagerdata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
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
				_ = store.Close() // TODO: handle store.Close() error properly
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
			_ = store.Close() // TODO: handle store.Close() error properly
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

func TestIsTopoUnavailable(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err  error
		want bool
	}{
		"nil error":           {err: nil, want: false},
		"UNAVAILABLE error":   {err: errors.New("Code: UNAVAILABLE"), want: true},
		"no connection error": {err: errors.New("no connection available"), want: true},
		"unrelated error":     {err: errors.New("permission denied"), want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := isTopoUnavailable(tc.err); got != tc.want {
				t.Errorf("isTopoUnavailable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestEvaluateBackups_Healthy(t *testing.T) {
	t.Parallel()

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
	}

	recentID := time.Now().Add(-1 * time.Hour).Format("20060102-150405")
	backups := []*multipoolermanagerdata.BackupMetadata{
		{
			BackupId: recentID,
			Status:   multipoolermanagerdata.BackupMetadata_COMPLETE,
			Type:     "full",
		},
	}

	result := evaluateBackups(shard, backups)
	if !result.Healthy {
		t.Errorf("expected healthy, got message: %s", result.Message)
	}
	if result.LastBackupType != "full" {
		t.Errorf("expected type=full, got %s", result.LastBackupType)
	}
	if result.LastBackupTime == nil {
		t.Error("expected LastBackupTime to be set")
	}
}

func TestEvaluateBackups_Stale(t *testing.T) {
	t.Parallel()

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
	}

	oldID := time.Now().Add(-48 * time.Hour).Format("20060102-150405")
	backups := []*multipoolermanagerdata.BackupMetadata{
		{
			BackupId: oldID,
			Status:   multipoolermanagerdata.BackupMetadata_COMPLETE,
			Type:     "diff",
		},
	}

	result := evaluateBackups(shard, backups)
	if result.Healthy {
		t.Error("expected unhealthy for 48h-old backup")
	}
	if result.LastBackupType != "diff" {
		t.Errorf("expected type=diff, got %s", result.LastBackupType)
	}
}

func TestEvaluateBackups_NoBackups(t *testing.T) {
	t.Parallel()

	shard := &multigresv1alpha1.Shard{}
	result := evaluateBackups(shard, nil)
	if result.Healthy {
		t.Error("expected unhealthy when no backups")
	}
	if result.Message != "No backups found" {
		t.Errorf("unexpected message: %s", result.Message)
	}
}

func TestEvaluateBackups_NoCompleted(t *testing.T) {
	t.Parallel()

	shard := &multigresv1alpha1.Shard{}
	backups := []*multipoolermanagerdata.BackupMetadata{
		{
			BackupId: "20260224-120000",
			Status:   multipoolermanagerdata.BackupMetadata_INCOMPLETE,
			Type:     "full",
		},
	}

	result := evaluateBackups(shard, backups)
	if result.Healthy {
		t.Error("expected unhealthy when no completed backups")
	}
	if result.Message != "No completed backups found" {
		t.Errorf("unexpected message: %s", result.Message)
	}
}

func TestEvaluateBackups_SelectsMostRecent(t *testing.T) {
	t.Parallel()

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
	}

	recentID := time.Now().Add(-1 * time.Hour).Format("20060102-150405")
	olderID := time.Now().Add(-5 * time.Hour).Format("20060102-150405")
	backups := []*multipoolermanagerdata.BackupMetadata{
		{
			BackupId: olderID,
			Status:   multipoolermanagerdata.BackupMetadata_COMPLETE,
			Type:     "full",
		},
		{
			BackupId: recentID,
			Status:   multipoolermanagerdata.BackupMetadata_COMPLETE,
			Type:     "incr",
		},
	}

	result := evaluateBackups(shard, backups)
	if result.LastBackupType != "incr" {
		t.Errorf("expected most recent backup type=incr, got %s", result.LastBackupType)
	}
}

func TestApplyBackupHealth(t *testing.T) {
	t.Parallel()

	t.Run("sets healthy condition", func(t *testing.T) {
		t.Parallel()

		shard := &multigresv1alpha1.Shard{
			ObjectMeta: metav1.ObjectMeta{Generation: 5},
		}
		now := metav1.Now()
		result := &backupHealthResult{
			Healthy:        true,
			LastBackupTime: &now,
			LastBackupType: "full",
			Message:        "backup is healthy",
		}

		applyBackupHealth(shard, result)

		if len(shard.Status.Conditions) != 1 {
			t.Fatalf("expected 1 condition, got %d", len(shard.Status.Conditions))
		}
		c := shard.Status.Conditions[0]
		if c.Type != conditionBackupHealthy {
			t.Errorf("expected condition type %s, got %s", conditionBackupHealthy, c.Type)
		}
		if c.Status != metav1.ConditionTrue {
			t.Errorf("expected True, got %s", c.Status)
		}
		if c.Reason != "BackupRecent" {
			t.Errorf("expected reason BackupRecent, got %s", c.Reason)
		}
		if shard.Status.LastBackupType != "full" {
			t.Errorf("expected LastBackupType=full, got %s", shard.Status.LastBackupType)
		}
	})

	t.Run("sets unhealthy condition", func(t *testing.T) {
		t.Parallel()

		shard := &multigresv1alpha1.Shard{}
		result := &backupHealthResult{
			Healthy: false,
			Message: "backup is stale",
		}

		applyBackupHealth(shard, result)

		if len(shard.Status.Conditions) != 1 {
			t.Fatalf("expected 1 condition, got %d", len(shard.Status.Conditions))
		}
		c := shard.Status.Conditions[0]
		if c.Status != metav1.ConditionFalse {
			t.Errorf("expected False, got %s", c.Status)
		}
		if c.Reason != "BackupStale" {
			t.Errorf("expected reason BackupStale, got %s", c.Reason)
		}
	})

	t.Run("nil result is no-op", func(t *testing.T) {
		t.Parallel()

		shard := &multigresv1alpha1.Shard{}
		applyBackupHealth(shard, nil)
		if len(shard.Status.Conditions) != 0 {
			t.Error("expected no conditions for nil result")
		}
	})
}

func TestParseBackupTime(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  time.Time
	}{
		"valid": {
			input: "20260224-143055",
			want:  time.Date(2026, 2, 24, 14, 30, 55, 0, time.UTC),
		},
		"with suffix": {
			input: "20260224-143055F123456",
			want:  time.Date(2026, 2, 24, 14, 30, 55, 0, time.UTC),
		},
		"too short": {input: "20260224", want: time.Time{}},
		"empty":     {input: "", want: time.Time{}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := parseBackupTime(tc.input)
			if !got.Equal(tc.want) {
				t.Errorf("parseBackupTime(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
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
			if got := isConditionTrue(conditions, tc.condType); got != tc.want {
				t.Errorf("isConditionTrue(%q) = %v, want %v", tc.condType, got, tc.want)
			}
		})
	}
}

func TestCollectCells(t *testing.T) {
	t.Parallel()

	shard := &multigresv1alpha1.Shard{
		Spec: multigresv1alpha1.ShardSpec{
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"primary": {Cells: []multigresv1alpha1.CellName{"zone-a", "zone-b"}},
				"replica": {Cells: []multigresv1alpha1.CellName{"zone-b", "zone-c"}},
			},
		},
	}

	cells := collectCells(shard)
	if len(cells) != 3 {
		t.Errorf("expected 3 unique cells, got %d: %v", len(cells), cells)
	}

	cellSet := make(map[string]bool)
	for _, c := range cells {
		cellSet[c] = true
	}
	for _, want := range []string{"zone-a", "zone-b", "zone-c"} {
		if !cellSet[want] {
			t.Errorf("expected cell %q in result", want)
		}
	}
}

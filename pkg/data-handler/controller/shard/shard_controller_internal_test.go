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
	"github.com/numtide/multigres-operator/pkg/testutil"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
	"github.com/numtide/multigres-operator/pkg/util/status"
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

func TestGetBackupLocation(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	r := &ShardReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme: scheme,
	}

	t.Run("S3 backup", func(t *testing.T) {
		t.Parallel()
		shard := &multigresv1alpha1.Shard{
			Spec: multigresv1alpha1.ShardSpec{
				Backup: &multigresv1alpha1.BackupConfig{
					Type: multigresv1alpha1.BackupTypeS3,
					S3: &multigresv1alpha1.S3BackupConfig{
						Bucket:            "my-bucket",
						Region:            "us-west-2",
						Endpoint:          "https://s3.example.com",
						KeyPrefix:         "prefix/",
						UseEnvCredentials: true,
					},
				},
			},
		}
		loc := r.getBackupLocation(shard)
		s3 := loc.GetS3()
		if s3 == nil {
			t.Fatal("expected S3 backup location")
		}
		if s3.Bucket != "my-bucket" {
			t.Errorf("expected bucket my-bucket, got %s", s3.Bucket)
		}
		if s3.Region != "us-west-2" {
			t.Errorf("expected region us-west-2, got %s", s3.Region)
		}
		if s3.Endpoint != "https://s3.example.com" {
			t.Errorf("expected endpoint, got %s", s3.Endpoint)
		}
		if s3.KeyPrefix != "prefix/" {
			t.Errorf("expected key prefix, got %s", s3.KeyPrefix)
		}
		if !s3.UseEnvCredentials {
			t.Error("expected UseEnvCredentials=true")
		}
	})

	t.Run("filesystem with custom path", func(t *testing.T) {
		t.Parallel()
		shard := &multigresv1alpha1.Shard{
			Spec: multigresv1alpha1.ShardSpec{
				Backup: &multigresv1alpha1.BackupConfig{
					Type: multigresv1alpha1.BackupTypeFilesystem,
					Filesystem: &multigresv1alpha1.FilesystemBackupConfig{
						Path: "/custom/backups",
					},
				},
			},
		}
		loc := r.getBackupLocation(shard)
		fs := loc.GetFilesystem()
		if fs == nil {
			t.Fatal("expected filesystem backup location")
		}
		if fs.Path != "/custom/backups" {
			t.Errorf("expected path /custom/backups, got %s", fs.Path)
		}
	})

	t.Run("default filesystem", func(t *testing.T) {
		t.Parallel()
		shard := &multigresv1alpha1.Shard{}
		loc := r.getBackupLocation(shard)
		fs := loc.GetFilesystem()
		if fs == nil {
			t.Fatal("expected filesystem backup location")
		}
		if fs.Path != "/backups" {
			t.Errorf("expected default path /backups, got %s", fs.Path)
		}
	})
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

func TestParseBackupTime_InvalidFormat(t *testing.T) {
	t.Parallel()
	got := parseBackupTime("ABCDEFG-HIJKLMN")
	if !got.IsZero() {
		t.Errorf("expected zero time for invalid format, got %v", got)
	}
}

func TestFindPrimaryPooler(t *testing.T) {
	t.Parallel()

	t.Run("returns nil when no primary exists", func(t *testing.T) {
		t.Parallel()
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
		store := topoclient.NewWithFactory(
			factory,
			"",
			[]string{""},
			topoclient.NewDefaultTopoConfig(),
		)
		defer func() { _ = store.Close() }()

		shard := &multigresv1alpha1.Shard{
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName:   "db",
				TableGroupName: "tg",
				ShardName:      "0",
			},
		}

		primary, err := findPrimaryPooler(context.Background(), store, shard, []string{"cell1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if primary != nil {
			t.Error("expected nil primary when none registered")
		}
	})

	t.Run("returns error for non-unavailable topo errors", func(t *testing.T) {
		t.Parallel()
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell2")
		store := topoclient.NewWithFactory(
			factory,
			"",
			[]string{""},
			topoclient.NewDefaultTopoConfig(),
		)
		defer func() { _ = store.Close() }()

		shard := &multigresv1alpha1.Shard{
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName:   "db",
				TableGroupName: "tg",
				ShardName:      "0",
			},
		}

		// "nonexistent-cell" returns a NoNode error (not UNAVAILABLE), which should propagate.
		_, err := findPrimaryPooler(
			context.Background(),
			store,
			shard,
			[]string{"nonexistent-cell"},
		)
		if err == nil {
			t.Error("expected error for non-unavailable topo error")
		}
	})

	t.Run("returns primary from second cell", func(t *testing.T) {
		t.Parallel()
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1", "cell2")
		store := topoclient.NewWithFactory(
			factory,
			"",
			[]string{""},
			topoclient.NewDefaultTopoConfig(),
		)
		defer func() { _ = store.Close() }()

		ctx := context.Background()
		_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
			Id:         &clustermetadata.ID{Cell: "cell1", Name: "replica-pod"},
			Hostname:   "replica-pod",
			Type:       clustermetadata.PoolerType_REPLICA,
			Database:   "db",
			TableGroup: "tg",
			Shard:      "0",
		}, false)
		_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
			Id:         &clustermetadata.ID{Cell: "cell2", Name: "primary-pod"},
			Hostname:   "primary-pod",
			Type:       clustermetadata.PoolerType_PRIMARY,
			Database:   "db",
			TableGroup: "tg",
			Shard:      "0",
		}, false)

		shard := &multigresv1alpha1.Shard{
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName:   "db",
				TableGroupName: "tg",
				ShardName:      "0",
			},
		}

		primary, err := findPrimaryPooler(ctx, store, shard, []string{"cell1", "cell2"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if primary == nil {
			t.Fatal("expected primary to be found")
		}
		if primary.Id.Name != "primary-pod" {
			t.Errorf("expected primary-pod, got %s", primary.Id.Name)
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

func TestForceUnregister(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	t.Run("returns nil for empty cell label", func(t *testing.T) {
		t.Parallel()
		r := &ShardReconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
			Scheme: scheme,
		}
		shard := &multigresv1alpha1.Shard{}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "pod",
				Labels: map[string]string{},
			},
		}
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
		store := topoclient.NewWithFactory(
			factory,
			"",
			[]string{""},
			topoclient.NewDefaultTopoConfig(),
		)
		defer func() { _ = store.Close() }()

		err := r.forceUnregister(context.Background(), store, shard, pod)
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("skips unregistration when no matching pooler found", func(t *testing.T) {
		t.Parallel()
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
		store := topoclient.NewWithFactory(
			factory,
			"",
			[]string{""},
			topoclient.NewDefaultTopoConfig(),
		)
		defer func() { _ = store.Close() }()

		ctx := context.Background()
		_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
			Id:         &clustermetadata.ID{Cell: "cell1", Name: "other-pod"},
			Hostname:   "other-pod",
			Type:       clustermetadata.PoolerType_REPLICA,
			Database:   "db",
			TableGroup: "tg",
			Shard:      "0",
		}, false)

		r := &ShardReconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
			Scheme: scheme,
		}
		shard := &multigresv1alpha1.Shard{
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName:   "db",
				TableGroupName: "tg",
				ShardName:      "0",
			},
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "nonexistent-pod",
				Labels: map[string]string{
					metadata.LabelMultigresCell: "cell1",
				},
			},
		}

		err := r.forceUnregister(ctx, store, shard, pod)
		if err != nil {
			t.Errorf("expected nil error for missing pooler, got %v", err)
		}

		// Verify other-pod is still registered
		poolers, _ := store.GetMultiPoolersByCell(ctx, "cell1", nil)
		if len(poolers) != 1 {
			t.Errorf("expected 1 pooler remaining, got %d", len(poolers))
		}
	})
}

func TestIsPrimaryDraining(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
		},
	}

	t.Run("returns false for nil primary", func(t *testing.T) {
		t.Parallel()
		r := &ShardReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
		if r.isPrimaryDraining(context.Background(), shard, nil) {
			t.Error("expected false for nil primary")
		}
	})

	t.Run("returns false for nil primary ID", func(t *testing.T) {
		t.Parallel()
		r := &ShardReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
		primary := &clustermetadata.MultiPooler{}
		if r.isPrimaryDraining(context.Background(), shard, primary) {
			t.Error("expected false for nil primary ID")
		}
	})

	t.Run("returns false when primary pod not found", func(t *testing.T) {
		t.Parallel()
		r := &ShardReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
		primary := &clustermetadata.MultiPooler{
			Id: &clustermetadata.ID{Cell: "cell1", Name: "missing-pod"},
		}
		if r.isPrimaryDraining(context.Background(), shard, primary) {
			t.Error("expected false when pod not found")
		}
	})

	t.Run("returns false when no drain annotation", func(t *testing.T) {
		t.Parallel()
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "primary-pod",
				Namespace: "default",
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
		r := &ShardReconciler{Client: c}
		primary := &clustermetadata.MultiPooler{
			Id: &clustermetadata.ID{Cell: "cell1", Name: "primary-pod"},
		}
		if r.isPrimaryDraining(context.Background(), shard, primary) {
			t.Error("expected false when no drain annotation")
		}
	})

	t.Run("returns true when drain annotation present", func(t *testing.T) {
		t.Parallel()
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "primary-pod",
				Namespace: "default",
				Annotations: map[string]string{
					metadata.AnnotationDrainState: metadata.DrainStateDraining,
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
		r := &ShardReconciler{Client: c}
		primary := &clustermetadata.MultiPooler{
			Id: &clustermetadata.ID{Cell: "cell1", Name: "primary-pod"},
		}
		if !r.isPrimaryDraining(context.Background(), shard, primary) {
			t.Error("expected true when drain annotation present")
		}
	})
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
					Type:   conditionBackupHealthy,
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
	if status.IsConditionTrue(updatedShard.Status.Conditions, conditionBackupHealthy) {
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

func TestUpdateDrainState_NilAnnotations(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	r := &ShardReconciler{Client: c, Scheme: scheme}

	requeue, err := r.updateDrainState(context.Background(), pod, metadata.DrainStateDraining)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !requeue {
		t.Error("expected requeue")
	}
	if pod.Annotations[metadata.AnnotationDrainState] != metadata.DrainStateDraining {
		t.Errorf("expected state to be set, got %v", pod.Annotations)
	}
}

func TestForceUnregister_GetPoolersError(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	r := &ShardReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme: scheme,
	}
	shard := &multigresv1alpha1.Shard{
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
			Labels: map[string]string{
				metadata.LabelMultigresCell: "cell1",
			},
		},
	}

	store := &errorGetPoolersStore{}
	err := r.forceUnregister(context.Background(), store, shard, pod)
	if err == nil {
		t.Error("expected error when GetMultiPoolersByCell fails")
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
	r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}

	store := &errorGetPoolersStore{}

	_, err := r.executeDrainStateMachine(context.Background(), store, shardObj, pod)
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
	r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}

	// Use an error store that has a pooler matching our pod but fails on unregister
	store := &errorUnregisterStore{
		podName: "test-pod-0",
	}

	_, err := r.executeDrainStateMachine(context.Background(), store, shardObj, pod)
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
	r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
	store := topoclient.NewWithFactory(factory, "", []string{""}, topoclient.NewDefaultTopoConfig())
	defer func() { _ = store.Close() }()

	requeue, err := r.executeDrainStateMachine(context.Background(), store, shardObj, pod)
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
	r := &ShardReconciler{
		Client: c, Scheme: scheme,
		Recorder: record.NewFakeRecorder(10), rpcClient: rpcMock,
	}

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

	requeue, err := r.executeDrainStateMachine(context.Background(), store, shardObj, replicaPod)
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

func TestFindPrimaryPooler_TopoUnavailableSkip(t *testing.T) {
	t.Parallel()

	store := &multiCellStore{
		cells: map[string][]*topoclient.MultiPoolerInfo{
			"cell2": {
				{
					MultiPooler: &clustermetadata.MultiPooler{
						Id:       &clustermetadata.ID{Cell: "cell2", Name: "primary-pod"},
						Hostname: "primary-pod",
						Type:     clustermetadata.PoolerType_PRIMARY,
					},
				},
			},
		},
		errorCells: map[string]error{
			"cell1": errors.New("Code: UNAVAILABLE"),
		},
	}

	shard := &multigresv1alpha1.Shard{
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "0",
		},
	}

	primary, err := findPrimaryPooler(
		context.Background(),
		store,
		shard,
		[]string{"cell1", "cell2"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if primary == nil {
		t.Fatal("expected primary from cell2 after skipping unavailable cell1")
	}
	if primary.Id.Name != "primary-pod" {
		t.Errorf("expected primary-pod, got %s", primary.Id.Name)
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

package topo_test

import (
	"context"
	"testing"

	"github.com/multigres/multigres/go/common/topoclient"
	"github.com/multigres/multigres/go/common/topoclient/memorytopo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/data-handler/topo"
)

func newMemoryStore(t *testing.T, cells ...string) topoclient.Store {
	t.Helper()
	_, factory := memorytopo.NewServerAndFactory(context.Background(), cells...)
	store := topoclient.NewWithFactory(
		factory, "", []string{""}, topoclient.NewDefaultTopoConfig(),
	)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestRegisterDatabaseFromSpec(t *testing.T) {
	t.Parallel()

	owner := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "default"},
	}

	t.Run("creates database with filesystem backup", func(t *testing.T) {
		t.Parallel()
		store := newMemoryStore(t, "cell1")
		recorder := record.NewFakeRecorder(10)

		dbConfig := multigresv1alpha1.DatabaseConfig{
			Name: "mydb",
		}

		err := topo.RegisterDatabaseFromSpec(
			context.Background(), store, recorder, owner,
			dbConfig, []string{"cell1"}, nil, "",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		db, err := store.GetDatabase(context.Background(), "mydb")
		if err != nil {
			t.Fatalf("database not found: %v", err)
		}
		if db.DurabilityPolicy != "AT_LEAST_2" {
			t.Errorf("expected default durability AT_LEAST_2, got %s", db.DurabilityPolicy)
		}
		fs := db.BackupLocation.GetFilesystem()
		if fs == nil || fs.Path != "/backups" {
			t.Errorf("expected filesystem backup at /backups, got %v", db.BackupLocation)
		}
	})

	t.Run("creates database with S3 backup", func(t *testing.T) {
		t.Parallel()
		store := newMemoryStore(t, "cell1")
		recorder := record.NewFakeRecorder(10)

		backup := &multigresv1alpha1.BackupConfig{
			Type: multigresv1alpha1.BackupTypeS3,
			S3: &multigresv1alpha1.S3BackupConfig{
				Bucket: "my-bucket",
				Region: "us-east-1",
			},
		}

		err := topo.RegisterDatabaseFromSpec(
			context.Background(), store, recorder, owner,
			multigresv1alpha1.DatabaseConfig{Name: "s3db"},
			[]string{"cell1"}, backup, "",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		db, err := store.GetDatabase(context.Background(), "s3db")
		if err != nil {
			t.Fatalf("database not found: %v", err)
		}
		s3 := db.BackupLocation.GetS3()
		if s3 == nil || s3.Bucket != "my-bucket" {
			t.Errorf("expected S3 backup with bucket my-bucket, got %v", db.BackupLocation)
		}
	})

	t.Run("creates database with custom filesystem path", func(t *testing.T) {
		t.Parallel()
		store := newMemoryStore(t, "cell1")
		recorder := record.NewFakeRecorder(10)

		backup := &multigresv1alpha1.BackupConfig{
			Type: multigresv1alpha1.BackupTypeFilesystem,
			Filesystem: &multigresv1alpha1.FilesystemBackupConfig{
				Path: "/custom/path",
			},
		}

		err := topo.RegisterDatabaseFromSpec(
			context.Background(), store, recorder, owner,
			multigresv1alpha1.DatabaseConfig{Name: "fsdb"},
			[]string{"cell1"}, backup, "",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		db, err := store.GetDatabase(context.Background(), "fsdb")
		if err != nil {
			t.Fatalf("database not found: %v", err)
		}
		fs := db.BackupLocation.GetFilesystem()
		if fs == nil || fs.Path != "/custom/path" {
			t.Errorf("expected filesystem backup at /custom/path, got %v", db.BackupLocation)
		}
	})

	t.Run("updates existing database on re-registration", func(t *testing.T) {
		t.Parallel()
		store := newMemoryStore(t, "cell1", "cell2")
		recorder := record.NewFakeRecorder(10)
		ctx := context.Background()

		dbConfig := multigresv1alpha1.DatabaseConfig{Name: "upddb"}
		if err := topo.RegisterDatabaseFromSpec(
			ctx, store, recorder, owner, dbConfig,
			[]string{"cell1"}, nil, "",
		); err != nil {
			t.Fatalf("first registration: %v", err)
		}

		// Re-register with different cells.
		if err := topo.RegisterDatabaseFromSpec(
			ctx, store, recorder, owner, dbConfig,
			[]string{"cell1", "cell2"}, nil, "MULTI_CELL_AT_LEAST_2",
		); err != nil {
			t.Fatalf("re-registration: %v", err)
		}

		db, err := store.GetDatabase(ctx, "upddb")
		if err != nil {
			t.Fatalf("database not found: %v", err)
		}
		if len(db.Cells) != 2 {
			t.Errorf("expected 2 cells, got %d", len(db.Cells))
		}
		if db.DurabilityPolicy != "MULTI_CELL_AT_LEAST_2" {
			t.Errorf("expected MULTI_CELL_AT_LEAST_2, got %s", db.DurabilityPolicy)
		}
	})

	t.Run("uses database-level durability policy", func(t *testing.T) {
		t.Parallel()
		store := newMemoryStore(t, "cell1")
		recorder := record.NewFakeRecorder(10)

		dbConfig := multigresv1alpha1.DatabaseConfig{
			Name:             "durdb",
			DurabilityPolicy: "NONE",
		}

		err := topo.RegisterDatabaseFromSpec(
			context.Background(), store, recorder, owner,
			dbConfig, []string{"cell1"}, nil, "AT_LEAST_2",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		db, err := store.GetDatabase(context.Background(), "durdb")
		if err != nil {
			t.Fatalf("database not found: %v", err)
		}
		if db.DurabilityPolicy != "NONE" {
			t.Errorf("expected database-level policy NONE, got %s", db.DurabilityPolicy)
		}
	})
}

func TestRegisterCellFromSpec(t *testing.T) {
	t.Parallel()

	topoRef := multigresv1alpha1.GlobalTopoServerRef{
		Address:  "global:2379",
		RootPath: "/multigres/global",
	}
	owner := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "default"},
	}

	t.Run("creates new cell", func(t *testing.T) {
		t.Parallel()
		store := newMemoryStore(t, "cell1")
		recorder := record.NewFakeRecorder(10)

		cellCfg := multigresv1alpha1.CellConfig{Name: "cell1"}
		err := topo.RegisterCellFromSpec(
			context.Background(), store, recorder, owner, cellCfg, topoRef,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cell, err := store.GetCell(context.Background(), "cell1")
		if err != nil {
			t.Fatalf("cell not found: %v", err)
		}
		if cell.Name != "cell1" {
			t.Errorf("expected cell1, got %s", cell.Name)
		}
	})

	t.Run("idempotent on re-registration", func(t *testing.T) {
		t.Parallel()
		store := newMemoryStore(t, "cell1")
		recorder := record.NewFakeRecorder(10)
		ctx := context.Background()

		cellCfg := multigresv1alpha1.CellConfig{Name: "cell1"}
		if err := topo.RegisterCellFromSpec(
			ctx,
			store,
			recorder,
			owner,
			cellCfg,
			topoRef,
		); err != nil {
			t.Fatalf("first: %v", err)
		}
		if err := topo.RegisterCellFromSpec(
			ctx,
			store,
			recorder,
			owner,
			cellCfg,
			topoRef,
		); err != nil {
			t.Fatalf("second: %v", err)
		}
	})
}

func TestPruneDatabases(t *testing.T) {
	t.Parallel()

	owner := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "default"},
	}

	t.Run("removes stale database", func(t *testing.T) {
		t.Parallel()
		store := newMemoryStore(t, "cell1")
		recorder := record.NewFakeRecorder(10)
		ctx := context.Background()

		// Register two databases.
		for _, name := range []string{"db1", "db2"} {
			err := topo.RegisterDatabaseFromSpec(
				ctx, store, recorder, owner,
				multigresv1alpha1.DatabaseConfig{Name: multigresv1alpha1.DatabaseName(name)},
				[]string{"cell1"}, nil, "",
			)
			if err != nil {
				t.Fatalf("registering %s: %v", name, err)
			}
		}

		// Prune, keeping only db1.
		if err := topo.PruneDatabases(ctx, store, recorder, owner, []string{"db1"}); err != nil {
			t.Fatalf("prune: %v", err)
		}

		if _, err := store.GetDatabase(ctx, "db1"); err != nil {
			t.Error("db1 should still exist")
		}
		if _, err := store.GetDatabase(ctx, "db2"); err == nil {
			t.Error("db2 should have been pruned")
		}
	})

	t.Run("no-op when all databases active", func(t *testing.T) {
		t.Parallel()
		store := newMemoryStore(t, "cell1")
		recorder := record.NewFakeRecorder(10)
		ctx := context.Background()

		err := topo.RegisterDatabaseFromSpec(
			ctx, store, recorder, owner,
			multigresv1alpha1.DatabaseConfig{Name: "db1"},
			[]string{"cell1"}, nil, "",
		)
		if err != nil {
			t.Fatalf("registering: %v", err)
		}

		if err := topo.PruneDatabases(ctx, store, recorder, owner, []string{"db1"}); err != nil {
			t.Fatalf("prune: %v", err)
		}

		if _, err := store.GetDatabase(ctx, "db1"); err != nil {
			t.Error("db1 should still exist")
		}
	})
}

func TestPruneCells(t *testing.T) {
	t.Parallel()

	topoRef := multigresv1alpha1.GlobalTopoServerRef{
		Address:  "global:2379",
		RootPath: "/multigres/global",
	}
	owner := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "default"},
	}

	t.Run("removes stale cell", func(t *testing.T) {
		t.Parallel()
		store := newMemoryStore(t, "cell1", "cell2")
		recorder := record.NewFakeRecorder(10)
		ctx := context.Background()

		for _, c := range []string{"cell1", "cell2"} {
			if err := topo.RegisterCellFromSpec(
				ctx, store, recorder, owner,
				multigresv1alpha1.CellConfig{Name: multigresv1alpha1.CellName(c)}, topoRef,
			); err != nil {
				t.Fatalf("registering %s: %v", c, err)
			}
		}

		// Prune, keeping only cell1.
		if err := topo.PruneCells(ctx, store, recorder, owner, []string{"cell1"}); err != nil {
			t.Fatalf("prune: %v", err)
		}

		if _, err := store.GetCell(ctx, "cell1"); err != nil {
			t.Error("cell1 should still exist")
		}
		if _, err := store.GetCell(ctx, "cell2"); err == nil {
			t.Error("cell2 should have been pruned")
		}
	})

	t.Run("no-op when all cells active", func(t *testing.T) {
		t.Parallel()
		store := newMemoryStore(t, "cell1")
		recorder := record.NewFakeRecorder(10)
		ctx := context.Background()

		if err := topo.RegisterCellFromSpec(
			ctx, store, recorder, owner,
			multigresv1alpha1.CellConfig{Name: "cell1"}, topoRef,
		); err != nil {
			t.Fatalf("registering: %v", err)
		}

		if err := topo.PruneCells(ctx, store, recorder, owner, []string{"cell1"}); err != nil {
			t.Fatalf("prune: %v", err)
		}

		if _, err := store.GetCell(ctx, "cell1"); err != nil {
			t.Error("cell1 should still exist")
		}
	})
}

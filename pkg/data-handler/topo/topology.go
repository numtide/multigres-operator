package topo

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/multigres/multigres/go/common/topoclient"
	clustermetadatapb "github.com/multigres/multigres/go/pb/clustermetadata"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// RegisterDatabaseFromSpec registers a database in the topology using
// the cluster spec. It collects cells from all pools across all shards
// in the database's table groups.
func RegisterDatabaseFromSpec(
	ctx context.Context,
	store topoclient.Store,
	recorder record.EventRecorder,
	owner runtime.Object,
	dbConfig multigresv1alpha1.DatabaseConfig,
	allCellNames []string,
	backup *multigresv1alpha1.BackupConfig,
) error {
	logger := log.FromContext(ctx)
	dbName := string(dbConfig.Name)

	dbMetadata := &clustermetadatapb.Database{
		Name:             dbName,
		DurabilityPolicy: "ANY_2",
		Cells:            allCellNames,
	}

	if backup != nil && backup.Type == multigresv1alpha1.BackupTypeS3 && backup.S3 != nil {
		dbMetadata.BackupLocation = &clustermetadatapb.BackupLocation{
			Location: &clustermetadatapb.BackupLocation_S3{
				S3: &clustermetadatapb.S3Backup{
					Bucket:            backup.S3.Bucket,
					Region:            backup.S3.Region,
					Endpoint:          backup.S3.Endpoint,
					KeyPrefix:         backup.S3.KeyPrefix,
					UseEnvCredentials: backup.S3.UseEnvCredentials,
				},
			},
		}
	} else {
		path := "/backups"
		if backup != nil && backup.Type == multigresv1alpha1.BackupTypeFilesystem &&
			backup.Filesystem != nil && backup.Filesystem.Path != "" {
			path = backup.Filesystem.Path
		}
		dbMetadata.BackupLocation = &clustermetadatapb.BackupLocation{
			Location: &clustermetadatapb.BackupLocation_Filesystem{
				Filesystem: &clustermetadatapb.FilesystemBackup{
					Path: path,
				},
			},
		}
	}

	if err := store.CreateDatabase(ctx, dbName, dbMetadata); err != nil {
		var topoErr topoclient.TopoError
		if isNodeExists(err, &topoErr) {
			if err := store.UpdateDatabaseFields(
				ctx,
				dbName,
				func(existing *clustermetadatapb.Database) error {
					existing.BackupLocation = dbMetadata.BackupLocation
					existing.Cells = dbMetadata.Cells
					return nil
				},
			); err != nil {
				return fmt.Errorf("updating existing database %s in topology: %w", dbName, err)
			}
			logger.V(1).Info("Updated existing database in topology", "database", dbName)
			return nil
		}
		return fmt.Errorf("failed to create database in topology: %w", err)
	}

	logger.Info("Database metadata stored in topology", "database", dbName)
	recorder.Eventf(
		owner,
		"Normal",
		"TopoRegistered",
		"Registered database '%s' in topology",
		dbName,
	)
	return nil
}

// RegisterCellFromSpec registers a cell in the topology using the cluster
// spec. It uses the GlobalTopoServerRef to build the cell metadata.
func RegisterCellFromSpec(
	ctx context.Context,
	store topoclient.Store,
	recorder record.EventRecorder,
	owner runtime.Object,
	cellConfig multigresv1alpha1.CellConfig,
	topoRef multigresv1alpha1.GlobalTopoServerRef,
) error {
	logger := log.FromContext(ctx)
	cellName := string(cellConfig.Name)

	cellMetadata := &clustermetadatapb.Cell{
		Name:            cellName,
		ServerAddresses: []string{topoRef.Address},
		Root:            topoRef.RootPath,
	}

	if err := store.CreateCell(ctx, cellName, cellMetadata); err != nil {
		var topoErr topoclient.TopoError
		if isNodeExists(err, &topoErr) {
			logger.Info("Cell already registered in topology, skipping update "+
				"(cell fields are immutable in v1alpha1)",
				"cellName", cellName)
			return nil
		}
		return fmt.Errorf("failed to create cell in topology: %w", err)
	}

	logger.Info("Cell metadata stored in topology", "cellName", cellName)
	recorder.Eventf(
		owner,
		"Normal",
		"TopoRegistered",
		"Registered cell '%s' in topology",
		cellName,
	)
	return nil
}

// PruneDatabases removes stale database entries from the topology that
// are not present in the cluster spec.
func PruneDatabases(
	ctx context.Context,
	store topoclient.Store,
	recorder record.EventRecorder,
	owner runtime.Object,
	specDBNames []string,
) error {
	logger := log.FromContext(ctx)

	existingDBs, err := store.GetDatabaseNames(ctx)
	if err != nil {
		return fmt.Errorf("listing databases from topology: %w", err)
	}

	for _, dbName := range existingDBs {
		if slices.Contains(specDBNames, dbName) {
			continue
		}

		if err := store.DeleteDatabase(ctx, dbName, false); err != nil {
			var topoErr topoclient.TopoError
			if isNoNode(err, &topoErr) {
				continue
			}
			recorder.Eventf(
				owner,
				"Warning",
				"TopoCleanupFailed",
				"Failed to prune database '%s' from topology: %v",
				dbName, err,
			)
			return fmt.Errorf("pruning database %s from topology: %w", dbName, err)
		}
		logger.Info("Pruned stale database from topology", "database", dbName)
		recorder.Eventf(
			owner,
			"Normal",
			"TopoCleanup",
			"Pruned stale database '%s' from topology",
			dbName,
		)
	}
	return nil
}

// PruneCells removes stale cell entries from the topology that
// are not present in the cluster spec.
func PruneCells(
	ctx context.Context,
	store topoclient.Store,
	recorder record.EventRecorder,
	owner runtime.Object,
	specCellNames []string,
) error {
	logger := log.FromContext(ctx)

	existingCells, err := store.GetCellNames(ctx)
	if err != nil {
		return fmt.Errorf("listing cells from topology: %w", err)
	}

	for _, cellName := range existingCells {
		if slices.Contains(specCellNames, cellName) {
			continue
		}

		if err := store.DeleteCell(ctx, cellName, false); err != nil {
			var topoErr topoclient.TopoError
			if isNoNode(err, &topoErr) {
				continue
			}
			recorder.Eventf(
				owner,
				"Warning",
				"TopoCleanupFailed",
				"Failed to prune cell '%s' from topology: %v",
				cellName, err,
			)
			return fmt.Errorf("pruning cell %s from topology: %w", cellName, err)
		}
		logger.Info("Pruned stale cell from topology", "cell", cellName)
		recorder.Eventf(
			owner,
			"Normal",
			"TopoCleanup",
			"Pruned stale cell '%s' from topology",
			cellName,
		)
	}
	return nil
}

// isNodeExists checks if a topo error indicates the node already exists.
func isNodeExists(err error, topoErr *topoclient.TopoError) bool {
	if errors.As(err, topoErr) && topoErr.Code == topoclient.NodeExists {
		return true
	}
	return false
}

// isNoNode checks if a topo error indicates the node does not exist.
func isNoNode(err error, topoErr *topoclient.TopoError) bool {
	if errors.As(err, topoErr) && topoErr.Code == topoclient.NoNode {
		return true
	}
	return false
}

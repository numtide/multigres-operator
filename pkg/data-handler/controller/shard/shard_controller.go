package shard

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/multigres/multigres/go/common/topoclient"
	"github.com/multigres/multigres/go/pb/clustermetadata"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"k8s.io/client-go/tools/record"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

const (
	finalizerName = "shard.data-handler.multigres.com/finalizer"
)

// ShardReconciler reconciles Shard data plane operations.
// This controller is responsible for:
// - Registering databases in the etcd topology based on shards
// - Managing database metadata in the global topology
// - Cleaning up database data from topology on deletion
//
// The database name comes from Shard.Spec.DatabaseName
// The cells come from the pools defined in the shard
type ShardReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Recorder        record.EventRecorder
	createTopoStore func(*multigresv1alpha1.Shard) (topoclient.Store, error)
}

// Reconcile handles Shard resource reconciliation for data plane operations.
func (r *ShardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	start := time.Now()
	logger := log.FromContext(ctx)
	logger.V(1).Info("reconcile started")

	// Fetch the Shard instance
	shard := &multigresv1alpha1.Shard{}
	if err := r.Get(ctx, req.NamespacedName, shard); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Shard resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Shard")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !shard.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, shard)
	}

	// Add finalizer if not present
	if !slices.Contains(shard.Finalizers, finalizerName) {
		shard.Finalizers = append(shard.Finalizers, finalizerName)
		if err := r.Update(ctx, shard); err != nil {
			r.Recorder.Eventf(
				shard,
				"Warning",
				"FinalizerFailed",
				"Failed to add finalizer: %v",
				err,
			)
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Register database in topology
	if err := r.registerDatabaseInTopology(ctx, shard); err != nil {
		r.Recorder.Eventf(
			shard,
			"Warning",
			"TopologyError",
			"Failed to register database in topology: %v",
			err,
		)
		logger.Error(err, "Failed to register database in topology")
		return ctrl.Result{}, err
	}

	logger.V(1).Info("reconcile complete", "duration", time.Since(start).String())
	logger.Info("Database registered in topology successfully")
	r.Recorder.Event(shard, "Normal", "Synced", "Successfully reconciled database topology")
	return ctrl.Result{}, nil
}

// handleDeletion handles cleanup when Shard is being deleted.
func (r *ShardReconciler) handleDeletion(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if slices.Contains(shard.Finalizers, finalizerName) {
		// Clean up database data from topology
		if err := r.unregisterDatabaseFromTopology(ctx, shard); err != nil {
			r.Recorder.Eventf(
				shard,
				"Warning",
				"CleanupFailed",
				"Failed to clean up database from topology: %v",
				err,
			)
			logger.Error(err, "Failed to unregister database from topology")
			return ctrl.Result{}, err
		}

		// Remove finalizer
		shard.Finalizers = slices.DeleteFunc(shard.Finalizers, func(s string) bool {
			return s == finalizerName
		})
		if err := r.Update(ctx, shard); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// registerDatabaseInTopology registers the database metadata in the global topology.
func (r *ShardReconciler) registerDatabaseInTopology(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
	logger := log.FromContext(ctx)

	store, err := r.getTopoStore(shard)
	if err != nil {
		return fmt.Errorf("failed to create topology store: %w", err)
	}
	// TODO: handle store.Close() error properly
	defer func() { _ = store.Close() }()

	dbName := string(shard.Spec.DatabaseName)

	// Collect cells from pools
	cellsMap := make(map[string]bool)
	for _, pool := range shard.Spec.Pools {
		for _, cell := range pool.Cells {
			cellsMap[string(cell)] = true
		}
	}

	cells := make([]string, 0, len(cellsMap))
	for cell := range cellsMap {
		cells = append(cells, cell)
	}
	slices.Sort(cells) // Sort for deterministic output

	// Build database metadata
	dbMetadata := &clustermetadata.Database{
		Name: dbName,
		BackupLocation: &clustermetadata.BackupLocation{
			Location: &clustermetadata.BackupLocation_Filesystem{
				Filesystem: &clustermetadata.FilesystemBackup{
					Path: r.getBackupLocation(shard),
				},
			},
		},
		DurabilityPolicy: r.getDurabilityPolicy(shard),
		Cells:            cells,
	}

	// Register database in topology
	if err := store.CreateDatabase(ctx, dbName, dbMetadata); err != nil {
		// Check if the error is because the database already exists
		var topoErr topoclient.TopoError
		if errors.As(err, &topoErr) && topoErr.Code == topoclient.NodeExists {
			logger.V(1).
				Info("Database already exists in topology, skipping creation", "database", dbName)
			return nil
		}
		r.Recorder.Eventf(
			shard,
			"Warning",
			"RegistrationFailed",
			"Failed to register database in topology: %v",
			err,
		)
		return fmt.Errorf("failed to create database in topology: %w", err)
	}

	logger.Info("Database metadata stored in topology", "database", dbName)
	r.Recorder.Eventf(
		shard,
		"Normal",
		"DatabaseRegistered",
		"Registered database '%s' in topology",
		dbName,
	)
	return nil
}

// unregisterDatabaseFromTopology removes the database metadata from the global topology.
func (r *ShardReconciler) unregisterDatabaseFromTopology(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
	logger := log.FromContext(ctx)

	store, err := r.getTopoStore(shard)
	if err != nil {
		return fmt.Errorf("failed to create topology store: %w", err)
	}
	// TODO: handle store.Close() error properly
	defer func() { _ = store.Close() }()

	dbName := string(shard.Spec.DatabaseName)

	// Delete database from topology (force=false means it will fail if database still has references)
	if err := store.DeleteDatabase(ctx, dbName, false); err != nil {
		// Check if the error is because the database doesn't exist
		var topoErr topoclient.TopoError
		if errors.As(err, &topoErr) && topoErr.Code == topoclient.NoNode {
			logger.V(1).
				Info("Database does not exist in topology, skipping deletion", "database", dbName)
			return nil
		}
		r.Recorder.Eventf(
			shard,
			"Warning",
			"UnregistrationFailed",
			"Failed to remove database from topology: %v",
			err,
		)
		return fmt.Errorf("failed to delete database %s from topology: %w", dbName, err)
	}

	logger.Info("Database metadata removed from topology", "database", dbName)
	r.Recorder.Eventf(
		shard,
		"Normal",
		"DatabaseUnregistered",
		"Removed database '%s' from topology",
		dbName,
	)
	return nil
}

// getBackupLocation extracts the backup location from the shard config.
// TODO: This should come from a field in the Shard spec once available.
func (r *ShardReconciler) getBackupLocation(shard *multigresv1alpha1.Shard) string {
	// Default value matching the demo setup
	return "/backups"
}

// getDurabilityPolicy extracts the durability policy from the shard config.
// TODO: This should come from a field in the Shard spec once available.
func (r *ShardReconciler) getDurabilityPolicy(shard *multigresv1alpha1.Shard) string {
	// NOTE: multiorch currently only supports ANY_2 or MULTI_CELL_ANY_2 durability policies.
	// Single-node policies like NONE are not yet supported by multiorch.
	// See: https://github.com/multigres/multigres/issues/XXX
	// For now, always use ANY_2 even for single-replica setups.
	return "ANY_2"
}

// defaultCreateTopoStore creates a topoclient.Store for interacting with the global topology.
func defaultCreateTopoStore(shard *multigresv1alpha1.Shard) (topoclient.Store, error) {
	// Determine the implementation type
	implementation := shard.Spec.GlobalTopoServer.Implementation
	if implementation == "" {
		implementation = "etcd" // default to etcd
	}

	rootPath := shard.Spec.GlobalTopoServer.RootPath
	serverAddrs := []string{shard.Spec.GlobalTopoServer.Address}

	// Create config
	config := topoclient.NewDefaultTopoConfig()

	// Create topology store
	store, err := topoclient.OpenServer(implementation, rootPath, serverAddrs, config)
	if err != nil {
		return nil, fmt.Errorf("failed to open topology store: %w", err)
	}

	return store, nil
}

// getTopoStore returns a topology store, using the custom factory if set, otherwise the default.
func (r *ShardReconciler) getTopoStore(shard *multigresv1alpha1.Shard) (topoclient.Store, error) {
	if r.createTopoStore != nil {
		return r.createTopoStore(shard)
	}
	return defaultCreateTopoStore(shard)
}

// SetCreateTopoStore sets a custom topology store creation function for testing.
func (r *ShardReconciler) SetCreateTopoStore(
	f func(*multigresv1alpha1.Shard) (topoclient.Store, error),
) {
	r.createTopoStore = f
}

// SetupWithManager sets up the controller with the Manager.
func (r *ShardReconciler) SetupWithManager(mgr ctrl.Manager, opts ...controller.Options) error {
	controllerOpts := controller.Options{}
	if len(opts) > 0 {
		controllerOpts = opts[0]
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("shard-datahandler").
		For(&multigresv1alpha1.Shard{}).
		WithOptions(controllerOpts).
		Complete(r)
}

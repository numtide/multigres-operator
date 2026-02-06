package cell

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/multigres/multigres/go/common/topoclient"
	"github.com/multigres/multigres/go/pb/clustermetadata"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

const (
	finalizerName = "cell.data-handler.multigres.com/finalizer"
)

// CellReconciler reconciles Cell data plane operations.
// This controller is responsible for:
// - Registering cells in the etcd topology upon startup
// - Managing cell metadata in the global topology
// - Cleaning up cell data from topology on deletion
//
// This separates data plane concerns (Multigres-specific topology management)
// from resource plane concerns (Kubernetes Pods, Services, etc.) handled by
// the resource-handler.
type CellReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	createTopoStore func(*multigresv1alpha1.Cell) (topoclient.Store, error)
}

// Reconcile handles Cell resource reconciliation for data plane operations.
func (r *CellReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Cell instance
	cell := &multigresv1alpha1.Cell{}
	if err := r.Get(ctx, req.NamespacedName, cell); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Cell resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Cell")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !cell.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, cell)
	}

	// Add finalizer if not present
	if !slices.Contains(cell.Finalizers, finalizerName) {
		cell.Finalizers = append(cell.Finalizers, finalizerName)
		if err := r.Update(ctx, cell); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		// Return without requeue - the Update will trigger a new reconciliation via watch
		return ctrl.Result{}, nil
	}

	// Only register cell if the TopologyReconciliation flag is set
	if !cell.Spec.TopologyReconciliation.RegisterCell {
		logger.V(1).Info("Cell registration disabled, skipping")
		return ctrl.Result{}, nil
	}

	// Register cell in topology
	if err := r.registerCellInTopology(ctx, cell); err != nil {
		logger.Error(err, "Failed to register cell in topology")
		return ctrl.Result{}, err
	}

	logger.Info("Cell registered in topology successfully")
	return ctrl.Result{}, nil
}

// handleDeletion handles cleanup when Cell is being deleted.
func (r *CellReconciler) handleDeletion(
	ctx context.Context,
	cell *multigresv1alpha1.Cell,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if slices.Contains(cell.Finalizers, finalizerName) {
		// Clean up cell data from topology
		if err := r.unregisterCellFromTopology(ctx, cell); err != nil {
			logger.Error(err, "Failed to unregister cell from topology")
			return ctrl.Result{}, err
		}

		// Remove finalizer
		cell.Finalizers = slices.DeleteFunc(cell.Finalizers, func(s string) bool {
			return s == finalizerName
		})
		if err := r.Update(ctx, cell); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// registerCellInTopology registers the cell metadata in the global topology
// using the upstream multigres topoclient library.
func (r *CellReconciler) registerCellInTopology(
	ctx context.Context,
	cell *multigresv1alpha1.Cell,
) error {
	logger := log.FromContext(ctx)

	store, err := r.getTopoStore(cell)
	if err != nil {
		return fmt.Errorf("failed to create topology store: %w", err)
	}
	defer store.Close()

	cellName := string(cell.Spec.Name)

	// Build cell metadata
	// The Cell in topology describes how to connect to the global topology server for this cell
	cellMetadata := &clustermetadata.Cell{
		Name:            cellName,
		ServerAddresses: []string{cell.Spec.GlobalTopoServer.Address},
		Root:            cell.Spec.GlobalTopoServer.RootPath,
	}

	// Register cell in topology
	if err := store.CreateCell(ctx, cellName, cellMetadata); err != nil {
		// Check if the error is because the cell already exists
		var topoErr topoclient.TopoError
		if errors.As(err, &topoErr) && topoErr.Code == topoclient.NodeExists {
			logger.V(1).Info("Cell already exists in topology, skipping creation")
			return nil
		}
		return fmt.Errorf("failed to create cell in topology: %w", err)
	}

	logger.Info("Cell metadata stored in topology", "cellName", cellName)
	return nil
}

// unregisterCellFromTopology removes the cell metadata from the global topology.
func (r *CellReconciler) unregisterCellFromTopology(
	ctx context.Context,
	cell *multigresv1alpha1.Cell,
) error {
	logger := log.FromContext(ctx)

	store, err := r.getTopoStore(cell)
	if err != nil {
		return fmt.Errorf("failed to create topology store: %w", err)
	}
	defer store.Close()

	cellName := string(cell.Spec.Name)

	// Delete cell from topology (force=false means it will fail if cell still has references)
	if err := store.DeleteCell(ctx, cellName, false); err != nil {
		// Check if the error is because the cell doesn't exist
		var topoErr topoclient.TopoError
		if errors.As(err, &topoErr) && topoErr.Code == topoclient.NoNode {
			logger.V(1).Info("Cell does not exist in topology, skipping deletion")
			return nil
		}
		return fmt.Errorf("failed to delete cell from topology: %w", err)
	}

	logger.Info("Cell metadata removed from topology", "cellName", cellName)
	return nil
}

// defaultCreateTopoStore creates a topoclient.Store for interacting with the global topology.
func defaultCreateTopoStore(cell *multigresv1alpha1.Cell) (topoclient.Store, error) {
	// Parse the global topo server configuration
	implementation := cell.Spec.GlobalTopoServer.Implementation
	if implementation == "" {
		implementation = "etcd" // default to etcd
	}

	rootPath := cell.Spec.GlobalTopoServer.RootPath
	serverAddrs := []string{cell.Spec.GlobalTopoServer.Address}

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
func (r *CellReconciler) getTopoStore(cell *multigresv1alpha1.Cell) (topoclient.Store, error) {
	if r.createTopoStore != nil {
		return r.createTopoStore(cell)
	}
	return defaultCreateTopoStore(cell)
}

// SetCreateTopoStore sets a custom topology store creation function for testing.
func (r *CellReconciler) SetCreateTopoStore(
	f func(*multigresv1alpha1.Cell) (topoclient.Store, error),
) {
	r.createTopoStore = f
}

// SetupWithManager sets up the controller with the Manager.
func (r *CellReconciler) SetupWithManager(mgr ctrl.Manager, opts ...controller.Options) error {
	controllerOpts := controller.Options{}
	if len(opts) > 0 {
		controllerOpts = opts[0]
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("cell-datahandler").
		For(&multigresv1alpha1.Cell{}).
		WithOptions(controllerOpts).
		Complete(r)
}

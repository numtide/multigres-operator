package topo

import (
	"context"
	"errors"
	"fmt"

	"github.com/multigres/multigres/go/common/topoclient"
	"github.com/multigres/multigres/go/pb/clustermetadata"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multigresv1alpha1 "github.com/multigres/multigres-operator/api/v1alpha1"
)

// RegisterCell registers the cell metadata in the global topology.
func RegisterCell(
	ctx context.Context,
	store topoclient.Store,
	recorder record.EventRecorder,
	cell *multigresv1alpha1.Cell,
) error {
	logger := log.FromContext(ctx)

	cellName := string(cell.Spec.Name)

	cellMetadata := &clustermetadata.Cell{
		Name:            cellName,
		ServerAddresses: []string{cell.Spec.GlobalTopoServer.Address},
		Root:            cell.Spec.GlobalTopoServer.RootPath,
	}

	if err := store.CreateCell(ctx, cellName, cellMetadata); err != nil {
		var topoErr topoclient.TopoError
		if errors.As(err, &topoErr) && topoErr.Code == topoclient.NodeExists {
			logger.V(1).Info("Cell already exists in topology; "+
				"if cell config changed, manual topo cleanup may be required",
				"cellName", cellName)
			return nil
		}
		recorder.Eventf(
			cell,
			"Warning",
			"RegistrationFailed",
			"Failed to register cell in topology: %v",
			err,
		)
		return fmt.Errorf("failed to create cell in topology: %w", err)
	}

	logger.Info("Cell metadata stored in topology", "cellName", cellName)
	recorder.Eventf(
		cell,
		"Normal",
		"CellRegistered",
		"Registered cell '%s' in topology",
		cellName,
	)
	return nil
}

// UnregisterCell removes the cell metadata from the global topology.
func UnregisterCell(
	ctx context.Context,
	store topoclient.Store,
	recorder record.EventRecorder,
	cell *multigresv1alpha1.Cell,
) error {
	logger := log.FromContext(ctx)

	cellName := string(cell.Spec.Name)

	if err := store.DeleteCell(ctx, cellName, false); err != nil {
		var topoErr topoclient.TopoError
		if errors.As(err, &topoErr) && topoErr.Code == topoclient.NoNode {
			logger.V(1).Info("Cell does not exist in topology, skipping deletion")
			return nil
		}
		if !IsTopoUnavailable(err) {
			recorder.Eventf(
				cell,
				"Warning",
				"UnregistrationFailed",
				"Failed to remove cell from topology: %v",
				err,
			)
		}
		return fmt.Errorf("failed to delete cell from topology: %w", err)
	}

	logger.Info("Cell metadata removed from topology", "cellName", cellName)
	recorder.Eventf(
		cell,
		"Normal",
		"CellUnregistered",
		"Removed cell '%s' from topology",
		cellName,
	)
	return nil
}

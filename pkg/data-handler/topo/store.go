// Package topo provides utility functions for interacting with the Multigres
// global topology server (etcd). It consolidates store creation and common
// topology operations (database/cell registration, pooler queries) that were
// previously duplicated across data-handler controllers.
package topo

import (
	"fmt"
	"strings"

	"github.com/multigres/multigres/go/common/topoclient"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// NewStoreFromShard creates a topoclient.Store using the GlobalTopoServer
// configuration from a Shard resource.
func NewStoreFromShard(shard *multigresv1alpha1.Shard) (topoclient.Store, error) {
	implementation := shard.Spec.GlobalTopoServer.Implementation
	if implementation == "" {
		implementation = "etcd"
	}

	rootPath := shard.Spec.GlobalTopoServer.RootPath
	serverAddrs := []string{shard.Spec.GlobalTopoServer.Address}
	config := topoclient.NewDefaultTopoConfig()

	store, err := topoclient.OpenServer(implementation, rootPath, serverAddrs, config)
	if err != nil {
		return nil, fmt.Errorf("failed to open topology store: %w", err)
	}
	return store, nil
}

// NewStoreFromCell creates a topoclient.Store using the GlobalTopoServer
// configuration from a Cell resource.
func NewStoreFromCell(cell *multigresv1alpha1.Cell) (topoclient.Store, error) {
	implementation := cell.Spec.GlobalTopoServer.Implementation
	if implementation == "" {
		implementation = "etcd"
	}

	rootPath := cell.Spec.GlobalTopoServer.RootPath
	serverAddrs := []string{cell.Spec.GlobalTopoServer.Address}
	config := topoclient.NewDefaultTopoConfig()

	store, err := topoclient.OpenServer(implementation, rootPath, serverAddrs, config)
	if err != nil {
		return nil, fmt.Errorf("failed to open topology store: %w", err)
	}
	return store, nil
}

// IsTopoUnavailable returns true if the error indicates the topology server
// is not reachable (e.g., gRPC UNAVAILABLE during startup).
func IsTopoUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNAVAILABLE") ||
		strings.Contains(msg, "no connection available")
}

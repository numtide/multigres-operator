package v1alpha1

// NOTE: The Makefile extracts image references from this file (lines matching
// = "...") to pre-load them into the kind cluster.
const (
	// DefaultPostgresImage is the default container image for PostgreSQL instances.
	// Uses the pgctld image which bundles PostgreSQL, pgctld, and pgbackrest.
	DefaultPostgresImage = "ghcr.io/multigres/pgctld:sha-0faa227"

	// DefaultEtcdImage is the default container image for the managed Etcd cluster.
	DefaultEtcdImage = "gcr.io/etcd-development/etcd:v3.6.7"

	// DefaultMultiAdminImage is the default container image for the MultiAdmin component.
	DefaultMultiAdminImage = "ghcr.io/multigres/multigres:sha-0faa227"

	// DefaultMultiAdminWebImage is the default container image for the MultiAdminWeb component.
	DefaultMultiAdminWebImage = "ghcr.io/multigres/multiadmin-web:sha-d7be6e4"

	// DefaultMultiOrchImage is the default container image for the MultiOrch component.
	DefaultMultiOrchImage = "ghcr.io/multigres/multigres:sha-0faa227"

	// DefaultMultiPoolerImage is the default container image for the MultiPooler component.
	DefaultMultiPoolerImage = "ghcr.io/multigres/multigres:sha-0faa227"

	// DefaultMultiGatewayImage is the default container image for the MultiGateway component.
	DefaultMultiGatewayImage = "ghcr.io/multigres/multigres:sha-0faa227"
)

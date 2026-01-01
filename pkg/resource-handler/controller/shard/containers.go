package shard

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

const (
	// DefaultMultigresImage is the base image for all Multigres components (multipooler, multiorch, pgctld)
	// Different components use different subcommands.
	DefaultMultigresImage = "ghcr.io/multigres/multigres:main"

	// DefaultPostgresImage is the default PostgreSQL database container image
	DefaultPostgresImage = "postgres:17"

	// PgctldVolumeName is the name of the shared volume for pgctld binary
	PgctldVolumeName = "pgctld-bin"

	// PgctldMountPath is the mount path for pgctld binary in postgres container
	PgctldMountPath = "/usr/local/bin/pgctld"

	// DataVolumeName is the name of the data volume for PostgreSQL
	DataVolumeName = "pgdata"

	// DataMountPath is the mount path for PostgreSQL data
	DataMountPath = "/var/lib/postgresql/data"
)

// sidecarRestartPolicy is the restart policy for native sidecar containers
var sidecarRestartPolicy = corev1.ContainerRestartPolicyAlways

// buildPostgresContainer creates the postgres container spec for a pool.
// This runs pgctld binary (which wraps postgres) and mounts persistent data storage.
func buildPostgresContainer(
	shard *multigresv1alpha1.Shard,
	pool multigresv1alpha1.PoolSpec,
) corev1.Container {
	image := DefaultPostgresImage
	if shard.Spec.Images.Postgres != "" {
		image = shard.Spec.Images.Postgres
	}

	return corev1.Container{
		Name:      "postgres",
		Image:     image,
		Resources: pool.Postgres.Resources,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      DataVolumeName,
				MountPath: DataMountPath,
			},
			{
				Name:      PgctldVolumeName,
				MountPath: PgctldMountPath,
			},
		},
	}
}

// buildMultiPoolerSidecar creates the multipooler sidecar container spec.
// This is implemented as a native sidecar using init container with
// restartPolicy: Always (K8s 1.28+).
// cellName specifies which cell this container is running in.
// If cellName is empty, defaults to the global topology cell.
func buildMultiPoolerSidecar(
	shard *multigresv1alpha1.Shard,
	pool multigresv1alpha1.PoolSpec,
	poolName string,
	cellName string,
) corev1.Container {
	image := DefaultMultigresImage
	if shard.Spec.Images.MultiPooler != "" {
		image = shard.Spec.Images.MultiPooler
	}

	// TODO: Add remaining command line arguments:
	// --pooler-dir, --grpc-socket-file, --log-level, --log-output, --hostname, --service-map
	// --pgbackrest-stanza, --connpool-admin-password, --socket-file

	args := []string{
		"multipooler", // Subcommand
		"--http-port", "15200",
		"--grpc-port", "15270",
		"--topo-global-server-addresses", shard.Spec.GlobalTopoServer.Address,
		"--topo-global-root", shard.Spec.GlobalTopoServer.RootPath,
		"--topo-implementation", shard.Spec.GlobalTopoServer.Implementation,
		"--cell", cellName,
		"--database", shard.Spec.DatabaseName,
		"--table-group", shard.Spec.TableGroupName,
		"--shard", shard.Spec.ShardName,
		"--service-id", getPoolServiceID(shard.Name, poolName),
		"--pgctld-addr", "localhost:15470",
		"--pg-port", "5432",
	}

	return corev1.Container{
		Name:          "multipooler",
		Image:         image,
		Args:          args,
		Ports:         buildMultiPoolerContainerPorts(),
		Resources:     pool.Multipooler.Resources,
		RestartPolicy: &sidecarRestartPolicy,
	}
}

// buildPgctldInitContainer creates the pgctld init container spec.
// This copies the pgctld binary to a shared volume for use by the postgres container.
func buildPgctldInitContainer(shard *multigresv1alpha1.Shard) corev1.Container {
	image := DefaultMultigresImage
	// TODO: Add pgctld image field to Shard spec if needed

	return corev1.Container{
		Name:  "pgctld-init",
		Image: image,
		Args: []string{
			"pgctld", // Subcommand
			"copy-binary",
			"--output", "/shared/pgctld",
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      PgctldVolumeName,
				MountPath: "/shared",
			},
		},
	}
}

// buildMultiOrchContainer creates the MultiOrch container spec for a specific cell.
func buildMultiOrchContainer(shard *multigresv1alpha1.Shard, cellName string) corev1.Container {
	image := DefaultMultigresImage
	if shard.Spec.Images.MultiOrch != "" {
		image = shard.Spec.Images.MultiOrch
	}

	// TODO: Add remaining command line arguments:
	// --watch-targets, --log-level, --log-output, --hostname
	// --cluster-metadata-refresh-interval, --pooler-health-check-interval, --recovery-cycle-interval

	args := []string{
		"multiorch", // Subcommand
		"--http-port", "15300",
		"--grpc-port", "15370",
		"--topo-global-server-addresses", shard.Spec.GlobalTopoServer.Address,
		"--topo-global-root", shard.Spec.GlobalTopoServer.RootPath,
		"--topo-implementation", shard.Spec.GlobalTopoServer.Implementation,
		"--cell", cellName,
	}

	return corev1.Container{
		Name:      "multiorch",
		Image:     image,
		Args:      args,
		Ports:     buildMultiOrchContainerPorts(),
		Resources: shard.Spec.MultiOrch.Resources,
	}
}

// buildPgctldVolume creates the shared emptyDir volume for pgctld binary.
func buildPgctldVolume() corev1.Volume {
	return corev1.Volume{
		Name: PgctldVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
}

// getPoolServiceID generates a unique service ID for a pool.
// This is used in multipooler and pgctld arguments.
func getPoolServiceID(shardName string, poolName string) string {
	// TODO: Use proper ID generation (UUID or consistent hash)
	// For now, use simple format
	return fmt.Sprintf("%s-pool-%s", shardName, poolName)
}

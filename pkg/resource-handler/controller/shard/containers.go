package shard

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

const (
	// DefaultMultigresImage is the base image for all Multigres components (multipooler, multiorch)
	// Different components use different subcommands.
	DefaultMultigresImage = "ghcr.io/multigres/multigres:main"

	// DefaultPgctldImage is the image containing the pgctld binary
	DefaultPgctldImage = "ghcr.io/multigres/multigres/pgctld:main"

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
		Env: []corev1.EnvVar{
			// NOTE: This is for MVP demo setup.
			{
				Name:  "POSTGRES_HOST_AUTH_METHOD",
				Value: "trust",
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:    ptr.To(int64(999)), // postgres user in postgres:17 image
			RunAsGroup:   ptr.To(int64(999)),
			RunAsNonRoot: ptr.To(true),
		},
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
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:    ptr.To(int64(999)), // Run as postgres user to access pg_data
			RunAsGroup:   ptr.To(int64(999)),
			RunAsNonRoot: ptr.To(true),
		},
	}
}

// buildPgctldInitContainer creates the pgctld init container spec.
// This copies the pgctld binary from the dedicated pgctld image to a shared volume
// for use by the postgres container.
func buildPgctldInitContainer(shard *multigresv1alpha1.Shard) corev1.Container {
	// Use dedicated pgctld image (ghcr.io/multigres/multigres/pgctld:main)
	// TODO: Add pgctld image field to Shard spec if users need to override
	image := DefaultPgctldImage

	return corev1.Container{
		Name:    "pgctld-init",
		Image:   image,
		Command: []string{"/bin/sh", "-c"},
		Args: []string{
			// Copy pgctld from /usr/local/bin/pgctld (location in pgctld image) to shared volume
			"cp /usr/local/bin/pgctld /shared/pgctld",
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
	// --log-level, --log-output, --hostname
	// --cluster-metadata-refresh-interval, --pooler-health-check-interval, --recovery-cycle-interval

	// TODO: Verify correct format for --watch-targets flag.
	// Currently using static "postgres" based on demo, but may need to be:
	// - Just database name (e.g., "postgres")
	// - Full path (e.g., "database/tablegroup/shard")
	// - Multiple targets (e.g., "postgres,otherdb")
	args := []string{
		"multiorch", // Subcommand
		"--http-port", "15300",
		"--grpc-port", "15370",
		"--topo-global-server-addresses", shard.Spec.GlobalTopoServer.Address,
		"--topo-global-root", shard.Spec.GlobalTopoServer.RootPath,
		"--cell", cellName,
		"--watch-targets", "postgres",
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

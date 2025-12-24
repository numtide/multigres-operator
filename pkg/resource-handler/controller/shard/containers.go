package shard

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

const (
	// DefaultMultiPoolerImage is the default multipooler container image
	DefaultMultiPoolerImage = "ghcr.io/multigres/multipooler:latest"

	// DefaultPgctldImage is the default pgctld container image
	DefaultPgctldImage = "ghcr.io/multigres/pgctld:latest"

	// DefaultPostgresImage is the default postgres container image
	DefaultPostgresImage = "postgres:17"

	// DefaultMultiOrchImage is the default multiorch container image
	DefaultMultiOrchImage = "numtide/multigres-operator:latest"

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
func buildMultiPoolerSidecar(
	shard *multigresv1alpha1.Shard,
	pool multigresv1alpha1.PoolSpec,
	poolName string,
) corev1.Container {
	image := DefaultMultiPoolerImage
	if shard.Spec.Images.MultiPooler != "" {
		image = shard.Spec.Images.MultiPooler
	}

	// TODO: Add remaining command line arguments:
	// --topo-global-server-addresses (needs global topo server ref in ShardSpec)
	// --topo-global-root (needs global topo server ref in ShardSpec)
	// --pooler-dir, --grpc-socket-file, --log-level, --log-output, --hostname, --service-map

	args := []string{
		"--http-port", "15200",
		"--grpc-port", "15270",
		"--topo-implementation", "etcd2",
		"--cell", cell,
		"--database", shard.Spec.DatabaseName,
		"--table-group", shard.Spec.TableGroupName,
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
	image := DefaultPgctldImage
	// TODO: Add pgctld image field to Shard spec if needed

	return corev1.Container{
		Name:    "pgctld-init",
		Image:   image,
		Command: []string{"sh", "-c", "cp /pgctld /shared/pgctld && chmod +x /shared/pgctld"},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      PgctldVolumeName,
				MountPath: "/shared",
			},
		},
	}
}

// buildMultiOrchContainer creates the MultiOrch container spec.
func buildMultiOrchContainer(shard *multigresv1alpha1.Shard) corev1.Container {
	image := DefaultMultiOrchImage
	if shard.Spec.Images.MultiOrch != "" {
		image = shard.Spec.Images.MultiOrch
	}

	// TODO: Add remaining command line arguments:
	// --topo-global-server-addresses (needs global topo server ref in ShardSpec)
	// --topo-global-root (needs global topo server ref in ShardSpec)
	// --cell (needs to be determined per-pod using StatefulSet with topology spread or env var from Downward API based on shard.Spec.MultiOrch.Cells)
	// --log-level, --log-output, --hostname

	args := []string{
		"--http-port", "15300",
		"--grpc-port", "15370",
		"--topo-implementation", "etcd2",
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

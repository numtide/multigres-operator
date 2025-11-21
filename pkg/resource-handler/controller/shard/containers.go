package shard

import (
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
func buildPostgresContainer(shard *multigresv1alpha1.Shard, pool multigresv1alpha1.ShardPoolSpec) corev1.Container {
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
func buildMultiPoolerSidecar(shard *multigresv1alpha1.Shard, pool multigresv1alpha1.ShardPoolSpec) corev1.Container {
	image := DefaultMultiPoolerImage
	if shard.Spec.Images.MultiPooler != "" {
		image = shard.Spec.Images.MultiPooler
	}

	return corev1.Container{
		Name:          "multipooler",
		Image:         image,
		Ports:         buildMultiPoolerContainerPorts(),
		Resources:     pool.MultiPooler.Resources,
		RestartPolicy: &sidecarRestartPolicy,
	}
}

// buildPgctldInitContainer creates the pgctld init container spec.
// This copies the pgctld binary to a shared volume for use by the postgres container.
func buildPgctldInitContainer(shard *multigresv1alpha1.Shard) corev1.Container {
	image := DefaultPgctldImage
	// TODO: Add pgctld image field to Shard spec if needed

	return corev1.Container{
		Name:  "pgctld-init",
		Image: image,
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
	// TODO: Add multiorch image field to Shard spec if needed

	return corev1.Container{
		Name:  "multiorch",
		Image: image,
		Ports: buildMultiOrchContainerPorts(),
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

// buildDataVolumeClaimTemplate creates the PVC template for PostgreSQL data.
func buildDataVolumeClaimTemplate(pool multigresv1alpha1.ShardPoolSpec) corev1.PersistentVolumeClaim {
	// Use the pool's DataVolumeClaimTemplate directly if provided
	return corev1.PersistentVolumeClaim{
		Spec: pool.DataVolumeClaimTemplate,
	}
}

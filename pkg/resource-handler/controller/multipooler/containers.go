package multipooler

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

	// PgctldVolumeName is the name of the shared volume for pgctld binary
	PgctldVolumeName = "pgctld-bin"

	// PgctldMountPath is the mount path for pgctld binary
	PgctldMountPath = "/usr/local/bin/pgctld"
)

var (
	// sidecarRestartPolicy is the restart policy for native sidecar containers
	sidecarRestartPolicy = corev1.ContainerRestartPolicyAlways
)

// buildMultiPoolerContainer creates the multipooler container spec.
func buildMultiPoolerContainer(multipooler *multigresv1alpha1.MultiPooler) corev1.Container {
	image := DefaultMultiPoolerImage
	if multipooler.Spec.MultiPooler.Image != "" {
		image = multipooler.Spec.MultiPooler.Image
	}

	return corev1.Container{
		Name:  "multipooler",
		Image: image,
		Ports: buildContainerPorts(multipooler),
		// TODO: We may want to define some sensible defaults when none provided.
		Resources: multipooler.Spec.MultiPooler.Resources,
	}
}

// buildPgctldInitContainer creates the pgctld init container spec.
// This copies the pgctld binary to a shared volume for use by the postgres
// container.
//
// TODO: This may be not necessary once the OCI image as volume mount is
// ennabled / supported by default.
// Ref: https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/
func buildPgctldInitContainer(multipooler *multigresv1alpha1.MultiPooler) corev1.Container {
	image := DefaultPgctldImage
	if multipooler.Spec.Pgctld.Image != "" {
		image = multipooler.Spec.Pgctld.Image
	}

	return corev1.Container{
		Name:  "pgctld-init",
		Image: image,
		// TODO: We may want to define some sensible defaults when none provided.
		Resources: multipooler.Spec.Pgctld.Resources,
		// TODO: Add command to copy pgctld binary to shared volume
		// Command: []string{"sh", "-c", "cp /pgctld /shared/pgctld && chmod +x /shared/pgctld"},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      PgctldVolumeName,
				MountPath: "/shared",
			},
		},
	}
}

// buildPostgresContainer creates the postgres container spec.
// This runs pgctld binary (which wraps postgres) and mounts persistent data storage.
func buildPostgresContainer(multipooler *multigresv1alpha1.MultiPooler) corev1.Container {
	image := DefaultPostgresImage
	if multipooler.Spec.Postgres.Image != "" {
		image = multipooler.Spec.Postgres.Image
	}

	return corev1.Container{
		Name:  "postgres",
		Image: image,
		// TODO: We may want to define some sensible defaults when none provided.
		Resources: multipooler.Spec.Postgres.Resources,
		// TODO: Set command to run pgctld instead of default postgres
		// Command: []string{"/usr/local/bin/pgctld/pgctld"},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      DataVolumeName,
				MountPath: DataMountPath,
			},
			{
				Name:      PgctldVolumeName,
				MountPath: "/usr/local/bin/pgctld",
			},
		},
	}
}

// buildMultiPoolerSidecar creates the multipooler sidecar container spec.
// This is implemented as a native sidecar using init container with
// restartPolicy: Always (K8s 1.28+).
func buildMultiPoolerSidecar(multipooler *multigresv1alpha1.MultiPooler) corev1.Container {
	container := buildMultiPoolerContainer(multipooler)
	container.RestartPolicy = &sidecarRestartPolicy
	return container
}

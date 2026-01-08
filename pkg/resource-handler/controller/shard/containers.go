package shard

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

const (
	// DefaultMultigresImage is the base image for all Multigres components (multipooler, multiorch)
	// Different components use different subcommands.
	DefaultMultigresImage = "ghcr.io/multigres/multigres:main"

	// DefaultPgctldImage is the image containing the pgctld binary
	// Used by buildPgctldInitContainer() for the binary-copy approach
	DefaultPgctldImage = "ghcr.io/multigres/pgctld:main"

	// DefaultPostgresImage is the default PostgreSQL database container image
	// Used by buildPostgresContainer() for the original stock postgres:17 approach
	// NOTE: Currently unused - buildPgctldContainer() uses ghcr.io/multigres/pgctld:main instead
	DefaultPostgresImage = "postgres:17"

	// PgctldVolumeName is the name of the shared volume for pgctld binary
	// Used only by alternative approach (binary-copy via init container)
	PgctldVolumeName = "pgctld-bin"

	// PgctldBinDir is the directory where pgctld binary is mounted
	// Subdirectory avoids shadowing postgres binaries in /usr/local/bin
	// Used only by alternative approach (binary-copy via init container)
	PgctldBinDir = "/usr/local/bin/multigres"

	// PgctldMountPath is the full path to pgctld binary
	// Used only by alternative approach (binary-copy via init container)
	PgctldMountPath = PgctldBinDir + "/pgctld"

	// DataVolumeName is the name of the data volume for PostgreSQL
	DataVolumeName = "pgdata"

	// DataMountPath is where the PVC is mounted
	// Mounted at parent directory because mounting directly at pg_data/ prevents
	// initdb from setting directory permissions (non-root can't chmod mount points).
	// pgctld creates pg_data/ subdirectory with proper 0700/0750 permissions.
	DataMountPath = "/var/lib/pooler"

	// PgDataPath is the actual postgres data directory (PGDATA env var value)
	// pgctld expects postgres data at <pooler-dir>/pg_data
	PgDataPath = "/var/lib/pooler/pg_data"

	// PoolerDirMountPath must equal DataMountPath because both containers share the PVC
	// and pgctld derives postgres data directory as <pooler-dir>/pg_data
	PoolerDirMountPath = "/var/lib/pooler"

	// SocketDirVolumeName is the name of the shared volume for unix sockets
	SocketDirVolumeName = "socket-dir"

	// SocketDirMountPath is the mount path for unix sockets (postgres and pgctld communicate here)
	// We use /var/run/postgresql because that is the default socket directory for the official postgres image.
	SocketDirMountPath = "/var/run/postgresql"

	// BackupVolumeName is the name of the backup volume for pgbackrest
	BackupVolumeName = "backup-data"

	// BackupMountPath is where the backup volume is mounted
	// pgbackrest stores backups here via --repo1-path
	BackupMountPath = "/backups"
)

// buildSocketDirVolume creates the shared emptyDir volume for unix sockets.
func buildSocketDirVolume() corev1.Volume {
	return corev1.Volume{
		Name: SocketDirVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
}

// sidecarRestartPolicy is the restart policy for native sidecar containers
var sidecarRestartPolicy = corev1.ContainerRestartPolicyAlways

// buildPostgresContainer creates the postgres container spec for a pool.
// ORIGINAL APPROACH: Uses stock postgres:17 image with pgctld binary copied via init container.
// Currently unused - replaced by buildPgctldContainer. Kept for reference.
//
// This approach requires:
//   - buildPgctldInitContainer() in InitContainers
//   - buildPgctldVolume() in Volumes
//   - Does NOT include pgbackrest (would need manual installation)
func buildPostgresContainer(
	shard *multigresv1alpha1.Shard,
	pool multigresv1alpha1.PoolSpec,
) corev1.Container {
	image := DefaultPostgresImage
	if shard.Spec.Images.Postgres != "" {
		image = shard.Spec.Images.Postgres
	}

	return corev1.Container{
		Name:    "postgres",
		Image:   image,
		Command: []string{PgctldMountPath},
		Args: []string{
			"server",
			"--pooler-dir=" + PoolerDirMountPath,
			"--grpc-port=15470",
			"--pg-port=5432",
			"--pg-listen-addresses=*",
			"--pg-database=postgres",
			"--pg-user=postgres",
			"--timeout=30",
			"--log-level=info",
			"--grpc-socket-file=" + PoolerDirMountPath + "/pgctld.sock",
		},
		Resources: pool.Postgres.Resources,
		Env: []corev1.EnvVar{
			// NOTE: This is for MVP demo setup.
			{
				Name:  "POSTGRES_HOST_AUTH_METHOD",
				Value: "trust",
			},
			{
				Name:  "PGDATA",
				Value: PgDataPath,
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:    ptr.To(int64(999)), // Must match postgres:17 image UID for file access
			RunAsGroup:   ptr.To(int64(999)),
			RunAsNonRoot: ptr.To(true), // pgctld refuses to run as root
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      DataVolumeName,
				MountPath: DataMountPath,
			},
			{
				Name:      PgctldVolumeName,
				MountPath: PgctldBinDir,
			},
		},
	}
}

// buildPgctldContainer creates the postgres container spec using the pgctld image.
// CURRENT APPROACH: Uses DefaultPgctldImage (ghcr.io/multigres/pgctld:main) which includes:
//   - PostgreSQL 17
//   - pgctld binary at /usr/local/bin/pgctld
//   - pgbackrest for backup/restore operations
//
// This approach does NOT require:
//   - buildPgctldInitContainer() (pgctld already in image)
//   - buildPgctldVolume() (no binary copying needed)
//
// Key differences from buildPostgresContainer:
//   - Uses DefaultPgctldImage instead of DefaultPostgresImage (stock postgres:17)
//   - Includes backup volume mount (for pgbackrest)
//   - Includes socket dir volume mount (for shared sockets)
//   - Does NOT include pgctld-bin volume mount
func buildPgctldContainer(
	shard *multigresv1alpha1.Shard,
	pool multigresv1alpha1.PoolSpec,
) corev1.Container {
	image := DefaultPgctldImage
	if shard.Spec.Images.Postgres != "" {
		image = shard.Spec.Images.Postgres
	}

	return corev1.Container{
		Name:    "postgres",
		Image:   image,
		Command: []string{"/usr/local/bin/pgctld"},
		Args: []string{
			"server",
			"--pooler-dir=" + PoolerDirMountPath,
			"--grpc-port=15470",
			"--pg-port=5432",
			"--pg-listen-addresses=*",
			"--pg-database=postgres",
			"--pg-user=postgres",
			"--timeout=30",
			"--log-level=info",
			"--grpc-socket-file=" + PoolerDirMountPath + "/pgctld.sock",
		},
		Resources: pool.Postgres.Resources,
		Env: []corev1.EnvVar{
			{
				Name:  "POSTGRES_HOST_AUTH_METHOD",
				Value: "trust",
			},
			{
				Name:  "PGDATA",
				Value: PgDataPath,
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:    ptr.To(int64(999)),
			RunAsGroup:   ptr.To(int64(999)),
			RunAsNonRoot: ptr.To(true),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      DataVolumeName,
				MountPath: DataMountPath,
			},
			{
				Name:      BackupVolumeName,
				MountPath: BackupMountPath,
			},
			{
				Name:      SocketDirVolumeName,
				MountPath: SocketDirMountPath,
			},
		},
	}
}

// buildMultiPoolerSidecar creates the multipooler sidecar container spec.
// Implemented as native sidecar (init container with restartPolicy: Always) because
// multipooler must restart with postgres to maintain connection pool consistency.
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
	// --grpc-socket-file, --log-level, --log-output, --hostname
	// --pgbackrest-stanza, --connpool-admin-password

	args := []string{
		"multipooler", // Subcommand
		"--http-port", "15200",
		"--grpc-port", "15270",
		"--pooler-dir", PoolerDirMountPath,
		"--socket-file", PoolerDirMountPath + "/pg_sockets/.s.PGSQL.5432", // Unix socket uses trust auth (no password)
		"--service-map", "grpc-pooler", // Only enable grpc-pooler service (disables auto-restore service)
		"--topo-global-server-addresses", shard.Spec.GlobalTopoServer.Address,
		"--topo-global-root", shard.Spec.GlobalTopoServer.RootPath,
		"--cell", cellName,
		"--database", shard.Spec.DatabaseName,
		"--table-group", shard.Spec.TableGroupName,
		"--shard", shard.Spec.ShardName,
		"--service-id", "$(POD_NAME)", // Use pod name as unique service ID
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
			RunAsUser:    ptr.To(int64(999)), // Must match postgres UID to access pg_data directory
			RunAsGroup:   ptr.To(int64(999)),
			RunAsNonRoot: ptr.To(true),
		},
		Env: []corev1.EnvVar{
			{
				Name: "POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.name",
					},
				},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      DataVolumeName, // Shares PVC with postgres for pgbackrest configs and sockets
				MountPath: PoolerDirMountPath,
			},
			{
				Name:      BackupVolumeName,
				MountPath: BackupMountPath,
			},
			{
				Name:      SocketDirVolumeName,
				MountPath: SocketDirMountPath,
			},
		},
	}
}

// buildPgctldInitContainer creates the pgctld init container spec.
// ALTERNATIVE APPROACH: Copies pgctld binary to shared volume for stock postgres:17 image.
// Currently unused - kept for reference. Active approach uses pgctld image with built-in pgctld.
func buildPgctldInitContainer(shard *multigresv1alpha1.Shard) corev1.Container {
	return corev1.Container{
		Name:    "pgctld-init",
		Image:   DefaultPgctldImage,
		Command: []string{"/bin/sh", "-c"},
		Args: []string{
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
		"--cluster-metadata-refresh-interval", "500ms",
		"--pooler-health-check-interval", "500ms",
		"--recovery-cycle-interval", "500ms",
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
// ALTERNATIVE APPROACH: Used only when copying pgctld via init container.
// Currently unused - kept for reference. Active approach uses pgctld image.
func buildPgctldVolume() corev1.Volume {
	return corev1.Volume{
		Name: PgctldVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
}

// buildBackupVolume creates the backup volume for pgbackrest.
// Uses either hostPath (for single-node testing) or PVC (for production) based on backupStorageType.
func buildBackupVolume(poolName string, backupStorageType string) corev1.Volume {
	if backupStorageType == "pvc" {
		return corev1.Volume{
			Name: BackupVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "backup-data-" + poolName,
				},
			},
		}
	}

	// Default to hostPath for single-node testing (kind, minikube, etc.)
	pathType := corev1.HostPathDirectoryOrCreate
	return corev1.Volume{
		Name: BackupVolumeName,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/data/backups",
				Type: &pathType,
			},
		},
	}
}

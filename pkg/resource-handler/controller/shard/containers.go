package shard

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/util/name"
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

	// PgHbaVolumeName is the name of the volume for pg_hba template
	PgHbaVolumeName = "pg-hba-template"

	// PgHbaMountPath is where the pg_hba template is mounted
	PgHbaMountPath = "/etc/pgctld"

	// PgHbaTemplatePath is the full path to the pg_hba template file
	PgHbaTemplatePath = PgHbaMountPath + "/pg_hba_template.conf"

	// PostgresPasswordSecretKey is the key within the Secret that holds the password
	PostgresPasswordSecretKey = "password"

	// PgBackRestCertVolumeName is the name of the volume for pgBackRest TLS certificates
	PgBackRestCertVolumeName = "pgbackrest-certs"

	// PgBackRestCertMountPath is where pgBackRest TLS certificates are mounted
	PgBackRestCertMountPath = "/certs/pgbackrest"

	// PgBackRestPort is the port for the pgBackRest TLS server
	PgBackRestPort = 8432
)

// PgHbaConfigMapName returns the per-shard ConfigMap name for the pg_hba template.
func PgHbaConfigMapName(shardName string) string {
	return shardName + "-pg-hba"
}

// PostgresPasswordSecretName returns the per-shard Secret name for the postgres password.
func PostgresPasswordSecretName(shardName string) string {
	return shardName + "-postgres-password"
}

// buildSocketDirVolume creates the shared emptyDir volume for unix sockets.
func buildSocketDirVolume() corev1.Volume {
	return corev1.Volume{
		Name: SocketDirVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
}

// buildPgHbaVolume creates the volume for pg_hba template from ConfigMap.
func buildPgHbaVolume(shardName string) corev1.Volume {
	return corev1.Volume{
		Name: PgHbaVolumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: PgHbaConfigMapName(shardName),
				},
			},
		},
	}
}

// sidecarRestartPolicy is the restart policy for native sidecar containers
var sidecarRestartPolicy = corev1.ContainerRestartPolicyAlways

// buildPostgresContainer creates the postgres container spec for a pool.
// Uses stock postgres:17 image with pgctld and pgbackrest binaries copied via init container.
//
// This approach requires:
//   - buildPgctldInitContainer() in InitContainers (copies pgctld and pgbackrest)
//   - buildPgctldVolume() in Volumes
func buildPostgresContainer(
	shard *multigresv1alpha1.Shard,
	pool multigresv1alpha1.PoolSpec,
) corev1.Container {
	image := DefaultPostgresImage
	if shard.Spec.Images.Postgres != "" {
		image = string(shard.Spec.Images.Postgres)
	}

	c := corev1.Container{
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
			"--pg-hba-template=" + PgHbaTemplatePath,
		},
		Resources: pool.Postgres.Resources,
		Env: []corev1.EnvVar{
			{
				Name:  "PGDATA",
				Value: PgDataPath,
			},
			pgPasswordEnvVar(shard.Name),
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
			{
				Name:      BackupVolumeName,
				MountPath: BackupMountPath,
			},
			{
				Name:      SocketDirVolumeName,
				MountPath: SocketDirMountPath,
			},
			{
				Name:      PgHbaVolumeName,
				MountPath: PgHbaMountPath,
				ReadOnly:  true,
			},
		},
	}
	if envVars := multigresv1alpha1.BuildOTELEnvVars(shard.Spec.Observability); len(envVars) > 0 {
		c.Env = append(c.Env, envVars...)
	}
	return c
}

// buildPgctldContainer creates the postgres container spec using the pgctld image.
// Uses DefaultPgctldImage (ghcr.io/multigres/pgctld:main) which includes:
//   - PostgreSQL 17
//   - pgctld binary at /usr/local/bin/pgctld
//   - pgbackrest for backup/restore operations
//
// This approach does NOT require:
//   - buildPgctldInitContainer() (pgctld already in image)
//   - buildPgctldVolume() (no binary copying needed)
func buildPgctldContainer(
	shard *multigresv1alpha1.Shard,
	pool multigresv1alpha1.PoolSpec,
) corev1.Container {
	image := DefaultPgctldImage
	if shard.Spec.Images.Postgres != "" {
		image = string(shard.Spec.Images.Postgres)
	}

	args := []string{
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
		"--pg-hba-template=" + PgHbaTemplatePath,
	}

	if shard.Spec.Backup != nil {
		// pgBackRest TLS cert dir and port (enables the pgBackRest TLS server).
		// Backup type/path/S3 config is resolved by multipooler from the etcd
		// topology Database record, not via pgctld CLI flags.
		args = append(args,
			fmt.Sprintf("--pgbackrest-cert-dir=%s", PgBackRestCertMountPath),
			fmt.Sprintf("--pgbackrest-port=%d", PgBackRestPort),
		)
	}

	env := []corev1.EnvVar{
		{
			Name:  "PGDATA",
			Value: PgDataPath,
		},
		pgPasswordEnvVar(shard.Name),
	}
	env = append(env, s3EnvVars(shard.Spec.Backup)...)
	if otelVars := multigresv1alpha1.BuildOTELEnvVars(shard.Spec.Observability); len(otelVars) > 0 {
		env = append(env, otelVars...)
	}

	volumeMounts := []corev1.VolumeMount{
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
		{
			Name:      PgHbaVolumeName,
			MountPath: PgHbaMountPath,
			ReadOnly:  true,
		},
	}
	if shard.Spec.Backup != nil {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      PgBackRestCertVolumeName,
			MountPath: PgBackRestCertMountPath,
			ReadOnly:  true,
		})
	}

	return corev1.Container{
		Name:      "postgres",
		Image:     image,
		Command:   []string{"/usr/local/bin/pgctld"},
		Args:      args,
		Resources: pool.Postgres.Resources,
		Env:       env,
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:    ptr.To(int64(999)),
			RunAsGroup:   ptr.To(int64(999)),
			RunAsNonRoot: ptr.To(true),
		},
		VolumeMounts: volumeMounts,
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
		image = string(shard.Spec.Images.MultiPooler)
	}

	// TODO: Add remaining command line arguments:
	// --grpc-socket-file, --log-level, --log-output, --hostname
	// --pgbackrest-stanza

	args := []string{
		"multipooler", // Subcommand
		"--http-port=15200",
		"--grpc-port=15270",
		"--pooler-dir=" + PoolerDirMountPath,
		"--socket-file=" + PoolerDirMountPath + "/pg_sockets/.s.PGSQL.5432", // Unix socket uses trust auth (no password)
		"--service-map=grpc-pooler",                                         // Only enable grpc-pooler service (disables auto-restore service)
		"--topo-global-server-addresses=" + shard.Spec.GlobalTopoServer.Address,
		"--topo-global-root=" + shard.Spec.GlobalTopoServer.RootPath,
		"--cell=" + cellName,
		"--database=" + string(shard.Spec.DatabaseName),
		"--table-group=" + string(shard.Spec.TableGroupName),
		"--shard=" + string(shard.Spec.ShardName),
		"--service-id=$(POD_NAME)", // Use pod name as unique service ID
		"--pgctld-addr=localhost:15470",
		"--pg-port=5432",
		"--connpool-admin-password=$(CONNPOOL_ADMIN_PASSWORD)", // Resolved from env var below
	}

	if shard.Spec.Backup != nil {
		args = append(args,
			"--pgbackrest-cert-file="+PgBackRestCertMountPath+"/pgbackrest.crt",
			"--pgbackrest-key-file="+PgBackRestCertMountPath+"/pgbackrest.key",
			"--pgbackrest-ca-file="+PgBackRestCertMountPath+"/ca.crt",
		)
	}

	c := corev1.Container{
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
		StartupProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/ready",
					Port: intstr.FromInt32(DefaultMultiPoolerHTTPPort),
				},
			},
			PeriodSeconds:    5,
			FailureThreshold: 30,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/live",
					Port: intstr.FromInt32(DefaultMultiPoolerHTTPPort),
				},
			},
			PeriodSeconds: 10,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/ready",
					Port: intstr.FromInt32(DefaultMultiPoolerHTTPPort),
				},
			},
			PeriodSeconds: 5,
		},
	}

	env := []corev1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		connpoolAdminPasswordEnvVar(shard.Name),
	}
	env = append(env, s3EnvVars(shard.Spec.Backup)...)
	if otelVars := multigresv1alpha1.BuildOTELEnvVars(shard.Spec.Observability); len(otelVars) > 0 {
		env = append(env, otelVars...)
	}
	c.Env = env

	c.VolumeMounts = []corev1.VolumeMount{
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
	}
	if shard.Spec.Backup != nil {
		c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{
			Name:      PgBackRestCertVolumeName,
			MountPath: PgBackRestCertMountPath,
			ReadOnly:  true,
		})
	}
	return c
}

// buildPgctldInitContainer creates the pgctld init container spec.
// Copies pgctld and pgbackrest binaries to shared volume for use with stock postgres:17 image.
// Used with buildPostgresContainer() and buildPgctldVolume().
func buildPgctldInitContainer(shard *multigresv1alpha1.Shard) corev1.Container {
	return corev1.Container{
		Name:    "pgctld-init",
		Image:   DefaultPgctldImage,
		Command: []string{"/bin/sh", "-c"},
		Args: []string{
			"cp /usr/local/bin/pgctld /usr/bin/pgbackrest /shared/",
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
		image = string(shard.Spec.Images.MultiOrch)
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
		"--http-port=15300",
		"--grpc-port=15370",
		"--topo-global-server-addresses=" + shard.Spec.GlobalTopoServer.Address,
		"--topo-global-root=" + shard.Spec.GlobalTopoServer.RootPath,
		"--cell=" + cellName,
		"--watch-targets=postgres",
		"--cluster-metadata-refresh-interval=500ms",
		"--pooler-health-check-interval=500ms",
		"--recovery-cycle-interval=500ms",
	}

	c := corev1.Container{
		Name:      "multiorch",
		Image:     image,
		Args:      args,
		Ports:     buildMultiOrchContainerPorts(),
		Resources: shard.Spec.MultiOrch.Resources,
		StartupProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/ready",
					Port: intstr.FromInt32(DefaultMultiOrchHTTPPort),
				},
			},
			PeriodSeconds:    5,
			FailureThreshold: 30,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/live",
					Port: intstr.FromInt32(DefaultMultiOrchHTTPPort),
				},
			},
			PeriodSeconds: 10,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/ready",
					Port: intstr.FromInt32(DefaultMultiOrchHTTPPort),
				},
			},
			PeriodSeconds: 5,
		},
	}
	if envVars := multigresv1alpha1.BuildOTELEnvVars(shard.Spec.Observability); len(envVars) > 0 {
		c.Env = append(c.Env, envVars...)
	}
	return c
}

// buildPoolVolumes assembles the complete list of volumes for a pool pod.
// Conditionally includes the pgBackRest cert volume when backup is configured.
func buildPoolVolumes(shard *multigresv1alpha1.Shard, cellName string) []corev1.Volume {
	volumes := []corev1.Volume{
		// buildPgctldVolume(),
		buildSharedBackupVolume(shard, cellName),
		buildSocketDirVolume(),
		buildPgHbaVolume(shard.Name),
	}
	if certVol := buildPgBackRestCertVolume(shard); certVol != nil {
		volumes = append(volumes, *certVol)
	}
	return volumes
}

// buildPgctldVolume creates the shared emptyDir volume for pgctld and pgbackrest binaries.
// Used with buildPgctldInitContainer() and buildPostgresContainer().
func buildPgctldVolume() corev1.Volume {
	return corev1.Volume{
		Name: PgctldVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
}

// buildSharedBackupVolume creates the backup volume for pgbackrest.
// If type is Filesystem, references the shared PVC (per-cell).
// If type is S3 (or nil), uses an emptyDir for local scratch/spool.
func buildSharedBackupVolume(shard *multigresv1alpha1.Shard, cellName string) corev1.Volume {
	// Default to EmptyDir (for S3 or no backup config)
	source := corev1.VolumeSource{
		EmptyDir: &corev1.EmptyDirVolumeSource{},
	}

	if shard.Spec.Backup != nil &&
		shard.Spec.Backup.Type == multigresv1alpha1.BackupTypeFilesystem {
		clusterName := shard.Labels["multigres.com/cluster"]
		claimName := name.JoinWithConstraints(
			name.ServiceConstraints,
			"backup-data",
			clusterName,
			string(shard.Spec.DatabaseName),
			string(shard.Spec.TableGroupName),
			string(shard.Spec.ShardName),
			cellName,
		)
		source = corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: claimName,
			},
		}
	}

	return corev1.Volume{
		Name:         BackupVolumeName,
		VolumeSource: source,
	}
}

// buildPgBackRestCertVolume creates the volume for pgBackRest TLS certificates.
// Returns nil if backup is not configured.
//
// Both modes use a projected volume to rename tls.crt → pgbackrest.crt and
// tls.key → pgbackrest.key, matching upstream pgctld/multipooler expectations.
// This makes the user-provided mode directly compatible with cert-manager's
// standard Secret output (ca.crt, tls.crt, tls.key).
//
//   - User-provided: projects from the single Secret specified in PgBackRestTLS.SecretName.
//   - Auto-generated: projects from two operator-managed Secrets
//     ({shard}-pgbackrest-ca and {shard}-pgbackrest-tls).
func buildPgBackRestCertVolume(shard *multigresv1alpha1.Shard) *corev1.Volume {
	if shard.Spec.Backup == nil {
		return nil
	}

	defaultMode := int32(0o440)

	// User-provided Secret: project with key renaming for cert-manager compatibility.
	// Cert-manager outputs ca.crt, tls.crt, tls.key — we rename to match upstream.
	if shard.Spec.Backup.PgBackRestTLS != nil &&
		shard.Spec.Backup.PgBackRestTLS.SecretName != "" {
		secretName := shard.Spec.Backup.PgBackRestTLS.SecretName
		return &corev1.Volume{
			Name: PgBackRestCertVolumeName,
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					DefaultMode: &defaultMode,
					Sources: []corev1.VolumeProjection{
						{
							Secret: &corev1.SecretProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: secretName,
								},
								Items: []corev1.KeyToPath{
									{Key: "ca.crt", Path: "ca.crt"},
									{Key: "tls.crt", Path: "pgbackrest.crt"},
									{Key: "tls.key", Path: "pgbackrest.key"},
								},
							},
						},
					},
				},
			},
		}
	}

	// Auto-generated: projected volume combining CA and server cert Secrets.
	// Renames tls.crt → pgbackrest.crt and tls.key → pgbackrest.key to match upstream.
	return &corev1.Volume{
		Name: PgBackRestCertVolumeName,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				DefaultMode: &defaultMode,
				Sources: []corev1.VolumeProjection{
					{
						Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: shard.Name + "-pgbackrest-ca",
							},
							Items: []corev1.KeyToPath{
								{Key: "ca.crt", Path: "ca.crt"},
							},
						},
					},
					{
						Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: shard.Name + "-pgbackrest-tls",
							},
							Items: []corev1.KeyToPath{
								{Key: "tls.crt", Path: "pgbackrest.crt"},
								{Key: "tls.key", Path: "pgbackrest.key"},
							},
						},
					},
				},
			},
		},
	}
}

// pgPasswordEnvVar returns a PGPASSWORD env var sourced from the per-shard postgres password Secret.
// pgctld reads this during initdb to set the superuser password.
func pgPasswordEnvVar(shardName string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: "PGPASSWORD",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: PostgresPasswordSecretName(shardName),
				},
				Key: PostgresPasswordSecretKey,
			},
		},
	}
}

// connpoolAdminPasswordEnvVar returns a CONNPOOL_ADMIN_PASSWORD env var sourced
// from the per-shard postgres password Secret. Multipooler uses this to authenticate
// with PostgreSQL for connection pool administration.
func connpoolAdminPasswordEnvVar(shardName string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: "CONNPOOL_ADMIN_PASSWORD",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: PostgresPasswordSecretName(shardName),
				},
				Key: PostgresPasswordSecretKey,
			},
		},
	}
}

// s3EnvVars returns the AWS environment variables needed for S3 backup.
// Returns nil if backup is not configured for S3.
func s3EnvVars(backup *multigresv1alpha1.BackupConfig) []corev1.EnvVar {
	if backup == nil ||
		backup.Type != multigresv1alpha1.BackupTypeS3 ||
		backup.S3 == nil {
		return nil
	}

	var envs []corev1.EnvVar

	if backup.S3.Region != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "AWS_REGION",
			Value: backup.S3.Region,
		})
	}

	if backup.S3.CredentialsSecret != "" {
		envs = append(envs, corev1.EnvVar{
			Name: "AWS_ACCESS_KEY_ID",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: backup.S3.CredentialsSecret,
					},
					Key: "AWS_ACCESS_KEY_ID",
				},
			},
		})
		envs = append(envs, corev1.EnvVar{
			Name: "AWS_SECRET_ACCESS_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: backup.S3.CredentialsSecret,
					},
					Key: "AWS_SECRET_ACCESS_KEY",
				},
			},
		})
	}

	return envs
}

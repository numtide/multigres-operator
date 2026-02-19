package shard

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func TestBuildPostgresContainer(t *testing.T) {
	tests := map[string]struct {
		shard    *multigresv1alpha1.Shard
		poolSpec multigresv1alpha1.PoolSpec
		want     corev1.Container
	}{
		"default postgres image with no resources": {
			shard: &multigresv1alpha1.Shard{
				Spec: multigresv1alpha1.ShardSpec{},
			},
			poolSpec: multigresv1alpha1.PoolSpec{},
			want: corev1.Container{
				Name:    "postgres",
				Image:   DefaultPostgresImage,
				Command: []string{"/usr/local/bin/multigres/pgctld"},
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
				Resources: corev1.ResourceRequirements{},
				Env: []corev1.EnvVar{
					{
						Name:  "PGDATA",
						Value: PgDataPath,
					},
					pgPasswordEnvVar(),
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
			},
		},
		"custom postgres image": {
			shard: &multigresv1alpha1.Shard{
				Spec: multigresv1alpha1.ShardSpec{
					Images: multigresv1alpha1.ShardImages{
						Postgres: "postgres:16",
					},
				},
			},
			poolSpec: multigresv1alpha1.PoolSpec{},
			want: corev1.Container{
				Name:    "postgres",
				Image:   "postgres:16",
				Command: []string{"/usr/local/bin/multigres/pgctld"},
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
				Resources: corev1.ResourceRequirements{},
				Env: []corev1.EnvVar{
					{
						Name:  "PGDATA",
						Value: PgDataPath,
					},
					pgPasswordEnvVar(),
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
			},
		},
		"with resource requirements": {
			shard: &multigresv1alpha1.Shard{
				Spec: multigresv1alpha1.ShardSpec{},
			},
			poolSpec: multigresv1alpha1.PoolSpec{
				Postgres: multigresv1alpha1.ContainerConfig{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
				},
			},
			want: corev1.Container{
				Name:    "postgres",
				Image:   DefaultPostgresImage,
				Command: []string{"/usr/local/bin/multigres/pgctld"},
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
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
				Env: []corev1.EnvVar{
					{
						Name:  "PGDATA",
						Value: PgDataPath,
					},
					pgPasswordEnvVar(),
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
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildPostgresContainer(tc.shard, tc.poolSpec)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildPostgresContainer() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildMultiPoolerSidecar(t *testing.T) {
	tests := map[string]struct {
		shard    *multigresv1alpha1.Shard
		poolSpec multigresv1alpha1.PoolSpec
		cellName string
		want     corev1.Container
	}{
		"default multipooler image with no resources": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{Name: "test-shard"},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					ShardName:      "0",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "global-topo:2379",
						RootPath:       "/multigres/global",
						Implementation: "etcd",
					},
				},
			},
			poolSpec: multigresv1alpha1.PoolSpec{
				Cells: []multigresv1alpha1.CellName{"zone1"},
			},
			cellName: "zone1",
			want: corev1.Container{
				Name:  "multipooler",
				Image: DefaultMultigresImage,
				Args: []string{
					"multipooler",
					"--http-port=15200",
					"--grpc-port=15270",
					"--pooler-dir=" + PoolerDirMountPath,
					"--socket-file=/var/lib/pooler/pg_sockets/.s.PGSQL.5432",
					"--service-map=grpc-pooler",
					"--topo-global-server-addresses=global-topo:2379",
					"--topo-global-root=/multigres/global",
					"--cell=zone1",
					"--database=testdb",
					"--table-group=default",
					"--shard=0",
					"--service-id=$(POD_NAME)",
					"--pgctld-addr=localhost:15470",
					"--pg-port=5432",
					"--connpool-admin-password=$(CONNPOOL_ADMIN_PASSWORD)",
				},
				Ports:         buildMultiPoolerContainerPorts(),
				Resources:     corev1.ResourceRequirements{},
				RestartPolicy: &sidecarRestartPolicy,
				SecurityContext: &corev1.SecurityContext{
					RunAsUser:    ptr.To(int64(999)),
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
				Env: []corev1.EnvVar{
					{
						Name: "POD_NAME",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.name",
							},
						},
					},
					connpoolAdminPasswordEnvVar(),
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      DataVolumeName,
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
			},
		},
		"custom multipooler image": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-shard"},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "proddb",
					TableGroupName: "orders",
					ShardName:      "1",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "global-topo:2379",
						RootPath:       "/multigres/global",
						Implementation: "etcd",
					},
					Images: multigresv1alpha1.ShardImages{
						MultiPooler: "custom/multipooler:v1.0.0",
					},
				},
			},
			poolSpec: multigresv1alpha1.PoolSpec{
				Cells: []multigresv1alpha1.CellName{"zone2"},
			},
			cellName: "zone2",
			want: corev1.Container{
				Name:  "multipooler",
				Image: "custom/multipooler:v1.0.0",
				Args: []string{
					"multipooler",
					"--http-port=15200",
					"--grpc-port=15270",
					"--pooler-dir=" + PoolerDirMountPath,
					"--socket-file=/var/lib/pooler/pg_sockets/.s.PGSQL.5432",
					"--service-map=grpc-pooler",
					"--topo-global-server-addresses=global-topo:2379",
					"--topo-global-root=/multigres/global",
					"--cell=zone2",
					"--database=proddb",
					"--table-group=orders",
					"--shard=1",
					"--service-id=$(POD_NAME)",
					"--pgctld-addr=localhost:15470",
					"--pg-port=5432",
					"--connpool-admin-password=$(CONNPOOL_ADMIN_PASSWORD)",
				},
				Ports:         buildMultiPoolerContainerPorts(),
				Resources:     corev1.ResourceRequirements{},
				RestartPolicy: &sidecarRestartPolicy,
				SecurityContext: &corev1.SecurityContext{
					RunAsUser:    ptr.To(int64(999)),
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
				Env: []corev1.EnvVar{
					{
						Name: "POD_NAME",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.name",
							},
						},
					},
					connpoolAdminPasswordEnvVar(),
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      DataVolumeName,
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
			},
		},
		"with resource requirements": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{Name: "resource-shard"},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "mydb",
					TableGroupName: "default",
					ShardName:      "0",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "global-topo:2379",
						RootPath:       "/multigres/global",
						Implementation: "etcd",
					},
				},
			},
			poolSpec: multigresv1alpha1.PoolSpec{
				Cells: []multigresv1alpha1.CellName{"zone1"},
				Multipooler: multigresv1alpha1.ContainerConfig{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			},
			cellName: "zone1",
			want: corev1.Container{
				Name:  "multipooler",
				Image: DefaultMultigresImage,
				Args: []string{
					"multipooler",
					"--http-port=15200",
					"--grpc-port=15270",
					"--pooler-dir=" + PoolerDirMountPath,
					"--socket-file=/var/lib/pooler/pg_sockets/.s.PGSQL.5432",
					"--service-map=grpc-pooler",
					"--topo-global-server-addresses=global-topo:2379",
					"--topo-global-root=/multigres/global",
					"--cell=zone1",
					"--database=mydb",
					"--table-group=default",
					"--shard=0",
					"--service-id=$(POD_NAME)",
					"--pgctld-addr=localhost:15470",
					"--pg-port=5432",
					"--connpool-admin-password=$(CONNPOOL_ADMIN_PASSWORD)",
				},
				Ports: buildMultiPoolerContainerPorts(),
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
				},
				RestartPolicy: &sidecarRestartPolicy,
				SecurityContext: &corev1.SecurityContext{
					RunAsUser:    ptr.To(int64(999)),
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
				Env: []corev1.EnvVar{
					{
						Name: "POD_NAME",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.name",
							},
						},
					},
					connpoolAdminPasswordEnvVar(),
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      DataVolumeName,
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
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildMultiPoolerSidecar(tc.shard, tc.poolSpec, "primary", tc.cellName)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildMultiPoolerSidecar() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildPgctldInitContainer(t *testing.T) {
	tests := map[string]struct {
		shard *multigresv1alpha1.Shard
		want  corev1.Container
	}{
		"default pgctld init container": {
			shard: &multigresv1alpha1.Shard{
				Spec: multigresv1alpha1.ShardSpec{},
			},
			want: corev1.Container{
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
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildPgctldInitContainer(tc.shard)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildPgctldInitContainer() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildMultiOrchContainer(t *testing.T) {
	tests := map[string]struct {
		shard    *multigresv1alpha1.Shard
		cellName string
		want     corev1.Container
	}{
		"default multiorch container": {
			shard: &multigresv1alpha1.Shard{
				Spec: multigresv1alpha1.ShardSpec{
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "global-topo:2379",
						RootPath:       "/multigres/global",
						Implementation: "etcd",
					},
				},
			},
			cellName: "zone1",
			want: corev1.Container{
				Name:  "multiorch",
				Image: DefaultMultigresImage,
				Args: []string{
					"multiorch",
					"--http-port=15300",
					"--grpc-port=15370",
					"--topo-global-server-addresses=global-topo:2379",
					"--topo-global-root=/multigres/global",
					"--cell=zone1",
					"--watch-targets=postgres",
					"--cluster-metadata-refresh-interval=500ms",
					"--pooler-health-check-interval=500ms",
					"--recovery-cycle-interval=500ms",
				},
				Ports:     buildMultiOrchContainerPorts(),
				Resources: corev1.ResourceRequirements{},
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
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildMultiOrchContainer(tc.shard, tc.cellName)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildMultiOrchContainer() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildPgctldVolume(t *testing.T) {
	want := corev1.Volume{
		Name: PgctldVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}

	got := buildPgctldVolume()

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("buildPgctldVolume() mismatch (-want +got):\n%s", diff)
	}
}

// otelShard returns a Shard with observability configured for testing the
// OTEL env var injection branch in each container builder.
func otelShard() *multigresv1alpha1.Shard {
	return &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{Name: "otel-shard"},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "testdb",
			TableGroupName: "default",
			ShardName:      "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				Address:        "global-topo:2379",
				RootPath:       "/multigres/global",
				Implementation: "etcd",
			},
			Observability: &multigresv1alpha1.ObservabilityConfig{
				OTLPEndpoint: "http://tempo:4318",
			},
		},
	}
}

func TestBuildPostgresContainer_WithObservability(t *testing.T) {
	c := buildPostgresContainer(otelShard(), multigresv1alpha1.PoolSpec{})
	assertContainsOTELEnvVar(t, c.Env, "buildPostgresContainer")
}

func TestBuildPgctldContainer(t *testing.T) {
	t.Run("default image", func(t *testing.T) {
		shard := &multigresv1alpha1.Shard{Spec: multigresv1alpha1.ShardSpec{}}
		c := buildPgctldContainer(shard, multigresv1alpha1.PoolSpec{})
		if c.Image != DefaultPgctldImage {
			t.Errorf("Image = %q, want %q", c.Image, DefaultPgctldImage)
		}
		if c.Command[0] != "/usr/local/bin/pgctld" {
			t.Errorf("Command = %v, want /usr/local/bin/pgctld", c.Command)
		}
	})

	t.Run("custom image", func(t *testing.T) {
		shard := &multigresv1alpha1.Shard{
			Spec: multigresv1alpha1.ShardSpec{
				Images: multigresv1alpha1.ShardImages{Postgres: "custom/pgctld:v1"},
			},
		}
		c := buildPgctldContainer(shard, multigresv1alpha1.PoolSpec{})
		if c.Image != "custom/pgctld:v1" {
			t.Errorf("Image = %q, want %q", c.Image, "custom/pgctld:v1")
		}
	})

	t.Run("with observability", func(t *testing.T) {
		c := buildPgctldContainer(otelShard(), multigresv1alpha1.PoolSpec{})
		assertContainsOTELEnvVar(t, c.Env, "buildPgctldContainer")
	})

	t.Run("with backup filesystem", func(t *testing.T) {
		shard := &multigresv1alpha1.Shard{
			Spec: multigresv1alpha1.ShardSpec{
				Backup: &multigresv1alpha1.BackupConfig{
					Type: multigresv1alpha1.BackupTypeFilesystem,
					Filesystem: &multigresv1alpha1.FilesystemBackupConfig{
						Path: "/custom-backups",
					},
				},
			},
		}
		c := buildPgctldContainer(shard, multigresv1alpha1.PoolSpec{})
		assertContainsFlag(t, c.Args, "--backup-type=filesystem")
		assertContainsFlag(t, c.Args, "--backup-path=/custom-backups")
	})

	t.Run("with backup s3", func(t *testing.T) {
		shard := &multigresv1alpha1.Shard{
			Spec: multigresv1alpha1.ShardSpec{
				Backup: &multigresv1alpha1.BackupConfig{
					Type: multigresv1alpha1.BackupTypeS3,
					S3: &multigresv1alpha1.S3BackupConfig{
						Bucket: "my-bucket",
						Region: "us-west-2",
					},
				},
			},
		}
		c := buildPgctldContainer(shard, multigresv1alpha1.PoolSpec{})
		assertContainsFlag(t, c.Args, "--backup-type=s3")
		assertContainsFlag(t, c.Args, "--backup-path=/backups") // always required by upstream
		assertContainsFlag(t, c.Args, "--backup-bucket=my-bucket")
		assertContainsFlag(t, c.Args, "--backup-region=us-west-2")
	})

	t.Run("with backup s3 all flags", func(t *testing.T) {
		shard := &multigresv1alpha1.Shard{
			Spec: multigresv1alpha1.ShardSpec{
				Backup: &multigresv1alpha1.BackupConfig{
					Type: multigresv1alpha1.BackupTypeS3,
					S3: &multigresv1alpha1.S3BackupConfig{
						Bucket:            "my-bucket",
						Region:            "us-west-2",
						Endpoint:          "https://minio.local:9000",
						KeyPrefix:         "prod/backups",
						UseEnvCredentials: true,
					},
				},
			},
		}
		c := buildPgctldContainer(shard, multigresv1alpha1.PoolSpec{})
		assertContainsFlag(t, c.Args, "--backup-type=s3")
		assertContainsFlag(t, c.Args, "--backup-path=/backups")
		assertContainsFlag(t, c.Args, "--backup-bucket=my-bucket")
		assertContainsFlag(t, c.Args, "--backup-region=us-west-2")
		assertContainsFlag(t, c.Args, "--backup-endpoint=https://minio.local:9000")
		assertContainsFlag(t, c.Args, "--backup-key-prefix=prod/backups")
		assertContainsFlag(t, c.Args, "--backup-use-env-credentials")
	})

	t.Run("s3 credentials secret injects AWS env vars", func(t *testing.T) {
		shard := &multigresv1alpha1.Shard{
			Spec: multigresv1alpha1.ShardSpec{
				Backup: &multigresv1alpha1.BackupConfig{
					Type: multigresv1alpha1.BackupTypeS3,
					S3: &multigresv1alpha1.S3BackupConfig{
						Bucket:            "my-bucket",
						Region:            "us-west-2",
						CredentialsSecret: "aws-creds",
					},
				},
			},
		}
		c := buildPgctldContainer(shard, multigresv1alpha1.PoolSpec{})
		assertContainsEnvVar(t, c.Env, "AWS_REGION")
		assertContainsEnvVar(t, c.Env, "AWS_ACCESS_KEY_ID")
		assertContainsEnvVar(t, c.Env, "AWS_SECRET_ACCESS_KEY")
	})

	t.Run("filesystem backup does not inject AWS env vars", func(t *testing.T) {
		shard := &multigresv1alpha1.Shard{
			Spec: multigresv1alpha1.ShardSpec{
				Backup: &multigresv1alpha1.BackupConfig{
					Type: multigresv1alpha1.BackupTypeFilesystem,
				},
			},
		}
		c := buildPgctldContainer(shard, multigresv1alpha1.PoolSpec{})
		assertNotContainsEnvVar(t, c.Env, "AWS_REGION")
		assertNotContainsEnvVar(t, c.Env, "AWS_ACCESS_KEY_ID")
	})
}

func TestS3EnvVars(t *testing.T) {
	t.Run("nil backup returns nil", func(t *testing.T) {
		got := s3EnvVars(nil)
		if got != nil {
			t.Errorf("s3EnvVars(nil) = %v, want nil", got)
		}
	})

	t.Run("filesystem backup returns nil", func(t *testing.T) {
		got := s3EnvVars(&multigresv1alpha1.BackupConfig{
			Type: multigresv1alpha1.BackupTypeFilesystem,
		})
		if got != nil {
			t.Errorf("s3EnvVars(filesystem) = %v, want nil", got)
		}
	})

	t.Run("s3 with region only", func(t *testing.T) {
		got := s3EnvVars(&multigresv1alpha1.BackupConfig{
			Type: multigresv1alpha1.BackupTypeS3,
			S3: &multigresv1alpha1.S3BackupConfig{
				Bucket: "b",
				Region: "eu-west-1",
			},
		})
		if len(got) != 1 || got[0].Name != "AWS_REGION" || got[0].Value != "eu-west-1" {
			t.Errorf("s3EnvVars(region-only) = %v, want [{AWS_REGION eu-west-1}]", got)
		}
	})

	t.Run("s3 with credentials secret", func(t *testing.T) {
		got := s3EnvVars(&multigresv1alpha1.BackupConfig{
			Type: multigresv1alpha1.BackupTypeS3,
			S3: &multigresv1alpha1.S3BackupConfig{
				Bucket:            "b",
				Region:            "us-east-1",
				CredentialsSecret: "my-secret",
			},
		})
		if len(got) != 3 {
			t.Fatalf("s3EnvVars(full) returned %d vars, want 3", len(got))
		}
		assertContainsEnvVar(t, got, "AWS_REGION")
		assertContainsEnvVar(t, got, "AWS_ACCESS_KEY_ID")
		assertContainsEnvVar(t, got, "AWS_SECRET_ACCESS_KEY")

		// Verify it references the correct secret
		for _, e := range got {
			if e.Name == "AWS_ACCESS_KEY_ID" {
				if e.ValueFrom == nil || e.ValueFrom.SecretKeyRef == nil {
					t.Fatal("AWS_ACCESS_KEY_ID missing SecretKeyRef")
				}
				if e.ValueFrom.SecretKeyRef.Name != "my-secret" {
					t.Errorf("AWS_ACCESS_KEY_ID secret = %q, want %q",
						e.ValueFrom.SecretKeyRef.Name, "my-secret")
				}
			}
		}
	})

	t.Run("s3 with no region", func(t *testing.T) {
		got := s3EnvVars(&multigresv1alpha1.BackupConfig{
			Type: multigresv1alpha1.BackupTypeS3,
			S3: &multigresv1alpha1.S3BackupConfig{
				Bucket:            "b",
				CredentialsSecret: "my-secret",
			},
		})
		// Should have 2 vars: KEY_ID and SECRET_KEY, but no REGION
		if len(got) != 2 {
			t.Fatalf("s3EnvVars(no-region) returned %d vars, want 2", len(got))
		}
		assertNotContainsEnvVar(t, got, "AWS_REGION")
		assertContainsEnvVar(t, got, "AWS_ACCESS_KEY_ID")
	})
}

func TestBuildSharedBackupVolume_S3(t *testing.T) {
	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"multigres.com/cluster": "test-cluster"},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "postgres",
			TableGroupName: "default",
			ShardName:      "0-inf",
			Backup: &multigresv1alpha1.BackupConfig{
				Type: multigresv1alpha1.BackupTypeS3,
				S3: &multigresv1alpha1.S3BackupConfig{
					Bucket: "my-bucket",
					Region: "us-east-1",
				},
			},
		},
	}
	vol := buildSharedBackupVolume(shard, "zone-1")

	if vol.Name != BackupVolumeName {
		t.Errorf("volume name = %q, want %q", vol.Name, BackupVolumeName)
	}
	if vol.VolumeSource.EmptyDir == nil {
		t.Error("S3 backup volume should use EmptyDir, got PVC or other source")
	}
	if vol.VolumeSource.PersistentVolumeClaim != nil {
		t.Error("S3 backup volume should NOT use PersistentVolumeClaim")
	}
}

func assertContainsFlag(t *testing.T, args []string, want string) {
	t.Helper()
	for _, arg := range args {
		if arg == want {
			return
		}
	}
	t.Errorf("args %v does not contain flag %q", args, want)
}

func TestBuildMultiPoolerSidecar_WithObservability(t *testing.T) {
	c := buildMultiPoolerSidecar(otelShard(), multigresv1alpha1.PoolSpec{}, "primary", "zone1")
	assertContainsOTELEnvVar(t, c.Env, "buildMultiPoolerSidecar")
}

func TestBuildMultiOrchContainer_WithObservability(t *testing.T) {
	c := buildMultiOrchContainer(otelShard(), "zone1")
	assertContainsOTELEnvVar(t, c.Env, "buildMultiOrchContainer")
}

func assertContainsOTELEnvVar(t *testing.T, envVars []corev1.EnvVar, fnName string) {
	t.Helper()
	for _, e := range envVars {
		if e.Name == "OTEL_EXPORTER_OTLP_ENDPOINT" {
			return
		}
	}
	t.Errorf("%s: expected OTEL_EXPORTER_OTLP_ENDPOINT env var, got none", fnName)
}

func assertContainsEnvVar(t *testing.T, envVars []corev1.EnvVar, name string) {
	t.Helper()
	for _, e := range envVars {
		if e.Name == name {
			return
		}
	}
	t.Errorf("expected env var %q, got none", name)
}

func assertNotContainsEnvVar(t *testing.T, envVars []corev1.EnvVar, name string) {
	t.Helper()
	for _, e := range envVars {
		if e.Name == name {
			t.Errorf("unexpected env var %q found", name)
			return
		}
	}
}

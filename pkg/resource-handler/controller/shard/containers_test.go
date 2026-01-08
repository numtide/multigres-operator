package shard

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
				Resources: corev1.ResourceRequirements{},
				Env: []corev1.EnvVar{
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
				Resources: corev1.ResourceRequirements{},
				Env: []corev1.EnvVar{
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
						Implementation: "etcd2",
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
					"--http-port", "15200",
					"--grpc-port", "15270",
					"--pooler-dir", PoolerDirMountPath,
					"--socket-file", SocketDirMountPath + "/.s.PGSQL.5432",
					"--service-map", "grpc-pooler",
					"--topo-global-server-addresses", "global-topo:2379",
					"--topo-global-root", "/multigres/global",
					"--cell", "zone1",
					"--database", "testdb",
					"--table-group", "default",
					"--shard", "0",
					"--service-id", "$(POD_NAME)",
					"--pgctld-addr", "localhost:15470",
					"--pg-port", "5432",
				},
				Ports:         buildMultiPoolerContainerPorts(),
				Resources:     corev1.ResourceRequirements{},
				RestartPolicy: &sidecarRestartPolicy,
				SecurityContext: &corev1.SecurityContext{
					RunAsUser:    ptr.To(int64(999)),
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
						Implementation: "etcd2",
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
					"--http-port", "15200",
					"--grpc-port", "15270",
					"--pooler-dir", PoolerDirMountPath,
					"--socket-file", SocketDirMountPath + "/.s.PGSQL.5432",
					"--service-map", "grpc-pooler",
					"--topo-global-server-addresses", "global-topo:2379",
					"--topo-global-root", "/multigres/global",
					"--cell", "zone2",
					"--database", "proddb",
					"--table-group", "orders",
					"--shard", "1",
					"--service-id", "$(POD_NAME)",
					"--pgctld-addr", "localhost:15470",
					"--pg-port", "5432",
				},
				Ports:         buildMultiPoolerContainerPorts(),
				Resources:     corev1.ResourceRequirements{},
				RestartPolicy: &sidecarRestartPolicy,
				SecurityContext: &corev1.SecurityContext{
					RunAsUser:    ptr.To(int64(999)),
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
						Implementation: "etcd2",
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
					"--http-port", "15200",
					"--grpc-port", "15270",
					"--pooler-dir", PoolerDirMountPath,
					"--socket-file", SocketDirMountPath + "/.s.PGSQL.5432",
					"--service-map", "grpc-pooler",
					"--topo-global-server-addresses", "global-topo:2379",
					"--topo-global-root", "/multigres/global",
					"--cell", "zone1",
					"--database", "mydb",
					"--table-group", "default",
					"--shard", "0",
					"--service-id", "$(POD_NAME)",
					"--pgctld-addr", "localhost:15470",
					"--pg-port", "5432",
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
					"cp /usr/local/bin/pgctld /shared/pgctld",
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
						Implementation: "etcd2",
					},
				},
			},
			cellName: "zone1",
			want: corev1.Container{
				Name:  "multiorch",
				Image: DefaultMultigresImage,
				Args: []string{
					"multiorch",
					"--http-port", "15300",
					"--grpc-port", "15370",
					"--topo-global-server-addresses", "global-topo:2379",
					"--topo-global-root", "/multigres/global",
					"--cell", "zone1",
					"--watch-targets", "postgres",
					"--cluster-metadata-refresh-interval", "500ms",
					"--pooler-health-check-interval", "500ms",
					"--recovery-cycle-interval", "500ms",
				},
				Ports:     buildMultiOrchContainerPorts(),
				Resources: corev1.ResourceRequirements{},
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


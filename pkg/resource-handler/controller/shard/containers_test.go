package shard

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
				Name:      "postgres",
				Image:     DefaultPostgresImage,
				Resources: corev1.ResourceRequirements{},
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
				Name:      "postgres",
				Image:     "postgres:16",
				Resources: corev1.ResourceRequirements{},
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
				Name:  "postgres",
				Image: DefaultPostgresImage,
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
				},
			},
			poolSpec: multigresv1alpha1.PoolSpec{
				Cells: []multigresv1alpha1.CellName{"zone1"},
			},
			cellName: "zone1",
			want: corev1.Container{
				Name:  "multipooler",
				Image: DefaultMultiPoolerImage,
				Args: []string{
					"--http-port", "15200",
					"--grpc-port", "15270",
					"--topo-implementation", "etcd2",
					"--cell", "zone1",
					"--database", "testdb",
					"--table-group", "default",
					"--service-id", "test-shard-pool-primary",
					"--pgctld-addr", "localhost:15470",
					"--pg-port", "5432",
				},
				Ports:         buildMultiPoolerContainerPorts(),
				Resources:     corev1.ResourceRequirements{},
				RestartPolicy: &sidecarRestartPolicy,
			},
		},
		"custom multipooler image": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-shard"},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "proddb",
					TableGroupName: "orders",
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
					"--http-port", "15200",
					"--grpc-port", "15270",
					"--topo-implementation", "etcd2",
					"--cell", "zone2",
					"--database", "proddb",
					"--table-group", "orders",
					"--service-id", "custom-shard-pool-primary",
					"--pgctld-addr", "localhost:15470",
					"--pg-port", "5432",
				},
				Ports:         buildMultiPoolerContainerPorts(),
				Resources:     corev1.ResourceRequirements{},
				RestartPolicy: &sidecarRestartPolicy,
			},
		},
		"with resource requirements": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{Name: "resource-shard"},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "mydb",
					TableGroupName: "default",
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
				Image: DefaultMultiPoolerImage,
				Args: []string{
					"--http-port", "15200",
					"--grpc-port", "15270",
					"--topo-implementation", "etcd2",
					"--cell", "zone1",
					"--database", "mydb",
					"--table-group", "default",
					"--service-id", "resource-shard-pool-primary",
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
				Name:  "pgctld-init",
				Image: DefaultPgctldImage,
				Command: []string{
					"sh",
					"-c",
					"cp /pgctld /shared/pgctld && chmod +x /shared/pgctld",
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
		shard *multigresv1alpha1.Shard
		want  corev1.Container
	}{
		"default multiorch container": {
			shard: &multigresv1alpha1.Shard{
				Spec: multigresv1alpha1.ShardSpec{},
			},
			want: corev1.Container{
				Name:  "multiorch",
				Image: DefaultMultiOrchImage,
				Args: []string{
					"--http-port", "15300",
					"--grpc-port", "15370",
					"--topo-implementation", "etcd2",
				},
				Ports:     buildMultiOrchContainerPorts(),
				Resources: corev1.ResourceRequirements{},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildMultiOrchContainer(tc.shard)

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

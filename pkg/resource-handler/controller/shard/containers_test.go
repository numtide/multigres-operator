package shard

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func TestBuildPostgresContainer(t *testing.T) {
	tests := map[string]struct {
		shard *multigresv1alpha1.Shard
		pool  multigresv1alpha1.ShardPoolSpec
		want  corev1.Container
	}{
		"default postgres image with no resources": {
			shard: &multigresv1alpha1.Shard{
				Spec: multigresv1alpha1.ShardSpec{},
			},
			pool: multigresv1alpha1.ShardPoolSpec{},
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
					Images: multigresv1alpha1.ShardImagesSpec{
						Postgres: "postgres:16",
					},
				},
			},
			pool: multigresv1alpha1.ShardPoolSpec{},
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
			pool: multigresv1alpha1.ShardPoolSpec{
				Postgres: multigresv1alpha1.PostgresSpec{
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
			got := buildPostgresContainer(tc.shard, tc.pool)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildPostgresContainer() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildMultiPoolerSidecar(t *testing.T) {
	tests := map[string]struct {
		shard *multigresv1alpha1.Shard
		pool  multigresv1alpha1.ShardPoolSpec
		want  corev1.Container
	}{
		"default multipooler image with no resources": {
			shard: &multigresv1alpha1.Shard{
				Spec: multigresv1alpha1.ShardSpec{},
			},
			pool: multigresv1alpha1.ShardPoolSpec{},
			want: corev1.Container{
				Name:          "multipooler",
				Image:         DefaultMultiPoolerImage,
				Ports:         buildMultiPoolerContainerPorts(),
				Resources:     corev1.ResourceRequirements{},
				RestartPolicy: &sidecarRestartPolicy,
			},
		},
		"custom multipooler image": {
			shard: &multigresv1alpha1.Shard{
				Spec: multigresv1alpha1.ShardSpec{
					Images: multigresv1alpha1.ShardImagesSpec{
						MultiPooler: "custom/multipooler:v1.0.0",
					},
				},
			},
			pool: multigresv1alpha1.ShardPoolSpec{},
			want: corev1.Container{
				Name:          "multipooler",
				Image:         "custom/multipooler:v1.0.0",
				Ports:         buildMultiPoolerContainerPorts(),
				Resources:     corev1.ResourceRequirements{},
				RestartPolicy: &sidecarRestartPolicy,
			},
		},
		"with resource requirements": {
			shard: &multigresv1alpha1.Shard{
				Spec: multigresv1alpha1.ShardSpec{},
			},
			pool: multigresv1alpha1.ShardPoolSpec{
				MultiPooler: multigresv1alpha1.MultiPoolerSpec{
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
			want: corev1.Container{
				Name:  "multipooler",
				Image: DefaultMultiPoolerImage,
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
			got := buildMultiPoolerSidecar(tc.shard, tc.pool)

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
				Ports: buildMultiOrchContainerPorts(),
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

func TestBuildDataVolumeClaimTemplate(t *testing.T) {
	tests := map[string]struct {
		pool multigresv1alpha1.ShardPoolSpec
		want corev1.PersistentVolumeClaim
	}{
		"with storage class and size": {
			pool: multigresv1alpha1.ShardPoolSpec{
				DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
					StorageClassName: ptr.To("fast-ssd"),
					AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			},
			want: corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: ptr.To("fast-ssd"),
					AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			},
		},
		"minimal spec": {
			pool: multigresv1alpha1.ShardPoolSpec{
				DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{},
			},
			want: corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildDataVolumeClaimTemplate(tc.pool)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildDataVolumeClaimTemplate() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

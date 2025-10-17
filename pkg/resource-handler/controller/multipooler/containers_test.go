package multipooler

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func TestBuildMultiPoolerContainer(t *testing.T) {
	tests := map[string]struct {
		multipooler *multigresv1alpha1.MultiPooler
		want        corev1.Container
	}{
		"default image and no resources": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			want: corev1.Container{
				Name:      "multipooler",
				Image:     DefaultMultiPoolerImage,
				Resources: corev1.ResourceRequirements{},
				Ports:     buildContainerPorts(&multigresv1alpha1.MultiPooler{}),
			},
		},
		"custom image and resources": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{
					MultiPooler: multigresv1alpha1.MultiPoolerContainerSpec{
						Image: "custom/multipooler:v1.0.0",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				},
			},
			want: corev1.Container{
				Name:  "multipooler",
				Image: "custom/multipooler:v1.0.0",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
				Ports: buildContainerPorts(&multigresv1alpha1.MultiPooler{}),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildMultiPoolerContainer(tc.multipooler)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildMultiPoolerContainer() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildPgctldInitContainer(t *testing.T) {
	tests := map[string]struct {
		multipooler *multigresv1alpha1.MultiPooler
		want        corev1.Container
	}{
		"default image and no resources": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			want: corev1.Container{
				Name:      "pgctld-init",
				Image:     DefaultPgctldImage,
				Resources: corev1.ResourceRequirements{},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      PgctldVolumeName,
						MountPath: "/shared",
					},
				},
			},
		},
		"custom image and resources": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{
					Pgctld: multigresv1alpha1.MultiPoolerContainerSpec{
						Image: "custom/pgctld:v2.0.0",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("64Mi"),
							},
						},
					},
				},
			},
			want: corev1.Container{
				Name:  "pgctld-init",
				Image: "custom/pgctld:v2.0.0",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("50m"),
						corev1.ResourceMemory: resource.MustParse("64Mi"),
					},
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
			got := buildPgctldInitContainer(tc.multipooler)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildPgctldInitContainer() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildPostgresContainer(t *testing.T) {
	tests := map[string]struct {
		multipooler *multigresv1alpha1.MultiPooler
		want        corev1.Container
	}{
		"default image and no resources": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
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
						MountPath: "/usr/local/bin/pgctld",
					},
				},
			},
		},
		"custom image and resources": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{
					Postgres: multigresv1alpha1.MultiPoolerContainerSpec{
						Image: "postgres:16",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("2000m"),
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
				},
			},
			want: corev1.Container{
				Name:  "postgres",
				Image: "postgres:16",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2000m"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
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
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildPostgresContainer(tc.multipooler)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildPostgresContainer() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

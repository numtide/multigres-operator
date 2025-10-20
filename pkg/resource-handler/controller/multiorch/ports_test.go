package multiorch

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func TestBuildContainerPorts(t *testing.T) {
	tests := map[string]struct {
		multiorch *multigresv1alpha1.MultiOrch
		want      []corev1.ContainerPort
	}{
		"default ports": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multiorch",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiOrchSpec{},
			},
			want: []corev1.ContainerPort{
				{
					Name:          "http",
					ContainerPort: 15300,
					Protocol:      corev1.ProtocolTCP,
				},
				{
					Name:          "grpc",
					ContainerPort: 15370,
					Protocol:      corev1.ProtocolTCP,
				},
			},
		},
		"custom ports": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multiorch",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiOrchSpec{
					HTTPPort: 8080,
					GRPCPort: 9090,
				},
			},
			want: []corev1.ContainerPort{
				{
					Name:          "http",
					ContainerPort: 8080,
					Protocol:      corev1.ProtocolTCP,
				},
				{
					Name:          "grpc",
					ContainerPort: 9090,
					Protocol:      corev1.ProtocolTCP,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildContainerPorts(tc.multiorch)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildContainerPorts() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildServicePorts(t *testing.T) {
	tests := map[string]struct {
		multiorch *multigresv1alpha1.MultiOrch
		want      []corev1.ServicePort
	}{
		"default ports": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multiorch",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiOrchSpec{},
			},
			want: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       15300,
					TargetPort: intstr.FromString("http"),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "grpc",
					Port:       15370,
					TargetPort: intstr.FromString("grpc"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
		"custom ports": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multiorch",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiOrchSpec{
					HTTPPort: 8080,
					GRPCPort: 9090,
				},
			},
			want: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8080,
					TargetPort: intstr.FromString("http"),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "grpc",
					Port:       9090,
					TargetPort: intstr.FromString("grpc"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildServicePorts(tc.multiorch)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildServicePorts() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

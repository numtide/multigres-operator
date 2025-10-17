package multipooler

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
		multipooler *multigresv1alpha1.MultiPooler
		want        []corev1.ContainerPort
	}{
		"default ports": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			want: []corev1.ContainerPort{
				{
					Name:          "http",
					ContainerPort: 15200,
					Protocol:      corev1.ProtocolTCP,
				},
				{
					Name:          "grpc",
					ContainerPort: 15270,
					Protocol:      corev1.ProtocolTCP,
				},
				{
					Name:          "postgres",
					ContainerPort: 5432,
					Protocol:      corev1.ProtocolTCP,
				},
			},
		},
		"custom ports": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{
					HTTPPort:     8080,
					GRPCPort:     9090,
					PostgresPort: 5433,
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
				{
					Name:          "postgres",
					ContainerPort: 5433,
					Protocol:      corev1.ProtocolTCP,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildContainerPorts(tc.multipooler)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildContainerPorts() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildHeadlessServicePorts(t *testing.T) {
	tests := map[string]struct {
		multipooler *multigresv1alpha1.MultiPooler
		want        []corev1.ServicePort
	}{
		"default ports": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			want: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       15200,
					TargetPort: intstr.FromString("http"),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "grpc",
					Port:       15270,
					TargetPort: intstr.FromString("grpc"),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "postgres",
					Port:       5432,
					TargetPort: intstr.FromString("postgres"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
		"custom ports": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{
					HTTPPort:     8080,
					GRPCPort:     9090,
					PostgresPort: 5433,
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
				{
					Name:       "postgres",
					Port:       5433,
					TargetPort: intstr.FromString("postgres"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildHeadlessServicePorts(tc.multipooler)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildHeadlessServicePorts() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

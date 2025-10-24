package multigateway

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
		mg   *multigresv1alpha1.MultiGateway
		want []corev1.ContainerPort
	}{
		"default ports": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{},
			},
			want: []corev1.ContainerPort{
				{
					Name:          "http",
					ContainerPort: 15100,
					Protocol:      corev1.ProtocolTCP,
				},
				{
					Name:          "grpc",
					ContainerPort: 15170,
					Protocol:      corev1.ProtocolTCP,
				},
				{
					Name:          "postgres",
					ContainerPort: 15432,
					Protocol:      corev1.ProtocolTCP,
				},
			},
		},
		"custom ports": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{
					HTTPPort:     1,
					GRPCPort:     2,
					PostgresPort: 3,
				},
			},
			want: []corev1.ContainerPort{
				{
					Name:          "http",
					ContainerPort: 1,
					Protocol:      corev1.ProtocolTCP,
				},

				{
					Name:          "grpc",
					ContainerPort: 2,
					Protocol:      corev1.ProtocolTCP,
				},
				{
					Name:          "postgres",
					ContainerPort: 3,
					Protocol:      corev1.ProtocolTCP,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildContainerPorts(tc.mg)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildContainerPorts() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildServicePorts(t *testing.T) {
	tests := map[string]struct {
		mg   *multigresv1alpha1.MultiGateway
		want []corev1.ServicePort
	}{
		"default ports": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{},
			},
			want: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       15100,
					TargetPort: intstr.FromString("http"),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "grpc",
					Port:       15170,
					TargetPort: intstr.FromString("grpc"),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "postgres",
					Port:       15432,
					TargetPort: intstr.FromString("postgres"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
		"custom ports": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{
					HTTPPort:     1,
					GRPCPort:     2,
					PostgresPort: 3,
				},
			},
			want: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       1,
					TargetPort: intstr.FromString("http"),
					Protocol:   corev1.ProtocolTCP,
				},

				{
					Name:       "grpc",
					Port:       2,
					TargetPort: intstr.FromString("grpc"),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "postgres",
					Port:       3,
					TargetPort: intstr.FromString("postgres"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildServicePorts(tc.mg)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildServicePorts() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

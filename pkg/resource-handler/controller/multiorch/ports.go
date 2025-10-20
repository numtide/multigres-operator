package multiorch

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

const (
	// DefaultHTTPPort is the default port for HTTP traffic.
	DefaultHTTPPort int32 = 15300

	// DefaultGRPCPort is the default port for gRPC traffic.
	DefaultGRPCPort int32 = 15370
)

// buildContainerPorts creates the port definitions for the multiorch container.
// Returns ports for HTTP and gRPC traffic.
func buildContainerPorts(multiorch *multigresv1alpha1.MultiOrch) []corev1.ContainerPort {
	httpPort := DefaultHTTPPort
	if multiorch.Spec.HTTPPort != 0 {
		httpPort = multiorch.Spec.HTTPPort
	}

	grpcPort := DefaultGRPCPort
	if multiorch.Spec.GRPCPort != 0 {
		grpcPort = multiorch.Spec.GRPCPort
	}

	return []corev1.ContainerPort{
		{
			Name:          "http",
			ContainerPort: httpPort,
			Protocol:      corev1.ProtocolTCP,
		},
		{
			Name:          "grpc",
			ContainerPort: grpcPort,
			Protocol:      corev1.ProtocolTCP,
		},
	}
}

// buildServicePorts creates service ports for the MultiOrch service.
func buildServicePorts(multiorch *multigresv1alpha1.MultiOrch) []corev1.ServicePort {
	httpPort := DefaultHTTPPort
	if multiorch.Spec.HTTPPort != 0 {
		httpPort = multiorch.Spec.HTTPPort
	}

	grpcPort := DefaultGRPCPort
	if multiorch.Spec.GRPCPort != 0 {
		grpcPort = multiorch.Spec.GRPCPort
	}

	return []corev1.ServicePort{
		{
			Name:       "http",
			Port:       httpPort,
			TargetPort: intstr.FromString("http"),
			Protocol:   corev1.ProtocolTCP,
		},
		{
			Name:       "grpc",
			Port:       grpcPort,
			TargetPort: intstr.FromString("grpc"),
			Protocol:   corev1.ProtocolTCP,
		},
	}
}

package multigateway

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

const (
	// HTTPPort is the default port for HTTP connections.
	HTTPPort int32 = 15100

	// GRPCPort is the default port for GRPC connections.
	GRPCPort int32 = 15170

	// PostgresPort is the default port for database connections.
	PostgresPort int32 = 15432
)

// buildContainerPorts creates the port definitions for the etcd container.
// Uses default ports since MultiGatewaySpec doesn't have port configuration yet.
func buildContainerPorts(mg *multigresv1alpha1.MultiGateway) []corev1.ContainerPort {
	httpPort := HTTPPort
	grpcPort := GRPCPort
	postgresPort := PostgresPort

	if mg.Spec.HTTPPort != 0 {
		httpPort = mg.Spec.HTTPPort
	}
	if mg.Spec.GRPCPort != 0 {
		grpcPort = mg.Spec.GRPCPort
	}
	if mg.Spec.PostgresPort != 0 {
		postgresPort = mg.Spec.PostgresPort
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
		{
			Name:          "postgres",
			ContainerPort: postgresPort,
			Protocol:      corev1.ProtocolTCP,
		},
	}
}

// buildClientServicePorts creates service ports for the client service.
// Only includes the client port for external access.
func buildClientServicePorts(mg *multigresv1alpha1.MultiGateway) []corev1.ServicePort {
	httpPort := HTTPPort
	grpcPort := GRPCPort
	postgresPort := PostgresPort

	if mg.Spec.HTTPPort != 0 {
		httpPort = mg.Spec.HTTPPort
	}
	if mg.Spec.GRPCPort != 0 {
		grpcPort = mg.Spec.GRPCPort
	}
	if mg.Spec.PostgresPort != 0 {
		postgresPort = mg.Spec.PostgresPort
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
			Protocol:   corev1.ProtocolTCP,
			TargetPort: intstr.FromString("grpc"),
		},
		{
			Name:       "postgres",
			Port:       postgresPort,
			Protocol:   corev1.ProtocolTCP,
			TargetPort: intstr.FromString("postgres"),
		},
	}
}

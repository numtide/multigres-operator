package multipooler

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

const (
	// DefaultHTTPPort is the default port for HTTP traffic.
	DefaultHTTPPort int32 = 15200

	// DefaultGRPCPort is the default port for gRPC traffic.
	DefaultGRPCPort int32 = 15270

	// DefaultPostgresPort is the default port for PostgreSQL protocol traffic.
	DefaultPostgresPort int32 = 5432
)

// buildContainerPorts creates the port definitions for the multipooler container.
// Returns ports for HTTP, gRPC, and PostgreSQL traffic.
func buildContainerPorts(multipooler *multigresv1alpha1.MultiPooler) []corev1.ContainerPort {
	httpPort := DefaultHTTPPort
	if multipooler.Spec.HTTPPort != 0 {
		httpPort = multipooler.Spec.HTTPPort
	}

	grpcPort := DefaultGRPCPort
	if multipooler.Spec.GRPCPort != 0 {
		grpcPort = multipooler.Spec.GRPCPort
	}

	postgresPort := DefaultPostgresPort
	if multipooler.Spec.PostgresPort != 0 {
		postgresPort = multipooler.Spec.PostgresPort
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

// buildHeadlessServicePorts creates service ports for the headless service.
// Includes HTTP, gRPC, and PostgreSQL ports for StatefulSet pod discovery.
func buildHeadlessServicePorts(multipooler *multigresv1alpha1.MultiPooler) []corev1.ServicePort {
	httpPort := DefaultHTTPPort
	if multipooler.Spec.HTTPPort != 0 {
		httpPort = multipooler.Spec.HTTPPort
	}

	grpcPort := DefaultGRPCPort
	if multipooler.Spec.GRPCPort != 0 {
		grpcPort = multipooler.Spec.GRPCPort
	}

	postgresPort := DefaultPostgresPort
	if multipooler.Spec.PostgresPort != 0 {
		postgresPort = multipooler.Spec.PostgresPort
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
		{
			Name:       "postgres",
			Port:       postgresPort,
			TargetPort: intstr.FromString("postgres"),
			Protocol:   corev1.ProtocolTCP,
		},
	}
}

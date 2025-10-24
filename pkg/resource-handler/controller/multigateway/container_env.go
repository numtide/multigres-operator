package multigateway

import (
	corev1 "k8s.io/api/core/v1"
)

// buildContainerEnv constructs all environment variables for the MultiGateway
// container.
func buildContainerEnv() []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		{
			// TODO: get etcd endpoints and forward them to MultiGateway
			Name:  "ETCD_ENDPOINTS",
			Value: "",
		},
		{
			// TODO: is there an env var for HTTP port?
			Name:  "HTTP_PORT",
			Value: "",
		},
		{
			// TODO: is there an env var for GRPC port?
			Name:  "GRPC_PORT",
			Value: "",
		},
		{
			// TODO: is there an env var for Postgres port?
			Name:  "POSTGRES_PORT",
			Value: "",
		},
	}

	return envVars
}

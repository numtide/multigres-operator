package resolver

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// DefaultEtcdReplicas is the default number of replicas for the managed Etcd cluster if not specified.
	DefaultEtcdReplicas int32 = 3

	// DefaultAdminReplicas is the default number of replicas for the MultiAdmin deployment if not specified.
	DefaultAdminReplicas int32 = 1

	// FallbackCoreTemplate is the name of the template to look for if no specific CoreTemplate is referenced.
	FallbackCoreTemplate = "default"

	// FallbackCellTemplate is the name of the template to look for if no specific CellTemplate is referenced.
	FallbackCellTemplate = "default"

	// FallbackShardTemplate is the name of the template to look for if no specific ShardTemplate is referenced.
	FallbackShardTemplate = "default"

	// DefaultSystemDatabaseName is the name of the mandatory system database.
	DefaultSystemDatabaseName = "postgres"

	// DefaultSystemTableGroupName is the name of the mandatory default table group.
	DefaultSystemTableGroupName = "default"

	// DefaultPostgresImage is the default container image used for PostgreSQL instances.
	DefaultPostgresImage = "ghcr.io/multigres/pgctld:main"

	// DefaultEtcdImage is the default container image used for the managed Etcd cluster.
	DefaultEtcdImage = "gcr.io/etcd-development/etcd:v3.6.7"

	// DefaultMultiAdminImage is the default container image used for the MultiAdmin component.
	DefaultMultiAdminImage = "ghcr.io/multigres/multigres:main"

	// DefaultMultiAdminWebImage is the default container image used for the MultiAdminWeb component.
	DefaultMultiAdminWebImage = "docker.io/multigres/multiadmin-web:latest"

	// DefaultMultiAdminWebReplicas is the default number of replicas for the MultiAdminWeb deployment if not specified.
	DefaultMultiAdminWebReplicas int32 = 1

	// DefaultMultiOrchImage is the default container image used for the MultiOrch component.
	DefaultMultiOrchImage = "ghcr.io/multigres/multigres:main"

	// DefaultMultiPoolerImage is the default container image used for the MultiPooler component.
	DefaultMultiPoolerImage = "ghcr.io/multigres/multigres:main"

	// DefaultMultiGatewayImage is the default container image used for the MultiGateway component.
	DefaultMultiGatewayImage = "ghcr.io/multigres/multigres:main"

	// DefaultImagePullPolicy is the default image pull policy used for all components if not specified.
	DefaultImagePullPolicy = corev1.PullIfNotPresent

	// DefaultEtcdStorageSize is the default PVC size for the managed Etcd cluster if not specified.
	DefaultEtcdStorageSize = "1Gi"
)

// DefaultResourcesAdmin returns the default resource requests and limits for the MultiAdmin deployment.
// It requests 100m CPU and 128Mi memory, with a limit of 256Mi memory.
func DefaultResourcesAdmin() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
}

// DefaultResourcesEtcd returns the default resource requests and limits for the managed Etcd cluster.
// It requests 100m CPU and 256Mi memory, with a limit of 512Mi memory.
func DefaultResourcesEtcd() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
	}
}

// DefaultResourcesGateway returns the default resource requests and limits for the MultiGateway deployment.
func DefaultResourcesGateway() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
}

// DefaultResourcesOrch returns the default resource requests and limits for the MultiOrch deployment.
func DefaultResourcesOrch() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
	}
}

// DefaultResourcesPostgres returns the default resources for the Postgres container in a pool.
func DefaultResourcesPostgres() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
	}
}

// DefaultResourcesPooler returns the default resources for the Multipooler container in a pool.
func DefaultResourcesPooler() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
}

// DefaultResourcesAdminWeb returns the default resource requests and limits for the MultiAdminWeb deployment.
// It requests 50m CPU and 64Mi memory, with a limit of 128Mi memory.
func DefaultResourcesAdminWeb() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
	}
}

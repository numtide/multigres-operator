package defaults

const (
	// DefaultEtcdReplicas is the default number of replicas for the managed Etcd cluster if not specified.
	DefaultEtcdReplicas int32 = 3

	// DefaultAdminReplicas is the default number of replicas for the MultiAdmin deployment if not specified.
	DefaultAdminReplicas int32 = 1

	// FallbackCoreTemplate is the name of the template to look for if no specific template is referenced.
	FallbackCoreTemplate = "default"

	// FallbackCellTemplate is the name of the template to look for if no specific template is referenced.
	FallbackCellTemplate = "default"

	// FallbackShardTemplate is the name of the template to look for if no specific template is referenced.
	FallbackShardTemplate = "default"
)

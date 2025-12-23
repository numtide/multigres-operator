package multigrescluster

// NOTE: We may want to consider moving this to different module/package before implementing the Mutating Webhook.
// This separation is critical to prevent circular dependencies between the Webhook and Controller packages
// and ensures that the "Level 4" defaulting logic is reusable as a Single Source of Truth for both the reconciliation loop
// and admission requests.

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

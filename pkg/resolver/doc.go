// Package resolver provides the central logic for calculating the "Effective Specification" of a MultigresCluster.
//
// In the Multigres Operator, a cluster's configuration can come from multiple sources:
//  1. Inline Configurations (defined directly in the MultigresCluster CR).
//  2. Template References (pointing to CoreTemplate, CellTemplate, or ShardTemplate CRs).
//  3. Overrides (partial patches applied on top of a template).
//  4. Hardcoded Defaults (fallback values for safety).
//
// The Resolver is the single source of truth for merging these sources. It ensures that
// the Reconciler and the Webhook always agree on what the final configuration should be.
//
// # Logic Hierarchy
//
// When calculating the final configuration for a component, the Resolver applies the
// following precedence (highest to lowest):
//
//  1. Inline Spec (if provided, it ignores templates entirely).
//  2. Overrides (patches specific fields of the template).
//  3. Template Spec (the base configuration from the referenced CR).
//  4. Global Defaults (hardcoded constants like default images or replicas).
//
// # Dual Usage
//
// This package is designed to be used in two distinct phases:
//
//  1. Mutation (Webhook):
//     The 'PopulateClusterDefaults' method is safe to call during admission. It applies
//     static defaults (images, explicit template names) to the API object itself,
//     solving the "Invisible Defaults" problem without making external API calls.
//
//  2. Reconciliation (Controller):
//     The 'Resolve...' and 'Merge...' methods are used during reconciliation. They
//     fetch external templates from the API server and merge them to determine the
//     actual state the child resources (StatefulSets, Services) should match.
//
// Usage:
//
//	// Create a resolver
//	res := resolver.NewResolver(client, namespace, cluster.Spec.TemplateDefaults)
//
//	// Webhook: Apply static defaults to the object
//	res.PopulateClusterDefaults(cluster)
//
//	// Controller: Calculate final config for a specific component
//	template, err := res.ResolveShardTemplate(ctx, "my-shard-template")
//	finalConfig := resolver.MergeShardConfig(template, overrides, nil)
package resolver

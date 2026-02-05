// Package resolver provides the central logic for calculating the "Effective Specification" of a MultigresCluster.
//
// In the Multigres Operator, a cluster's configuration can come from multiple sources:
//  1. Inline Configurations (defined directly in the MultigresCluster CR).
//  2. Template References (pointing to CoreTemplate, CellTemplate, or ShardTemplate CRs).
//  3. Hardcoded Defaults (fallback values for safety).
//
// The Resolver is the single source of truth for determining the final configuration. It ensures that
// the Reconciler and the Webhook always agree on what the final configuration should be.
//
// # Logic Hierarchy
//
// When calculating the final configuration for a component, the Resolver applies the
// following precedence (highest to lowest):
//
//  1. Inline Spec (if provided, it takes precedence over the template).
//  2. Template Spec (the base configuration from the referenced CR).
//  3. Global Defaults (hardcoded constants like default images or replicas).
//
// # Dual Usage
//
// This package is designed to be used in two distinct phases:
//
//  1. Mutation (Webhook):
//     The 'PopulateClusterDefaults' method is safe to call during admission. It applies
//     static defaults (images) and "Smart Defaults" (injecting mandatory system
//     databases, table groups, and shards) to the API object itself. This solves the
//     "Invisible Defaults" problem without making external API calls.
//
//  2. Reconciliation (Controller):
//     The 'Resolve...' methods (e.g., ResolveGlobalTopo) are used during reconciliation.
//     They fetch external templates from the API server and determine the effective
//     configuration by checking for inline definitions vs. template references.
//
//  3. Validation (Webhook):
//     The 'Validate...' methods (e.g. ValidateClusterIntegrity) are used during admission
//     to enforce strict referential integrity and logical consistency. They simulate the
//     resolution process to catch configuration errors (like referencing missing templates
//     or invalid overrides) before the cluster is persisted.
//
// Usage:
//
//		// Create a resolver
//		res := resolver.NewResolver(client, namespace)
//
//		// Webhook: Apply static and smart defaults to the object
//		res.PopulateClusterDefaults(cluster)
//
//	 // Webhook: Validate the cluster structure
//	 if err := res.ValidateClusterIntegrity(ctx, cluster); err != nil {
//	     return err
//	 }
//
//		// Controller: Calculate final config for a specific component
//		// This handles the Inline > Template > Default precedence logic
//		globalTopoSpec, err := res.ResolveGlobalTopo(ctx, cluster)
package resolver

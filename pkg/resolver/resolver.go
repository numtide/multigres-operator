package resolver

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// Resolver handles the logic for fetching templates and calculating defaults.
// It serves as the single source of truth for defaulting logic across the operator.
type Resolver struct {
	// Client is the kubernetes client used to fetch templates or other cluster resources.
	Client client.Client
	// Namespace is the namespace where templates/resources are expected to exist.
	Namespace string
	// TemplateDefaults contains the cluster-level template references.
	TemplateDefaults multigresv1alpha1.TemplateDefaults
	// CoreTemplateCache is a request-scoped cache for CoreTemplates.
	CoreTemplateCache map[string]*multigresv1alpha1.CoreTemplate
	// CellTemplateCache is a request-scoped cache for CellTemplates.
	CellTemplateCache map[string]*multigresv1alpha1.CellTemplate
	// ShardTemplateCache is a request-scoped cache for ShardTemplates.
	ShardTemplateCache map[string]*multigresv1alpha1.ShardTemplate
}

// NewResolver creates a new defaults.Resolver.
func NewResolver(
	c client.Client,
	namespace string,
	tplDefaults multigresv1alpha1.TemplateDefaults,
) *Resolver {
	return &Resolver{
		Client:             c,
		Namespace:          namespace,
		TemplateDefaults:   tplDefaults,
		CoreTemplateCache:  make(map[string]*multigresv1alpha1.CoreTemplate),
		CellTemplateCache:  make(map[string]*multigresv1alpha1.CellTemplate),
		ShardTemplateCache: make(map[string]*multigresv1alpha1.ShardTemplate),
	}
}

// ValidateCoreTemplateReference checks if a CoreTemplate reference is valid.
// It returns nil if:
// 1. The name is empty (no reference).
// 2. The name matches the FallbackCoreTemplate (assumed to be a system default or implicitly allowed).
// 3. The referenced CoreTemplate exists in the Resolver's namespace.
// Otherwise, it returns an error.
func (r *Resolver) ValidateCoreTemplateReference(
	ctx context.Context,
	name multigresv1alpha1.TemplateRef,
) error {
	if name == "" {
		return nil
	}
	if name == FallbackCoreTemplate {
		return nil
	}

	exists, err := r.CoreTemplateExists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf(
			"referenced CoreTemplate '%s' not found in namespace '%s'",
			name,
			r.Namespace,
		)
	}
	return nil
}

// CoreTemplateExists checks if a CoreTemplate with the given name exists in the current namespace.
func (r *Resolver) CoreTemplateExists(
	ctx context.Context,
	name multigresv1alpha1.TemplateRef,
) (bool, error) {
	if name == "" {
		return false, nil
	}
	tpl := &multigresv1alpha1.CoreTemplate{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: string(name), Namespace: r.Namespace}, tpl)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ValidateCellTemplateReference checks if a CellTemplate reference is valid.
// See ValidateCoreTemplateReference for logic details.
func (r *Resolver) ValidateCellTemplateReference(
	ctx context.Context,
	name multigresv1alpha1.TemplateRef,
) error {
	if name == "" {
		return nil
	}
	if name == FallbackCellTemplate {
		return nil
	}

	exists, err := r.CellTemplateExists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf(
			"referenced CellTemplate '%s' not found in namespace '%s'",
			name,
			r.Namespace,
		)
	}
	return nil
}

// CellTemplateExists checks if a CellTemplate with the given name exists in the current namespace.
func (r *Resolver) CellTemplateExists(
	ctx context.Context,
	name multigresv1alpha1.TemplateRef,
) (bool, error) {
	if name == "" {
		return false, nil
	}
	tpl := &multigresv1alpha1.CellTemplate{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: string(name), Namespace: r.Namespace}, tpl)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ValidateShardTemplateReference checks if a ShardTemplate reference is valid.
// See ValidateCoreTemplateReference for logic details.
func (r *Resolver) ValidateShardTemplateReference(
	ctx context.Context,
	name multigresv1alpha1.TemplateRef,
) error {
	if name == "" {
		return nil
	}
	if name == FallbackShardTemplate {
		return nil
	}

	exists, err := r.ShardTemplateExists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf(
			"referenced ShardTemplate '%s' not found in namespace '%s'",
			name,
			r.Namespace,
		)
	}
	return nil
}

// ShardTemplateExists checks if a ShardTemplate with the given name exists in the current namespace.
func (r *Resolver) ShardTemplateExists(
	ctx context.Context,
	name multigresv1alpha1.TemplateRef,
) (bool, error) {
	if name == "" {
		return false, nil
	}
	tpl := &multigresv1alpha1.ShardTemplate{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: string(name), Namespace: r.Namespace}, tpl)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ============================================================================
// Shared Merge Helpers
// ============================================================================

func mergeStatelessSpec(
	base *multigresv1alpha1.StatelessSpec,
	override *multigresv1alpha1.StatelessSpec,
) {
	if override.Replicas != nil {
		base.Replicas = override.Replicas
	}

	// Safety: Use DeepCopy to ensure we don't share mutable map references (Requests/Limits)
	if !isResourcesZero(override.Resources) {
		base.Resources = *override.Resources.DeepCopy()
	}

	// Safety: DeepCopy Affinity to avoid sharing pointers
	if override.Affinity != nil {
		base.Affinity = override.Affinity.DeepCopy()
	}

	for k, v := range override.PodAnnotations {
		if base.PodAnnotations == nil {
			base.PodAnnotations = make(map[string]string)
		}
		base.PodAnnotations[k] = v
	}
	for k, v := range override.PodLabels {
		if base.PodLabels == nil {
			base.PodLabels = make(map[string]string)
		}
		base.PodLabels[k] = v
	}
}

// isResourcesZero checks if the resource requirements are strictly the zero value (nil maps).
// This mimics reflect.DeepEqual(res, corev1.ResourceRequirements{}) but is safer and faster.
// It is used for merging logic where we want to distinguish "inherit" (nil) from "empty" (set to empty).
func isResourcesZero(res corev1.ResourceRequirements) bool {
	return res.Requests == nil && res.Limits == nil && res.Claims == nil
}

// ============================================================================
// Shared Defaulting Helpers
// ============================================================================

// defaultEtcdSpec applies hardcoded safety defaults to an inline Etcd spec.
func defaultEtcdSpec(spec *multigresv1alpha1.EtcdSpec) {
	if spec.Image == "" {
		spec.Image = DefaultEtcdImage
	}
	if spec.Storage.Size == "" {
		spec.Storage.Size = DefaultEtcdStorageSize
	}
	if spec.Replicas == nil {
		r := DefaultEtcdReplicas
		spec.Replicas = &r
	}
	// Use isResourcesZero to ensure we respect overrides that only have Claims
	if isResourcesZero(spec.Resources) {
		// Safety: DefaultResourcesEtcd() returns a fresh struct, so no DeepCopy needed.
		spec.Resources = DefaultResourcesEtcd()
	}
}

// defaultStatelessSpec applies hardcoded safety defaults to any stateless spec.
func defaultStatelessSpec(
	spec *multigresv1alpha1.StatelessSpec,
	defaultRes corev1.ResourceRequirements,
	defaultReplicas int32,
) {
	if spec.Replicas == nil {
		spec.Replicas = &defaultReplicas
	}
	// Use isResourcesZero to ensure we respect overrides that only have Claims
	if isResourcesZero(spec.Resources) {
		// Safety: We assume defaultRes is passed by value (a fresh copy from the default function).
		// We perform a DeepCopy to ensure spec.Resources owns its own maps, independent of the input defaultRes.
		spec.Resources = *defaultRes.DeepCopy()
	}
}

// ============================================================================
// Cluster Validation
// ============================================================================

// ValidateClusterIntegrity checks that all templates referenced by the cluster actually exist.
// This corresponds to the Level 4 Referential Integrity check.
//
// This method is primarily used by the Validating Webhook to reject clusters with broken references.
func (r *Resolver) ValidateClusterIntegrity(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
) error {
	// 1. Validate Core Templates
	if err := r.ValidateCoreTemplateReference(ctx, cluster.Spec.TemplateDefaults.CoreTemplate); err != nil {
		return err
	}
	if cluster.Spec.MultiAdmin != nil && cluster.Spec.MultiAdmin.TemplateRef != "" {
		if err := r.ValidateCoreTemplateReference(ctx, cluster.Spec.MultiAdmin.TemplateRef); err != nil {
			return err
		}
	}
	if cluster.Spec.GlobalTopoServer != nil && cluster.Spec.GlobalTopoServer.TemplateRef != "" {
		if err := r.ValidateCoreTemplateReference(ctx, cluster.Spec.GlobalTopoServer.TemplateRef); err != nil {
			return err
		}
	}
	if cluster.Spec.MultiAdminWeb != nil && cluster.Spec.MultiAdminWeb.TemplateRef != "" {
		if err := r.ValidateCoreTemplateReference(ctx, cluster.Spec.MultiAdminWeb.TemplateRef); err != nil {
			return err
		}
	}

	// 2. Validate Cell Templates
	if err := r.ValidateCellTemplateReference(ctx, cluster.Spec.TemplateDefaults.CellTemplate); err != nil {
		return err
	}
	for _, cell := range cluster.Spec.Cells {
		if err := r.ValidateCellTemplateReference(ctx, cell.CellTemplate); err != nil {
			return err
		}
	}

	// 3. Validate Shard Templates
	if err := r.ValidateShardTemplateReference(ctx, cluster.Spec.TemplateDefaults.ShardTemplate); err != nil {
		return err
	}
	for _, db := range cluster.Spec.Databases {
		for _, tg := range db.TableGroups {
			for _, shard := range tg.Shards {
				if err := r.ValidateShardTemplateReference(ctx, shard.ShardTemplate); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// ValidateClusterLogic performs deep logic checks and strict validation on the cluster configuration.
// It simulates the resolution process to identify broken references, empty cells, or invalid overrides.
//
// This method is primarily used by the Validating Webhook to enforce logical correctness.
func (r *Resolver) ValidateClusterLogic(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
) (admission.Warnings, error) {
	var warnings admission.Warnings

	// Extract all valid cell names for this cluster
	var cellNames []multigresv1alpha1.CellName
	validCells := make(map[multigresv1alpha1.CellName]bool)
	for _, c := range cluster.Spec.Cells {
		cellNames = append(cellNames, c.Name)
		validCells[c.Name] = true
	}

	// Iterate through every Shard and "Simulate" Resolution
	for _, db := range cluster.Spec.Databases {
		for _, tg := range db.TableGroups {
			for _, shard := range tg.Shards {
				// ------------------------------------------------------------------
				// 1. Orphan Override Check
				// ------------------------------------------------------------------
				if shard.Overrides != nil && len(shard.Overrides.Pools) > 0 {
					// We must resolve the template to know what pools *should* exist.
					// Pass empty string if ShardTemplate is empty to resolve default/implicit.

					tpl, err := r.ResolveShardTemplate(ctx, shard.ShardTemplate)
					if err != nil {
						// This should have been caught by ValidateClusterIntegrity, but handling it safe.
						return nil, fmt.Errorf(
							"failed to resolve template for orphan check: %w",
							err,
						)
					}

					if tpl != nil {
						for poolName := range shard.Overrides.Pools {
							if _, exists := tpl.Spec.Pools[poolName]; !exists {
								warnings = append(warnings, fmt.Sprintf(
									"Pool '%s' defined in overrides for shard '%s' does not exist in template '%s'. A new pool will be created.",
									poolName,
									shard.Name,
									tpl.Name,
								))
							}
						}
					}
				}

				// ------------------------------------------------------------------
				// 2. Logic Resolution
				// ------------------------------------------------------------------
				// Dry-Run Resolution
				// We pass allCellNames just like the Reconciler would, to simulate the final state
				orch, pools, err := r.ResolveShard(ctx, &shard, cellNames)
				if err != nil {
					return nil, fmt.Errorf(
						"validation failed: cannot resolve shard '%s': %w",
						shard.Name,
						err,
					)
				}

				// Check 1: Empty Cells (Orphaned Shard)
				// If after resolution (and defaulting), cells are STILL empty, it's a broken config.
				if len(orch.Cells) == 0 {
					return nil, fmt.Errorf(
						"shard '%s' matches NO cells (check your cell names or template configuration)",
						shard.Name,
					)
				}

				for poolName, pool := range pools {
					// Check 1b: Empty Pool cells
					if len(pool.Cells) == 0 {
						return nil, fmt.Errorf(
							"pool '%s' in shard '%s' matches NO cells",
							poolName,
							shard.Name,
						)
					}
				}

				// Check 2: Invalid Cells (Reference Validity)
				for _, c := range orch.Cells {
					if !validCells[multigresv1alpha1.CellName(c)] {
						return nil, fmt.Errorf(
							"shard '%s' is assigned to non-existent cell '%s'",
							shard.Name,
							c,
						)
					}
				}

				for poolName, pool := range pools {
					// Check 2b: Invalid Pool cells
					for _, c := range pool.Cells {
						if !validCells[multigresv1alpha1.CellName(c)] {
							return nil, fmt.Errorf(
								"pool '%s' in shard '%s' is assigned to non-existent cell '%s'",
								poolName,
								shard.Name,
								c,
							)
						}
					}
				}
			}
		}
	}

	return warnings, nil
}

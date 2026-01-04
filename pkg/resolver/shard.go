package resolver

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// ResolveShard determines the final configuration for a specific Shard.
// It orchestrates: Template Lookup -> Fetch -> Merge -> Defaulting.
func (r *Resolver) ResolveShard(
	ctx context.Context,
	shardSpec *multigresv1alpha1.ShardConfig,
) (*multigresv1alpha1.MultiOrchSpec, map[string]multigresv1alpha1.PoolSpec, error) {
	// 1. Fetch Template (Logic handles defaults)
	templateName := shardSpec.ShardTemplate
	tpl, err := r.ResolveShardTemplate(ctx, templateName)
	if err != nil {
		return nil, nil, err
	}

	// 2. Merge Logic
	multiOrch, pools := mergeShardConfig(tpl, shardSpec.Overrides, shardSpec.Spec)

	// 3. Apply Deep Defaults (Level 4)
	defaultStatelessSpec(&multiOrch.StatelessSpec, corev1.ResourceRequirements{}, 1)

	// Note: We do not apply strict defaults to Pools here yet,
	// as Pool defaults are often highly context-specific (storage class, etc).
	// However, we could apply safety defaults if needed.

	return &multiOrch, pools, nil
}

// ResolveShardTemplate fetches and resolves a ShardTemplate by name.
// If name is empty, it resolves using the Cluster Defaults, then the Namespace Default.
func (r *Resolver) ResolveShardTemplate(
	ctx context.Context,
	name string,
) (*multigresv1alpha1.ShardTemplate, error) {
	resolvedName := name
	isImplicitFallback := false

	if resolvedName == "" {
		resolvedName = r.TemplateDefaults.ShardTemplate
	}
	if resolvedName == "" {
		resolvedName = FallbackShardTemplate
		isImplicitFallback = true
	}

	tpl := &multigresv1alpha1.ShardTemplate{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: resolvedName, Namespace: r.Namespace}, tpl)
	if err != nil {
		if errors.IsNotFound(err) {
			if isImplicitFallback {
				// We return an empty struct instead of nil to satisfy tests expecting non-nil structure.
				return &multigresv1alpha1.ShardTemplate{}, nil
			}
			return nil, fmt.Errorf("referenced ShardTemplate '%s' not found: %w", resolvedName, err)
		}
		return nil, fmt.Errorf("failed to get ShardTemplate: %w", err)
	}
	return tpl, nil
}

// mergeShardConfig merges a template spec with overrides and an inline spec.
func mergeShardConfig(
	template *multigresv1alpha1.ShardTemplate,
	overrides *multigresv1alpha1.ShardOverrides,
	inline *multigresv1alpha1.ShardInlineSpec,
) (multigresv1alpha1.MultiOrchSpec, map[string]multigresv1alpha1.PoolSpec) {
	if inline != nil {
		orch := *inline.MultiOrch.DeepCopy()
		pools := make(map[string]multigresv1alpha1.PoolSpec)
		for k, v := range inline.Pools {
			pools[k] = *v.DeepCopy()
		}
		return orch, pools
	}

	var multiOrch multigresv1alpha1.MultiOrchSpec
	pools := make(map[string]multigresv1alpha1.PoolSpec)

	if template != nil {
		if template.Spec.MultiOrch != nil {
			multiOrch = *template.Spec.MultiOrch.DeepCopy()
		}
		for k, v := range template.Spec.Pools {
			pools[k] = *v.DeepCopy()
		}
	}

	if overrides != nil {
		if overrides.MultiOrch != nil {
			mergeMultiOrchSpec(&multiOrch, overrides.MultiOrch)
		}

		for k, v := range overrides.Pools {
			if existingPool, exists := pools[k]; exists {
				mergedPool := mergePoolSpec(existingPool, v)
				pools[k] = mergedPool
			} else {
				pools[k] = v
			}
		}
	}

	return multiOrch, pools
}

func mergeMultiOrchSpec(
	base *multigresv1alpha1.MultiOrchSpec,
	override *multigresv1alpha1.MultiOrchSpec,
) {
	mergeStatelessSpec(&base.StatelessSpec, &override.StatelessSpec)
	if len(override.Cells) > 0 {
		base.Cells = override.Cells
	}
}

func mergePoolSpec(
	base multigresv1alpha1.PoolSpec,
	override multigresv1alpha1.PoolSpec,
) multigresv1alpha1.PoolSpec {
	out := base
	if override.Type != "" {
		out.Type = override.Type
	}
	if len(override.Cells) > 0 {
		out.Cells = override.Cells
	}
	if override.ReplicasPerCell != nil {
		out.ReplicasPerCell = override.ReplicasPerCell
	}
	if override.Storage.Size != "" {
		out.Storage = override.Storage
	}
	// Safety: Use DeepCopy to avoid sharing pointers to maps within ResourceRequirements
	if !isResourcesZero(override.Postgres.Resources) {
		out.Postgres.Resources = *override.Postgres.Resources.DeepCopy()
	}
	if !isResourcesZero(override.Multipooler.Resources) {
		out.Multipooler.Resources = *override.Multipooler.Resources.DeepCopy()
	}
	if override.Affinity != nil {
		out.Affinity = override.Affinity.DeepCopy()
	}
	return out
}

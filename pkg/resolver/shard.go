package resolver

import (
	"context"
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// ResolveShard determines the final configuration for a specific Shard.
func (r *Resolver) ResolveShard(
	ctx context.Context,
	shardSpec *multigresv1alpha1.ShardConfig,
	allCellNames []multigresv1alpha1.CellName,
) (*multigresv1alpha1.MultiOrchSpec, map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec, error) {
	// 1. Fetch Template
	templateName := shardSpec.ShardTemplate
	tpl, err := r.ResolveShardTemplate(ctx, templateName)
	if err != nil {
		return nil, nil, err
	}

	// 2. Merge Logic
	multiOrch, pools := mergeShardConfig(tpl, shardSpec.Overrides, shardSpec.Spec)

	// 3. Apply Deep Defaults (Level 4)
	defaultStatelessSpec(&multiOrch.StatelessSpec, DefaultResourcesOrch(), 1)

	// Contextual Defaulting: Lazy Cell Injection
	// If the resolved configuration has no cells defined, it means "run everywhere".
	// We inject the full list of cluster cells here.
	if len(multiOrch.Cells) == 0 && len(allCellNames) > 0 {
		for _, c := range allCellNames {
			multiOrch.Cells = append(multiOrch.Cells, multigresv1alpha1.CellName(c))
		}
		// Sort for deterministic output
		sort.Slice(multiOrch.Cells, func(i, j int) bool {
			return multiOrch.Cells[i] < multiOrch.Cells[j]
		})
	}

	if len(pools) == 0 {
		pools["default"] = multigresv1alpha1.PoolSpec{
			Type:  "readWrite",
			Cells: multiOrch.Cells,
		}
	}

	for name := range pools {
		p := pools[name]
		defaultPoolSpec(&p)

		// Contextual Defaulting for Pools
		if len(p.Cells) == 0 && len(allCellNames) > 0 {
			for _, c := range allCellNames {
				p.Cells = append(p.Cells, multigresv1alpha1.CellName(c))
			}
			// Sort for deterministic output
			sort.Slice(p.Cells, func(i, j int) bool {
				return p.Cells[i] < p.Cells[j]
			})
		}

		pools[name] = p
	}

	return &multiOrch, pools, nil
}

// ResolveShardTemplate fetches and resolves a ShardTemplate by name.
func (r *Resolver) ResolveShardTemplate(
	ctx context.Context,
	name multigresv1alpha1.TemplateRef,
) (*multigresv1alpha1.ShardTemplate, error) {
	resolvedName := name
	isImplicitFallback := false

	if resolvedName == "" {
		resolvedName = r.TemplateDefaults.ShardTemplate
	}
	if resolvedName == "" || resolvedName == FallbackShardTemplate {
		resolvedName = FallbackShardTemplate
		isImplicitFallback = true
	}

	// Check cache first
	if cached, found := r.ShardTemplateCache[string(resolvedName)]; found {
		return cached, nil
	}

	// 2. Fetch
	tpl := &multigresv1alpha1.ShardTemplate{}
	key := types.NamespacedName{Name: string(resolvedName), Namespace: r.Namespace}
	if err := r.Client.Get(ctx, key, tpl); err != nil {
		if errors.IsNotFound(err) {
			if isImplicitFallback {
				return &multigresv1alpha1.ShardTemplate{}, nil
			}
			return nil, fmt.Errorf("referenced ShardTemplate '%s' not found: %w", resolvedName, err)
		}
		return nil, fmt.Errorf("failed to get ShardTemplate: %w", err)
	}

	// 3. Cache
	r.ShardTemplateCache[string(resolvedName)] = tpl
	return tpl.DeepCopy(), nil
}

// mergeShardConfig merges a template spec with overrides and an inline spec.
func mergeShardConfig(
	template *multigresv1alpha1.ShardTemplate,
	overrides *multigresv1alpha1.ShardOverrides,
	inline *multigresv1alpha1.ShardInlineSpec,
) (multigresv1alpha1.MultiOrchSpec, map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec) {
	// 1. Start with Template (Base)
	var multiOrch multigresv1alpha1.MultiOrchSpec
	pools := make(map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec)

	if template != nil {
		if template.Spec.MultiOrch != nil {
			multiOrch = *template.Spec.MultiOrch.DeepCopy()
		}
		for k, v := range template.Spec.Pools {
			pools[k] = *v.DeepCopy()
		}
	}

	// 2. Apply Overrides (Explicit Template Modification)
	if overrides != nil {
		if overrides.MultiOrch != nil {
			mergeMultiOrchSpec(&multiOrch, overrides.MultiOrch)
		}
		for k, v := range overrides.Pools {
			if existingPool, exists := pools[k]; exists {
				pools[k] = mergePoolSpec(existingPool, v)
			} else {
				pools[k] = v
			}
		}
	}

	// 3. Apply Inline Spec (Primary Overlay)
	// This merges the inline definition on top of the template+overrides.
	if inline != nil {
		mergeMultiOrchSpec(&multiOrch, &inline.MultiOrch)

		for k, v := range inline.Pools {
			if existingPool, exists := pools[k]; exists {
				pools[k] = mergePoolSpec(existingPool, v)
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

func defaultPoolSpec(spec *multigresv1alpha1.PoolSpec) {
	if spec.ReplicasPerCell == nil {
		spec.ReplicasPerCell = ptr.To(int32(1))
	}
	if spec.Storage.Size == "" {
		spec.Storage.Size = DefaultEtcdStorageSize
	}
	if isResourcesZero(spec.Postgres.Resources) {
		spec.Postgres.Resources = DefaultResourcesPostgres()
	}
	if isResourcesZero(spec.Multipooler.Resources) {
		spec.Multipooler.Resources = DefaultResourcesPooler()
	}
}

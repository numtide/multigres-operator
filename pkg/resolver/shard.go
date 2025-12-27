package resolver

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// ResolveShardTemplate fetches and resolves a ShardTemplate by name, handling defaults.
func (r *Resolver) ResolveShardTemplate(
	ctx context.Context,
	templateName string,
) (*multigresv1alpha1.ShardTemplate, error) {
	name := templateName
	isImplicitFallback := false

	if name == "" {
		name = r.TemplateDefaults.ShardTemplate
	}
	if name == "" {
		name = FallbackShardTemplate
		isImplicitFallback = true
	}

	tpl := &multigresv1alpha1.ShardTemplate{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: r.Namespace}, tpl)
	if err != nil {
		if errors.IsNotFound(err) {
			if isImplicitFallback {
				return &multigresv1alpha1.ShardTemplate{}, nil
			}
			return nil, fmt.Errorf("referenced ShardTemplate '%s' not found: %w", name, err)
		}
		return nil, fmt.Errorf("failed to get ShardTemplate: %w", err)
	}
	return tpl, nil
}

// MergeShardConfig merges a template spec with overrides and an inline spec to produce the final configuration.
func MergeShardConfig(
	template *multigresv1alpha1.ShardTemplate,
	overrides *multigresv1alpha1.ShardOverrides,
	inline *multigresv1alpha1.ShardInlineSpec,
) (multigresv1alpha1.MultiOrchSpec, map[string]multigresv1alpha1.PoolSpec) {
	if inline != nil {
		return inline.MultiOrch, inline.Pools
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
	if !reflect.DeepEqual(override.Postgres.Resources, corev1.ResourceRequirements{}) {
		out.Postgres.Resources = *override.Postgres.Resources.DeepCopy()
	}
	if !reflect.DeepEqual(override.Multipooler.Resources, corev1.ResourceRequirements{}) {
		out.Multipooler.Resources = *override.Multipooler.Resources.DeepCopy()
	}
	if override.Affinity != nil {
		out.Affinity = override.Affinity.DeepCopy()
	}
	return out
}

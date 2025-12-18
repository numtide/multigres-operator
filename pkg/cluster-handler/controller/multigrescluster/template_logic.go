package multigrescluster

import (
	"context"
	"reflect"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TemplateResolver handles the logic for fetching and merging templates.
type TemplateResolver struct {
	// Client is the kubernetes client used to fetch templates.
	Client client.Client
	// Namespace is the namespace where templates are expected to exist.
	Namespace string
	// Defaults contains the cluster-level template references to use when explicit ones are missing.
	Defaults multigresv1alpha1.TemplateDefaults
}

// ResolveCoreTemplate fetches and resolves a CoreTemplate by name, handling defaults.
func (r *TemplateResolver) ResolveCoreTemplate(ctx context.Context, templateName string) (*multigresv1alpha1.CoreTemplate, error) {
	name := templateName
	if name == "" {
		name = r.Defaults.CoreTemplate
	}
	if name == "" {
		name = "default"
	}

	tpl := &multigresv1alpha1.CoreTemplate{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: r.Namespace}, tpl)
	if err != nil {
		if errors.IsNotFound(err) {
			return &multigresv1alpha1.CoreTemplate{}, nil
		}
		return nil, err
	}
	return tpl, nil
}

// ResolveCellTemplate fetches and resolves a CellTemplate by name, handling defaults.
func (r *TemplateResolver) ResolveCellTemplate(ctx context.Context, templateName string) (*multigresv1alpha1.CellTemplate, error) {
	name := templateName
	if name == "" {
		name = r.Defaults.CellTemplate
	}
	if name == "" {
		name = "default"
	}

	tpl := &multigresv1alpha1.CellTemplate{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: r.Namespace}, tpl)
	if err != nil {
		if errors.IsNotFound(err) {
			return &multigresv1alpha1.CellTemplate{}, nil
		}
		return nil, err
	}
	return tpl, nil
}

// ResolveShardTemplate fetches and resolves a ShardTemplate by name, handling defaults.
func (r *TemplateResolver) ResolveShardTemplate(ctx context.Context, templateName string) (*multigresv1alpha1.ShardTemplate, error) {
	name := templateName
	if name == "" {
		name = r.Defaults.ShardTemplate
	}
	if name == "" {
		name = "default"
	}

	tpl := &multigresv1alpha1.ShardTemplate{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: r.Namespace}, tpl)
	if err != nil {
		if errors.IsNotFound(err) {
			return &multigresv1alpha1.ShardTemplate{}, nil
		}
		return nil, err
	}
	return tpl, nil
}

// MergeCellConfig merges a template spec with overrides and an inline spec to produce the final configuration.
func MergeCellConfig(template *multigresv1alpha1.CellTemplate, overrides *multigresv1alpha1.CellOverrides, inline *multigresv1alpha1.CellInlineSpec) (multigresv1alpha1.StatelessSpec, *multigresv1alpha1.LocalTopoServerSpec) {
	var gateway multigresv1alpha1.StatelessSpec
	var localTopo *multigresv1alpha1.LocalTopoServerSpec

	if template != nil {
		if template.Spec.MultiGateway != nil {
			gateway = *template.Spec.MultiGateway.DeepCopy()
		}
		if template.Spec.LocalTopoServer != nil {
			localTopo = template.Spec.LocalTopoServer.DeepCopy()
		}
	}

	if overrides != nil {
		if overrides.MultiGateway != nil {
			mergeStatelessSpec(&gateway, overrides.MultiGateway)
		}
	}

	if inline != nil {
		return inline.MultiGateway, inline.LocalTopoServer
	}

	return gateway, localTopo
}

// MergeShardConfig merges a template spec with overrides and an inline spec to produce the final configuration.
func MergeShardConfig(template *multigresv1alpha1.ShardTemplate, overrides *multigresv1alpha1.ShardOverrides, inline *multigresv1alpha1.ShardInlineSpec) (multigresv1alpha1.MultiOrchSpec, map[string]multigresv1alpha1.PoolSpec) {
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

func mergeStatelessSpec(base *multigresv1alpha1.StatelessSpec, override *multigresv1alpha1.StatelessSpec) {
	if override.Replicas != nil {
		base.Replicas = override.Replicas
	}
	if !reflect.DeepEqual(override.Resources, corev1.ResourceRequirements{}) {
		base.Resources = override.Resources
	}
	if override.Affinity != nil {
		base.Affinity = override.Affinity
	}
	if len(override.PodAnnotations) > 0 {
		if base.PodAnnotations == nil {
			base.PodAnnotations = make(map[string]string)
		}
		for k, v := range override.PodAnnotations {
			base.PodAnnotations[k] = v
		}
	}
	if len(override.PodLabels) > 0 {
		if base.PodLabels == nil {
			base.PodLabels = make(map[string]string)
		}
		for k, v := range override.PodLabels {
			base.PodLabels[k] = v
		}
	}
}

func mergeMultiOrchSpec(base *multigresv1alpha1.MultiOrchSpec, override *multigresv1alpha1.MultiOrchSpec) {
	mergeStatelessSpec(&base.StatelessSpec, &override.StatelessSpec)
	if len(override.Cells) > 0 {
		base.Cells = override.Cells
	}
}

func mergePoolSpec(base multigresv1alpha1.PoolSpec, override multigresv1alpha1.PoolSpec) multigresv1alpha1.PoolSpec {
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
	if !reflect.DeepEqual(override.Postgres.Resources, corev1.ResourceRequirements{}) {
		out.Postgres.Resources = override.Postgres.Resources
	}
	if !reflect.DeepEqual(override.Multipooler.Resources, corev1.ResourceRequirements{}) {
		out.Multipooler.Resources = override.Multipooler.Resources
	}
	if override.Affinity != nil {
		out.Affinity = override.Affinity
	}
	return out
}

// ResolveGlobalTopo determines the final GlobalTopoServer configuration by preferring inline config over templates.
func ResolveGlobalTopo(spec *multigresv1alpha1.GlobalTopoServerSpec, coreTemplate *multigresv1alpha1.CoreTemplate) *multigresv1alpha1.GlobalTopoServerSpec {
	// If inline config is present, use it.
	if spec.Etcd != nil || spec.External != nil {
		return spec
	}

	// Otherwise, use the template (loaded by caller based on TemplateRef or Defaults)
	if coreTemplate != nil && coreTemplate.Spec.GlobalTopoServer != nil {
		return &multigresv1alpha1.GlobalTopoServerSpec{
			Etcd: coreTemplate.Spec.GlobalTopoServer.Etcd,
		}
	}

	return spec
}

// ResolveMultiAdmin determines the final MultiAdmin configuration by preferring inline config over templates.
func ResolveMultiAdmin(spec *multigresv1alpha1.MultiAdminConfig, coreTemplate *multigresv1alpha1.CoreTemplate) *multigresv1alpha1.StatelessSpec {
	// If inline spec is present, use it.
	if spec.Spec != nil {
		return spec.Spec
	}

	// Otherwise, use the template (loaded by caller based on TemplateRef or Defaults)
	if coreTemplate != nil && coreTemplate.Spec.MultiAdmin != nil {
		return coreTemplate.Spec.MultiAdmin
	}

	return nil
}

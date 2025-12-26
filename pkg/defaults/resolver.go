package defaults

import (
	"context"
	"fmt"
	"reflect"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
}

// NewResolver creates a new defaults.Resolver.
func NewResolver(
	c client.Client,
	namespace string,
	tplDefaults multigresv1alpha1.TemplateDefaults,
) *Resolver {
	return &Resolver{
		Client:           c,
		Namespace:        namespace,
		TemplateDefaults: tplDefaults,
	}
}

// PopulateClusterDefaults applies static defaults to the Cluster Spec.
// This is safe for the Mutating Webhook because it DOES NOT fetch external templates.
// It ensures that "invisible defaults" (Images, default template names) are made visible
// and applies safety limits to any inline configurations provided by the user.
func (r *Resolver) PopulateClusterDefaults(cluster *multigresv1alpha1.MultigresCluster) {
	// 1. Default Images
	if cluster.Spec.Images.Postgres == "" {
		cluster.Spec.Images.Postgres = DefaultPostgresImage
	}
	if cluster.Spec.Images.MultiAdmin == "" {
		cluster.Spec.Images.MultiAdmin = DefaultMultiAdminImage
	}
	if cluster.Spec.Images.MultiOrch == "" {
		cluster.Spec.Images.MultiOrch = DefaultMultiOrchImage
	}
	if cluster.Spec.Images.MultiPooler == "" {
		cluster.Spec.Images.MultiPooler = DefaultMultiPoolerImage
	}
	if cluster.Spec.Images.MultiGateway == "" {
		cluster.Spec.Images.MultiGateway = DefaultMultiGatewayImage
	}
	if cluster.Spec.Images.ImagePullPolicy == "" {
		cluster.Spec.Images.ImagePullPolicy = DefaultImagePullPolicy
	}

	// 2. Default Template Refs (Strings only)
	if cluster.Spec.TemplateDefaults.CoreTemplate == "" {
		cluster.Spec.TemplateDefaults.CoreTemplate = FallbackCoreTemplate
	}
	if cluster.Spec.TemplateDefaults.CellTemplate == "" {
		cluster.Spec.TemplateDefaults.CellTemplate = FallbackCellTemplate
	}
	if cluster.Spec.TemplateDefaults.ShardTemplate == "" {
		cluster.Spec.TemplateDefaults.ShardTemplate = FallbackShardTemplate
	}

	// 3. Default Inline Configs (Deep Defaulting)
	// We ONLY default these if the user explicitly provided the block (Inline).
	// We DO NOT fetch templates here, adhering to the "Non-Goal" of the design doc.

	// GlobalTopoServer: If user provided 'etcd: {}', fill in the details.
	if cluster.Spec.GlobalTopoServer.Etcd != nil {
		defaultEtcdSpec(cluster.Spec.GlobalTopoServer.Etcd)
	}

	// MultiAdmin: If user provided 'spec: {}', fill in the details.
	if cluster.Spec.MultiAdmin.Spec != nil {
		defaultStatelessSpec(
			cluster.Spec.MultiAdmin.Spec,
			DefaultResourcesAdmin,
			DefaultAdminReplicas,
		)
	}

	// Cells: Default inline specs
	for i := range cluster.Spec.Cells {
		if cluster.Spec.Cells[i].Spec != nil {
			defaultStatelessSpec(
				&cluster.Spec.Cells[i].Spec.MultiGateway,
				// Note: You might want to define specific defaults for Gateway if they differ from Admin.
				// For now using the same pattern or generic defaults.
				// Assuming you might add DefaultResourcesGateway later, but using valid struct defaults here.
				corev1.ResourceRequirements{}, // Placeholder or define specific constant if needed
				1,                             // Default replicas
			)
		}
	}
}

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
	if reflect.DeepEqual(spec.Resources, corev1.ResourceRequirements{}) {
		spec.Resources = DefaultResourcesEtcd
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
	if reflect.DeepEqual(spec.Resources, corev1.ResourceRequirements{}) {
		spec.Resources = defaultRes
	}
}

// ============================================================================
// Resolution Logic (Used by Controller ONLY)
// These functions fetch Templates and Merge them. They are NOT for the Webhook.
// ============================================================================

// ResolveCoreTemplate determines the target CoreTemplate name and fetches it.
//
// If templateName is empty, it uses the following precedence:
// 1. The cluster-level default defined in TemplateDefaults.
// 2. A CoreTemplate named "default" found in the same namespace where MultigresCluster is deployed.
//
// If an explicit template (param or cluster default) is not found, it returns an error.
// If the implicit "default" template is not found, it returns an empty object (safe fallback).
func (r *Resolver) ResolveCoreTemplate(
	ctx context.Context,
	templateName string,
) (*multigresv1alpha1.CoreTemplate, error) {
	name := templateName
	isImplicitFallback := false

	if name == "" {
		name = r.TemplateDefaults.CoreTemplate
	}
	if name == "" {
		name = FallbackCoreTemplate
		isImplicitFallback = true
	}

	tpl := &multigresv1alpha1.CoreTemplate{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: r.Namespace}, tpl)
	if err != nil {
		if errors.IsNotFound(err) {
			if isImplicitFallback {
				return &multigresv1alpha1.CoreTemplate{}, nil
			}
			return nil, fmt.Errorf("referenced CoreTemplate '%s' not found: %w", name, err)
		}
		return nil, fmt.Errorf("failed to get CoreTemplate: %w", err)
	}
	return tpl, nil
}

// ResolveCellTemplate fetches and resolves a CellTemplate by name, handling defaults.
func (r *Resolver) ResolveCellTemplate(
	ctx context.Context,
	templateName string,
) (*multigresv1alpha1.CellTemplate, error) {
	name := templateName
	isImplicitFallback := false

	if name == "" {
		name = r.TemplateDefaults.CellTemplate
	}
	if name == "" {
		name = FallbackCellTemplate
		isImplicitFallback = true
	}

	tpl := &multigresv1alpha1.CellTemplate{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: r.Namespace}, tpl)
	if err != nil {
		if errors.IsNotFound(err) {
			if isImplicitFallback {
				return &multigresv1alpha1.CellTemplate{}, nil
			}
			return nil, fmt.Errorf("referenced CellTemplate '%s' not found: %w", name, err)
		}
		return nil, fmt.Errorf("failed to get CellTemplate: %w", err)
	}
	return tpl, nil
}

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

// MergeCellConfig merges a template spec with overrides and an inline spec to produce the final configuration.
func MergeCellConfig(
	template *multigresv1alpha1.CellTemplate,
	overrides *multigresv1alpha1.CellOverrides,
	inline *multigresv1alpha1.CellInlineSpec,
) (multigresv1alpha1.StatelessSpec, *multigresv1alpha1.LocalTopoServerSpec) {
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

// ResolveGlobalTopo determines the final GlobalTopoServer configuration.
// It prioritizes Inline Config > Template > Implicit Default (Managed Etcd).
// It applies deep defaults (safety limits) to the final result.
func ResolveGlobalTopo(
	spec *multigresv1alpha1.GlobalTopoServerSpec,
	coreTemplate *multigresv1alpha1.CoreTemplate,
) *multigresv1alpha1.GlobalTopoServerSpec {
	var finalSpec *multigresv1alpha1.GlobalTopoServerSpec

	// 1. Determine base config
	if spec.Etcd != nil || spec.External != nil {
		finalSpec = spec
	} else if coreTemplate != nil && coreTemplate.Spec.GlobalTopoServer != nil {
		// Copy from template
		finalSpec = &multigresv1alpha1.GlobalTopoServerSpec{
			Etcd: coreTemplate.Spec.GlobalTopoServer.Etcd.DeepCopy(),
		}
	} else {
		// Fallback: Default to an empty Etcd spec if nothing found.
		finalSpec = &multigresv1alpha1.GlobalTopoServerSpec{
			Etcd: &multigresv1alpha1.EtcdSpec{},
		}
	}

	// 2. Apply Deep Defaults to Etcd if present
	if finalSpec.Etcd != nil {
		defaultEtcdSpec(finalSpec.Etcd)
	}

	return finalSpec
}

// ResolveMultiAdmin determines the final MultiAdmin configuration.
// It prioritizes Inline Config > Template > Implicit Default.
// It applies deep defaults (safety limits) to the final result.
func ResolveMultiAdmin(
	spec *multigresv1alpha1.MultiAdminConfig,
	coreTemplate *multigresv1alpha1.CoreTemplate,
) *multigresv1alpha1.StatelessSpec {
	var finalSpec *multigresv1alpha1.StatelessSpec

	// 1. Determine base config
	if spec.Spec != nil {
		finalSpec = spec.Spec
	} else if coreTemplate != nil && coreTemplate.Spec.MultiAdmin != nil {
		finalSpec = coreTemplate.Spec.MultiAdmin
	} else {
		// Fallback to empty spec so we can apply defaults
		finalSpec = &multigresv1alpha1.StatelessSpec{}
	}

	// 2. Apply Deep Defaults
	defaultStatelessSpec(finalSpec, DefaultResourcesAdmin, DefaultAdminReplicas)

	return finalSpec
}

// ============================================================================
// Merge Helpers
// ============================================================================

func mergeStatelessSpec(
	base *multigresv1alpha1.StatelessSpec,
	override *multigresv1alpha1.StatelessSpec,
) {
	if override.Replicas != nil {
		base.Replicas = override.Replicas
	}
	if !reflect.DeepEqual(override.Resources, corev1.ResourceRequirements{}) {
		base.Resources = override.Resources
	}
	if override.Affinity != nil {
		base.Affinity = override.Affinity
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

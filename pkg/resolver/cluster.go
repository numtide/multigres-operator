package resolver

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

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
			DefaultResourcesAdmin(),
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
	if isResourcesEmpty(spec.Resources) {
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
	if isResourcesEmpty(spec.Resources) {
		// Safety: We assume defaultRes is passed by value (a fresh copy from the default function).
		// We perform a DeepCopy to ensure spec.Resources owns its own maps, independent of the input defaultRes.
		spec.Resources = *defaultRes.DeepCopy()
	}
}

// isResourcesEmpty checks if the resource requirements are effectively empty.
// We prefer this over reflect.DeepEqual to handle cases where maps are initialized but empty.
func isResourcesEmpty(res corev1.ResourceRequirements) bool {
	return len(res.Requests) == 0 && len(res.Limits) == 0
}

// isResourcesZero checks if the resource requirements are strictly the zero value (nil maps).
// This mimics reflect.DeepEqual(res, corev1.ResourceRequirements{}) but is safer and faster.
// It is used for merging logic where we want to distinguish "inherit" (nil) from "empty" (set to empty).
func isResourcesZero(res corev1.ResourceRequirements) bool {
	return res.Requests == nil && res.Limits == nil && res.Claims == nil
}

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
	defaultStatelessSpec(finalSpec, DefaultResourcesAdmin(), DefaultAdminReplicas)

	return finalSpec
}

package resolver

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// ResolveCell determines the final configuration for a specific Cell.
// It orchestrates: Template Lookup -> Fetch -> Merge -> Defaulting.
func (r *Resolver) ResolveCell(
	ctx context.Context,
	cellSpec *multigresv1alpha1.CellConfig,
) (*multigresv1alpha1.StatelessSpec, *multigresv1alpha1.LocalTopoServerSpec, error) {
	// 1. Fetch Template (Logic handles defaults)
	templateName := cellSpec.CellTemplate
	tpl, err := r.ResolveCellTemplate(ctx, templateName)
	if err != nil {
		return nil, nil, err
	}

	// 2. Merge Logic
	gateway, localTopo := mergeCellConfig(tpl, cellSpec.Overrides, cellSpec.Spec)

	// 3. Apply Deep Defaults (Level 4)
	// We use empty resources for Gateway default, as the specific values are often deployment-dependent,
	// but we must ensure Replicas is at least 1.
	defaultStatelessSpec(gateway, corev1.ResourceRequirements{}, 1)

	// Note: We do NOT default LocalTopo here because it is optional.
	// If it is nil, it remains nil (meaning the cell uses Global Topo).
	// If it is non-nil (e.g. from template), we apply Etcd defaults.
	if localTopo != nil && localTopo.Etcd != nil {
		defaultEtcdSpec(localTopo.Etcd)
	}

	return gateway, localTopo, nil
}

// ResolveCellTemplate fetches and resolves a CellTemplate by name.
// If name is empty, it resolves using the Cluster Defaults, then the Namespace Default.
func (r *Resolver) ResolveCellTemplate(
	ctx context.Context,
	name string,
) (*multigresv1alpha1.CellTemplate, error) {
	resolvedName := name
	isImplicitFallback := false

	if resolvedName == "" {
		resolvedName = r.TemplateDefaults.CellTemplate
	}
	if resolvedName == "" {
		resolvedName = FallbackCellTemplate
		isImplicitFallback = true
	}

	tpl := &multigresv1alpha1.CellTemplate{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: resolvedName, Namespace: r.Namespace}, tpl)
	if err != nil {
		if errors.IsNotFound(err) {
			if isImplicitFallback {
				// We return an empty struct instead of nil to satisfy tests expecting non-nil structure.
				return &multigresv1alpha1.CellTemplate{}, nil
			}
			return nil, fmt.Errorf("referenced CellTemplate '%s' not found: %w", resolvedName, err)
		}
		return nil, fmt.Errorf("failed to get CellTemplate: %w", err)
	}
	return tpl, nil
}

// mergeCellConfig merges a template spec with overrides and an inline spec.
func mergeCellConfig(
	template *multigresv1alpha1.CellTemplate,
	overrides *multigresv1alpha1.CellOverrides,
	inline *multigresv1alpha1.CellInlineSpec,
) (*multigresv1alpha1.StatelessSpec, *multigresv1alpha1.LocalTopoServerSpec) {
	gateway := &multigresv1alpha1.StatelessSpec{}
	var localTopo *multigresv1alpha1.LocalTopoServerSpec

	if template != nil {
		if template.Spec.MultiGateway != nil {
			gateway = template.Spec.MultiGateway.DeepCopy()
		}
		if template.Spec.LocalTopoServer != nil {
			localTopo = template.Spec.LocalTopoServer.DeepCopy()
		}
	}

	if overrides != nil {
		if overrides.MultiGateway != nil {
			mergeStatelessSpec(gateway, overrides.MultiGateway)
		}
	}

	if inline != nil {
		// Inline spec completely replaces the template for the components it defines
		// However, for Multigres 'Spec' blocks, usually 'Spec' is exclusive to 'TemplateRef'.
		// The design allows "Inline Spec" OR "Template + Overrides".
		// If Inline Spec is present, we generally prefer it entirely.
		gw := inline.MultiGateway.DeepCopy()
		var topo *multigresv1alpha1.LocalTopoServerSpec
		if inline.LocalTopoServer != nil {
			topo = inline.LocalTopoServer.DeepCopy()
		}
		return gw, topo
	}

	return gateway, localTopo
}

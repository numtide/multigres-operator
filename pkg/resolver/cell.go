package resolver

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

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

// MergeCellConfig merges a template spec with overrides and an inline spec to produce the final configuration.
func MergeCellConfig(
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
		gw := inline.MultiGateway.DeepCopy()
		var topo *multigresv1alpha1.LocalTopoServerSpec
		if inline.LocalTopoServer != nil {
			topo = inline.LocalTopoServer.DeepCopy()
		}
		return gw, topo
	}

	return gateway, localTopo
}

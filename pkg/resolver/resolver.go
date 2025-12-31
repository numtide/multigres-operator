// Package resolver provides the central logic for resolving MultigresCluster
// defaults, templates, and configurations.
//
// It serves as the single source of truth for merging user inputs,
// cluster-level defaults, and external templates into a final resource specification.
package resolver

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
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

package handlers

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resolver"
)

// +kubebuilder:webhook:path=/mutate-multigres-com-v1alpha1-multigrescluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=multigres.com,resources=multigresclusters,verbs=create;update,versions=v1alpha1,name=mmultigrescluster.kb.io,admissionReviewVersions=v1

// MultigresClusterDefaulter handles the mutation of MultigresCluster resources.
type MultigresClusterDefaulter struct {
	Resolver *resolver.Resolver
}

var _ webhook.CustomDefaulter = &MultigresClusterDefaulter{}

// NewMultigresClusterDefaulter creates a new defaulter handler.
func NewMultigresClusterDefaulter(r *resolver.Resolver) *MultigresClusterDefaulter {
	return &MultigresClusterDefaulter{
		Resolver: r,
	}
}

// Default implements webhook.CustomDefaulter.
func (d *MultigresClusterDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	// SAFETY CHECK
	if d.Resolver == nil {
		return fmt.Errorf("defaulter not initialized: resolver is nil")
	}

	cluster, ok := obj.(*multigresv1alpha1.MultigresCluster)
	if !ok {
		return fmt.Errorf("expected MultigresCluster, got %T", obj)
	}

	// 1. Static Defaulting (Images, System Catalog)
	if _, err := d.Resolver.PopulateClusterDefaults(ctx, cluster); err != nil {
		return fmt.Errorf("failed to populate cluster defaults: %w", err)
	}

	// 2. Create a "Request Scoped" Resolver
	// We copy the resolver and point it to the Object's Namespace.
	scopedResolver := *d.Resolver
	scopedResolver.Namespace = cluster.Namespace
	scopedResolver.TemplateDefaults = cluster.Spec.TemplateDefaults

	// 2.5 Promote Implicit Defaults to Explicit
	// If the user hasn't specified a template, but a "default" one exists,
	// we explicitly set it in the Spec. This ensures the user KNOWS a template is being used
	// instead of it happening magically behind the scenes.
	{
		if cluster.Spec.TemplateDefaults.CoreTemplate == "" {
			exists, _ := scopedResolver.CoreTemplateExists(ctx, resolver.FallbackCoreTemplate)
			if exists {
				cluster.Spec.TemplateDefaults.CoreTemplate = resolver.FallbackCoreTemplate
				scopedResolver.TemplateDefaults.CoreTemplate = resolver.FallbackCoreTemplate
			}
		}
		if cluster.Spec.TemplateDefaults.CellTemplate == "" {
			exists, _ := scopedResolver.CellTemplateExists(ctx, resolver.FallbackCellTemplate)
			if exists {
				cluster.Spec.TemplateDefaults.CellTemplate = resolver.FallbackCellTemplate
				scopedResolver.TemplateDefaults.CellTemplate = resolver.FallbackCellTemplate
			}
		}
		if cluster.Spec.TemplateDefaults.ShardTemplate == "" {
			exists, _ := scopedResolver.ShardTemplateExists(ctx, resolver.FallbackShardTemplate)
			if exists {
				cluster.Spec.TemplateDefaults.ShardTemplate = resolver.FallbackShardTemplate
				scopedResolver.TemplateDefaults.ShardTemplate = resolver.FallbackShardTemplate
			}
		}
	}

	// 3. Stateful Resolution (Visible Defaults)

	// A. Resolve Global Topo Server
	// Logic:
	// 1. If explicit Inline Template -> Skip (Dynamic)
	// 2. If explicit Global Template (TemplateDefaults) -> Skip (Dynamic)
	// 3. If implicit "default" Template exists -> Skip (Dynamic)
	// 4. Else -> Materialize Hardcoded Defaults.

	// Helper to check intent
	hasGlobalCore := cluster.Spec.TemplateDefaults.CoreTemplate != ""
	hasImplicitCore, _ := scopedResolver.CoreTemplateExists(
		ctx,
		resolver.FallbackCoreTemplate,
	) // Ignore error, treat as false

	// GlobalTopo
	{
		hasInline := cluster.Spec.GlobalTopoServer != nil &&
			cluster.Spec.GlobalTopoServer.TemplateRef != ""
			// We also check if the user provided inline CONFIG (External or Etcd spec).
			// If they provided config but no template, we might still want to merge defaults?
			// User rule: "When NOT using templates, materialize whatever defaults".
			// "Using templates" means Inline OR Global OR Implicit exists.

		isUsingTemplate := hasInline || hasGlobalCore || hasImplicitCore

		if !isUsingTemplate {
			// No template involved. Materialize defaults.
			globalTopo, err := scopedResolver.ResolveGlobalTopo(ctx, cluster)
			if err != nil {
				return fmt.Errorf("failed to resolve globalTopoServer: %w", err)
			}
			cluster.Spec.GlobalTopoServer = globalTopo
		}
	}

	// B. Resolve MultiAdmin
	{
		hasInline := cluster.Spec.MultiAdmin != nil && cluster.Spec.MultiAdmin.TemplateRef != ""
		isUsingTemplate := hasInline || hasGlobalCore || hasImplicitCore

		if !isUsingTemplate {
			multiAdmin, err := scopedResolver.ResolveMultiAdmin(ctx, cluster)
			if err != nil {
				return fmt.Errorf("failed to resolve multiadmin: %w", err)
			}
			if cluster.Spec.MultiAdmin == nil {
				cluster.Spec.MultiAdmin = &multigresv1alpha1.MultiAdminConfig{}
			}
			if multiAdmin != nil {
				cluster.Spec.MultiAdmin.Spec = multiAdmin
			}
		}
	}

	// B2. Resolve MultiAdminWeb
	{
		hasInline := cluster.Spec.MultiAdminWeb != nil &&
			cluster.Spec.MultiAdminWeb.TemplateRef != ""
		isUsingTemplate := hasInline || hasGlobalCore || hasImplicitCore

		if !isUsingTemplate {
			multiAdminWeb, err := scopedResolver.ResolveMultiAdminWeb(ctx, cluster)
			if err != nil {
				return fmt.Errorf("failed to resolve multiadmin-web: %w", err)
			}
			if cluster.Spec.MultiAdminWeb == nil {
				cluster.Spec.MultiAdminWeb = &multigresv1alpha1.MultiAdminWebConfig{}
			}
			if multiAdminWeb != nil {
				cluster.Spec.MultiAdminWeb.Spec = multiAdminWeb
			}
		}
	}

	// C. Resolve Cells
	hasGlobalCell := cluster.Spec.TemplateDefaults.CellTemplate != ""
	hasImplicitCell, _ := scopedResolver.CellTemplateExists(ctx, resolver.FallbackCellTemplate)

	for i := range cluster.Spec.Cells {
		cell := &cluster.Spec.Cells[i]
		hasInline := cell.CellTemplate != ""

		isUsingTemplate := hasInline || hasGlobalCell || hasImplicitCell

		if !isUsingTemplate {
			gatewaySpec, localTopoSpec, err := scopedResolver.ResolveCell(ctx, cell)
			if err != nil {
				return fmt.Errorf("failed to resolve cell '%s': %w", cell.Name, err)
			}
			cell.Spec = &multigresv1alpha1.CellInlineSpec{
				MultiGateway:    *gatewaySpec,
				LocalTopoServer: localTopoSpec,
			}
		}
	}

	// D. Resolve Shards
	hasGlobalShard := cluster.Spec.TemplateDefaults.ShardTemplate != ""
	hasImplicitShard, _ := scopedResolver.ShardTemplateExists(ctx, resolver.FallbackShardTemplate)

	for i := range cluster.Spec.Databases {
		for j := range cluster.Spec.Databases[i].TableGroups {
			for k := range cluster.Spec.Databases[i].TableGroups[j].Shards {
				shard := &cluster.Spec.Databases[i].TableGroups[j].Shards[k]
				hasInline := shard.ShardTemplate != ""

				isUsingTemplate := hasInline || hasGlobalShard || hasImplicitShard

				if !isUsingTemplate {
					// We pass 'nil' for allCellNames to prevent "Sticky Context Defaults".
					// We want the Stored Spec to remain empty (dynamic) rather than locking in the current list of cells.
					multiOrchSpec, poolsSpec, err := scopedResolver.ResolveShard(ctx, shard, nil)
					if err != nil {
						return fmt.Errorf("failed to resolve shard '%s': %w", shard.Name, err)
					}
					shard.Spec = &multigresv1alpha1.ShardInlineSpec{
						MultiOrch: *multiOrchSpec,
						Pools:     poolsSpec,
					}
				}
			}
		}
	}

	return nil
}

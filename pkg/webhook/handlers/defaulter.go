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
	d.Resolver.PopulateClusterDefaults(cluster)

	// 2. Create a "Request Scoped" Resolver
	// We copy the resolver and point it to the Object's Namespace.
	scopedResolver := *d.Resolver
	scopedResolver.Namespace = cluster.Namespace
	scopedResolver.TemplateDefaults = cluster.Spec.TemplateDefaults

	// 3. Stateful Resolution (Visible Defaults)

	// A. Resolve Global Topo Server
	if cluster.Spec.GlobalTopoServer == nil ||
		(cluster.Spec.GlobalTopoServer.TemplateRef == "" && cluster.Spec.GlobalTopoServer.External == nil) {
		globalTopo, err := scopedResolver.ResolveGlobalTopo(ctx, cluster)
		if err != nil {
			return fmt.Errorf("failed to resolve globalTopoServer: %w", err)
		}
		cluster.Spec.GlobalTopoServer = globalTopo
	}

	// B. Resolve MultiAdmin
	if cluster.Spec.MultiAdmin == nil || cluster.Spec.MultiAdmin.TemplateRef == "" {
		multiAdmin, err := scopedResolver.ResolveMultiAdmin(ctx, cluster)
		if err != nil {
			return fmt.Errorf("failed to resolve multiadmin: %w", err)
		}
		if cluster.Spec.MultiAdmin == nil {
			cluster.Spec.MultiAdmin = &multigresv1alpha1.MultiAdminConfig{}
		}
		cluster.Spec.MultiAdmin.Spec = multiAdmin
	}

	// C. Resolve Cells
	for i := range cluster.Spec.Cells {
		cell := &cluster.Spec.Cells[i]
		if cell.CellTemplate == "" {
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
	for i := range cluster.Spec.Databases {
		for j := range cluster.Spec.Databases[i].TableGroups {
			for k := range cluster.Spec.Databases[i].TableGroups[j].Shards {
				shard := &cluster.Spec.Databases[i].TableGroups[j].Shards[k]
				if shard.ShardTemplate == "" {
					multiOrchSpec, poolsSpec, err := scopedResolver.ResolveShard(ctx, shard)
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

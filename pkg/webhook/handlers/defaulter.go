package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resolver"
)

// +kubebuilder:webhook:path=/mutate-multigres-com-v1alpha1-multigrescluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=multigres.com,resources=multigresclusters,verbs=create;update,versions=v1alpha1,name=mmultigrescluster.kb.io,admissionReviewVersions=v1

// MultigresClusterDefaulter handles the mutation of MultigresCluster resources.
type MultigresClusterDefaulter struct {
	Resolver *resolver.Resolver
	decoder  admission.Decoder
}

// NewMultigresClusterDefaulter creates a new defaulter handler.
func NewMultigresClusterDefaulter(r *resolver.Resolver) *MultigresClusterDefaulter {
	return &MultigresClusterDefaulter{
		Resolver: r,
	}
}

// InjectDecoder injects the decoder.
func (d *MultigresClusterDefaulter) InjectDecoder(decoder admission.Decoder) error {
	d.decoder = decoder
	return nil
}

// Handle implements the admission.Handler interface.
func (d *MultigresClusterDefaulter) Handle(
	ctx context.Context,
	req admission.Request,
) admission.Response {
	// SAFETY CHECK
	if d.Resolver == nil {
		return admission.Errored(
			http.StatusInternalServerError,
			fmt.Errorf("defaulter not initialized: resolver is nil"),
		)
	}

	cluster := &multigresv1alpha1.MultigresCluster{}

	if d.decoder != nil {
		err := d.decoder.Decode(req, cluster)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	} else {
		if err := json.Unmarshal(req.Object.Raw, cluster); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	}

	// 1. Static Defaulting (Images, System Catalog)
	d.Resolver.PopulateClusterDefaults(cluster)

	// 2. Create a "Request Scoped" Resolver
	// CRITICAL FIX: We must copy the resolver and point it to the Request's Namespace.
	// Otherwise, it looks for templates in the Operator's namespace and finds nothing.
	scopedResolver := *d.Resolver
	scopedResolver.Namespace = req.Namespace
	scopedResolver.TemplateDefaults = cluster.Spec.TemplateDefaults

	// 3. Stateful Resolution (Visible Defaults)

	// A. Resolve Global Topo Server
	if cluster.Spec.GlobalTopoServer == nil ||
		(cluster.Spec.GlobalTopoServer.TemplateRef == "" && cluster.Spec.GlobalTopoServer.External == nil) {
		globalTopo, err := scopedResolver.ResolveGlobalTopo(ctx, cluster)
		if err != nil {
			return admission.Errored(
				http.StatusInternalServerError,
				fmt.Errorf("failed to resolve globalTopoServer: %w", err),
			)
		}
		cluster.Spec.GlobalTopoServer = globalTopo
	}

	// B. Resolve MultiAdmin
	if cluster.Spec.MultiAdmin == nil || cluster.Spec.MultiAdmin.TemplateRef == "" {
		multiAdmin, err := scopedResolver.ResolveMultiAdmin(ctx, cluster)
		if err != nil {
			return admission.Errored(
				http.StatusInternalServerError,
				fmt.Errorf("failed to resolve multiadmin: %w", err),
			)
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
				return admission.Errored(
					http.StatusInternalServerError,
					fmt.Errorf("failed to resolve cell '%s': %w", cell.Name, err),
				)
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
						return admission.Errored(
							http.StatusInternalServerError,
							fmt.Errorf("failed to resolve shard '%s': %w", shard.Name, err),
						)
					}
					shard.Spec = &multigresv1alpha1.ShardInlineSpec{
						MultiOrch: *multiOrchSpec,
						Pools:     poolsSpec,
					}
				}
			}
		}
	}

	marshaled, err := json.Marshal(cluster)
	if err != nil {
		return admission.Errored(
			http.StatusInternalServerError,
			fmt.Errorf("failed to marshal defaulted object: %w", err),
		)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaled)
}

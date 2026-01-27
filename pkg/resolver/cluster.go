package resolver

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// PopulateClusterDefaults applies static defaults to the Cluster Spec.
func (r *Resolver) PopulateClusterDefaults(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
) error {
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

	// 2. Smart Defaulting: System Catalog
	if len(cluster.Spec.Databases) == 0 {
		cluster.Spec.Databases = append(cluster.Spec.Databases, multigresv1alpha1.DatabaseConfig{
			Name:    DefaultSystemDatabaseName,
			Default: true,
		})
	}

	var defaultCells []multigresv1alpha1.CellName
	for _, c := range cluster.Spec.Cells {
		defaultCells = append(defaultCells, multigresv1alpha1.CellName(c.Name))
	}

	// Logic: Should we inject the "default" pool inline?
	// Rule 1: If user EXPLICITLY requested a template, NEVER inject defaults. Trust the user.
	// Rule 2: If user requested NOTHING, check if "default" template exists.
	//         If exists -> Do not inject (use implicit template).
	//         If missing -> Inject defaults (Zero Config mode).
	shouldInjectDefaults := false

	userExplicitTemplate := cluster.Spec.TemplateDefaults.ShardTemplate
	if userExplicitTemplate != "" {
		// Rule 1: Explicit template -> No defaults
		shouldInjectDefaults = false
	} else {
		// Rule 2: No explicit template. Check for implicit "default".
		implicitExists, err := r.ShardTemplateExists(ctx, "default")
		if err != nil {
			return fmt.Errorf("failed to check for implicit shard template: %w", err)
		}
		if implicitExists {
			shouldInjectDefaults = false
		} else {
			shouldInjectDefaults = true
		}
	}

	for i := range cluster.Spec.Databases {
		if len(cluster.Spec.Databases[i].TableGroups) == 0 {
			cluster.Spec.Databases[i].TableGroups = append(
				cluster.Spec.Databases[i].TableGroups,
				multigresv1alpha1.TableGroupConfig{
					Name:    DefaultSystemTableGroupName,
					Default: true,
				},
			)
		}

		for j := range cluster.Spec.Databases[i].TableGroups {
			if len(cluster.Spec.Databases[i].TableGroups[j].Shards) == 0 {
				shardCfg := multigresv1alpha1.ShardConfig{
					Name: "0-inf",
				}

				if len(defaultCells) > 0 {
					shardCfg.Spec = &multigresv1alpha1.ShardInlineSpec{
						MultiOrch: multigresv1alpha1.MultiOrchSpec{
							// Clean Spec: Do not inject default cells statically.
							// This allows dynamic contextual resolution later.
							// Cells: defaultCells,
						},
						Pools: make(map[string]multigresv1alpha1.PoolSpec),
					}

					// Apply the decision made above
					if shouldInjectDefaults {
						shardCfg.Spec.Pools["default"] = multigresv1alpha1.PoolSpec{
							Type: "readWrite",
							// Clean Spec: Do not inject default cells.
							// Cells: defaultCells,
						}
					}
				}

				cluster.Spec.Databases[i].TableGroups[j].Shards = append(
					cluster.Spec.Databases[i].TableGroups[j].Shards,
					shardCfg,
				)
			}
		}
	}

	return nil
}

// ResolveGlobalTopo determines the final GlobalTopoServer configuration.
func (r *Resolver) ResolveGlobalTopo(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
) (*multigresv1alpha1.GlobalTopoServerSpec, error) {
	var templateName string
	var spec *multigresv1alpha1.GlobalTopoServerSpec

	if cluster.Spec.GlobalTopoServer != nil {
		templateName = cluster.Spec.GlobalTopoServer.TemplateRef
		spec = cluster.Spec.GlobalTopoServer
	}

	coreTemplate, err := r.ResolveCoreTemplate(ctx, templateName)
	if err != nil {
		return nil, err
	}

	var finalSpec *multigresv1alpha1.GlobalTopoServerSpec

	if coreTemplate != nil && coreTemplate.Spec.GlobalTopoServer != nil {
		finalSpec = &multigresv1alpha1.GlobalTopoServerSpec{
			Etcd: coreTemplate.Spec.GlobalTopoServer.Etcd.DeepCopy(),
		}
	} else {
		finalSpec = &multigresv1alpha1.GlobalTopoServerSpec{
			Etcd: &multigresv1alpha1.EtcdSpec{},
		}
	}

	if spec != nil {
		if spec.External != nil {
			finalSpec.External = spec.External.DeepCopy()
			finalSpec.Etcd = nil
		} else if spec.Etcd != nil {
			if finalSpec.Etcd == nil {
				finalSpec.Etcd = &multigresv1alpha1.EtcdSpec{}
			}
			mergeEtcdSpec(finalSpec.Etcd, spec.Etcd)
		}
	}

	if finalSpec.Etcd != nil {
		defaultEtcdSpec(finalSpec.Etcd)
	}

	return finalSpec, nil
}

// ResolveMultiAdmin determines the final MultiAdmin configuration.
func (r *Resolver) ResolveMultiAdmin(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
) (*multigresv1alpha1.StatelessSpec, error) {
	var templateName string
	var spec *multigresv1alpha1.MultiAdminConfig

	if cluster.Spec.MultiAdmin != nil {
		templateName = cluster.Spec.MultiAdmin.TemplateRef
		spec = cluster.Spec.MultiAdmin
	}

	coreTemplate, err := r.ResolveCoreTemplate(ctx, templateName)
	if err != nil {
		return nil, err
	}

	finalSpec := &multigresv1alpha1.StatelessSpec{}

	if coreTemplate != nil && coreTemplate.Spec.MultiAdmin != nil {
		finalSpec = coreTemplate.Spec.MultiAdmin.DeepCopy()
	}

	if spec != nil && spec.Spec != nil {
		mergeStatelessSpec(finalSpec, spec.Spec)
	}

	defaultStatelessSpec(finalSpec, DefaultResourcesAdmin(), DefaultAdminReplicas)

	return finalSpec, nil
}

func (r *Resolver) ResolveCoreTemplate(
	ctx context.Context,
	name string,
) (*multigresv1alpha1.CoreTemplate, error) {
	resolvedName := name
	isImplicitFallback := false

	if resolvedName == "" {
		resolvedName = r.TemplateDefaults.CoreTemplate
	}
	if resolvedName == "" || resolvedName == FallbackCoreTemplate {
		resolvedName = FallbackCoreTemplate
		isImplicitFallback = true
	}

	tpl := &multigresv1alpha1.CoreTemplate{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: resolvedName, Namespace: r.Namespace}, tpl)
	if err != nil {
		if errors.IsNotFound(err) {
			if isImplicitFallback {
				return &multigresv1alpha1.CoreTemplate{}, nil
			}
			return nil, fmt.Errorf("referenced CoreTemplate '%s' not found: %w", resolvedName, err)
		}
		return nil, fmt.Errorf("failed to get CoreTemplate: %w", err)
	}
	return tpl, nil
}

func mergeEtcdSpec(base *multigresv1alpha1.EtcdSpec, override *multigresv1alpha1.EtcdSpec) {
	if override.Image != "" {
		base.Image = override.Image
	}
	if override.Replicas != nil {
		base.Replicas = override.Replicas
	}
	if override.Storage.Size != "" {
		base.Storage = override.Storage
	}
	if !isResourcesZero(override.Resources) {
		base.Resources = *override.Resources.DeepCopy()
	}
	if override.RootPath != "" {
		base.RootPath = override.RootPath
	}
}

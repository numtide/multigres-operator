package resolver

import (
	"context"
	"fmt"

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

	// 3. Smart Defaulting: System Catalog
	// If no databases are defined, inject the mandatory system database "postgres".
	if len(cluster.Spec.Databases) == 0 {
		cluster.Spec.Databases = append(cluster.Spec.Databases, multigresv1alpha1.DatabaseConfig{
			Name:    DefaultSystemDatabaseName,
			Default: true,
		})
	}

	// Iterate databases to ensure TableGroups and Shards exist
	for i := range cluster.Spec.Databases {
		// If any database has no tablegroups, inject the mandatory default tablegroup "default".
		if len(cluster.Spec.Databases[i].TableGroups) == 0 {
			cluster.Spec.Databases[i].TableGroups = append(
				cluster.Spec.Databases[i].TableGroups,
				multigresv1alpha1.TableGroupConfig{
					Name:    DefaultSystemTableGroupName,
					Default: true,
				},
			)
		}

		// If any TableGroup has no Shards, inject the mandatory default Shard "0".
		// This ensures minimal configs result in actual running pods.
		for j := range cluster.Spec.Databases[i].TableGroups {
			if len(cluster.Spec.Databases[i].TableGroups[j].Shards) == 0 {
				cluster.Spec.Databases[i].TableGroups[j].Shards = append(
					cluster.Spec.Databases[i].TableGroups[j].Shards,
					multigresv1alpha1.ShardConfig{
						Name: "0",
						// ShardTemplate defaults to "" here, which will be resolved
						// to "default" (FallbackShardTemplate) by ResolveShard logic later.
					},
				)
			}
		}
	}
}

// ResolveGlobalTopo determines the final GlobalTopoServer configuration.
// It handles the precedence: Inline > TemplateRef (Specific) > TemplateRef (Cluster Default) > Fallback.
// It performs the necessary I/O to fetch the CoreTemplate.
func (r *Resolver) ResolveGlobalTopo(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
) (*multigresv1alpha1.GlobalTopoServerSpec, error) {
	// 1. Fetch Template (Logic handles defaults)
	templateName := cluster.Spec.GlobalTopoServer.TemplateRef
	coreTemplate, err := r.ResolveCoreTemplate(ctx, templateName)
	if err != nil {
		return nil, err
	}

	// 2. Merge Config
	var finalSpec *multigresv1alpha1.GlobalTopoServerSpec
	spec := cluster.Spec.GlobalTopoServer

	if spec.Etcd != nil || spec.External != nil {
		// Inline definition takes precedence
		finalSpec = spec.DeepCopy()
	} else if coreTemplate != nil && coreTemplate.Spec.GlobalTopoServer != nil {
		// Copy from template
		finalSpec = &multigresv1alpha1.GlobalTopoServerSpec{
			Etcd: coreTemplate.Spec.GlobalTopoServer.Etcd.DeepCopy(),
		}
	} else {
		// Fallback: Default to an empty Etcd spec if nothing found
		finalSpec = &multigresv1alpha1.GlobalTopoServerSpec{
			Etcd: &multigresv1alpha1.EtcdSpec{},
		}
	}

	// 3. Apply Deep Defaults (Level 4)
	if finalSpec.Etcd != nil {
		defaultEtcdSpec(finalSpec.Etcd)
	}

	return finalSpec, nil
}

// ResolveMultiAdmin determines the final MultiAdmin configuration.
// It handles the precedence: Inline > TemplateRef (Specific) > TemplateRef (Cluster Default) > Fallback.
func (r *Resolver) ResolveMultiAdmin(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
) (*multigresv1alpha1.StatelessSpec, error) {
	// 1. Fetch Template (Logic handles defaults)
	templateName := cluster.Spec.MultiAdmin.TemplateRef
	coreTemplate, err := r.ResolveCoreTemplate(ctx, templateName)
	if err != nil {
		return nil, err
	}

	// 2. Merge Config
	var finalSpec *multigresv1alpha1.StatelessSpec
	spec := cluster.Spec.MultiAdmin

	if spec.Spec != nil {
		finalSpec = spec.Spec.DeepCopy()
	} else if coreTemplate != nil && coreTemplate.Spec.MultiAdmin != nil {
		finalSpec = coreTemplate.Spec.MultiAdmin.DeepCopy()
	} else {
		finalSpec = &multigresv1alpha1.StatelessSpec{}
	}

	// 3. Apply Deep Defaults (Level 4)
	defaultStatelessSpec(finalSpec, DefaultResourcesAdmin(), DefaultAdminReplicas)

	return finalSpec, nil
}

// ResolveCoreTemplate fetches a CoreTemplate by name.
// If name is empty, it resolves using the Cluster Defaults, then the Namespace Default.
func (r *Resolver) ResolveCoreTemplate(
	ctx context.Context,
	name string,
) (*multigresv1alpha1.CoreTemplate, error) {
	resolvedName := name
	isImplicitFallback := false

	if resolvedName == "" {
		resolvedName = r.TemplateDefaults.CoreTemplate
	}
	if resolvedName == "" {
		resolvedName = FallbackCoreTemplate
		isImplicitFallback = true
	}

	tpl := &multigresv1alpha1.CoreTemplate{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: resolvedName, Namespace: r.Namespace}, tpl)
	if err != nil {
		if errors.IsNotFound(err) {
			if isImplicitFallback {
				// We return an empty struct instead of nil to satisfy tests expecting non-nil structure.
				return &multigresv1alpha1.CoreTemplate{}, nil
			}
			return nil, fmt.Errorf("referenced CoreTemplate '%s' not found: %w", resolvedName, err)
		}
		return nil, fmt.Errorf("failed to get CoreTemplate: %w", err)
	}
	return tpl, nil
}

//go:build e2e

package framework

import (
	"testing"

	"github.com/stretchr/testify/require"

	multigresv1alpha1 "github.com/multigres/multigres-operator/api/v1alpha1"
)

func TestMinimalFixtureUsesFailureSafeBootstrapCohort(t *testing.T) {
	cluster := MustLoadCluster("config/samples/minimal.yaml", "test")
	pool := cluster.Spec.Databases[0].TableGroups[0].Shards[0].Spec.Pools["default"]
	if pool.ReplicasPerCell == nil {
		t.Fatal("synthetic default pool replicasPerCell is nil")
	}
	if got, want := *pool.ReplicasPerCell, int32(3); got != want {
		t.Fatalf("synthetic default pool replicasPerCell = %d, want %d", got, want)
	}
}

func TestTemplatedFixturePreservesReferences(t *testing.T) {
	cluster := MustLoadCluster("test/e2e/fixtures/templated.yaml", "test")
	WithCIResources(&cluster.Spec) // Callers may apply resources more than once.
	require.Equal(
		t,
		multigresv1alpha1.TemplateRef("e2e-core"),
		cluster.Spec.TemplateDefaults.CoreTemplate,
	)
	require.Nil(t, cluster.Spec.GlobalTopoServer)
	require.Nil(t, cluster.Spec.Multiadmin)
	require.Nil(t, cluster.Spec.MultiadminWeb)
	require.Equal(t, multigresv1alpha1.TemplateRef("e2e-cell"), cluster.Spec.Cells[0].CellTemplate)
	require.Nil(t, cluster.Spec.Cells[0].Spec)
	shard := cluster.Spec.Databases[0].TableGroups[0].Shards[0]
	require.Equal(t, multigresv1alpha1.TemplateRef("e2e-shard"), shard.ShardTemplate)
	require.Nil(t, shard.Spec)

	template := MustLoadShardTemplate("test/e2e/fixtures/templates/shard.yaml", "test")
	pool := template.Spec.Pools["default"]
	require.NotNil(t, pool.ReplicasPerCell)
	require.Equal(t, int32(3), *pool.ReplicasPerCell)
}

func TestWithCIResourcesPreservesTemplateConfiguration(t *testing.T) {
	for _, defaults := range []bool{false, true} {
		name := "explicit references"
		if defaults {
			name = "template defaults"
		}
		t.Run(name, func(t *testing.T) {
			spec := multigresv1alpha1.MultigresClusterSpec{
				GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{TemplateRef: "core"},
				Multiadmin:       &multigresv1alpha1.MultiadminConfig{TemplateRef: "core"},
				MultiadminWeb:    &multigresv1alpha1.MultiadminWebConfig{TemplateRef: "core"},
				Cells: []multigresv1alpha1.CellConfig{
					{Name: "cell", ZoneID: "us-central1-a", CellTemplate: "cell"},
				},
				Databases: []multigresv1alpha1.DatabaseConfig{
					{Name: "postgres", TableGroups: []multigresv1alpha1.TableGroupConfig{
						{
							Name: "default",
							Shards: []multigresv1alpha1.ShardConfig{
								{Name: "0-inf", ShardTemplate: "shard"},
							},
						},
					}},
				},
			}
			if defaults {
				spec.TemplateDefaults = multigresv1alpha1.TemplateDefaults{
					CoreTemplate:  "core",
					CellTemplate:  "cell",
					ShardTemplate: "shard",
				}
				spec.GlobalTopoServer, spec.Multiadmin, spec.MultiadminWeb = nil, nil, nil
				spec.Cells[0].CellTemplate = ""
				spec.Databases[0].TableGroups[0].Shards[0].ShardTemplate = ""
			}
			before := spec.DeepCopy()
			WithCIResources(&spec)
			WithCIResources(&spec)
			require.Equal(t, before, &spec)
			if defaults {
				spec.Databases[0].TableGroups[0].Shards = nil
				WithCIResources(&spec)
				require.Nil(t, spec.Databases[0].TableGroups[0].Shards[0].Spec,
					"a synthesized shard must still inherit its default template")
			}
		})
	}
}

func TestWithCIResourcesPreservesOverridesAndExternalTopo(t *testing.T) {
	spec := multigresv1alpha1.MultigresClusterSpec{
		GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
			External: &multigresv1alpha1.ExternalTopoServerSpec{
				Endpoints: []multigresv1alpha1.EndpointUrl{"http://etcd:2379"},
			},
		},
		Cells: []multigresv1alpha1.CellConfig{
			{Name: "cell", Overrides: &multigresv1alpha1.CellOverrides{}},
		},
		Databases: []multigresv1alpha1.DatabaseConfig{
			{Name: "postgres", TableGroups: []multigresv1alpha1.TableGroupConfig{
				{
					Name: "default",
					Shards: []multigresv1alpha1.ShardConfig{
						{Name: "0-inf", Overrides: &multigresv1alpha1.ShardOverrides{}},
					},
				},
			}},
		},
	}
	WithCIResources(&spec)
	require.Nil(t, spec.GlobalTopoServer.Etcd)
	require.Equal(
		t,
		[]multigresv1alpha1.EndpointUrl{"http://etcd:2379"},
		spec.GlobalTopoServer.External.Endpoints,
	)
	require.Nil(t, spec.Cells[0].Spec)
	require.NotNil(t, spec.Cells[0].Overrides)
	require.Nil(t, spec.Databases[0].TableGroups[0].Shards[0].Spec)
	require.NotNil(t, spec.Databases[0].TableGroups[0].Shards[0].Overrides)
}

func TestWithCIResourcesPreservesInlineConfiguration(t *testing.T) {
	cluster := MustLoadCluster("config/samples/no-templates.yaml", "test")
	spec := &cluster.Spec
	before := spec.DeepCopy()
	// Explicit inline configuration still wins over template defaults.
	spec.TemplateDefaults = multigresv1alpha1.TemplateDefaults{
		CoreTemplate:  "core",
		CellTemplate:  "cell",
		ShardTemplate: "shard",
	}
	WithCIResources(spec)
	require.Equal(t, before.GlobalTopoServer, spec.GlobalTopoServer)
	require.Equal(t, before.Multiadmin, spec.Multiadmin)
	require.Equal(t, before.MultiadminWeb, spec.MultiadminWeb)
	require.Equal(t, before.Cells, spec.Cells)
	require.Equal(t, before.Databases, spec.Databases)
}

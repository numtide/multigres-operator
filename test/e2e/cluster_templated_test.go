//go:build e2e

package e2e_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

// TestTemplatedCluster applies templates and a templated cluster CR, then
// verifies the full resource tree, pod health, and psql connectivity.
func TestTemplatedCluster(t *testing.T) {
	tc, operatorSetup := newClusterWithOperator(t)
	ns := createNamespace(t, tc)
	c := newCRClient(t, tc)

	feat := features.New("templated-cluster").
		Setup(operatorSetup).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			t.Helper()

			// Create CoreTemplate.
			coreTemplate := &multigresv1alpha1.CoreTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "standard-core", Namespace: ns},
				Spec: multigresv1alpha1.CoreTemplateSpec{
					GlobalTopoServer: &multigresv1alpha1.TopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{
							Image:     "gcr.io/etcd-development/etcd:v3.6.7",
							Replicas:  ptr.To(int32(3)),
							Storage:   multigresv1alpha1.StorageSpec{Size: "1Gi"},
							Resources: ciResources(),
						},
					},
					MultiAdmin: &multigresv1alpha1.StatelessSpec{
						Replicas:  ptr.To(int32(2)),
						Resources: ciResources(),
					},
				},
			}
			if err := c.Create(ctx, coreTemplate); err != nil {
				t.Fatalf("Failed to create CoreTemplate: %v", err)
			}

			// Create CellTemplate.
			cellTemplate := &multigresv1alpha1.CellTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "standard-cell", Namespace: ns},
				Spec: multigresv1alpha1.CellTemplateSpec{
					MultiGateway: &multigresv1alpha1.StatelessSpec{
						Replicas:  ptr.To(int32(2)),
						Resources: ciResources(),
					},
				},
			}
			if err := c.Create(ctx, cellTemplate); err != nil {
				t.Fatalf("Failed to create CellTemplate: %v", err)
			}

			// Create ShardTemplate.
			shardTemplate := &multigresv1alpha1.ShardTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "standard-shard", Namespace: ns},
				Spec: multigresv1alpha1.ShardTemplateSpec{
					MultiOrch: &multigresv1alpha1.MultiOrchSpec{
						StatelessSpec: multigresv1alpha1.StatelessSpec{
							Resources: ciResources(),
						},
					},
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"main-app": {
							Type:            "readWrite",
							ReplicasPerCell: ptr.To(int32(2)),
							Storage:         multigresv1alpha1.StorageSpec{Size: "1Gi"},
							Postgres:        ciContainerConfig(),
							Multipooler:     ciContainerConfig(),
						},
					},
				},
			}
			if err := c.Create(ctx, shardTemplate); err != nil {
				t.Fatalf("Failed to create ShardTemplate: %v", err)
			}

			// Create the cluster referencing templates.
			cluster := &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "standard-ha-cluster", Namespace: ns},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					TemplateDefaults: multigresv1alpha1.TemplateDefaults{
						CoreTemplate:  "standard-core",
						CellTemplate:  "standard-cell",
						ShardTemplate: "standard-shard",
					},
					GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
						TemplateRef: "standard-core",
					},
					MultiAdmin: &multigresv1alpha1.MultiAdminConfig{
						TemplateRef: "standard-core",
					},
					Cells: []multigresv1alpha1.CellConfig{
						{Name: "us-east-1a"},
						{Name: "us-east-1b"},
					},
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name:    "postgres",
							Default: true,
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{
									Name:    "default",
									Default: true,
									Shards: []multigresv1alpha1.ShardConfig{
										{
											Name: "0-inf",
											Overrides: &multigresv1alpha1.ShardOverrides{
												Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
													"main-app": {
														Cells: []multigresv1alpha1.CellName{"us-east-1a", "us-east-1b"},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			if err := c.Create(ctx, cluster); err != nil {
				t.Fatalf("Failed to create MultigresCluster: %v", err)
			}
			return ctx
		}).
		Assess("two cells and topo created", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			waitForCRDCount(t, c, ns,
				&multigresv1alpha1.CellList{},
				func(l *multigresv1alpha1.CellList) int { return len(l.Items) },
				2, "2 Cells",
			)
			waitForCRDCount(t, c, ns,
				&multigresv1alpha1.TopoServerList{},
				func(l *multigresv1alpha1.TopoServerList) int { return len(l.Items) },
				1, "TopoServer",
			)
			waitForCRDCount(t, c, ns,
				&multigresv1alpha1.ShardList{},
				func(l *multigresv1alpha1.ShardList) int { return len(l.Items) },
				1, "Shard",
			)
			return ctx
		}).
		Assess("leaf resources from templates", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			sts := waitForStatefulSetWithContainer(t, c, ns, "etcd")
			if sts.Spec.Replicas != nil && *sts.Spec.Replicas != 3 {
				t.Errorf("etcd replicas = %d, want 3", *sts.Spec.Replicas)
			}

			dep := waitForDeploymentWithContainer(t, c, ns, "multiadmin")
			if dep.Spec.Replicas != nil && *dep.Spec.Replicas != 2 {
				t.Errorf("multiadmin replicas = %d, want 2", *dep.Spec.Replicas)
			}

			waitForPodWithContainer(t, c, ns, "postgres")
			waitForDeploymentWithContainer(t, c, ns, "multiorch")
			return ctx
		}).
		Assess("all pods ready", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			testutil.WaitForAllPodsReady(t, tc.Clientset(), ns)
			return ctx
		}).
		Assess("psql SELECT 1 through multigateway", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			gwSvc := findGatewayServiceName(t, tc, ns)
			waitForQueryServingSafe(t, tc, ns, gwSvc)
			return ctx
		}).
		Feature()

	tc.Test(t, feat)
}

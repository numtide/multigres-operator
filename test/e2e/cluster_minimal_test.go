//go:build e2e

package e2e_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

// TestMinimalCluster applies the equivalent of config/samples/minimal.yaml and
// verifies the full resource tree is provisioned, all pods become ready, and
// psql SELECT 1 succeeds through the multigateway.
func TestMinimalCluster(t *testing.T) {
	tc, operatorSetup := newClusterWithOperator(t)
	ns := createNamespace(t, tc)
	c := newCRClient(t, tc)

	feat := features.New("minimal-cluster").
		Setup(operatorSetup).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			t.Helper()
			cluster := &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "minimal",
					Namespace: ns,
				},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					PVCDeletionPolicy: &multigresv1alpha1.PVCDeletionPolicy{
						WhenDeleted: multigresv1alpha1.DeletePVCRetentionPolicy,
						WhenScaled:  multigresv1alpha1.DeletePVCRetentionPolicy,
					},
					Cells: []multigresv1alpha1.CellConfig{
						{Name: "zone-a"},
					},
				},
			}
			if err := c.Create(ctx, cluster); err != nil {
				t.Fatalf("Failed to create MultigresCluster: %v", err)
			}
			return ctx
		}).
		Assess("child CRDs created", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			waitForCRDCount(t, c, ns,
				&multigresv1alpha1.TopoServerList{},
				func(l *multigresv1alpha1.TopoServerList) int { return len(l.Items) },
				1, "TopoServer",
			)
			waitForCRDCount(t, c, ns,
				&multigresv1alpha1.CellList{},
				func(l *multigresv1alpha1.CellList) int { return len(l.Items) },
				1, "Cell",
			)
			waitForCRDCount(t, c, ns,
				&multigresv1alpha1.TableGroupList{},
				func(l *multigresv1alpha1.TableGroupList) int { return len(l.Items) },
				1, "TableGroup",
			)
			waitForCRDCount(t, c, ns,
				&multigresv1alpha1.ShardList{},
				func(l *multigresv1alpha1.ShardList) int { return len(l.Items) },
				1, "Shard",
			)
			return ctx
		}).
		Assess("leaf resources created", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			waitForStatefulSetWithContainer(t, c, ns, "etcd")
			waitForDeploymentWithContainer(t, c, ns, "multiadmin")
			waitForDeploymentWithContainer(t, c, ns, "multigateway")
			waitForDeploymentWithContainer(t, c, ns, "multiorch")
			waitForPodWithContainer(t, c, ns, "postgres")
			waitForServiceWithPort(t, c, ns, "postgres", 15432)
			waitForServiceWithPort(t, c, ns, "client", 2379)
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

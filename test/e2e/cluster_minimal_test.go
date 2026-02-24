//go:build e2e

package e2e_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// TestMinimalCluster applies the equivalent of config/samples/minimal.yaml and
// verifies the full resource tree is provisioned, all pods become ready, and
// psql SELECT 1 succeeds through the multigateway.
//
// The operator runs inside the kind cluster as a real Deployment. All
// controllers (cluster-handler, resource-handler, data-handler) are active.
// Data-handler creates cell/database metadata in etcd; multiorch bootstraps
// postgres; multigateway serves queries.
func TestMinimalCluster(t *testing.T) {
	tc := setUpCluster(t)
	ctx := t.Context()
	c := tc.client
	ns := tc.namespace

	// Apply the minimal cluster spec — operator resolves all defaults.
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
				{Name: "zone-a", Zone: "us-east-1a"},
			},
		},
	}

	if err := c.Create(ctx, cluster); err != nil {
		t.Fatalf("Failed to create MultigresCluster: %v", err)
	}

	// ----- Verify CRDs created by the cluster controller -----

	t.Run("TopoServer created", func(t *testing.T) {
		waitForMinCount(t, ctx, c, ns,
			func() *multigresv1alpha1.TopoServerList { return &multigresv1alpha1.TopoServerList{} },
			func(l *multigresv1alpha1.TopoServerList) int { return len(l.Items) },
			1, "TopoServer",
		)
	})

	t.Run("Cell created for zone-a", func(t *testing.T) {
		waitForMinCount(t, ctx, c, ns,
			func() *multigresv1alpha1.CellList { return &multigresv1alpha1.CellList{} },
			func(l *multigresv1alpha1.CellList) int {
				count := 0
				for _, cell := range l.Items {
					if cell.Spec.Name == "zone-a" {
						count++
					}
				}
				return count
			},
			1, "Cell zone-a",
		)
	})

	t.Run("TableGroup created", func(t *testing.T) {
		waitForMinCount(t, ctx, c, ns,
			func() *multigresv1alpha1.TableGroupList { return &multigresv1alpha1.TableGroupList{} },
			func(l *multigresv1alpha1.TableGroupList) int { return len(l.Items) },
			1, "TableGroup",
		)
	})

	t.Run("Shard created", func(t *testing.T) {
		waitForMinCount(t, ctx, c, ns,
			func() *multigresv1alpha1.ShardList { return &multigresv1alpha1.ShardList{} },
			func(l *multigresv1alpha1.ShardList) int { return len(l.Items) },
			1, "Shard",
		)
	})

	// ----- Verify leaf Kubernetes resources -----

	t.Run("Etcd StatefulSet created", func(t *testing.T) {
		waitForStatefulSetWithContainer(t, ctx, c, ns, "etcd")
	})

	t.Run("MultiAdmin Deployment created", func(t *testing.T) {
		waitForDeploymentWithContainer(t, ctx, c, ns, "multiadmin")
	})

	t.Run("MultiGateway Deployment created", func(t *testing.T) {
		waitForDeploymentWithContainer(t, ctx, c, ns, "multigateway")
	})

	t.Run("MultiOrch Deployment created", func(t *testing.T) {
		waitForDeploymentWithContainer(t, ctx, c, ns, "multiorch")
	})

	t.Run("Postgres StatefulSet created", func(t *testing.T) {
		waitForStatefulSetWithContainer(t, ctx, c, ns, "postgres")
	})

	t.Run("MultiGateway Service with postgres port", func(t *testing.T) {
		waitForServiceWithPort(t, ctx, c, ns, "postgres", 15432)
	})

	t.Run("Etcd client Service", func(t *testing.T) {
		waitForServiceWithPort(t, ctx, c, ns, "client", 2379)
	})

	// ----- Verify all pods healthy -----

	t.Run("All pods ready", func(t *testing.T) {
		waitForAllPodsReady(t, ctx, c, ns)
	})

	// ----- Verify psql connectivity through multigateway -----

	t.Run("Query serving via multigateway", func(t *testing.T) {
		gwSvc := findGatewayServiceName(t, ctx, c, ns)
		waitForQueryServing(t, tc, gwSvc)
	})
}

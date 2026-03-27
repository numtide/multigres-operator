//go:build e2e

package templated_test

import (
	"context"
	"testing"

	multigresv1alpha1 "github.com/multigres/multigres-operator/api/v1alpha1"
	"github.com/multigres/multigres-operator/test/e2e/framework"
)

// TestTemplatedCluster applies the template CRs from config/samples/templates/
// and the templated cluster CR from config/samples/templated-cluster.yaml, then
// verifies the full resource tree, pod health, and psql connectivity.
func TestTemplatedCluster(t *testing.T) {
	ns := cluster.CreateNamespace(t)
	c, err := cluster.CRClient()
	if err != nil {
		t.Fatalf("create CR client: %v", err)
	}
	ctx := context.Background()

	// Create templates first — the cluster CR references them.
	coreTmpl := framework.MustLoadCoreTemplate("config/samples/templates/core.yaml", ns)
	if err := c.Create(ctx, coreTmpl); err != nil {
		t.Fatalf("create CoreTemplate: %v", err)
	}
	cellTmpl := framework.MustLoadCellTemplate("config/samples/templates/cell.yaml", ns)
	if err := c.Create(ctx, cellTmpl); err != nil {
		t.Fatalf("create CellTemplate: %v", err)
	}
	shardTmpl := framework.MustLoadShardTemplate("config/samples/templates/shard.yaml", ns)
	if err := c.Create(ctx, shardTmpl); err != nil {
		t.Fatalf("create ShardTemplate: %v", err)
	}

	// Create the cluster referencing the templates.
	cr := framework.MustLoadCluster("config/samples/templated-cluster.yaml", ns)
	if err := c.Create(ctx, cr); err != nil {
		t.Fatalf("create MultigresCluster: %v", err)
	}

	// Verify child CRDs.
	framework.WaitForCRDCount(t, c, ns,
		&multigresv1alpha1.CellList{},
		func(l *multigresv1alpha1.CellList) int { return len(l.Items) },
		2, "2 Cells",
	)
	framework.WaitForCRDCount(t, c, ns,
		&multigresv1alpha1.TopoServerList{},
		func(l *multigresv1alpha1.TopoServerList) int { return len(l.Items) },
		1, "TopoServer",
	)
	framework.WaitForCRDCount(t, c, ns,
		&multigresv1alpha1.ShardList{},
		func(l *multigresv1alpha1.ShardList) int { return len(l.Items) },
		1, "Shard",
	)

	// Verify leaf resources.
	framework.WaitForStatefulSet(t, c, ns, "etcd")
	framework.WaitForDeployment(t, c, ns, "multiadmin")
	framework.WaitForDeployment(t, c, ns, "multiorch")
	framework.WaitForPod(t, c, ns, "postgres")

	// All pods ready.
	cluster.WaitForAllPodsReady(t, ns)

	// SELECT 1 through multigateway.
	gwSvc := framework.FindGatewayService(t, cluster, ns)
	framework.WaitForQueryServing(t, cluster, ns, gwSvc)
}

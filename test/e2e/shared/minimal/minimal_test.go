//go:build e2e

package minimal_test

import (
	"context"
	"testing"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/test/e2e/framework"
)

// TestMinimalCluster applies the equivalent of config/samples/minimal.yaml and
// verifies the full resource tree is provisioned, all pods become ready, and
// psql SELECT 1 succeeds through the multigateway.
func TestMinimalCluster(t *testing.T) {
	t.Parallel()
	ns := cluster.CreateNamespace(t)
	c, err := cluster.CRClient()
	if err != nil {
		t.Fatalf("create CR client: %v", err)
	}

	// Load and apply the minimal sample.
	cr := framework.MustLoadCluster("config/samples/minimal.yaml", ns)
	if err := c.Create(context.Background(), cr); err != nil {
		t.Fatalf("create MultigresCluster: %v", err)
	}

	// Verify child CRDs.
	framework.WaitForCRDCount(t, c, ns,
		&multigresv1alpha1.TopoServerList{},
		func(l *multigresv1alpha1.TopoServerList) int { return len(l.Items) },
		1, "TopoServer",
	)
	framework.WaitForCRDCount(t, c, ns,
		&multigresv1alpha1.CellList{},
		func(l *multigresv1alpha1.CellList) int { return len(l.Items) },
		1, "Cell",
	)
	framework.WaitForCRDCount(t, c, ns,
		&multigresv1alpha1.TableGroupList{},
		func(l *multigresv1alpha1.TableGroupList) int { return len(l.Items) },
		1, "TableGroup",
	)
	framework.WaitForCRDCount(t, c, ns,
		&multigresv1alpha1.ShardList{},
		func(l *multigresv1alpha1.ShardList) int { return len(l.Items) },
		1, "Shard",
	)

	// Verify leaf resources.
	framework.WaitForStatefulSet(t, c, ns, "etcd")
	framework.WaitForDeployment(t, c, ns, "multiadmin")
	framework.WaitForDeployment(t, c, ns, "multigateway")
	framework.WaitForDeployment(t, c, ns, "multiorch")
	framework.WaitForPod(t, c, ns, "postgres")
	framework.WaitForService(t, c, ns, "postgres", 15432)
	framework.WaitForService(t, c, ns, "client", 2379)

	// All pods ready.
	cluster.WaitForAllPodsReady(t, ns)

	// SELECT 1 through multigateway.
	gwSvc := framework.FindGatewayService(t, cluster, ns)
	framework.WaitForQueryServing(t, cluster, ns, gwSvc)
}

//go:build e2e

package deletion_test

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/multigres/multigres-operator/api/v1alpha1"
	"github.com/multigres/multigres-operator/test/e2e/framework"
)

// TestClusterDeletion verifies that deleting a MultigresCluster triggers
// cascading deletion of all child resources (CRDs and Kubernetes resources).
func TestClusterDeletion(t *testing.T) {
	ns := cluster.CreateNamespace(t)
	c, err := cluster.CRClient()
	if err != nil {
		t.Fatalf("create CR client: %v", err)
	}
	ctx := context.Background()

	// Load and create the minimal sample.
	cr := framework.MustLoadCluster("config/samples/minimal.yaml", ns)
	cr.Name = "delete-me" // distinct name for clarity in logs
	if err := c.Create(ctx, cr); err != nil {
		t.Fatalf("create MultigresCluster: %v", err)
	}

	// Wait for full provisioning.
	cluster.WaitForAllPodsReady(t, ns)
	t.Log("cluster fully provisioned, initiating deletion...")

	// Delete the cluster.
	clusterKey := client.ObjectKeyFromObject(cr)
	if err := c.Delete(ctx, cr); err != nil {
		t.Fatalf("delete MultigresCluster: %v", err)
	}

	// Wait for the MultigresCluster object to disappear.
	pollCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	err = wait.PollUntilContextCancel(pollCtx, 3*time.Second, true, func(ctx context.Context) (bool, error) {
		err := c.Get(ctx, clusterKey, &multigresv1alpha1.MultigresCluster{})
		return apierrors.IsNotFound(err), nil
	})
	if err != nil {
		t.Fatalf("MultigresCluster not deleted: %v", err)
	}

	// Verify child CRDs are cleaned up.
	framework.WaitForEmpty(t, c, ns,
		&multigresv1alpha1.CellList{},
		func(l *multigresv1alpha1.CellList) int { return len(l.Items) },
		"Cells", 3*time.Minute,
	)
	framework.WaitForEmpty(t, c, ns,
		&multigresv1alpha1.TopoServerList{},
		func(l *multigresv1alpha1.TopoServerList) int { return len(l.Items) },
		"TopoServers", 3*time.Minute,
	)

	// Verify Kubernetes resources are cleaned up.
	framework.WaitForEmpty(t, c, ns,
		&appsv1.DeploymentList{},
		func(l *appsv1.DeploymentList) int { return len(l.Items) },
		"Deployments", 3*time.Minute,
	)
	framework.WaitForEmpty(t, c, ns,
		&appsv1.StatefulSetList{},
		func(l *appsv1.StatefulSetList) int { return len(l.Items) },
		"StatefulSets", 3*time.Minute,
	)
	framework.WaitForEmpty(t, c, ns,
		&corev1.ServiceList{},
		func(l *corev1.ServiceList) int { return len(l.Items) },
		"Services", 3*time.Minute,
	)
}

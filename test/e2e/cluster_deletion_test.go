//go:build e2e

package e2e_test

import (
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// TestClusterDeletion verifies that deleting a MultigresCluster triggers
// cascading deletion of all child resources, including data-handler cleanup
// of etcd topology entries.
func TestClusterDeletion(t *testing.T) {
	tc := setUpCluster(t)
	ctx := t.Context()
	c := tc.client
	ns := tc.namespace

	// Create a minimal cluster.
	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "delete-me",
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

	// Wait for full provisioning including pod health.
	waitForAllPodsReady(t, ctx, c, ns)
	t.Log("Cluster fully provisioned, initiating deletion...")

	// Delete the cluster.
	if err := c.Delete(ctx, cluster); err != nil {
		t.Fatalf("Failed to delete MultigresCluster: %v", err)
	}

	// Verify all child resources are cleaned up.
	// The data-handler finalizers unregister cells/shards from etcd topology,
	// then Kubernetes GC cascades the rest.

	t.Run("MultigresCluster deleted", func(t *testing.T) {
		pollUntil(t, 5*time.Minute, 3*time.Second, "MultigresCluster deletion", func() (bool, string) {
			err := c.Get(ctx, client.ObjectKeyFromObject(cluster), &multigresv1alpha1.MultigresCluster{})
			if apierrors.IsNotFound(err) {
				return true, ""
			}
			if err != nil {
				return false, fmt.Sprintf("get error: %v", err)
			}
			return false, "cluster still exists"
		})
	})

	t.Run("Cells deleted", func(t *testing.T) {
		pollUntil(t, 3*time.Minute, 3*time.Second, "Cell deletion", func() (bool, string) {
			list := &multigresv1alpha1.CellList{}
			if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
				return false, fmt.Sprintf("list error: %v", err)
			}
			if len(list.Items) == 0 {
				return true, ""
			}
			return false, fmt.Sprintf("%d cells remaining", len(list.Items))
		})
	})

	t.Run("TopoServers deleted", func(t *testing.T) {
		pollUntil(t, 3*time.Minute, 3*time.Second, "TopoServer deletion", func() (bool, string) {
			list := &multigresv1alpha1.TopoServerList{}
			if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
				return false, fmt.Sprintf("list error: %v", err)
			}
			if len(list.Items) == 0 {
				return true, ""
			}
			return false, fmt.Sprintf("%d toposervers remaining", len(list.Items))
		})
	})

	t.Run("Deployments deleted", func(t *testing.T) {
		pollUntil(t, 3*time.Minute, 3*time.Second, "Deployment cleanup", func() (bool, string) {
			list := &appsv1.DeploymentList{}
			if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
				return false, fmt.Sprintf("list error: %v", err)
			}
			if len(list.Items) == 0 {
				return true, ""
			}
			names := make([]string, len(list.Items))
			for i, d := range list.Items {
				names[i] = d.Name
			}
			return false, fmt.Sprintf("%d deployments remaining: %v", len(list.Items), names)
		})
	})

	t.Run("StatefulSets deleted", func(t *testing.T) {
		pollUntil(t, 3*time.Minute, 3*time.Second, "StatefulSet cleanup", func() (bool, string) {
			list := &appsv1.StatefulSetList{}
			if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
				return false, fmt.Sprintf("list error: %v", err)
			}
			if len(list.Items) == 0 {
				return true, ""
			}
			return false, fmt.Sprintf("%d statefulsets remaining", len(list.Items))
		})
	})

	t.Run("Services deleted", func(t *testing.T) {
		pollUntil(t, 3*time.Minute, 3*time.Second, "Service cleanup", func() (bool, string) {
			list := &corev1.ServiceList{}
			if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
				return false, fmt.Sprintf("list error: %v", err)
			}
			if len(list.Items) == 0 {
				return true, ""
			}
			return false, fmt.Sprintf("%d services remaining", len(list.Items))
		})
	})
}

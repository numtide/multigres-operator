//go:build e2e

package e2e_test

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

// TestClusterDeletion verifies that deleting a MultigresCluster triggers
// cascading deletion of all child resources.
func TestClusterDeletion(t *testing.T) {
	tc, operatorSetup := newClusterWithOperator(t)
	ns := createNamespace(t, tc)
	c := newCRClient(t, tc)

	clusterKey := client.ObjectKey{Name: "delete-me", Namespace: ns}

	feat := features.New("cluster-deletion").
		Setup(operatorSetup).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			t.Helper()
			cluster := &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterKey.Name,
					Namespace: clusterKey.Namespace,
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
			return ctx
		}).
		Assess("cluster fully provisioned", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			testutil.WaitForAllPodsReady(t, tc.Clientset(), ns)
			t.Log("Cluster fully provisioned, initiating deletion...")
			return ctx
		}).
		Assess("delete cluster", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			cluster := &multigresv1alpha1.MultigresCluster{}
			if err := c.Get(ctx, clusterKey, cluster); err != nil {
				t.Fatalf("Failed to get MultigresCluster: %v", err)
			}
			if err := c.Delete(ctx, cluster); err != nil {
				t.Fatalf("Failed to delete MultigresCluster: %v", err)
			}
			return ctx
		}).
		Assess("MultigresCluster gone", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			pollCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()
			err := wait.PollUntilContextCancel(pollCtx, 3*time.Second, true, func(ctx context.Context) (bool, error) {
				err := c.Get(ctx, clusterKey, &multigresv1alpha1.MultigresCluster{})
				if apierrors.IsNotFound(err) {
					return true, nil
				}
				return false, nil
			})
			if err != nil {
				t.Fatalf("MultigresCluster not deleted: %v", err)
			}
			return ctx
		}).
		Assess("child CRDs deleted", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			waitForEmpty(t, c, ns,
				&multigresv1alpha1.CellList{},
				func(l *multigresv1alpha1.CellList) int { return len(l.Items) },
				"Cells", 3*time.Minute,
			)
			waitForEmpty(t, c, ns,
				&multigresv1alpha1.TopoServerList{},
				func(l *multigresv1alpha1.TopoServerList) int { return len(l.Items) },
				"TopoServers", 3*time.Minute,
			)
			return ctx
		}).
		Assess("Kubernetes resources deleted", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			waitForEmpty(t, c, ns,
				&appsv1.DeploymentList{},
				func(l *appsv1.DeploymentList) int { return len(l.Items) },
				"Deployments", 3*time.Minute,
			)
			waitForEmpty(t, c, ns,
				&appsv1.StatefulSetList{},
				func(l *appsv1.StatefulSetList) int { return len(l.Items) },
				"StatefulSets", 3*time.Minute,
			)
			waitForEmpty(t, c, ns,
				&corev1.ServiceList{},
				func(l *corev1.ServiceList) int { return len(l.Items) },
				"Services", 3*time.Minute,
			)
			return ctx
		}).
		Feature()

	tc.Test(t, feat)
}

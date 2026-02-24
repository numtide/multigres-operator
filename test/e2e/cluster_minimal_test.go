//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

// TestMinimalCluster applies the equivalent of config/samples/minimal.yaml and
// verifies the full resource tree is provisioned, all pods become ready, and
// psql connectivity works through the multigateway.
//
// The data-handler controllers are skipped because they run in-process and
// cannot reach the in-cluster etcd via Kubernetes DNS. Instead, cluster
// metadata is initialized by exec'ing the createclustermetadata CLI inside a
// pod that has the multigres binary.
//
// Expected resource chain:
//
//	MultigresCluster
//	├── TopoServer (global etcd) → StatefulSet, Service, Service(headless)
//	├── Deployment (multiadmin)
//	├── Cell "zone-a" → Deployment (multigateway), Service
//	└── TableGroup → Shard
//	    └── Shard → StatefulSet (postgres), Deployment (multiorch), Services, ConfigMap, Secret
func TestMinimalCluster(t *testing.T) {
	clusterName := fmt.Sprintf("e2e-minimal-%s", randSuffix())
	_, c, ns := setUpOperator(t,
		withoutDataHandler(),
		withKindOptions(
			testutil.WithKindCluster(clusterName),
			testutil.WithKindCreateCluster(),
			testutil.WithKindImages(multigresImages...),
		),
	)
	ctx := t.Context()

	// Wait for CRD API to become available — on a fresh kind cluster the API
	// server needs a moment to register newly-installed CRDs.
	pollUntil(t, 60*time.Second, 2*time.Second, "CRD API available", func() (bool, string) {
		list := &multigresv1alpha1.MultigresClusterList{}
		if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
			return false, fmt.Sprintf("CRD not available yet: %v", err)
		}
		return true, ""
	})

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

	// ----- Initialize cluster metadata in etcd -----
	// The data-handler controllers are skipped (they can't reach in-cluster
	// etcd from the host), so we exec the createclustermetadata CLI inside a
	// pod that has the multigres binary.

	t.Run("Cluster metadata initialized", func(t *testing.T) {
		etcdSvc := findEtcdServiceName(t, ctx, c, ns)
		execCreateClusterMetadata(t, ctx, c, ns, etcdSvc)
	})

	// ----- Verify pod health -----

	t.Run("All pods ready", func(t *testing.T) {
		waitForAllPodsReady(t, ctx, c, ns)
	})

	// ----- Verify psql connectivity through multigateway -----

	t.Run("Query serving via multigateway", func(t *testing.T) {
		gwSvc := findGatewayServiceName(t, ctx, c, ns)
		waitForQueryServing(t, ctx, c, ns, gwSvc)
	})
}

// findEtcdServiceName finds the etcd client Service name in the namespace.
func findEtcdServiceName(t *testing.T, ctx context.Context, c client.Client, ns string) string {
	t.Helper()
	var svcName string
	pollUntil(t, 60*time.Second, 2*time.Second, "etcd client Service", func() (bool, string) {
		svcs := &corev1.ServiceList{}
		if err := c.List(ctx, svcs, client.InNamespace(ns)); err != nil {
			return false, fmt.Sprintf("list error: %v", err)
		}
		for _, svc := range svcs.Items {
			for _, port := range svc.Spec.Ports {
				if port.Name == "client" && port.Port == 2379 {
					svcName = svc.Name
					return true, ""
				}
			}
		}
		return false, "no Service with client:2379"
	})
	return svcName
}

// execCreateClusterMetadata execs into a pod with the multigres binary and
// runs the createclustermetadata command to populate etcd with cell metadata.
// This replaces what the data-handler controllers would do if they could reach
// the in-cluster etcd.
func execCreateClusterMetadata(t *testing.T, ctx context.Context, c client.Client, ns, etcdSvc string) {
	t.Helper()

	pollUntil(t, 3*time.Minute, 5*time.Second, "createclustermetadata", func() (bool, string) {
		// Find a running pod with the multigres binary (multigateway container).
		pods := &corev1.PodList{}
		if err := c.List(ctx, pods, client.InNamespace(ns)); err != nil {
			return false, fmt.Sprintf("list error: %v", err)
		}

		var targetPod string
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning {
				continue
			}
			for _, cont := range pod.Spec.Containers {
				if cont.Name == "multigateway" {
					targetPod = pod.Name
					break
				}
			}
			if targetPod != "" {
				break
			}
		}
		if targetPod == "" {
			return false, "no running pod with multigateway container"
		}

		cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(cmdCtx, "kubectl", "exec",
			"-n", ns,
			targetPod,
			"-c", "multigateway",
			"--",
			"/multigres/bin/multigres",
			"createclustermetadata",
			"--global-topo-address="+etcdSvc+":2379",
			"--global-topo-root=/multigres/global",
			"--cells=zone-a",
			"--durability-policy=ANY_2",
			"--backup-location=/backups",
		)
		cmd.Env = os.Environ()

		out, err := cmd.CombinedOutput()
		if err != nil {
			return false, fmt.Sprintf("exec error: %v (output: %s)", err, strings.TrimSpace(string(out)))
		}
		t.Logf("createclustermetadata output: %s", strings.TrimSpace(string(out)))
		return true, ""
	})
}

func randSuffix() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[rand.IntN(len(chars))]
	}
	return string(b)
}

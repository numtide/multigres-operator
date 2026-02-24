//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os/exec"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

// TestInlineCluster applies the equivalent of config/samples/no-templates.yaml
// and verifies the full resource tree is provisioned and that psql connectivity
// works through the multigateway. This cluster specifies all configuration
// inline (no template references).
//
// Data-handler controllers are skipped because they run in-process and cannot
// reach etcd via K8s DNS from the host. Instead, cluster metadata is created
// manually via kubectl exec into a multigres pod.
func TestInlineCluster(t *testing.T) {
	clusterName := fmt.Sprintf("e2e-inline-%04d", rand.IntN(10000))
	_, c, ns := setUpOperator(t,
		withoutDataHandler(),
		withKindOptions(
			testutil.WithKindCreateCluster(),
			testutil.WithKindImages(multigresImages...),
			testutil.WithKindCluster(clusterName),
		),
	)
	ctx := t.Context()

	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inline",
			Namespace: ns,
		},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			PVCDeletionPolicy: &multigresv1alpha1.PVCDeletionPolicy{
				WhenDeleted: multigresv1alpha1.DeletePVCRetentionPolicy,
				WhenScaled:  multigresv1alpha1.DeletePVCRetentionPolicy,
			},
			GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{
					Image:    "gcr.io/etcd-development/etcd:v3.6.7",
					Replicas: ptr.To(int32(3)),
					Storage:  multigresv1alpha1.StorageSpec{Size: "1Gi"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			},
			MultiAdmin: &multigresv1alpha1.MultiAdminConfig{
				Spec: &multigresv1alpha1.StatelessSpec{
					Replicas: ptr.To(int32(2)),
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
			},
			Cells: []multigresv1alpha1.CellConfig{
				{
					Name: "z1",
					Zone: "z1",
					Spec: &multigresv1alpha1.CellInlineSpec{
						MultiGateway: multigresv1alpha1.StatelessSpec{
							Replicas: ptr.To(int32(2)),
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
				},
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
									Spec: &multigresv1alpha1.ShardInlineSpec{
										MultiOrch: multigresv1alpha1.MultiOrchSpec{
											StatelessSpec: multigresv1alpha1.StatelessSpec{
												Resources: corev1.ResourceRequirements{
													Requests: corev1.ResourceList{
														corev1.ResourceCPU:    resource.MustParse("50m"),
														corev1.ResourceMemory: resource.MustParse("64Mi"),
													},
													Limits: corev1.ResourceList{
														corev1.ResourceCPU:    resource.MustParse("100m"),
														corev1.ResourceMemory: resource.MustParse("128Mi"),
													},
												},
											},
											Cells: []multigresv1alpha1.CellName{"z1"},
										},
										Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
											"main": {
												Type:            "readWrite",
												Cells:           []multigresv1alpha1.CellName{"z1"},
												ReplicasPerCell: ptr.To(int32(2)),
												Storage: multigresv1alpha1.StorageSpec{
													Size: "1Gi",
												},
												Postgres: multigresv1alpha1.ContainerConfig{
													Resources: corev1.ResourceRequirements{
														Requests: corev1.ResourceList{
															corev1.ResourceCPU:    resource.MustParse("100m"),
															corev1.ResourceMemory: resource.MustParse("256Mi"),
														},
														Limits: corev1.ResourceList{
															corev1.ResourceCPU:    resource.MustParse("500m"),
															corev1.ResourceMemory: resource.MustParse("512Mi"),
														},
													},
												},
												Multipooler: multigresv1alpha1.ContainerConfig{
													Resources: corev1.ResourceRequirements{
														Requests: corev1.ResourceList{
															corev1.ResourceCPU:    resource.MustParse("50m"),
															corev1.ResourceMemory: resource.MustParse("64Mi"),
														},
														Limits: corev1.ResourceList{
															corev1.ResourceCPU:    resource.MustParse("200m"),
															corev1.ResourceMemory: resource.MustParse("128Mi"),
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
				},
			},
		},
	}

	// Retry Create because the REST mapper may not have discovered the CRD yet
	// on a freshly-created kind cluster.
	pollUntil(t, 30*time.Second, 2*time.Second, "create MultigresCluster", func() (bool, string) {
		if err := c.Create(ctx, cluster); err != nil {
			return false, err.Error()
		}
		return true, ""
	})

	// ----- Verify CRDs -----

	t.Run("TopoServer with 3 replicas", func(t *testing.T) {
		waitForMinCount(t, ctx, c, ns,
			func() *multigresv1alpha1.TopoServerList { return &multigresv1alpha1.TopoServerList{} },
			func(l *multigresv1alpha1.TopoServerList) int {
				for _, ts := range l.Items {
					if ts.Spec.Etcd != nil && ts.Spec.Etcd.Replicas != nil && *ts.Spec.Etcd.Replicas == 3 {
						return 1
					}
				}
				return 0
			},
			1, "TopoServer with 3 replicas",
		)
	})

	t.Run("Cell z1", func(t *testing.T) {
		waitForMinCount(t, ctx, c, ns,
			func() *multigresv1alpha1.CellList { return &multigresv1alpha1.CellList{} },
			func(l *multigresv1alpha1.CellList) int {
				for _, cell := range l.Items {
					if cell.Spec.Name == "z1" {
						return 1
					}
				}
				return 0
			},
			1, "Cell z1",
		)
	})

	t.Run("Shard 0-inf", func(t *testing.T) {
		waitForMinCount(t, ctx, c, ns,
			func() *multigresv1alpha1.ShardList { return &multigresv1alpha1.ShardList{} },
			func(l *multigresv1alpha1.ShardList) int { return len(l.Items) },
			1, "Shard",
		)
	})

	// ----- Verify leaf resources -----

	t.Run("Etcd StatefulSet", func(t *testing.T) {
		sts := waitForStatefulSetWithContainer(t, ctx, c, ns, "etcd")
		if sts.Spec.Replicas != nil && *sts.Spec.Replicas != 3 {
			t.Errorf("etcd replicas = %d, want 3", *sts.Spec.Replicas)
		}
	})

	t.Run("MultiAdmin Deployment with 2 replicas", func(t *testing.T) {
		dep := waitForDeploymentWithContainer(t, ctx, c, ns, "multiadmin")
		if dep.Spec.Replicas != nil && *dep.Spec.Replicas != 2 {
			t.Errorf("multiadmin replicas = %d, want 2", *dep.Spec.Replicas)
		}
	})

	t.Run("MultiGateway Deployment with 2 replicas", func(t *testing.T) {
		dep := waitForDeploymentWithContainer(t, ctx, c, ns, "multigateway")
		if dep.Spec.Replicas != nil && *dep.Spec.Replicas != 2 {
			t.Errorf("multigateway replicas = %d, want 2", *dep.Spec.Replicas)
		}
	})

	t.Run("MultiOrch Deployment", func(t *testing.T) {
		waitForDeploymentWithContainer(t, ctx, c, ns, "multiorch")
	})

	t.Run("Postgres StatefulSet with 2 replicas", func(t *testing.T) {
		sts := waitForStatefulSetWithContainer(t, ctx, c, ns, "postgres")
		if sts.Spec.Replicas != nil && *sts.Spec.Replicas != 2 {
			t.Errorf("postgres replicas = %d, want 2", *sts.Spec.Replicas)
		}
	})

	t.Run("MultiGateway postgres Service", func(t *testing.T) {
		waitForServiceWithPort(t, ctx, c, ns, "postgres", 15432)
	})

	// ----- Create cluster metadata in etcd -----
	// Data-handler controllers are skipped (they run in-process and can't
	// reach etcd via K8s DNS). Manually create the cell/database metadata
	// that multipooler and multiorch need by exec'ing into a multigres pod.

	t.Run("Create cluster metadata in etcd", func(t *testing.T) {
		topoAddr := fmt.Sprintf("inline-global-topo.%s.svc:2379", ns)
		pollUntil(t, 3*time.Minute, 5*time.Second, "createclustermetadata", func() (bool, string) {
			// Find a running pod with the multigres binary (multiorch has it)
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
					if cont.Name == "multiorch" {
						targetPod = pod.Name
						break
					}
				}
				if targetPod != "" {
					break
				}
			}
			if targetPod == "" {
				return false, "no running multiorch pod found"
			}

			cmdCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()

			cmd := exec.CommandContext(cmdCtx, "kubectl", "exec",
				"-n", ns,
				targetPod,
				"-c", "multiorch",
				"--",
				"/multigres/bin/multigres",
				"createclustermetadata",
				"--global-topo-address="+topoAddr,
				"--global-topo-root=/multigres/global",
				"--cells=z1",
				"--durability-policy=ANY_2",
				"--backup-location=/backups",
			)

			out, err := cmd.CombinedOutput()
			if err != nil {
				return false, fmt.Sprintf("exec error: %v (output: %s)", err, strings.TrimSpace(string(out)))
			}
			t.Logf("createclustermetadata output: %s", strings.TrimSpace(string(out)))
			return true, ""
		})
	})

	// ----- Verify pod readiness -----

	t.Run("All pods ready", func(t *testing.T) {
		waitForAllPodsReady(t, ctx, c, ns)
	})

	// ----- Verify psql connectivity through multigateway -----

	t.Run("Query serving via multigateway", func(t *testing.T) {
		gatewaySvc := findGatewayServiceName(t, ctx, c, ns)
		waitForQueryServing(t, ctx, c, ns, gatewaySvc)
	})
}

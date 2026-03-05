//go:build e2e

package e2e_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

// TestInlineCluster applies the equivalent of config/samples/no-templates.yaml
// and verifies the full resource tree is provisioned, all pods become ready,
// and psql SELECT 1 succeeds through the multigateway.
func TestInlineCluster(t *testing.T) {
	tc, operatorSetup := newClusterWithOperator(t)
	ns := createNamespace(t, tc)
	c := newCRClient(t, tc)

	feat := features.New("inline-cluster").
		Setup(operatorSetup).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			t.Helper()
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
							},
						},
					},
					Cells: []multigresv1alpha1.CellConfig{
						{
							Name: "z1",
							Spec: &multigresv1alpha1.CellInlineSpec{
								MultiGateway: multigresv1alpha1.StatelessSpec{
									Replicas: ptr.To(int32(2)),
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("100m"),
											corev1.ResourceMemory: resource.MustParse("128Mi"),
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
														},
													},
													Cells: []multigresv1alpha1.CellName{"z1"},
												},
												Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
													"main": {
														Type:            "readWrite",
														Cells:           []multigresv1alpha1.CellName{"z1"},
														ReplicasPerCell: ptr.To(int32(2)),
														Storage:         multigresv1alpha1.StorageSpec{Size: "1Gi"},
														Postgres: multigresv1alpha1.ContainerConfig{
															Resources: corev1.ResourceRequirements{
																Requests: corev1.ResourceList{
																	corev1.ResourceCPU:    resource.MustParse("100m"),
																	corev1.ResourceMemory: resource.MustParse("256Mi"),
																},
															},
														},
														Multipooler: multigresv1alpha1.ContainerConfig{
															Resources: corev1.ResourceRequirements{
																Requests: corev1.ResourceList{
																	corev1.ResourceCPU:    resource.MustParse("50m"),
																	corev1.ResourceMemory: resource.MustParse("64Mi"),
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
			if err := c.Create(ctx, cluster); err != nil {
				t.Fatalf("Failed to create MultigresCluster: %v", err)
			}
			return ctx
		}).
		Assess("child CRDs created", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			waitForCRDCount(t, c, ns,
				&multigresv1alpha1.TopoServerList{},
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
			waitForCRDCount(t, c, ns,
				&multigresv1alpha1.CellList{},
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
			waitForCRDCount(t, c, ns,
				&multigresv1alpha1.ShardList{},
				func(l *multigresv1alpha1.ShardList) int { return len(l.Items) },
				1, "Shard",
			)
			return ctx
		}).
		Assess("leaf resources with correct replicas", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			sts := waitForStatefulSetWithContainer(t, c, ns, "etcd")
			if sts.Spec.Replicas != nil && *sts.Spec.Replicas != 3 {
				t.Errorf("etcd replicas = %d, want 3", *sts.Spec.Replicas)
			}

			dep := waitForDeploymentWithContainer(t, c, ns, "multiadmin")
			if dep.Spec.Replicas != nil && *dep.Spec.Replicas != 2 {
				t.Errorf("multiadmin replicas = %d, want 2", *dep.Spec.Replicas)
			}

			gwDep := waitForDeploymentWithContainer(t, c, ns, "multigateway")
			if gwDep.Spec.Replicas != nil && *gwDep.Spec.Replicas != 2 {
				t.Errorf("multigateway replicas = %d, want 2", *gwDep.Spec.Replicas)
			}

			waitForDeploymentWithContainer(t, c, ns, "multiorch")

			waitForPodWithContainer(t, c, ns, "postgres")

			waitForServiceWithPort(t, c, ns, "postgres", 15432)
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

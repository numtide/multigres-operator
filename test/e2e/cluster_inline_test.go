//go:build e2e

package e2e_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// TestInlineCluster applies the equivalent of config/samples/no-templates.yaml
// and verifies the full resource tree is provisioned, all pods become ready,
// and psql SELECT 1 succeeds through the multigateway.
func TestInlineCluster(t *testing.T) {
	tc := setUpCluster(t)
	ctx := t.Context()
	c := tc.client
	ns := tc.namespace

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
					Zone: "z1",
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
												Storage: multigresv1alpha1.StorageSpec{
													Size: "1Gi",
												},
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

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

// TestTemplatedCluster applies templates and a templated cluster CR, then
// verifies the full resource tree, pod health, and psql connectivity.
func TestTemplatedCluster(t *testing.T) {
	tc := setUpCluster(t)
	ctx := t.Context()
	c := tc.client
	ns := tc.namespace

	// Create templates first
	coreTemplate := &multigresv1alpha1.CoreTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "standard-core", Namespace: ns},
		Spec: multigresv1alpha1.CoreTemplateSpec{
			GlobalTopoServer: &multigresv1alpha1.TopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{
					Image:    "gcr.io/etcd-development/etcd:v3.6.7",
					Replicas: ptr.To(int32(3)),
					Storage:  multigresv1alpha1.StorageSpec{Size: "1Gi"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				},
			},
			MultiAdmin: &multigresv1alpha1.StatelessSpec{
				Replicas: ptr.To(int32(2)),
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
			},
		},
	}
	if err := c.Create(ctx, coreTemplate); err != nil {
		t.Fatalf("Failed to create CoreTemplate: %v", err)
	}

	cellTemplate := &multigresv1alpha1.CellTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "standard-cell", Namespace: ns},
		Spec: multigresv1alpha1.CellTemplateSpec{
			MultiGateway: &multigresv1alpha1.StatelessSpec{
				Replicas: ptr.To(int32(2)),
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
			},
		},
	}
	if err := c.Create(ctx, cellTemplate); err != nil {
		t.Fatalf("Failed to create CellTemplate: %v", err)
	}

	shardTemplate := &multigresv1alpha1.ShardTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "standard-shard", Namespace: ns},
		Spec: multigresv1alpha1.ShardTemplateSpec{
			MultiOrch: &multigresv1alpha1.MultiOrchSpec{
				StatelessSpec: multigresv1alpha1.StatelessSpec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				},
			},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"main-app": {
					Type:            "readWrite",
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
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				},
			},
		},
	}
	if err := c.Create(ctx, shardTemplate); err != nil {
		t.Fatalf("Failed to create ShardTemplate: %v", err)
	}

	// Create the cluster referencing templates
	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "standard-ha-cluster", Namespace: ns},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			TemplateDefaults: multigresv1alpha1.TemplateDefaults{
				CoreTemplate:  "standard-core",
				CellTemplate:  "standard-cell",
				ShardTemplate: "standard-shard",
			},
			GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
				TemplateRef: "standard-core",
			},
			MultiAdmin: &multigresv1alpha1.MultiAdminConfig{
				TemplateRef: "standard-core",
			},
			Cells: []multigresv1alpha1.CellConfig{
				{Name: "us-east-1a", Zone: "us-east-1a"},
				{Name: "us-east-1b", Zone: "us-east-1b"},
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
									Overrides: &multigresv1alpha1.ShardOverrides{
										Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
											"main-app": {
												Cells: []multigresv1alpha1.CellName{"us-east-1a", "us-east-1b"},
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

	// ----- Verify resources -----

	t.Run("Two Cells created", func(t *testing.T) {
		waitForMinCount(t, ctx, c, ns,
			func() *multigresv1alpha1.CellList { return &multigresv1alpha1.CellList{} },
			func(l *multigresv1alpha1.CellList) int { return len(l.Items) },
			2, "2 Cells",
		)
	})

	t.Run("TopoServer created", func(t *testing.T) {
		waitForMinCount(t, ctx, c, ns,
			func() *multigresv1alpha1.TopoServerList { return &multigresv1alpha1.TopoServerList{} },
			func(l *multigresv1alpha1.TopoServerList) int { return len(l.Items) },
			1, "TopoServer",
		)
	})

	t.Run("Shard created", func(t *testing.T) {
		waitForMinCount(t, ctx, c, ns,
			func() *multigresv1alpha1.ShardList { return &multigresv1alpha1.ShardList{} },
			func(l *multigresv1alpha1.ShardList) int { return len(l.Items) },
			1, "Shard",
		)
	})

	t.Run("Etcd StatefulSet with 3 replicas from template", func(t *testing.T) {
		sts := waitForStatefulSetWithContainer(t, ctx, c, ns, "etcd")
		if sts.Spec.Replicas != nil && *sts.Spec.Replicas != 3 {
			t.Errorf("etcd replicas = %d, want 3", *sts.Spec.Replicas)
		}
	})

	t.Run("MultiAdmin Deployment with 2 replicas from template", func(t *testing.T) {
		dep := waitForDeploymentWithContainer(t, ctx, c, ns, "multiadmin")
		if dep.Spec.Replicas != nil && *dep.Spec.Replicas != 2 {
			t.Errorf("multiadmin replicas = %d, want 2", *dep.Spec.Replicas)
		}
	})

	t.Run("Postgres StatefulSet", func(t *testing.T) {
		waitForStatefulSetWithContainer(t, ctx, c, ns, "postgres")
	})

	t.Run("MultiOrch Deployment", func(t *testing.T) {
		waitForDeploymentWithContainer(t, ctx, c, ns, "multiorch")
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

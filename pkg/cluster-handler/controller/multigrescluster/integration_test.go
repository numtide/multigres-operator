//go:build integration
// +build integration

package multigrescluster

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// TestMultigresCluster_Integration runs a full integration test using envtest.
func TestMultigresCluster_Integration(t *testing.T) {
	// 0. Enable Logger to see controller logs (helps debugging)
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	// 1. Setup the Integration Test Environment (envtest)
	g := NewWithT(t)
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	g.Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = testEnv.Stop()
	}()

	// 2. Setup the Scheme and Manager
	err = multigresv1alpha1.AddToScheme(scheme.Scheme)
	g.Expect(err).NotTo(HaveOccurred())

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	g.Expect(err).NotTo(HaveOccurred())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme.Scheme})
	g.Expect(err).NotTo(HaveOccurred())

	reconciler := &MultigresClusterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
	err = reconciler.SetupWithManager(mgr)
	g.Expect(err).NotTo(HaveOccurred())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = mgr.Start(ctx)
	}()

	// 3. The Actual Test Logic
	const (
		clusterName = "test-integration-cluster"
		namespace   = "default"
		timeout     = time.Second * 10
		interval    = time.Millisecond * 250
	)

	// Define the Cluster object
	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			Images: multigresv1alpha1.ClusterImages{
				MultiGateway: "gateway:latest",
				MultiOrch:    "orch:latest",
				MultiPooler:  "pooler:latest",
				MultiAdmin:   "admin:latest",
				Postgres:     "postgres:15",
			},
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{Image: "etcd:latest"},
			},
			MultiAdmin: multigresv1alpha1.MultiAdminConfig{
				Spec: &multigresv1alpha1.StatelessSpec{Replicas: ptrInt32(1)},
			},
			Cells: []multigresv1alpha1.CellConfig{
				{Name: "zone-a", Zone: "us-east-1a", Spec: &multigresv1alpha1.CellInlineSpec{
					MultiGateway: multigresv1alpha1.StatelessSpec{Replicas: ptrInt32(1)},
				}},
			},
			Databases: []multigresv1alpha1.DatabaseConfig{
				{
					Name: "db1",
					TableGroups: []multigresv1alpha1.TableGroupConfig{
						{Name: "tg1", Shards: []multigresv1alpha1.ShardConfig{{
							Name: "s1",
							Spec: &multigresv1alpha1.ShardInlineSpec{
								MultiOrch: multigresv1alpha1.MultiOrchSpec{StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptrInt32(1)}},
								Pools: map[string]multigresv1alpha1.PoolSpec{
									"primary": {ReplicasPerCell: ptrInt32(1), Type: "readWrite"},
								},
							},
						}}},
					},
				},
			},
		},
	}

	// Create the Cluster
	g.Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

	// A. Verify Global Components Created (TopoServer)
	topoKey := types.NamespacedName{Name: clusterName + "-global-topo", Namespace: namespace}
	createdTopo := &multigresv1alpha1.TopoServer{}
	g.Eventually(func() bool {
		err := k8sClient.Get(ctx, topoKey, createdTopo)
		return err == nil
	}, timeout, interval).Should(BeTrue(), "Global TopoServer should be created")

	g.Eventually(func() error {
		err := k8sClient.Get(ctx, topoKey, createdTopo)
		if err != nil {
			return err
		}
		// Patch status to mimic "Ready"
		createdTopo.Status.Conditions = []metav1.Condition{
			{Type: "Available", Status: metav1.ConditionTrue, Reason: "Test", Message: "Simulated", LastTransitionTime: metav1.Now()},
		}
		createdTopo.Status.ClientService = clusterName + "-global-topo-client" // Populate if used for service discovery
		return k8sClient.Status().Update(ctx, createdTopo)
	}, timeout, interval).Should(Succeed(), "Should update TopoServer status to Ready")
	// --------------------------------------------

	// B. Verify MultiAdmin Deployment Created
	adminKey := types.NamespacedName{Name: clusterName + "-multiadmin", Namespace: namespace}
	createdAdmin := &appsv1.Deployment{}
	g.Eventually(func() bool {
		err := k8sClient.Get(ctx, adminKey, createdAdmin)
		return err == nil
	}, timeout, interval).Should(BeTrue(), "MultiAdmin Deployment should be created")

	// C. Verify Cell Created
	// Now that TopoServer is "Ready", the controller should proceed to creating Cells.
	cellKey := types.NamespacedName{Name: clusterName + "-zone-a", Namespace: namespace}
	createdCell := &multigresv1alpha1.Cell{}
	g.Eventually(func() bool {
		err := k8sClient.Get(ctx, cellKey, createdCell)
		return err == nil
	}, timeout, interval).Should(BeTrue(), "Cell CR should be created")

	g.Expect(createdCell.Spec.Zone).To(Equal("us-east-1a"))

	// D. Verify TableGroup Created
	tgKey := types.NamespacedName{Name: clusterName + "-db1-tg1", Namespace: namespace}
	createdTG := &multigresv1alpha1.TableGroup{}
	g.Eventually(func() bool {
		err := k8sClient.Get(ctx, tgKey, createdTG)
		return err == nil
	}, timeout, interval).Should(BeTrue(), "TableGroup CR should be created")

	// E. Verify Finalizer Added
	g.Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, cluster)
		if err != nil {
			return false
		}
		for _, f := range cluster.Finalizers {
			if f == "multigres.com/finalizer" {
				return true
			}
		}
		return false
	}, timeout, interval).Should(BeTrue(), "Finalizer should be added to MultigresCluster")
}

func ptrInt32(i int32) *int32 {
	return &i
}

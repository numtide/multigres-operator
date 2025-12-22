package multigrescluster

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

// setupFixtures helper returns a fresh set of test objects to ensure isolation between test functions.
func setupFixtures() (
	*multigresv1alpha1.CoreTemplate,
	*multigresv1alpha1.CellTemplate,
	*multigresv1alpha1.ShardTemplate,
	*multigresv1alpha1.MultigresCluster,
	string, string, string,
) {
	clusterName := "test-cluster"
	namespace := "default"
	finalizerName := "multigres.com/finalizer"

	coreTpl := &multigresv1alpha1.CoreTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "default-core", Namespace: namespace},
		Spec: multigresv1alpha1.CoreTemplateSpec{
			GlobalTopoServer: &multigresv1alpha1.TopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{
					Image:    "etcd:v1",
					Replicas: ptr.To(int32(3)),
				},
			},
			MultiAdmin: &multigresv1alpha1.StatelessSpec{
				Replicas: ptr.To(int32(1)),
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: parseQty("100m")},
				},
			},
		},
	}

	cellTpl := &multigresv1alpha1.CellTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "default-cell", Namespace: namespace},
		Spec: multigresv1alpha1.CellTemplateSpec{
			MultiGateway: &multigresv1alpha1.StatelessSpec{
				Replicas: ptr.To(int32(2)),
			},
		},
	}

	shardTpl := &multigresv1alpha1.ShardTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "default-shard", Namespace: namespace},
		Spec: multigresv1alpha1.ShardTemplateSpec{
			MultiOrch: &multigresv1alpha1.MultiOrchSpec{
				StatelessSpec: multigresv1alpha1.StatelessSpec{
					Replicas: ptr.To(int32(3)),
				},
			},
			Pools: map[string]multigresv1alpha1.PoolSpec{
				"primary": {
					ReplicasPerCell: ptr.To(int32(2)),
					Type:            "readWrite",
				},
			},
		},
	}

	baseCluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       clusterName,
			Namespace:  namespace,
			Finalizers: []string{finalizerName},
		},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			Images: multigresv1alpha1.ClusterImages{
				MultiGateway: "gateway:latest",
				MultiOrch:    "orch:latest",
				MultiPooler:  "pooler:latest",
				MultiAdmin:   "admin:latest",
				Postgres:     "postgres:15",
			},
			TemplateDefaults: multigresv1alpha1.TemplateDefaults{
				CoreTemplate:  "default-core",
				CellTemplate:  "default-cell",
				ShardTemplate: "default-shard",
			},
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerSpec{
				TemplateRef: "default-core",
			},
			MultiAdmin: multigresv1alpha1.MultiAdminConfig{
				TemplateRef: "default-core",
			},
			Cells: []multigresv1alpha1.CellConfig{
				{Name: "zone-a", Zone: "us-east-1a"},
			},
			Databases: []multigresv1alpha1.DatabaseConfig{
				{
					Name: "db1",
					TableGroups: []multigresv1alpha1.TableGroupConfig{
						{Name: "tg1", Shards: []multigresv1alpha1.ShardConfig{{Name: "s1"}}},
					},
				},
			},
		},
	}

	return coreTpl, cellTpl, shardTpl, baseCluster, clusterName, namespace, finalizerName
}

func TestMultigresClusterReconciler_Reconcile_Success(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	coreTpl, cellTpl, shardTpl, baseCluster, clusterName, namespace, finalizerName := setupFixtures()

	tests := map[string]struct {
		multigrescluster   *multigresv1alpha1.MultigresCluster
		existingObjects    []client.Object
		preReconcileUpdate func(testing.TB, *multigresv1alpha1.MultigresCluster)
		validate           func(testing.TB, client.Client)
	}{
		"Create: Adds Finalizer": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Finalizers = nil // Explicitly remove finalizer to test addition
			},
			existingObjects: []client.Object{},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				updatedCluster := &multigresv1alpha1.MultigresCluster{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, updatedCluster); err != nil {
					t.Fatalf("failed to get updated cluster: %v", err)
				}
				if !controllerutil.ContainsFinalizer(updatedCluster, finalizerName) {
					t.Error("Finalizer was not added to Cluster")
				}
			},
		},
		"Create: Full Cluster Creation with Templates": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				updatedCluster := &multigresv1alpha1.MultigresCluster{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, updatedCluster); err != nil {
					t.Fatal(err)
				}
				if !controllerutil.ContainsFinalizer(updatedCluster, finalizerName) {
					t.Error("Finalizer was not added to Cluster")
				}

				// Verify Wiring
				cell := &multigresv1alpha1.Cell{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-zone-a", Namespace: namespace}, cell); err != nil {
					t.Fatal("Expected Cell 'zone-a' to exist")
				}

				expectedAddr := clusterName + "-global-topo-client." + namespace + ".svc:2379"
				if diff := cmp.Diff(expectedAddr, cell.Spec.GlobalTopoServer.Address); diff != "" {
					t.Errorf("Wiring Bug! Cell has wrong Topo Address mismatch (-want +got):\n%s", diff)
				}
			},
		},
		"Create: Independent Templates (Topo vs Admin)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.TemplateDefaults.CoreTemplate = ""
				c.Spec.GlobalTopoServer.TemplateRef = "topo-core"
				c.Spec.MultiAdmin.TemplateRef = "admin-core"
			},
			existingObjects: []client.Object{
				cellTpl, shardTpl,
				&multigresv1alpha1.CoreTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "topo-core", Namespace: namespace},
					Spec: multigresv1alpha1.CoreTemplateSpec{
						GlobalTopoServer: &multigresv1alpha1.TopoServerSpec{
							Etcd: &multigresv1alpha1.EtcdSpec{Image: "etcd:topo"},
						},
					},
				},
				&multigresv1alpha1.CoreTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "admin-core", Namespace: namespace},
					Spec: multigresv1alpha1.CoreTemplateSpec{
						MultiAdmin: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(5))},
					},
				},
			},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()

				// Check Topo uses topo-core
				ts := &multigresv1alpha1.TopoServer{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-global-topo", Namespace: namespace}, ts); err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff("etcd:topo", ts.Spec.Etcd.Image); diff != "" {
					t.Errorf("TopoServer image mismatch (-want +got):\n%s", diff)
				}

				// Check Admin uses admin-core
				deploy := &appsv1.Deployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-multiadmin", Namespace: namespace}, deploy); err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(int32(5), *deploy.Spec.Replicas); diff != "" {
					t.Errorf("MultiAdmin replicas mismatch (-want +got):\n%s", diff)
				}

				// Verify Wiring for independent template
				cell := &multigresv1alpha1.Cell{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-zone-a", Namespace: namespace}, cell); err != nil {
					t.Fatal("Expected Cell 'zone-a' to exist")
				}
				expectedAddr := clusterName + "-global-topo-client." + namespace + ".svc:2379"
				if diff := cmp.Diff(expectedAddr, cell.Spec.GlobalTopoServer.Address); diff != "" {
					t.Errorf("Wiring Bug (Independent)! Cell has wrong Topo Address mismatch (-want +got):\n%s", diff)
				}
			},
		},
		"Create: MultiAdmin TemplateRef Only": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.GlobalTopoServer = multigresv1alpha1.GlobalTopoServerSpec{
					External: &multigresv1alpha1.ExternalTopoServerSpec{Endpoints: []multigresv1alpha1.EndpointUrl{"http://ext:2379"}},
				}
				c.Spec.MultiAdmin = multigresv1alpha1.MultiAdminConfig{TemplateRef: "default-core"}
				c.Spec.TemplateDefaults.CoreTemplate = ""
			},
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				deploy := &appsv1.Deployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-multiadmin", Namespace: namespace}, deploy); err != nil {
					t.Fatal("MultiAdmin not created")
				}
			},
		},
		"Create: MultiOrch Placement Defaulting": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.TemplateDefaults.ShardTemplate = "" // Ensure no default template interferes
				// Define a database with explicit pools containing cells
				c.Spec.Databases = []multigresv1alpha1.DatabaseConfig{
					{
						Name: "db-defaulting",
						TableGroups: []multigresv1alpha1.TableGroupConfig{
							{
								Name: "tg1",
								Shards: []multigresv1alpha1.ShardConfig{
									{
										Name: "0",
										Spec: &multigresv1alpha1.ShardInlineSpec{
											// MultiOrch Cells explicitly EMPTY
											MultiOrch: multigresv1alpha1.MultiOrchSpec{
												StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
											},
											Pools: map[string]multigresv1alpha1.PoolSpec{
												"pool-a": {Cells: []multigresv1alpha1.CellName{"zone-a"}},
												"pool-b": {Cells: []multigresv1alpha1.CellName{"zone-b"}},
											},
										},
									},
								},
							},
						},
					},
				}
			},
			existingObjects: []client.Object{coreTpl, cellTpl},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				// Fetch the child TableGroup (which holds the resolved shard spec)
				tgName := clusterName + "-db-defaulting-tg1"
				tg := &multigresv1alpha1.TableGroup{}
				if err := c.Get(ctx, types.NamespacedName{Name: tgName, Namespace: namespace}, tg); err != nil {
					t.Fatal(err)
				}

				if diff := cmp.Diff(1, len(tg.Spec.Shards)); diff != "" {
					t.Fatalf("Shard count mismatch (-want +got):\n%s", diff)
				}

				orchCells := tg.Spec.Shards[0].MultiOrch.Cells
				wantCells := []multigresv1alpha1.CellName{"zone-a", "zone-b"}
				if diff := cmp.Diff(wantCells, orchCells); diff != "" {
					t.Errorf("MultiOrch cells mismatch (-want +got):\n%s", diff)
				}
			},
		},
		"Create: MultiAdmin with ImagePullSecrets": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.Images.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "my-secret"}}
			},
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				deploy := &appsv1.Deployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-multiadmin", Namespace: namespace}, deploy); err != nil {
					t.Fatal("MultiAdmin not created")
				}
				want := []corev1.LocalObjectReference{{Name: "my-secret"}}
				if diff := cmp.Diff(want, deploy.Spec.Template.Spec.ImagePullSecrets); diff != "" {
					t.Errorf("ImagePullSecrets mismatch (-want +got):\n%s", diff)
				}
			},
		},
		"Create: Inline Etcd": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				// Use inline Etcd for GlobalTopoServer to test getGlobalTopoRef branch
				c.Spec.GlobalTopoServer = multigresv1alpha1.GlobalTopoServerSpec{
					Etcd: &multigresv1alpha1.EtcdSpec{Image: "etcd:inline"},
				}
				c.Spec.TemplateDefaults.CoreTemplate = ""
			},
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				ts := &multigresv1alpha1.TopoServer{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-global-topo", Namespace: namespace}, ts); err != nil {
					t.Fatal("Global TopoServer not created")
				}
				if diff := cmp.Diff("etcd:inline", ts.Spec.Etcd.Image); diff != "" {
					t.Errorf("TopoServer image mismatch (-want +got):\n%s", diff)
				}
			},
		},
		"Create: Defaults and Optional Components": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.TemplateDefaults.CoreTemplate = "minimal-core"
				c.Spec.GlobalTopoServer = multigresv1alpha1.GlobalTopoServerSpec{} // Use defaults
				// Remove MultiAdmin to test skip logic
				c.Spec.MultiAdmin = multigresv1alpha1.MultiAdminConfig{}
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.CoreTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "minimal-core", Namespace: namespace},
					Spec: multigresv1alpha1.CoreTemplateSpec{
						GlobalTopoServer: &multigresv1alpha1.TopoServerSpec{
							Etcd: &multigresv1alpha1.EtcdSpec{Image: "etcd:v1"}, // Replicas nil
						},
					},
				},
				cellTpl, shardTpl,
			},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				// Verify TopoServer created with default replicas (3)
				ts := &multigresv1alpha1.TopoServer{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-global-topo", Namespace: namespace}, ts); err != nil {
					t.Fatal("Global TopoServer not created")
				}
				if diff := cmp.Diff(DefaultEtcdReplicas, *ts.Spec.Etcd.Replicas); diff != "" {
					t.Errorf("Expected default replicas mismatch (-want +got):\n%s", diff)
				}
				// Verify MultiAdmin NOT created
				deploy := &appsv1.Deployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-multiadmin", Namespace: namespace}, deploy); !apierrors.IsNotFound(err) {
					t.Error("MultiAdmin should not have been created")
				}
			},
		},
		"Create: Cell with Local Topo in Template": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.Cells[0].CellTemplate = "local-topo-cell"
			},
			existingObjects: []client.Object{
				coreTpl, shardTpl,
				&multigresv1alpha1.CellTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "local-topo-cell", Namespace: namespace},
					Spec: multigresv1alpha1.CellTemplateSpec{
						MultiGateway: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
						LocalTopoServer: &multigresv1alpha1.LocalTopoServerSpec{
							Etcd: &multigresv1alpha1.EtcdSpec{Image: "local-etcd:v1"},
						},
					},
				},
			},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				cell := &multigresv1alpha1.Cell{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-zone-a", Namespace: namespace}, cell); err != nil {
					t.Fatal(err)
				}
				// Updated to handle pointer dereference safety
				if cell.Spec.TopoServer == nil || cell.Spec.TopoServer.Etcd == nil || cell.Spec.TopoServer.Etcd.Image != "local-etcd:v1" {
					t.Error("LocalTopoServer not propagated to Cell")
				}
			},
		},
		"Create: External Topo with Empty Endpoints": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.TemplateDefaults.CoreTemplate = ""
				c.Spec.GlobalTopoServer = multigresv1alpha1.GlobalTopoServerSpec{
					External: &multigresv1alpha1.ExternalTopoServerSpec{
						Endpoints: []multigresv1alpha1.EndpointUrl{},
					},
				}
			},
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				cell := &multigresv1alpha1.Cell{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-zone-a", Namespace: namespace}, cell); err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff("", cell.Spec.GlobalTopoServer.Address); diff != "" {
					t.Errorf("Address mismatch (-want +got):\n%s", diff)
				}
			},
		},
		"Create: Inline Specs and Missing Templates": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.GlobalTopoServer = multigresv1alpha1.GlobalTopoServerSpec{External: &multigresv1alpha1.ExternalTopoServerSpec{Endpoints: []multigresv1alpha1.EndpointUrl{"http://ext:2379"}}}
				c.Spec.MultiAdmin = multigresv1alpha1.MultiAdminConfig{Spec: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(5))}}
				c.Spec.Cells[0].Spec = &multigresv1alpha1.CellInlineSpec{MultiGateway: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(4))}}
				c.Spec.Databases[0].TableGroups[0].Shards[0].Spec = &multigresv1alpha1.ShardInlineSpec{
					MultiOrch: multigresv1alpha1.MultiOrchSpec{StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(3))}},
				}
				c.Spec.TemplateDefaults = multigresv1alpha1.TemplateDefaults{}
			},
			existingObjects: []client.Object{},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				cell := &multigresv1alpha1.Cell{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-zone-a", Namespace: namespace}, cell); err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(int32(4), *cell.Spec.MultiGateway.Replicas); diff != "" {
					t.Errorf("Cell inline spec ignored (-want +got):\n%s", diff)
				}
			},
		},
		"Create: No Global Topo Config": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.GlobalTopoServer = multigresv1alpha1.GlobalTopoServerSpec{} // Empty
				c.Spec.TemplateDefaults = multigresv1alpha1.TemplateDefaults{}     // Empty
				c.Spec.MultiAdmin = multigresv1alpha1.MultiAdminConfig{}           // Empty
			},
			existingObjects: []client.Object{cellTpl, shardTpl}, // No Core Template
			validate: func(t testing.TB, c client.Client) {
				// Verify Cell got empty topo address
				cell := &multigresv1alpha1.Cell{}
				_ = c.Get(t.Context(), types.NamespacedName{Name: clusterName + "-zone-a", Namespace: namespace}, cell)
				if diff := cmp.Diff("", cell.Spec.GlobalTopoServer.Address); diff != "" {
					t.Errorf("Expected empty topo address mismatch (-want +got):\n%s", diff)
				}
			},
		},
		"Status: Aggregation Logic": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.Databases = append(c.Spec.Databases, multigresv1alpha1.DatabaseConfig{Name: "db2", TableGroups: []multigresv1alpha1.TableGroupConfig{}})
			},
			existingObjects: []client.Object{
				coreTpl, cellTpl, shardTpl,
				&multigresv1alpha1.Cell{
					ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-zone-a", Namespace: namespace, Labels: map[string]string{"multigres.com/cluster": clusterName}},
					Spec:       multigresv1alpha1.CellSpec{Name: "zone-a"},
					Status: multigresv1alpha1.CellStatus{
						Conditions: []metav1.Condition{{Type: "Available", Status: metav1.ConditionTrue}},
					},
				},
			},
			validate: func(t testing.TB, c client.Client) {
				cluster := &multigresv1alpha1.MultigresCluster{}
				if err := c.Get(t.Context(), types.NamespacedName{Name: clusterName, Namespace: namespace}, cluster); err != nil {
					t.Fatalf("failed to get cluster: %v", err)
				}
				if !meta.IsStatusConditionTrue(cluster.Status.Conditions, "Available") {
					t.Error("Cluster should be available")
				}
			},
		},
		"Delete: Allow Finalization if Children Gone": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
			},
			existingObjects: []client.Object{
				func() *multigresv1alpha1.MultigresCluster {
					c := baseCluster.DeepCopy()
					now := metav1.Now()
					c.DeletionTimestamp = &now
					c.Finalizers = []string{finalizerName}
					return c
				}(),
			},
			validate: func(t testing.TB, c client.Client) {
				updated := &multigresv1alpha1.MultigresCluster{}
				err := c.Get(t.Context(), types.NamespacedName{Name: clusterName, Namespace: namespace}, updated)
				if err == nil {
					if controllerutil.ContainsFinalizer(updated, finalizerName) {
						t.Error("Finalizer was not removed")
					}
				}
			},
		},
		"Error: Object Not Found (Clean Exit)": {
			existingObjects: []client.Object{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			clientBuilder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingObjects...).
				WithStatusSubresource(&multigresv1alpha1.MultigresCluster{}, &multigresv1alpha1.Cell{}, &multigresv1alpha1.TableGroup{})
			baseClient := clientBuilder.Build()

			var finalClient client.Client
			finalClient = baseClient

			// Apply defaults if no specific cluster is provided
			cluster := tc.multigrescluster
			if cluster == nil {
				cluster = baseCluster.DeepCopy()
			}

			// Apply pre-reconcile updates if defined
			if tc.preReconcileUpdate != nil {
				tc.preReconcileUpdate(t, cluster)
			}

			if !strings.Contains(name, "Object Not Found") {
				check := &multigresv1alpha1.MultigresCluster{}
				err := baseClient.Get(t.Context(), types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, check)
				if apierrors.IsNotFound(err) {
					if err := baseClient.Create(t.Context(), cluster); err != nil {
						t.Fatalf("failed to create initial cluster: %v", err)
					}
				}
			}

			reconciler := &MultigresClusterReconciler{
				Client: finalClient,
				Scheme: scheme,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				},
			}

			_, err := reconciler.Reconcile(t.Context(), req)

			if err != nil {
				t.Errorf("Unexpected error from Reconcile: %v", err)
			}

			if tc.validate != nil {
				tc.validate(t, baseClient)
			}
		})
	}
}

func TestMultigresClusterReconciler_Reconcile_Failure(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	coreTpl, cellTpl, shardTpl, baseCluster, clusterName, namespace, finalizerName := setupFixtures()
	errBoom := errors.New("boom")

	tests := map[string]struct {
		multigrescluster   *multigresv1alpha1.MultigresCluster
		existingObjects    []client.Object
		failureConfig      *testutil.FailureConfig
		preReconcileUpdate func(testing.TB, *multigresv1alpha1.MultigresCluster)
		validate           func(testing.TB, client.Client)
	}{
		"Delete: Block Finalization if Cells Exist": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
			},
			existingObjects: []client.Object{
				func() *multigresv1alpha1.MultigresCluster {
					c := baseCluster.DeepCopy()
					now := metav1.Now()
					c.DeletionTimestamp = &now
					c.Finalizers = []string{finalizerName}
					return c
				}(),
				&multigresv1alpha1.Cell{ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-zone-a", Namespace: namespace, Labels: map[string]string{"multigres.com/cluster": clusterName}}},
			},
		},
		"Delete: Block Finalization if TableGroups Exist": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
			},
			existingObjects: []client.Object{
				func() *multigresv1alpha1.MultigresCluster {
					c := baseCluster.DeepCopy()
					now := metav1.Now()
					c.DeletionTimestamp = &now
					c.Finalizers = []string{finalizerName}
					return c
				}(),
				&multigresv1alpha1.TableGroup{ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-db1-tg1", Namespace: namespace, Labels: map[string]string{"multigres.com/cluster": clusterName}}},
			},
		},
		"Delete: Block Finalization if TopoServer Exists": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
			},
			existingObjects: []client.Object{
				func() *multigresv1alpha1.MultigresCluster {
					c := baseCluster.DeepCopy()
					now := metav1.Now()
					c.DeletionTimestamp = &now
					c.Finalizers = []string{finalizerName}
					return c
				}(),
				&multigresv1alpha1.TopoServer{ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-global-topo", Namespace: namespace, Labels: map[string]string{"multigres.com/cluster": clusterName}}},
			},
		},
		"Error: Explicit Template Missing (Should Fail)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.TemplateDefaults.CoreTemplate = "non-existent-template"
			},
			existingObjects: []client.Object{}, // No templates exist
			failureConfig:   nil,               // No API failure, just logical failure
		},
		"Error: Explicit Cell Template Missing": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.Cells[0].CellTemplate = "missing-cell-tpl"
			},
			existingObjects: []client.Object{coreTpl, shardTpl}, // Missing cellTpl
		},
		"Error: Explicit Shard Template Missing": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.Databases[0].TableGroups[0].Shards[0].ShardTemplate = "missing-shard-tpl"
			},
			existingObjects: []client.Object{coreTpl, cellTpl}, // Missing shardTpl
		},
		"Error: Fetch Cluster Failed": {
			existingObjects: []client.Object{},
			failureConfig:   &testutil.FailureConfig{OnGet: testutil.FailOnKeyName(clusterName, errBoom)},
		},
		"Error: Add Finalizer Failed": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Finalizers = nil // Ensure we trigger the AddFinalizer path
			},
			existingObjects: []client.Object{},
			failureConfig:   &testutil.FailureConfig{OnUpdate: testutil.FailOnObjectName(clusterName, errBoom)},
		},
		"Error: Remove Finalizer Failed": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
			},
			existingObjects: []client.Object{
				func() *multigresv1alpha1.MultigresCluster {
					c := baseCluster.DeepCopy()
					now := metav1.Now()
					c.DeletionTimestamp = &now
					c.Finalizers = []string{finalizerName}
					return c
				}(),
			},
			failureConfig: &testutil.FailureConfig{OnUpdate: testutil.FailOnObjectName(clusterName, errBoom)},
		},
		"Error: CheckChildrenDeleted (List Cells Failed)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
			},
			existingObjects: []client.Object{
				func() *multigresv1alpha1.MultigresCluster {
					c := baseCluster.DeepCopy()
					now := metav1.Now()
					c.DeletionTimestamp = &now
					c.Finalizers = []string{finalizerName}
					return c
				}(),
			},
			failureConfig: &testutil.FailureConfig{
				OnList: func(list client.ObjectList) error {
					if _, ok := list.(*multigresv1alpha1.CellList); ok {
						return errBoom
					}
					return nil
				},
			},
		},
		"Error: CheckChildrenDeleted (List TableGroups Failed)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
			},
			existingObjects: []client.Object{
				func() *multigresv1alpha1.MultigresCluster {
					c := baseCluster.DeepCopy()
					now := metav1.Now()
					c.DeletionTimestamp = &now
					c.Finalizers = []string{finalizerName}
					return c
				}(),
			},
			failureConfig: &testutil.FailureConfig{
				OnList: func(list client.ObjectList) error {
					if _, ok := list.(*multigresv1alpha1.TableGroupList); ok {
						return errBoom
					}
					return nil
				},
			},
		},
		"Error: CheckChildrenDeleted (List TopoServers Failed)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
			},
			existingObjects: []client.Object{
				func() *multigresv1alpha1.MultigresCluster {
					c := baseCluster.DeepCopy()
					now := metav1.Now()
					c.DeletionTimestamp = &now
					c.Finalizers = []string{finalizerName}
					return c
				}(),
			},
			failureConfig: &testutil.FailureConfig{
				OnList: func(list client.ObjectList) error {
					if _, ok := list.(*multigresv1alpha1.TopoServerList); ok {
						return errBoom
					}
					return nil
				},
			},
		},
		"Error: Resolve CoreTemplate Failed": {
			existingObjects: []client.Object{coreTpl},
			failureConfig:   &testutil.FailureConfig{OnGet: testutil.FailOnKeyName("default-core", errBoom)},
		},
		"Error: Resolve Admin Template Failed (Second Call)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.TemplateDefaults.CoreTemplate = ""
				c.Spec.GlobalTopoServer.TemplateRef = "topo-core"
				c.Spec.MultiAdmin.TemplateRef = "admin-core-fail"
			},
			existingObjects: []client.Object{
				cellTpl, shardTpl,
				&multigresv1alpha1.CoreTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "topo-core", Namespace: namespace},
					// Minimal valid spec
					Spec: multigresv1alpha1.CoreTemplateSpec{},
				},
			},
			failureConfig: &testutil.FailureConfig{OnGet: testutil.FailOnKeyName("admin-core-fail", errBoom)},
		},
		"Error: Create GlobalTopo Failed": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig:   &testutil.FailureConfig{OnCreate: testutil.FailOnObjectName(clusterName+"-global-topo", errBoom)},
		},
		"Error: Create MultiAdmin Failed": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig:   &testutil.FailureConfig{OnCreate: testutil.FailOnObjectName(clusterName+"-multiadmin", errBoom)},
		},
		"Error: Resolve CellTemplate Failed": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig:   &testutil.FailureConfig{OnGet: testutil.FailOnKeyName("default-cell", errBoom)},
		},
		"Error: List Existing Cells Failed (Reconcile Loop)": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig: &testutil.FailureConfig{
				OnList: func(list client.ObjectList) error {
					if _, ok := list.(*multigresv1alpha1.CellList); ok {
						return errBoom
					}
					return nil
				},
			},
		},
		"Error: Create Cell Failed": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig:   &testutil.FailureConfig{OnCreate: testutil.FailOnObjectName(clusterName+"-zone-a", errBoom)},
		},
		"Error: Prune Cell Failed": {
			existingObjects: []client.Object{
				coreTpl, cellTpl, shardTpl,
				&multigresv1alpha1.Cell{ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-zone-b", Namespace: namespace, Labels: map[string]string{"multigres.com/cluster": clusterName}}},
			},
			failureConfig: &testutil.FailureConfig{OnDelete: testutil.FailOnObjectName(clusterName+"-zone-b", errBoom)},
		},
		"Error: List Existing TableGroups Failed": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig: &testutil.FailureConfig{
				OnList: func(list client.ObjectList) error {
					if _, ok := list.(*multigresv1alpha1.TableGroupList); ok {
						return errBoom
					}
					return nil
				},
			},
		},
		"Error: Resolve ShardTemplate Failed": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig:   &testutil.FailureConfig{OnGet: testutil.FailOnKeyName("default-shard", errBoom)},
		},
		"Error: Create TableGroup Failed": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig:   &testutil.FailureConfig{OnCreate: testutil.FailOnObjectName(clusterName+"-db1-tg1", errBoom)},
		},
		"Error: Prune TableGroup Failed": {
			existingObjects: []client.Object{
				coreTpl, cellTpl, shardTpl,
				&multigresv1alpha1.TableGroup{ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-orphan-tg", Namespace: namespace, Labels: map[string]string{"multigres.com/cluster": clusterName}}},
			},
			failureConfig: &testutil.FailureConfig{OnDelete: testutil.FailOnObjectName(clusterName+"-orphan-tg", errBoom)},
		},
		"Error: UpdateStatus (List Cells Failed)": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig: &testutil.FailureConfig{
				OnList: func() func(client.ObjectList) error {
					count := 0
					return func(list client.ObjectList) error {
						if _, ok := list.(*multigresv1alpha1.CellList); ok {
							count++
							if count > 1 {
								return errBoom
							}
						}
						return nil
					}
				}(),
			},
		},
		"Error: UpdateStatus (List TableGroups Failed)": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig: &testutil.FailureConfig{
				OnList: func() func(client.ObjectList) error {
					count := 0
					return func(list client.ObjectList) error {
						if _, ok := list.(*multigresv1alpha1.TableGroupList); ok {
							count++
							if count > 1 {
								return errBoom
							}
						}
						return nil
					}
				}(),
			},
		},
		"Error: Update Status Failed (API Error)": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig:   &testutil.FailureConfig{OnStatusUpdate: testutil.FailOnObjectName(clusterName, errBoom)},
		},
		"Error: Global Topo Resolution Failed (During Cell Reconcile)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.TemplateDefaults.CoreTemplate = ""
				c.Spec.GlobalTopoServer.TemplateRef = "topo-fail-cells"
				// Clear MultiAdmin to ensure predictable call counts
				c.Spec.MultiAdmin = multigresv1alpha1.MultiAdminConfig{}
			},
			existingObjects: []client.Object{
				cellTpl, shardTpl,
				&multigresv1alpha1.CoreTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "topo-fail-cells", Namespace: namespace},
					Spec:       multigresv1alpha1.CoreTemplateSpec{},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: func() func(client.ObjectKey) error {
					count := 0
					return func(key client.ObjectKey) error {
						if key.Name == "topo-fail-cells" {
							count++
							// Call 1: reconcileGlobalComponents -> ResolveCoreTemplate (Succeeds to proceed)
							// Call 2: reconcileCells -> getGlobalTopoRef -> ResolveCoreTemplate (Fails)
							if count == 2 {
								return errBoom
							}
						}
						return nil
					}
				}(),
			},
		},
		"Error: Global Topo Resolution Failed (During Database Reconcile)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.TemplateDefaults.CoreTemplate = ""
				c.Spec.GlobalTopoServer.TemplateRef = "topo-fail-db"
				// Clear MultiAdmin to ensure predictable call counts
				c.Spec.MultiAdmin = multigresv1alpha1.MultiAdminConfig{}
			},
			existingObjects: []client.Object{
				cellTpl, shardTpl,
				&multigresv1alpha1.CoreTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "topo-fail-db", Namespace: namespace},
					Spec:       multigresv1alpha1.CoreTemplateSpec{},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: func() func(client.ObjectKey) error {
					count := 0
					return func(key client.ObjectKey) error {
						if key.Name == "topo-fail-db" {
							count++
							// Call 1: reconcileGlobalComponents (Succeeds)
							// Call 2: reconcileCells (Succeeds)
							// Call 3: reconcileDatabases -> getGlobalTopoRef (Fails)
							if count == 3 {
								return errBoom
							}
						}
						return nil
					}
				}(),
			},
		},
		"Create: Long Names (Truncation Check)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				longName := strings.Repeat("a", 50)
				c.Spec.Databases[0].Name = longName
				c.Spec.Databases[0].TableGroups[0].Name = longName
			},
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			clientBuilder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingObjects...).
				WithStatusSubresource(&multigresv1alpha1.MultigresCluster{}, &multigresv1alpha1.Cell{}, &multigresv1alpha1.TableGroup{})
			baseClient := clientBuilder.Build()

			var finalClient client.Client
			finalClient = client.Client(baseClient)
			if tc.failureConfig != nil {
				finalClient = testutil.NewFakeClientWithFailures(baseClient, tc.failureConfig)
			}

			// Apply defaults if no specific cluster is provided
			cluster := tc.multigrescluster
			if cluster == nil {
				cluster = baseCluster.DeepCopy()
			}

			// Apply pre-reconcile updates if defined
			if tc.preReconcileUpdate != nil {
				tc.preReconcileUpdate(t, cluster)
			}

			if !strings.Contains(name, "Object Not Found") {
				check := &multigresv1alpha1.MultigresCluster{}
				err := baseClient.Get(t.Context(), types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, check)
				if apierrors.IsNotFound(err) {
					if err := baseClient.Create(t.Context(), cluster); err != nil {
						t.Fatalf("failed to create initial cluster: %v", err)
					}
				}
			}

			reconciler := &MultigresClusterReconciler{
				Client: finalClient,
				Scheme: scheme,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				},
			}

			_, err := reconciler.Reconcile(t.Context(), req)

			if err == nil {
				t.Error("Expected error from Reconcile, got nil")
			}

			if tc.validate != nil {
				tc.validate(t, baseClient)
			}
		})
	}
}

func TestSetupWithManager_Coverage(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r != nil {
			// Expected panic
		}
	}()
	reconciler := &MultigresClusterReconciler{}
	_ = reconciler.SetupWithManager(nil)
}

func TestTemplateLogic_Unit(t *testing.T) {
	t.Parallel()

	t.Run("MergeCellConfig", func(t *testing.T) {
		t.Parallel()

		tpl := &multigresv1alpha1.CellTemplate{
			Spec: multigresv1alpha1.CellTemplateSpec{
				MultiGateway: &multigresv1alpha1.StatelessSpec{
					Replicas:       ptr.To(int32(1)),
					PodAnnotations: map[string]string{"foo": "bar"},
					PodLabels:      map[string]string{"l1": "v1"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: parseQty("100m")},
					},
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{},
					},
				},
				LocalTopoServer: &multigresv1alpha1.LocalTopoServerSpec{
					Etcd: &multigresv1alpha1.EtcdSpec{Image: "base"},
				},
			},
		}
		overrides := &multigresv1alpha1.CellOverrides{
			MultiGateway: &multigresv1alpha1.StatelessSpec{
				Replicas:       ptr.To(int32(2)),
				PodAnnotations: map[string]string{"baz": "qux"},
				PodLabels:      map[string]string{"l2": "v2"},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceMemory: parseQty("1Gi")},
				},
				Affinity: &corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{},
				},
			},
		}

		gw, topo := MergeCellConfig(tpl, overrides, nil)

		wantGw := multigresv1alpha1.StatelessSpec{
			Replicas:       ptr.To(int32(2)),
			PodAnnotations: map[string]string{"foo": "bar", "baz": "qux"},
			PodLabels:      map[string]string{"l1": "v1", "l2": "v2"},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceMemory: parseQty("1Gi")},
			},
			Affinity: &corev1.Affinity{
				PodAntiAffinity: &corev1.PodAntiAffinity{},
			},
		}

		// Use IgnoreUnexported to handle resource.Quantity fields
		if diff := cmp.Diff(wantGw, gw, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
			t.Errorf("MergeCellConfig gateway mismatch (-want +got):\n%s", diff)
		}

		wantTopo := &multigresv1alpha1.LocalTopoServerSpec{
			Etcd: &multigresv1alpha1.EtcdSpec{Image: "base"},
		}
		if diff := cmp.Diff(wantTopo, topo); diff != "" {
			t.Errorf("MergeCellConfig topo mismatch (-want +got):\n%s", diff)
		}

		inline := &multigresv1alpha1.CellInlineSpec{
			MultiGateway: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(99))},
		}
		gw, _ = MergeCellConfig(tpl, overrides, inline)
		if diff := cmp.Diff(int32(99), *gw.Replicas); diff != "" {
			t.Errorf("MergeCellConfig inline priority mismatch (-want +got):\n%s", diff)
		}

		gw, _ = MergeCellConfig(nil, overrides, nil)
		if diff := cmp.Diff(int32(2), *gw.Replicas); diff != "" {
			t.Errorf("MergeCellConfig nil template mismatch (-want +got):\n%s", diff)
		}

		tplNil := &multigresv1alpha1.CellTemplate{
			Spec: multigresv1alpha1.CellTemplateSpec{
				MultiGateway: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
			},
		}
		gw, _ = MergeCellConfig(tplNil, overrides, nil)
		if diff := cmp.Diff("qux", gw.PodAnnotations["baz"]); diff != "" {
			t.Errorf("MergeCellConfig nil map init mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("MergeShardConfig", func(t *testing.T) {
		t.Parallel()

		tpl := &multigresv1alpha1.ShardTemplate{
			Spec: multigresv1alpha1.ShardTemplateSpec{
				MultiOrch: &multigresv1alpha1.MultiOrchSpec{
					StatelessSpec: multigresv1alpha1.StatelessSpec{
						Replicas: ptr.To(int32(1)),
					},
					Cells: []multigresv1alpha1.CellName{"a"},
				},
				Pools: map[string]multigresv1alpha1.PoolSpec{
					"p1": {
						Type:            "readOnly",
						ReplicasPerCell: ptr.To(int32(1)),
						Storage:         multigresv1alpha1.StorageSpec{Size: "1Gi"},
						Postgres:        multigresv1alpha1.ContainerConfig{Resources: corev1.ResourceRequirements{}},
					},
				},
			},
		}

		overrides := &multigresv1alpha1.ShardOverrides{
			MultiOrch: &multigresv1alpha1.MultiOrchSpec{
				Cells: []multigresv1alpha1.CellName{"b"},
			},
			Pools: map[string]multigresv1alpha1.PoolSpec{
				"p1": {
					Type:            "readWrite", // Added Type override here to hit coverage
					ReplicasPerCell: ptr.To(int32(2)),
					Storage:         multigresv1alpha1.StorageSpec{Size: "10Gi"},
					Postgres: multigresv1alpha1.ContainerConfig{
						Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: parseQty("1")}},
					},
					Multipooler: multigresv1alpha1.ContainerConfig{
						Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: parseQty("1")}},
					},
					Affinity: &corev1.Affinity{PodAntiAffinity: &corev1.PodAntiAffinity{}},
					Cells:    []multigresv1alpha1.CellName{"c2"},
				},
				"p2": {Type: "write"},
			},
		}

		orch, pools := MergeShardConfig(tpl, overrides, nil)

		wantOrchCells := []multigresv1alpha1.CellName{"b"}
		if diff := cmp.Diff(wantOrchCells, orch.Cells); diff != "" {
			t.Errorf("MergeShardConfig MultiOrch cells mismatch (-want +got):\n%s", diff)
		}

		p1 := pools["p1"]
		wantP1 := multigresv1alpha1.PoolSpec{
			Type:            "readWrite",
			ReplicasPerCell: ptr.To(int32(2)),
			Storage:         multigresv1alpha1.StorageSpec{Size: "10Gi"},
			Postgres: multigresv1alpha1.ContainerConfig{
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: parseQty("1")}},
			},
			Multipooler: multigresv1alpha1.ContainerConfig{
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: parseQty("1")}},
			},
			Affinity: &corev1.Affinity{PodAntiAffinity: &corev1.PodAntiAffinity{}},
			Cells:    []multigresv1alpha1.CellName{"c2"},
		}

		if diff := cmp.Diff(wantP1, p1, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
			t.Errorf("MergeShardConfig Pool p1 mismatch (-want +got):\n%s", diff)
		}

		inline := &multigresv1alpha1.ShardInlineSpec{
			MultiOrch: multigresv1alpha1.MultiOrchSpec{Cells: []multigresv1alpha1.CellName{"inline"}},
		}
		orch, _ = MergeShardConfig(tpl, overrides, inline)
		wantInlineCells := []multigresv1alpha1.CellName{"inline"}
		if diff := cmp.Diff(wantInlineCells, orch.Cells); diff != "" {
			t.Errorf("MergeShardConfig inline priority mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("ResolveGlobalTopo", func(t *testing.T) {
		t.Parallel()

		spec := &multigresv1alpha1.GlobalTopoServerSpec{TemplateRef: "t1"}
		core := &multigresv1alpha1.CoreTemplate{
			Spec: multigresv1alpha1.CoreTemplateSpec{
				GlobalTopoServer: &multigresv1alpha1.TopoServerSpec{Etcd: &multigresv1alpha1.EtcdSpec{Image: "resolved"}},
			},
		}
		res := ResolveGlobalTopo(spec, core)
		if diff := cmp.Diff("resolved", res.Etcd.Image); diff != "" {
			t.Errorf("ResolveGlobalTopo template mismatch (-want +got):\n%s", diff)
		}

		spec2 := &multigresv1alpha1.GlobalTopoServerSpec{TemplateRef: "t1", Etcd: &multigresv1alpha1.EtcdSpec{Image: "inline"}}
		res2 := ResolveGlobalTopo(spec2, nil)
		if diff := cmp.Diff("inline", res2.Etcd.Image); diff != "" {
			t.Errorf("ResolveGlobalTopo inline fallback mismatch (-want +got):\n%s", diff)
		}

		spec4 := &multigresv1alpha1.GlobalTopoServerSpec{}
		res4 := ResolveGlobalTopo(spec4, nil)
		if diff := cmp.Diff(spec4, res4); diff != "" {
			t.Errorf("ResolveGlobalTopo no-op mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("ResolveMultiAdmin", func(t *testing.T) {
		t.Parallel()

		spec := &multigresv1alpha1.MultiAdminConfig{TemplateRef: "t1"}
		core := &multigresv1alpha1.CoreTemplate{
			Spec: multigresv1alpha1.CoreTemplateSpec{
				MultiAdmin: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(10))},
			},
		}
		res := ResolveMultiAdmin(spec, core)
		if diff := cmp.Diff(int32(10), *res.Replicas); diff != "" {
			t.Errorf("ResolveMultiAdmin template mismatch (-want +got):\n%s", diff)
		}

		spec2 := &multigresv1alpha1.MultiAdminConfig{TemplateRef: "t1", Spec: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(5))}}
		res2 := ResolveMultiAdmin(spec2, nil)
		if diff := cmp.Diff(int32(5), *res2.Replicas); diff != "" {
			t.Errorf("ResolveMultiAdmin inline fallback mismatch (-want +got):\n%s", diff)
		}

		res3 := ResolveMultiAdmin(&multigresv1alpha1.MultiAdminConfig{}, nil)
		if res3 != nil {
			t.Error("ResolveMultiAdmin expected nil for empty config")
		}

		spec5 := &multigresv1alpha1.MultiAdminConfig{}
		res5 := ResolveMultiAdmin(spec5, nil)
		if res5 != nil {
			t.Error("ResolveMultiAdmin expected nil when no config and no template")
		}
	})
}

func parseQty(s string) resource.Quantity {
	return resource.MustParse(s)
}

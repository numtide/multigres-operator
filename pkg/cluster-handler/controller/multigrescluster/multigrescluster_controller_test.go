package multigrescluster

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

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

func TestMultigresClusterReconciler_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

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
			Finalizers: []string{finalizerName}, // Pre-populate finalizer to allow tests to reach reconcile logic
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

	errBoom := errors.New("boom")

	tests := []struct {
		name            string
		cluster         *multigresv1alpha1.MultigresCluster
		existingObjects []client.Object
		failureConfig   *testutil.FailureConfig
		setupFunc       func(client.Client)
		expectError     bool
		validate        func(t *testing.T, c client.Client)
	}{
		// ---------------------------------------------------------------------
		// Success Scenarios
		// ---------------------------------------------------------------------
		{
			name: "Create: Adds Finalizer",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Finalizers = nil // Explicitly remove finalizer to test addition
				return c
			}(),
			existingObjects: []client.Object{},
			expectError:     false,
			validate: func(t *testing.T, c client.Client) {
				ctx := context.Background()
				updatedCluster := &multigresv1alpha1.MultigresCluster{}
				_ = c.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, updatedCluster)
				if !controllerutil.ContainsFinalizer(updatedCluster, finalizerName) {
					t.Error("Finalizer was not added to Cluster")
				}
			},
		},
		{
			name:            "Create: Full Cluster Creation with Templates",
			cluster:         baseCluster.DeepCopy(),
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			expectError:     false,
			validate: func(t *testing.T, c client.Client) {
				ctx := context.Background()
				updatedCluster := &multigresv1alpha1.MultigresCluster{}
				_ = c.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, updatedCluster)
				if !controllerutil.ContainsFinalizer(updatedCluster, finalizerName) {
					t.Error("Finalizer was not added to Cluster")
				}
			},
		},
		{
			name: "Create: Independent Templates (Topo vs Admin)",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.TemplateDefaults.CoreTemplate = "" // clear default
				c.Spec.GlobalTopoServer.TemplateRef = "topo-core"
				c.Spec.MultiAdmin.TemplateRef = "admin-core"
				return c
			}(),
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
			expectError: false,
			validate: func(t *testing.T, c client.Client) {
				ctx := context.Background()

				// Check Topo uses topo-core
				ts := &multigresv1alpha1.TopoServer{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-global-topo", Namespace: namespace}, ts); err != nil {
					t.Fatal(err)
				}
				if ts.Spec.Etcd.Image != "etcd:topo" {
					t.Errorf("TopoServer did not use topo-core template, got image: %s", ts.Spec.Etcd.Image)
				}

				// Check Admin uses admin-core
				deploy := &appsv1.Deployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-multiadmin", Namespace: namespace}, deploy); err != nil {
					t.Fatal(err)
				}
				if *deploy.Spec.Replicas != 5 {
					t.Errorf("MultiAdmin did not use admin-core template, got replicas: %d", *deploy.Spec.Replicas)
				}

				// VERIFY FIX: Check that Child Cell received the correct Global Topo Address from the template
				// This confirms getGlobalTopoRef resolved the template correctly
				cell := &multigresv1alpha1.Cell{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-zone-a", Namespace: namespace}, cell); err != nil {
					t.Fatal(err)
				}
				expectedAddr := fmt.Sprintf("%s-global-topo-client.%s.svc:2379", clusterName, namespace)
				if cell.Spec.GlobalTopoServer.Address != expectedAddr {
					t.Errorf("Cell GlobalTopo address mismatch. Got %s, Want %s", cell.Spec.GlobalTopoServer.Address, expectedAddr)
				}
			},
		},
		{
			name: "Create: MultiAdmin TemplateRef Only",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.GlobalTopoServer = multigresv1alpha1.GlobalTopoServerSpec{
					External: &multigresv1alpha1.ExternalTopoServerSpec{Endpoints: []multigresv1alpha1.EndpointUrl{"http://ext:2379"}},
				}
				c.Spec.MultiAdmin = multigresv1alpha1.MultiAdminConfig{TemplateRef: "default-core"}
				c.Spec.TemplateDefaults.CoreTemplate = ""
				return c
			}(),
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			expectError:     false,
			validate: func(t *testing.T, c client.Client) {
				ctx := context.Background()
				deploy := &appsv1.Deployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-multiadmin", Namespace: namespace}, deploy); err != nil {
					t.Fatal("MultiAdmin not created")
				}
				// Verify External Topo Propagated
				cell := &multigresv1alpha1.Cell{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-zone-a", Namespace: namespace}, cell); err != nil {
					t.Fatal(err)
				}
				if cell.Spec.GlobalTopoServer.Address != "http://ext:2379" {
					t.Errorf("External address not propagated. Got %s", cell.Spec.GlobalTopoServer.Address)
				}
			},
		},
		{
			name: "Create: Inline Etcd",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				// Use inline Etcd for GlobalTopoServer to test getGlobalTopoRef branch
				c.Spec.GlobalTopoServer = multigresv1alpha1.GlobalTopoServerSpec{
					Etcd: &multigresv1alpha1.EtcdSpec{Image: "etcd:inline"},
				}
				c.Spec.TemplateDefaults.CoreTemplate = ""
				return c
			}(),
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			expectError:     false,
			validate: func(t *testing.T, c client.Client) {
				ctx := context.Background()
				ts := &multigresv1alpha1.TopoServer{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-global-topo", Namespace: namespace}, ts); err != nil {
					t.Fatal("Global TopoServer not created")
				}
				if ts.Spec.Etcd.Image != "etcd:inline" {
					t.Errorf("Unexpected image: %s", ts.Spec.Etcd.Image)
				}
			},
		},
		{
			name: "Create: Defaults and Optional Components",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.TemplateDefaults.CoreTemplate = "minimal-core"
				c.Spec.GlobalTopoServer = multigresv1alpha1.GlobalTopoServerSpec{} // Use defaults
				// Remove MultiAdmin to test skip logic
				c.Spec.MultiAdmin = multigresv1alpha1.MultiAdminConfig{}
				return c
			}(),
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
			expectError: false,
			validate: func(t *testing.T, c client.Client) {
				ctx := context.Background()
				// Verify TopoServer created with default replicas (3)
				ts := &multigresv1alpha1.TopoServer{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-global-topo", Namespace: namespace}, ts); err != nil {
					t.Fatal("Global TopoServer not created")
				}
				if *ts.Spec.Etcd.Replicas != 3 {
					t.Errorf("Expected default replicas 3, got %d", *ts.Spec.Etcd.Replicas)
				}
				// Verify MultiAdmin NOT created
				deploy := &appsv1.Deployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-multiadmin", Namespace: namespace}, deploy); !apierrors.IsNotFound(err) {
					t.Error("MultiAdmin should not have been created")
				}
			},
		},
		{
			name: "Create: Cell with Local Topo in Template",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.Cells[0].CellTemplate = "local-topo-cell"
				return c
			}(),
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
			expectError: false,
			validate: func(t *testing.T, c client.Client) {
				ctx := context.Background()
				cell := &multigresv1alpha1.Cell{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-zone-a", Namespace: namespace}, cell); err != nil {
					t.Fatal(err)
				}
				if cell.Spec.TopoServer.Etcd == nil || cell.Spec.TopoServer.Etcd.Image != "local-etcd:v1" {
					t.Error("LocalTopoServer not propagated to Cell")
				}
			},
		},
		{
			name: "Create: External Topo with Empty Endpoints",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.TemplateDefaults.CoreTemplate = ""
				c.Spec.GlobalTopoServer = multigresv1alpha1.GlobalTopoServerSpec{
					External: &multigresv1alpha1.ExternalTopoServerSpec{
						Endpoints: []multigresv1alpha1.EndpointUrl{},
					},
				}
				return c
			}(),
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			expectError:     false,
			validate: func(t *testing.T, c client.Client) {
				ctx := context.Background()
				cell := &multigresv1alpha1.Cell{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-zone-a", Namespace: namespace}, cell); err != nil {
					t.Fatal(err)
				}
				if cell.Spec.GlobalTopoServer.Address != "" {
					t.Errorf("Expected empty address, got %s", cell.Spec.GlobalTopoServer.Address)
				}
			},
		},
		{
			name: "Create: Inline Specs and Missing Templates",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.GlobalTopoServer = multigresv1alpha1.GlobalTopoServerSpec{External: &multigresv1alpha1.ExternalTopoServerSpec{Endpoints: []multigresv1alpha1.EndpointUrl{"http://ext:2379"}}}
				c.Spec.MultiAdmin = multigresv1alpha1.MultiAdminConfig{Spec: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(5))}}
				c.Spec.Cells[0].Spec = &multigresv1alpha1.CellInlineSpec{MultiGateway: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(4))}}
				c.Spec.Databases[0].TableGroups[0].Shards[0].Spec = &multigresv1alpha1.ShardInlineSpec{
					MultiOrch: multigresv1alpha1.MultiOrchSpec{StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(3))}},
				}
				c.Spec.TemplateDefaults = multigresv1alpha1.TemplateDefaults{}
				return c
			}(),
			existingObjects: []client.Object{},
			expectError:     false,
			validate: func(t *testing.T, c client.Client) {
				ctx := context.Background()
				cell := &multigresv1alpha1.Cell{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-zone-a", Namespace: namespace}, cell); err != nil {
					t.Fatal(err)
				}
				if *cell.Spec.MultiGateway.Replicas != 4 {
					t.Errorf("Cell inline spec ignored")
				}
			},
		},
		{
			name: "Create: Long Names (Truncation Check)",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				longName := strings.Repeat("a", 50)
				c.Spec.Databases[0].Name = longName
				c.Spec.Databases[0].TableGroups[0].Name = longName
				return c
			}(),
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			expectError:     true, // FIXED: Now expects error
			validate:        func(t *testing.T, c client.Client) {},
		},
		{
			name: "Status: Aggregation Logic",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.Databases = append(c.Spec.Databases, multigresv1alpha1.DatabaseConfig{Name: "db2", TableGroups: []multigresv1alpha1.TableGroupConfig{}})
				return c
			}(),
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			setupFunc: func(c client.Client) {
				ctx := context.Background()
				cell := &multigresv1alpha1.Cell{
					ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-zone-a", Namespace: namespace, Labels: map[string]string{"multigres.com/cluster": clusterName}},
					Spec:       multigresv1alpha1.CellSpec{Name: "zone-a"},
				}
				_ = c.Create(ctx, cell)
				cell.Status.Conditions = []metav1.Condition{{Type: "Available", Status: metav1.ConditionTrue}}
				_ = c.Status().Update(ctx, cell)
			},
			expectError: false,
			validate: func(t *testing.T, c client.Client) {
				cluster := &multigresv1alpha1.MultigresCluster{}
				_ = c.Get(context.Background(), types.NamespacedName{Name: clusterName, Namespace: namespace}, cluster)
				if !meta.IsStatusConditionTrue(cluster.Status.Conditions, "Available") {
					t.Error("Cluster should be available")
				}
			},
		},

		// ---------------------------------------------------------------------
		// Deletion Scenarios
		// ---------------------------------------------------------------------
		{
			name: "Delete: Block Finalization if Cells Exist",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
				return c
			}(),
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
			expectError: true,
		},
		{
			name: "Delete: Block Finalization if TableGroups Exist",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
				return c
			}(),
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
			expectError: true,
		},
		{
			name: "Delete: Block Finalization if TopoServer Exists",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
				return c
			}(),
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
			expectError: true,
		},
		{
			name: "Delete: Allow Finalization if Children Gone",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
				return c
			}(),
			existingObjects: []client.Object{
				func() *multigresv1alpha1.MultigresCluster {
					c := baseCluster.DeepCopy()
					now := metav1.Now()
					c.DeletionTimestamp = &now
					c.Finalizers = []string{finalizerName}
					return c
				}(),
			},
			expectError: false,
			validate: func(t *testing.T, c client.Client) {
				updated := &multigresv1alpha1.MultigresCluster{}
				err := c.Get(context.Background(), types.NamespacedName{Name: clusterName, Namespace: namespace}, updated)
				if err == nil {
					if controllerutil.ContainsFinalizer(updated, finalizerName) {
						t.Error("Finalizer was not removed")
					}
				}
			},
		},

		// ---------------------------------------------------------------------
		// Error Injection Scenarios
		// ---------------------------------------------------------------------
		{
			name:            "Error: Object Not Found (Clean Exit)",
			cluster:         baseCluster.DeepCopy(),
			existingObjects: []client.Object{},
			expectError:     false,
		},
		{
			name:            "Error: Fetch Cluster Failed",
			cluster:         baseCluster.DeepCopy(),
			existingObjects: []client.Object{},
			failureConfig:   &testutil.FailureConfig{OnGet: testutil.FailOnKeyName(clusterName, errBoom)},
			expectError:     true,
		},
		{
			name: "Error: Add Finalizer Failed",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Finalizers = nil // Ensure we trigger the AddFinalizer path
				return c
			}(),
			existingObjects: []client.Object{},
			failureConfig:   &testutil.FailureConfig{OnUpdate: testutil.FailOnObjectName(clusterName, errBoom)},
			expectError:     true,
		},
		{
			name: "Error: Remove Finalizer Failed",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
				return c
			}(),
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
			expectError:   true,
		},
		{
			name: "Error: CheckChildrenDeleted (List Cells Failed)",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
				return c
			}(),
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
			expectError: true,
		},
		{
			name: "Error: CheckChildrenDeleted (List TableGroups Failed)",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
				return c
			}(),
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
			expectError: true,
		},
		{
			name: "Error: CheckChildrenDeleted (List TopoServers Failed)",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
				return c
			}(),
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
			expectError: true,
		},
		{
			name:            "Error: Resolve CoreTemplate Failed",
			cluster:         baseCluster.DeepCopy(),
			existingObjects: []client.Object{coreTpl},
			failureConfig:   &testutil.FailureConfig{OnGet: testutil.FailOnKeyName("default-core", errBoom)},
			expectError:     true,
		},
		{
			name: "Error: Resolve Admin Template Failed (Second Call)",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				c.Spec.TemplateDefaults.CoreTemplate = ""
				c.Spec.GlobalTopoServer.TemplateRef = "topo-core"
				c.Spec.MultiAdmin.TemplateRef = "admin-core-fail"
				return c
			}(),
			existingObjects: []client.Object{
				cellTpl, shardTpl,
				&multigresv1alpha1.CoreTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "topo-core", Namespace: namespace},
					// Minimal valid spec
					Spec: multigresv1alpha1.CoreTemplateSpec{},
				},
			},
			failureConfig: &testutil.FailureConfig{OnGet: testutil.FailOnKeyName("admin-core-fail", errBoom)},
			expectError:   true,
		},
		{
			name:            "Error: Create GlobalTopo Failed",
			cluster:         baseCluster.DeepCopy(),
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig:   &testutil.FailureConfig{OnCreate: testutil.FailOnObjectName(clusterName+"-global-topo", errBoom)},
			expectError:     true,
		},
		{
			name:            "Error: Create MultiAdmin Failed",
			cluster:         baseCluster.DeepCopy(),
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig:   &testutil.FailureConfig{OnCreate: testutil.FailOnObjectName(clusterName+"-multiadmin", errBoom)},
			expectError:     true,
		},
		{
			name:            "Error: Resolve CellTemplate Failed",
			cluster:         baseCluster.DeepCopy(),
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig:   &testutil.FailureConfig{OnGet: testutil.FailOnKeyName("default-cell", errBoom)},
			expectError:     true,
		},
		{
			name:            "Error: List Existing Cells Failed (Reconcile Loop)",
			cluster:         baseCluster.DeepCopy(),
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig: &testutil.FailureConfig{
				OnList: func(list client.ObjectList) error {
					if _, ok := list.(*multigresv1alpha1.CellList); ok {
						return errBoom
					}
					return nil
				},
			},
			expectError: true,
		},
		{
			name:            "Error: Create Cell Failed",
			cluster:         baseCluster.DeepCopy(),
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig:   &testutil.FailureConfig{OnCreate: testutil.FailOnObjectName(clusterName+"-zone-a", errBoom)},
			expectError:     true,
		},
		{
			name: "Error: Prune Cell Failed",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				return c
			}(),
			existingObjects: []client.Object{
				coreTpl, cellTpl, shardTpl,
				&multigresv1alpha1.Cell{ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-zone-b", Namespace: namespace, Labels: map[string]string{"multigres.com/cluster": clusterName}}},
			},
			failureConfig: &testutil.FailureConfig{OnDelete: testutil.FailOnObjectName(clusterName+"-zone-b", errBoom)},
			expectError:   true,
		},
		{
			name:            "Error: List Existing TableGroups Failed",
			cluster:         baseCluster.DeepCopy(),
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig: &testutil.FailureConfig{
				OnList: func(list client.ObjectList) error {
					if _, ok := list.(*multigresv1alpha1.TableGroupList); ok {
						return errBoom
					}
					return nil
				},
			},
			expectError: true,
		},
		{
			name:            "Error: Resolve ShardTemplate Failed",
			cluster:         baseCluster.DeepCopy(),
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig:   &testutil.FailureConfig{OnGet: testutil.FailOnKeyName("default-shard", errBoom)},
			expectError:     true,
		},
		{
			name:            "Error: Create TableGroup Failed",
			cluster:         baseCluster.DeepCopy(),
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig:   &testutil.FailureConfig{OnCreate: testutil.FailOnObjectName(clusterName+"-db1-tg1", errBoom)},
			expectError:     true,
		},
		{
			name: "Error: Prune TableGroup Failed",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				return c
			}(),
			existingObjects: []client.Object{
				coreTpl, cellTpl, shardTpl,
				&multigresv1alpha1.TableGroup{ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-orphan-tg", Namespace: namespace, Labels: map[string]string{"multigres.com/cluster": clusterName}}},
			},
			failureConfig: &testutil.FailureConfig{OnDelete: testutil.FailOnObjectName(clusterName+"-orphan-tg", errBoom)},
			expectError:   true,
		},
		{
			name: "Error: UpdateStatus (List Cells Failed)",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				return c
			}(),
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
			expectError: true,
		},
		{
			name: "Error: UpdateStatus (List TableGroups Failed)",
			cluster: func() *multigresv1alpha1.MultigresCluster {
				c := baseCluster.DeepCopy()
				return c
			}(),
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
			expectError: true,
		},
		{
			name:            "Error: Update Status Failed (API Error)",
			cluster:         baseCluster.DeepCopy(),
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig:   &testutil.FailureConfig{OnStatusUpdate: testutil.FailOnObjectName(clusterName, errBoom)},
			expectError:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clientBuilder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingObjects...).
				WithStatusSubresource(&multigresv1alpha1.MultigresCluster{}, &multigresv1alpha1.Cell{}, &multigresv1alpha1.TableGroup{})
			baseClient := clientBuilder.Build()

			var finalClient client.Client
			if tc.failureConfig != nil {
				finalClient = testutil.NewFakeClientWithFailures(baseClient, tc.failureConfig)
			} else {
				finalClient = baseClient
			}

			if tc.setupFunc != nil {
				tc.setupFunc(baseClient)
			}

			if !strings.Contains(tc.name, "Object Not Found") {
				check := &multigresv1alpha1.MultigresCluster{}
				err := baseClient.Get(context.Background(), types.NamespacedName{Name: tc.cluster.Name, Namespace: tc.cluster.Namespace}, check)
				if apierrors.IsNotFound(err) {
					_ = baseClient.Create(context.Background(), tc.cluster)
				}
			}

			reconciler := &MultigresClusterReconciler{
				Client: finalClient,
				Scheme: scheme,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tc.cluster.Name,
					Namespace: tc.cluster.Namespace,
				},
			}

			_, err := reconciler.Reconcile(context.Background(), req)

			if tc.expectError {
				if err == nil {
					t.Error("Expected error from Reconcile, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error from Reconcile: %v", err)
				}
			}

			if tc.validate != nil {
				tc.validate(t, baseClient)
			}
		})
	}
}

func TestSetupWithManager_Coverage(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			// Expected panic
		}
	}()
	reconciler := &MultigresClusterReconciler{}
	_ = reconciler.SetupWithManager(nil)
}

func TestTemplateLogic_Unit(t *testing.T) {
	t.Run("MergeCellConfig", func(t *testing.T) {
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
		if *gw.Replicas != 2 {
			t.Errorf("Replicas merge failed")
		}
		if gw.PodAnnotations["foo"] != "bar" || gw.PodAnnotations["baz"] != "qux" {
			t.Errorf("Annotations merge failed")
		}
		if gw.PodLabels["l1"] != "v1" || gw.PodLabels["l2"] != "v2" {
			t.Errorf("Labels merge failed")
		}
		if !gw.Resources.Requests.Cpu().IsZero() {
			t.Error("Resources should have been replaced by override")
		}
		if gw.Affinity.PodAntiAffinity == nil {
			t.Error("Affinity should be replaced")
		}
		if topo.Etcd.Image != "base" {
			t.Error("Topo lost")
		}

		inline := &multigresv1alpha1.CellInlineSpec{
			MultiGateway: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(99))},
		}
		gw, _ = MergeCellConfig(tpl, overrides, inline)
		if *gw.Replicas != 99 {
			t.Errorf("Inline should take precedence")
		}

		gw, _ = MergeCellConfig(nil, overrides, nil)
		if *gw.Replicas != 2 {
			t.Error("Should work with nil template")
		}

		tplNil := &multigresv1alpha1.CellTemplate{
			Spec: multigresv1alpha1.CellTemplateSpec{
				MultiGateway: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
			},
		}
		gw, _ = MergeCellConfig(tplNil, overrides, nil)
		if gw.PodAnnotations["baz"] != "qux" {
			t.Error("Failed to initialize and merge Annotations")
		}
	})

	t.Run("MergeShardConfig", func(t *testing.T) {
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
		if len(orch.Cells) != 1 || orch.Cells[0] != "b" {
			t.Error("MultiOrch cells should be replaced")
		}

		p1 := pools["p1"]
		if *p1.ReplicasPerCell != 2 {
			t.Error("Pool p1 replicas not updated")
		}
		if p1.Type != "readWrite" { // Updated verification
			t.Error("Pool p1 type should be updated")
		}
		if p1.Storage.Size != "10Gi" {
			t.Error("Storage size not updated")
		}
		if p1.Postgres.Resources.Requests.Cpu().String() != "1" {
			t.Error("Postgres resources not updated")
		}
		if p1.Multipooler.Resources.Requests.Cpu().String() != "1" {
			t.Error("Multipooler resources not updated")
		}
		if p1.Affinity.PodAntiAffinity == nil {
			t.Error("Pool Affinity not replaced")
		}
		if len(p1.Cells) != 1 || p1.Cells[0] != "c2" {
			t.Error("Pool Cells not replaced")
		}

		inline := &multigresv1alpha1.ShardInlineSpec{
			MultiOrch: multigresv1alpha1.MultiOrchSpec{Cells: []multigresv1alpha1.CellName{"inline"}},
		}
		orch, _ = MergeShardConfig(tpl, overrides, inline)
		if orch.Cells[0] != "inline" {
			t.Error("Inline precedence failed")
		}
	})

	t.Run("ResolveGlobalTopo", func(t *testing.T) {
		spec := &multigresv1alpha1.GlobalTopoServerSpec{TemplateRef: "t1"}
		core := &multigresv1alpha1.CoreTemplate{
			Spec: multigresv1alpha1.CoreTemplateSpec{
				GlobalTopoServer: &multigresv1alpha1.TopoServerSpec{Etcd: &multigresv1alpha1.EtcdSpec{Image: "resolved"}},
			},
		}
		res := ResolveGlobalTopo(spec, core)
		if res.Etcd.Image != "resolved" {
			t.Error("Failed to resolve from template")
		}
		spec2 := &multigresv1alpha1.GlobalTopoServerSpec{TemplateRef: "t1", Etcd: &multigresv1alpha1.EtcdSpec{Image: "inline"}}
		res2 := ResolveGlobalTopo(spec2, nil)
		if res2.Etcd.Image != "inline" {
			t.Error("Failed to fallback to inline when core template nil")
		}

		// New Case: Missing everything (should return passed spec)
		spec4 := &multigresv1alpha1.GlobalTopoServerSpec{}
		res4 := ResolveGlobalTopo(spec4, nil)
		if res4 != spec4 {
			t.Error("Expected to return original spec when no inline config and no template")
		}
	})

	t.Run("ResolveMultiAdmin", func(t *testing.T) {
		spec := &multigresv1alpha1.MultiAdminConfig{TemplateRef: "t1"}
		core := &multigresv1alpha1.CoreTemplate{
			Spec: multigresv1alpha1.CoreTemplateSpec{
				MultiAdmin: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(10))},
			},
		}
		res := ResolveMultiAdmin(spec, core)
		if *res.Replicas != 10 {
			t.Error("Failed to resolve MultiAdmin from template")
		}
		spec2 := &multigresv1alpha1.MultiAdminConfig{TemplateRef: "t1", Spec: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(5))}}
		res2 := ResolveMultiAdmin(spec2, nil)
		if *res2.Replicas != 5 {
			t.Error("Failed to fallback to inline MultiAdmin")
		}
		res3 := ResolveMultiAdmin(&multigresv1alpha1.MultiAdminConfig{}, nil)
		if res3 != nil {
			t.Error("Expected nil for empty config")
		}

		// New Case: Missing everything (should return nil)
		spec5 := &multigresv1alpha1.MultiAdminConfig{}
		res5 := ResolveMultiAdmin(spec5, nil)
		if res5 != nil {
			t.Error("Expected nil when no config and no template")
		}
	})
}

func parseQty(s string) resource.Quantity {
	return resource.MustParse(s)
}

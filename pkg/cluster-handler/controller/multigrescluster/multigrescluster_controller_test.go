package multigrescluster

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
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
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

// setupFixtures helper returns a fresh set of test objects to ensure isolation between test functions.
func setupFixtures(tb testing.TB) (
	*multigresv1alpha1.CoreTemplate,
	*multigresv1alpha1.CellTemplate,
	*multigresv1alpha1.ShardTemplate,
	*multigresv1alpha1.MultigresCluster,
	string, string, string,
) {
	tb.Helper()

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
				MultiGateway:     "gateway:latest",
				MultiOrch:        "orch:latest",
				MultiPooler:      "pooler:latest",
				MultiAdmin:       "admin:latest",
				Postgres:         "postgres:15",
				ImagePullPolicy:  corev1.PullAlways,
				ImagePullSecrets: []corev1.LocalObjectReference{{Name: "pull-secret"}},
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

	coreTpl, cellTpl, shardTpl, baseCluster, clusterName, namespace, finalizerName := setupFixtures(
		t,
	)

	tests := map[string]struct {
		multigrescluster    *multigresv1alpha1.MultigresCluster
		existingObjects     []client.Object
		preReconcileUpdate  func(testing.TB, *multigresv1alpha1.MultigresCluster)
		skipClusterCreation bool
		validate            func(testing.TB, client.Client)
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
		"Create: Full Cluster Creation - Verify Images and Wiring": {
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				updatedCluster := &multigresv1alpha1.MultigresCluster{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, updatedCluster); err != nil {
					t.Fatal(err)
				}
				if !controllerutil.ContainsFinalizer(updatedCluster, finalizerName) {
					t.Error("Finalizer was not added to Cluster")
				}

				// Verify Cell
				cell := &multigresv1alpha1.Cell{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-zone-a", Namespace: namespace}, cell); err != nil {
					t.Fatal("Expected Cell 'zone-a' to exist")
				}

				// Check flattened image struct in Cell
				if got, want := cell.Spec.Images.MultiGateway, "gateway:latest"; got != want {
					t.Errorf("Cell image mismatch got %q, want %q", got, want)
				}
				if got, want := cell.Spec.Images.ImagePullPolicy, corev1.PullAlways; got != want {
					t.Errorf("Cell pull policy mismatch got %q, want %q", got, want)
				}
				if diff := cmp.Diff([]corev1.LocalObjectReference{{Name: "pull-secret"}}, cell.Spec.Images.ImagePullSecrets); diff != "" {
					t.Errorf("Cell pull secrets mismatch: %s", diff)
				}

				expectedAddr := clusterName + "-global-topo-client." + namespace + ".svc:2379"
				if got, want := cell.Spec.GlobalTopoServer.Address, expectedAddr; got != want {
					t.Errorf("Wiring Bug! Cell has wrong Topo Address got %q, want %q", got, want)
				}

				// Verify TableGroup
				tg := &multigresv1alpha1.TableGroup{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-db1-tg1", Namespace: namespace}, tg); err != nil {
					t.Fatal("Expected TableGroup to exist")
				}
				// Check propagated global images
				if got, want := tg.Spec.Images.MultiOrch, "orch:latest"; got != want {
					t.Errorf("TableGroup image mismatch got %q, want %q", got, want)
				}
				if got, want := tg.Spec.Images.ImagePullPolicy, corev1.PullAlways; got != want {
					t.Errorf("TableGroup pull policy mismatch got %q, want %q", got, want)
				}

				// Verify MultiAdmin Image
				deploy := &appsv1.Deployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-multiadmin", Namespace: namespace}, deploy); err != nil {
					t.Fatal("Expected MultiAdmin deployment to exist")
				}
				if got, want := deploy.Spec.Template.Spec.Containers[0].Image, "admin:latest"; got != want {
					t.Errorf("MultiAdmin image mismatch got %q, want %q", got, want)
				}
			},
		},
		"Create: Ultra-Minimalist (Shard Injection)": {
			// This test proves PopulateClusterDefaults logic is active in the controller
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.Databases = nil // Clear databases, rely on auto-injection
			},
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				// The system should have injected:
				// Database "postgres" -> TableGroup "default" -> Shard "0"
				tgName := clusterName + "-postgres-default"
				tg := &multigresv1alpha1.TableGroup{}
				if err := c.Get(ctx, types.NamespacedName{Name: tgName, Namespace: namespace}, tg); err != nil {
					t.Fatalf("System Catalog TableGroup not found: %v", err)
				}

				if len(tg.Spec.Shards) != 1 {
					t.Fatalf("Expected 1 shard (injected '0'), got %d", len(tg.Spec.Shards))
				}
				if got, want := tg.Spec.Shards[0].Name, "0"; got != want {
					t.Errorf("Expected injected shard name '0', got %q", got)
				}
				// Verify template resolution worked on injected shard
				if got, want := *tg.Spec.Shards[0].MultiOrch.Replicas, int32(3); got != want {
					t.Errorf(
						"Injected shard was not resolved against default template. Replicas: %d",
						got,
					)
				}
			},
		},
		"Create: Independent Templates (Topo vs Admin)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.TemplateDefaults.CoreTemplate = "" // clear default
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

				ts := &multigresv1alpha1.TopoServer{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-global-topo", Namespace: namespace}, ts); err != nil {
					t.Fatal(err)
				}
				if got, want := ts.Spec.Etcd.Image, "etcd:topo"; got != want {
					t.Errorf("TopoServer image mismatch got %q, want %q", got, want)
				}

				deploy := &appsv1.Deployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-multiadmin", Namespace: namespace}, deploy); err != nil {
					t.Fatal(err)
				}
				if got, want := *deploy.Spec.Replicas, int32(5); got != want {
					t.Errorf("MultiAdmin replicas mismatch got %d, want %d", got, want)
				}
			},
		},
		"Create: External Topo Integration": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.GlobalTopoServer = multigresv1alpha1.GlobalTopoServerSpec{
					External: &multigresv1alpha1.ExternalTopoServerSpec{
						Endpoints: []multigresv1alpha1.EndpointUrl{"http://external-etcd:2379"},
					},
				}
			},
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				ts := &multigresv1alpha1.TopoServer{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-global-topo", Namespace: namespace}, ts); !apierrors.IsNotFound(
					err,
				) {
					t.Fatal("Global TopoServer should NOT be created for External mode")
				}
				cell := &multigresv1alpha1.Cell{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-zone-a", Namespace: namespace}, cell); err != nil {
					t.Fatal(err)
				}
				if got, want := cell.Spec.GlobalTopoServer.Address, "http://external-etcd:2379"; got != want {
					t.Errorf("External address mismatch got %q, want %q", got, want)
				}
			},
		},
		"Create: MultiOrch Skip Defaulting (Explicit Cells)": {
			// This covers the branch `if len(orch.Cells) == 0` being FALSE
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.Databases[0].TableGroups[0].Shards[0].Spec = &multigresv1alpha1.ShardInlineSpec{
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone-custom"},
					},
				}
			},
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			validate: func(t testing.TB, c client.Client) {
				tg := &multigresv1alpha1.TableGroup{}
				if err := c.Get(
					t.Context(),
					types.NamespacedName{Name: clusterName + "-db1-tg1", Namespace: namespace},
					tg,
				); err != nil {
					t.Fatalf("failed to get tablegroup: %v", err)
				}
				if got := tg.Spec.Shards[0].MultiOrch.Cells[0]; got != "zone-custom" {
					t.Errorf("Expected explicit cell 'zone-custom', got %s", got)
				}
			},
		},
		"Reconcile: Prune Orphaned Resources": {
			existingObjects: []client.Object{
				coreTpl, cellTpl, shardTpl,
				// Orphaned Cell (not in spec)
				&multigresv1alpha1.Cell{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName + "-orphaned-cell",
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
					Spec: multigresv1alpha1.CellSpec{Name: "orphaned-cell"},
				},
				// Orphaned TableGroup (not in spec)
				&multigresv1alpha1.TableGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName + "-orphaned-tg",
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
				},
			},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				cell := &multigresv1alpha1.Cell{}
				err := c.Get(
					ctx,
					types.NamespacedName{
						Name:      clusterName + "-orphaned-cell",
						Namespace: namespace,
					},
					cell,
				)
				if !apierrors.IsNotFound(err) {
					t.Errorf("Expected orphaned cell to be deleted, got error: %v", err)
				}

				tg := &multigresv1alpha1.TableGroup{}
				err = c.Get(
					ctx,
					types.NamespacedName{Name: clusterName + "-orphaned-tg", Namespace: namespace},
					tg,
				)
				if !apierrors.IsNotFound(err) {
					t.Errorf("Expected orphaned tablegroup to be deleted, got error: %v", err)
				}
			},
		},
		"Reconcile: Status Available (All Ready)": {
			existingObjects: []client.Object{
				coreTpl, cellTpl, shardTpl,
				// Existing Cell that is Ready (Mocking status from child controller)
				&multigresv1alpha1.Cell{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName + "-zone-a",
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
					Spec: multigresv1alpha1.CellSpec{Name: "zone-a"},
					Status: multigresv1alpha1.CellStatus{
						Conditions: []metav1.Condition{
							{Type: "Available", Status: metav1.ConditionTrue},
						},
						GatewayReplicas: 2,
					},
				},
			},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				cluster := &multigresv1alpha1.MultigresCluster{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, cluster); err != nil {
					t.Fatal(err)
				}

				// Verify Aggregated Status
				cond := meta.FindStatusCondition(cluster.Status.Conditions, "Available")
				if cond == nil {
					t.Fatal("Available condition not found")
					return // explicit return to satisfy linter
				}
				if cond.Status != metav1.ConditionTrue {
					t.Errorf("Expected Available=True, got %s", cond.Status)
				}

				// Verify Cell Summary
				summary, ok := cluster.Status.Cells["zone-a"]
				if !ok {
					t.Fatal("Cell zone-a summary missing")
				}
				if !summary.Ready {
					t.Error("Expected Cell summary Ready=true")
				}
				if summary.GatewayReplicas != 2 {
					t.Errorf("Expected GatewayReplicas=2, got %d", summary.GatewayReplicas)
				}
			},
		},
		"Reconcile: Implicit Cell Sorting": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				// Define 2 cells to force multi-cell expansion
				c.Spec.Cells = []multigresv1alpha1.CellConfig{
					{Name: "zone-b", Zone: "us-east-1b"}, // b comes after a
					{Name: "zone-a", Zone: "us-east-1a"},
				}
				// We do NOT set Shard.Spec.MultiOrch.Cells explicitly.
			},
			// FIX: Use a ShardTemplate that HAS explicit pool cells, so the controller finds them
			// and populates MultiOrch.Cells, triggering the sort logic.
			existingObjects: []client.Object{
				coreTpl, cellTpl,
				&multigresv1alpha1.ShardTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "default-shard", Namespace: namespace},
					Spec: multigresv1alpha1.ShardTemplateSpec{
						MultiOrch: &multigresv1alpha1.MultiOrchSpec{
							StatelessSpec: multigresv1alpha1.StatelessSpec{
								Replicas: ptr.To(int32(3)),
							},
							// Cells EMPTY to trigger defaulting logic
						},
						Pools: map[string]multigresv1alpha1.PoolSpec{
							"primary": {
								ReplicasPerCell: ptr.To(int32(2)),
								Type:            "readWrite",
								// Explicit cells to be aggregated
								Cells: []multigresv1alpha1.CellName{"zone-b", "zone-a"},
							},
						},
					},
				},
			},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				tg := &multigresv1alpha1.TableGroup{}
				tgName := clusterName + "-db1-tg1"
				if err := c.Get(ctx, types.NamespacedName{Name: tgName, Namespace: namespace}, tg); err != nil {
					t.Fatal(err)
				}

				shards := tg.Spec.Shards
				if len(shards) == 0 {
					t.Fatal("No shards found")
				}
				cells := shards[0].MultiOrch.Cells
				if len(cells) != 2 {
					t.Fatalf("Expected 2 cells, got %d", len(cells))
				}
				// Verify Order (Must be sorted alphabetically: zone-a, zone-b)
				if cells[0] != "zone-a" || cells[1] != "zone-b" {
					t.Errorf("Cells not sorted: %v", cells)
				}
			},
		},
		"Delete: Allow Finalization if Children Gone": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
			},
			existingObjects: []client.Object{},
			validate: func(t testing.TB, c client.Client) {
				updated := &multigresv1alpha1.MultigresCluster{}
				err := c.Get(
					t.Context(),
					types.NamespacedName{Name: clusterName, Namespace: namespace},
					updated,
				)
				if err == nil {
					if controllerutil.ContainsFinalizer(updated, finalizerName) {
						t.Error("Finalizer was not removed")
					}
				}
			},
		},
		"Object Not Found (Clean Exit)": {
			skipClusterCreation: true,
			existingObjects:     []client.Object{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Default to all standard templates if existingObjects is nil
			objects := tc.existingObjects
			if objects == nil {
				objects = []client.Object{coreTpl, cellTpl, shardTpl}
			}

			clientBuilder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(&multigresv1alpha1.MultigresCluster{}, &multigresv1alpha1.Cell{}, &multigresv1alpha1.TableGroup{})
			baseClient := clientBuilder.Build()

			finalClient := baseClient

			// Apply defaults if no specific cluster is provided
			cluster := tc.multigrescluster
			if cluster == nil {
				cluster = baseCluster.DeepCopy()
			}

			// Apply pre-reconcile updates if defined
			if tc.preReconcileUpdate != nil {
				tc.preReconcileUpdate(t, cluster)
			}

			shouldDelete := cluster.GetDeletionTimestamp() != nil &&
				!cluster.GetDeletionTimestamp().IsZero()

			if !strings.Contains(name, "Object Not Found") {
				check := &multigresv1alpha1.MultigresCluster{}
				err := baseClient.Get(
					t.Context(),
					types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
					check,
				)
				if apierrors.IsNotFound(err) {
					if err := baseClient.Create(t.Context(), cluster); err != nil {
						t.Fatalf("failed to create initial cluster: %v", err)
					}

					// Ensure DeletionTimestamp is set in the API if the test requires it.
					// client.Create strips this field, so we must invoke Delete() to re-apply it.
					if shouldDelete {
						// 1. Refresh object to avoid ResourceVersion conflict
						if err := baseClient.Get(t.Context(), types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, cluster); err != nil {
							t.Fatalf("failed to refresh cluster before delete: %v", err)
						}
						// 2. Delete it
						if err := baseClient.Delete(t.Context(), cluster); err != nil {
							t.Fatalf("failed to set deletion timestamp: %v", err)
						}
						// 3. Refresh again to ensure the controller sees the deletion timestamp
						if err := baseClient.Get(t.Context(), types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, cluster); err != nil {
							t.Fatalf("failed to refresh cluster after deletion: %v", err)
						}
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

	coreTpl, cellTpl, shardTpl, baseCluster, clusterName, namespace, finalizerName := setupFixtures(
		t,
	)
	errSimulated := errors.New("simulated error for testing")

	tests := map[string]struct {
		multigrescluster    *multigresv1alpha1.MultigresCluster
		existingObjects     []client.Object
		failureConfig       *testutil.FailureConfig
		preReconcileUpdate  func(testing.TB, *multigresv1alpha1.MultigresCluster)
		skipClusterCreation bool
		wantErrMsg          string // Optional: assert specific error message
	}{
		"Delete: Block Finalization if Cells Exist": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.Cell{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName + "-zone-a",
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
				},
			},
			wantErrMsg: "cells still exist",
		},
		"Delete: Block Finalization if TableGroups Exist": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.TableGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName + "-db1-tg1",
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
				},
			},
			wantErrMsg: "tablegroups still exist",
		},
		"Delete: Block Finalization if TopoServer Exists": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.TopoServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName + "-global-topo",
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
				},
			},
			wantErrMsg: "toposervers still exist",
		},
		"Error: Explicit Core Template Missing (Should Fail)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.TemplateDefaults.CoreTemplate = "non-existent-template"
			},
			existingObjects: []client.Object{}, // No templates exist
			failureConfig:   nil,               // No API failure, just logical failure
			// Matches reconcileGlobalTopoServer wrapper + ResolveGlobalTopo implicit error
			wantErrMsg: "failed to resolve global topo",
		},
		"Error: Explicit Cell Template Missing": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.Cells[0].CellTemplate = "missing-cell-tpl"
			},
			// Ensure core and shard templates exist so reconciliation proceeds to Cells
			existingObjects: []client.Object{coreTpl, shardTpl},
			// Matches reconcileCells wrapper
			wantErrMsg: "failed to resolve cell",
		},
		"Error: Explicit Shard Template Missing": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.Databases[0].TableGroups[0].Shards[0].ShardTemplate = "missing-shard-tpl"
			},
			// Ensure core and cell templates exist so reconciliation proceeds to Databases
			existingObjects: []client.Object{coreTpl, cellTpl},
			// Matches reconcileDatabases wrapper
			wantErrMsg: "failed to resolve shard",
		},
		"Error: Fetch Cluster Failed": {
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName(clusterName, errSimulated),
			},
			wantErrMsg: "failed to get MultigresCluster",
		},
		"Error: Add Finalizer Failed": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Finalizers = nil // Ensure we trigger the Add Finalizer path
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName(clusterName, errSimulated),
			},
			wantErrMsg: "failed to add finalizer",
		},
		"Error: Remove Finalizer Failed": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName(clusterName, errSimulated),
			},
			wantErrMsg: "failed to remove finalizer",
		},
		"Error: CheckChildrenDeleted (List Cells Failed)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnList: func(list client.ObjectList) error {
					if _, ok := list.(*multigresv1alpha1.CellList); ok {
						return errSimulated
					}
					return nil
				},
			},
			wantErrMsg: "failed to list cells",
		},
		"Error: CheckChildrenDeleted (List TableGroups Failed)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnList: func(list client.ObjectList) error {
					if _, ok := list.(*multigresv1alpha1.TableGroupList); ok {
						return errSimulated
					}
					return nil
				},
			},
			wantErrMsg: "failed to list tablegroups",
		},
		"Error: CheckChildrenDeleted (List TopoServers Failed)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{finalizerName}
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnList: func(list client.ObjectList) error {
					if _, ok := list.(*multigresv1alpha1.TopoServerList); ok {
						return errSimulated
					}
					return nil
				},
			},
			wantErrMsg: "failed to list toposervers",
		},
		"Error: Resolve CoreTemplate Failed": {
			existingObjects: []client.Object{coreTpl},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName("default-core", errSimulated),
			},
			// Matches reconcileGlobalTopoServer wrapper
			wantErrMsg: "failed to resolve global topo",
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
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName("admin-core-fail", errSimulated),
			},
			// Matches reconcileMultiAdmin wrapper
			wantErrMsg: "failed to resolve multiadmin",
		},
		"Error: Create GlobalTopo Failed": {
			failureConfig: &testutil.FailureConfig{
				OnCreate: testutil.FailOnObjectName(clusterName+"-global-topo", errSimulated),
			},
			wantErrMsg: "failed to create/update global topo",
		},
		"Error: Create MultiAdmin Failed": {
			failureConfig: &testutil.FailureConfig{
				OnCreate: testutil.FailOnObjectName(clusterName+"-multiadmin", errSimulated),
			},
			wantErrMsg: "failed to create/update multiadmin",
		},
		"Error: Resolve CellTemplate Failed": {
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName("default-cell", errSimulated),
			},
			// Matches reconcileCells wrapper
			wantErrMsg: "failed to resolve cell",
		},
		"Error: List Existing Cells Failed (Reconcile Loop)": {
			// Important: We must populate existingObjects so early checks pass and execution reaches List()
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig: &testutil.FailureConfig{
				OnList: func(list client.ObjectList) error {
					if _, ok := list.(*multigresv1alpha1.CellList); ok {
						return errSimulated
					}
					return nil
				},
			},
			wantErrMsg: "failed to list existing cells",
		},
		"Error: Create Cell Failed": {
			failureConfig: &testutil.FailureConfig{
				OnCreate: testutil.FailOnObjectName(clusterName+"-zone-a", errSimulated),
			},
			wantErrMsg: "failed to create/update cell",
		},
		"Error: Prune Cell Failed": {
			existingObjects: []client.Object{
				coreTpl, cellTpl, shardTpl,
				&multigresv1alpha1.Cell{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName + "-zone-b",
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnDelete: testutil.FailOnObjectName(clusterName+"-zone-b", errSimulated),
			},
			wantErrMsg: "failed to delete orphaned cell",
		},
		"Error: List Existing TableGroups Failed": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig: &testutil.FailureConfig{
				OnList: func(list client.ObjectList) error {
					if _, ok := list.(*multigresv1alpha1.TableGroupList); ok {
						return errSimulated
					}
					return nil
				},
			},
			wantErrMsg: "failed to list existing tablegroups",
		},
		"Error: Resolve ShardTemplate Failed": {
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName("default-shard", errSimulated),
			},
			// Matches reconcileDatabases wrapper
			wantErrMsg: "failed to resolve shard",
		},
		"Error: Create TableGroup Failed": {
			failureConfig: &testutil.FailureConfig{
				OnCreate: testutil.FailOnObjectName(clusterName+"-db1-tg1", errSimulated),
			},
			wantErrMsg: "failed to create/update tablegroup",
		},
		"Error: Prune TableGroup Failed": {
			existingObjects: []client.Object{
				coreTpl, cellTpl, shardTpl,
				&multigresv1alpha1.TableGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName + "-orphan-tg",
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnDelete: testutil.FailOnObjectName(clusterName+"-orphan-tg", errSimulated),
			},
			wantErrMsg: "failed to delete orphaned tablegroup",
		},
		"Error: UpdateStatus (List Cells Failed)": {
			failureConfig: &testutil.FailureConfig{
				OnList: func() func(client.ObjectList) error {
					count := 0
					return func(list client.ObjectList) error {
						if _, ok := list.(*multigresv1alpha1.CellList); ok {
							count++
							if count > 1 {
								return errSimulated
							}
						}
						return nil
					}
				}(),
			},
			wantErrMsg: "failed to list cells for status",
		},
		"Error: UpdateStatus (List TableGroups Failed)": {
			failureConfig: &testutil.FailureConfig{
				OnList: func() func(client.ObjectList) error {
					count := 0
					return func(list client.ObjectList) error {
						if _, ok := list.(*multigresv1alpha1.TableGroupList); ok {
							count++
							if count > 1 {
								return errSimulated
							}
						}
						return nil
					}
				}(),
			},
			wantErrMsg: "failed to list tablegroups for status",
		},
		"Error: Update Status Failed (API Error)": {
			failureConfig: &testutil.FailureConfig{
				OnStatusUpdate: testutil.FailOnObjectName(clusterName, errSimulated),
			},
			wantErrMsg: "failed to update cluster status",
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
				&multigresv1alpha1.CoreTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: namespace},
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
								return errSimulated
							}
						}
						return nil
					}
				}(),
			},
			// This specifically validates we hit the error inside reconcileCells calling getGlobalTopoRef
			wantErrMsg: "failed to get global topo ref",
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
				&multigresv1alpha1.CoreTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: namespace},
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
								return errSimulated
							}
						}
						return nil
					}
				}(),
			},
			// This specifically validates we hit the error inside reconcileDatabases calling getGlobalTopoRef
			wantErrMsg: "failed to get global topo ref",
		},
		"Create: Long Names (Truncation Check)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				longName := strings.Repeat("a", 50)
				c.Spec.Databases[0].Name = longName
				c.Spec.Databases[0].TableGroups[0].Name = longName
			},
			wantErrMsg: "exceeds 50 characters",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			objects := tc.existingObjects
			if objects == nil {
				objects = []client.Object{coreTpl, cellTpl, shardTpl}
			}

			clientBuilder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
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

			shouldDelete := cluster.GetDeletionTimestamp() != nil &&
				!cluster.GetDeletionTimestamp().IsZero()

			if !strings.Contains(name, "Object Not Found") {
				check := &multigresv1alpha1.MultigresCluster{}
				err := baseClient.Get(
					t.Context(),
					types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
					check,
				)
				if apierrors.IsNotFound(err) {
					if err := baseClient.Create(t.Context(), cluster); err != nil {
						t.Fatalf("failed to create initial cluster: %v", err)
					}

					// Ensure DeletionTimestamp is set in the API if the test requires it.
					// client.Create strips this field, so we must invoke Delete() to re-apply it.
					if shouldDelete {
						// 1. Refresh object to avoid ResourceVersion conflict
						if err := baseClient.Get(t.Context(), types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, cluster); err != nil {
							t.Fatalf("failed to refresh cluster before delete: %v", err)
						}
						// 2. Delete it
						if err := baseClient.Delete(t.Context(), cluster); err != nil {
							t.Fatalf("failed to set deletion timestamp: %v", err)
						}
						// 3. Refresh again to ensure the controller sees the deletion timestamp
						if err := baseClient.Get(t.Context(), types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, cluster); err != nil {
							t.Fatalf("failed to refresh cluster after deletion: %v", err)
						}
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
			} else if tc.wantErrMsg != "" && !strings.Contains(err.Error(), tc.wantErrMsg) {
				t.Errorf("Error mismatch. Expected substring %q, got %q", tc.wantErrMsg, err.Error())
			}
		})
	}
}

func TestSetupWithManager_Coverage(t *testing.T) {
	t.Parallel()

	// Test the default path (no options)
	t.Run("No Options", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Recovered expected panic: %v", r)
			}
		}()
		reconciler := &MultigresClusterReconciler{}
		_ = reconciler.SetupWithManager(nil)
	})

	// Test the path with options to ensure coverage of the 'if len(opts) > 0' block
	t.Run("With Options", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Recovered expected panic: %v", r)
			}
		}()
		reconciler := &MultigresClusterReconciler{}
		_ = reconciler.SetupWithManager(nil, controller.Options{MaxConcurrentReconciles: 1})
	})
}

func parseQty(s string) resource.Quantity {
	return resource.MustParse(s)
}

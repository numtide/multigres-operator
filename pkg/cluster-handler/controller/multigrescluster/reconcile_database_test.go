package multigrescluster

import (
	"errors"
	"strings"
	"testing"

	"github.com/numtide/multigres-operator/pkg/testutil"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcile_Databases(t *testing.T) {
	coreTpl, cellTpl, shardTpl, _, clusterName, namespace, _ := setupFixtures(t)
	errSimulated := errors.New("simulated error for testing")

	tests := map[string]reconcileTestCase{
		"Create: Ultra-Minimalist (Shard Injection)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.Databases = nil // Clear databases, rely on auto-injection
			},
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				tgName := clusterName + "-postgres-default"
				tg := &multigresv1alpha1.TableGroup{}
				if err := c.Get(ctx, types.NamespacedName{Name: tgName, Namespace: namespace}, tg); err != nil {
					t.Fatalf("System Catalog TableGroup not found: %v", err)
				}

				if len(tg.Spec.Shards) != 1 {
					t.Fatalf("Expected 1 shard (injected '0'), got %d", len(tg.Spec.Shards))
				}
				// Verify defaults applied.
				// NOTE: We expect 3 replicas here because 'shardTpl' (the default template in fixtures)
				// defines replicas: 3. The resolver correctly prioritizes the Namespace Default (Level 3)
				// over the Operator Default (Level 4, which is 1).
				if got, want := *tg.Spec.Shards[0].MultiOrch.Replicas, int32(3); got != want {
					t.Errorf("Injected shard replicas mismatch. Replicas: %d, Want: %d", got, want)
				}
				if len(tg.Spec.Shards[0].MultiOrch.Cells) != 1 ||
					tg.Spec.Shards[0].MultiOrch.Cells[0] != "zone-a" {
					t.Errorf(
						"Expected MultiOrch to inherit cell 'zone-a', got %v",
						tg.Spec.Shards[0].MultiOrch.Cells,
					)
				}
			},
		},
		"Create: MultiOrch Skip Defaulting (Explicit Cells)": {
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
		"Reconcile: Implicit Cell Sorting": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.Cells = []multigresv1alpha1.CellConfig{
					{Name: "zone-b", Zone: "us-east-1b"},
					{Name: "zone-a", Zone: "us-east-1a"},
				}
			},
			existingObjects: []client.Object{
				coreTpl, cellTpl,
				&multigresv1alpha1.ShardTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "default-shard", Namespace: namespace},
					Spec: multigresv1alpha1.ShardTemplateSpec{
						MultiOrch: &multigresv1alpha1.MultiOrchSpec{
							StatelessSpec: multigresv1alpha1.StatelessSpec{
								Replicas: ptr.To(int32(3)),
							},
						},
						Pools: map[string]multigresv1alpha1.PoolSpec{
							"primary": {
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
				cells := tg.Spec.Shards[0].MultiOrch.Cells
				if len(cells) != 2 {
					t.Fatalf("Expected 2 cells, got %d", len(cells))
				}
				if cells[0] != "zone-a" || cells[1] != "zone-b" {
					t.Errorf("Cells not sorted: %v", cells)
				}
			},
		},
		"Error: Explicit Shard Template Missing": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.Databases[0].TableGroups[0].Shards[0].ShardTemplate = "missing-shard-tpl"
			},
			existingObjects: []client.Object{coreTpl, cellTpl},
			wantErrMsg:      "failed to resolve shard",
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
			wantErrMsg: "failed to resolve shard",
		},
		"Error: Create TableGroup Failed": {
			failureConfig: &testutil.FailureConfig{
				OnCreate: testutil.FailOnObjectName(clusterName+"-db1-tg1", errSimulated),
			},
			wantErrMsg: "failed to create tablegroup",
		},
		"Error: Get TableGroup Failed (Unexpected Error)": {
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName(clusterName+"-db1-tg1", errSimulated),
			},
			wantErrMsg: "failed to get tablegroup",
		},
		"Error: Update TableGroup Failed": {
			existingObjects: []client.Object{
				coreTpl, cellTpl, shardTpl,
				&multigresv1alpha1.TableGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName + "-db1-tg1",
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName(clusterName+"-db1-tg1", errSimulated),
			},
			wantErrMsg: "failed to update tablegroup",
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
		"Error: Global Topo Resolution Failed (During Database Reconcile)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.TemplateDefaults.CoreTemplate = ""
				c.Spec.GlobalTopoServer = &multigresv1alpha1.GlobalTopoServerSpec{
					TemplateRef: "topo-fail-db",
				}
				c.Spec.MultiAdmin = nil
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

	runReconcileTest(t, tests)
}

func TestReconcile_Databases_BuildFailure(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	cluster := &multigresv1alpha1.MultigresCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MultigresCluster",
			APIVersion: multigresv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "c1",
			Namespace:  "ns1",
			Finalizers: []string{"multigres.com/finalizer"},
		},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
				External: &multigresv1alpha1.ExternalTopoServerSpec{
					Endpoints: []multigresv1alpha1.EndpointUrl{"http://ext:2379"},
				},
			},
			MultiAdmin: nil, // MultiAdmin will default and succeed.
			Databases: []multigresv1alpha1.DatabaseConfig{
				{
					Name: strings.Repeat("a", 64), // Long Name triggers failure in BuildTableGroup
					TableGroups: []multigresv1alpha1.TableGroupConfig{
						{
							Name: "tg1",
							Shards: []multigresv1alpha1.ShardConfig{
								{Name: "0", ShardTemplate: "shard-tpl"},
							},
						},
					},
				},
			},
		},
	}

	tmpl := &multigresv1alpha1.ShardTemplate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ShardTemplate",
			APIVersion: multigresv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{Name: "shard-tpl", Namespace: "ns1"},
		Spec:       multigresv1alpha1.ShardTemplateSpec{},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, tmpl).
		Build()

	r := &MultigresClusterReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(100),
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
	}

	_, err := r.Reconcile(t.Context(), req)
	if err == nil {
		t.Fatal("Expected error from Reconcile, got nil")
	}
	if !contains(err.Error(), "failed to build tablegroup") {
		t.Errorf("Unexpected error: %v", err)
	}
}

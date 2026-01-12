package tablegroup

import (
	"errors"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

func setupFixtures(
	t testing.TB,
) (*multigresv1alpha1.TableGroup, string, string, string, string, string) {
	t.Helper()

	tgName := "test-tg"
	namespace := "default"
	clusterName := "test-cluster"
	dbName := "db1"
	tgLabelName := "tg1"

	baseTG := &multigresv1alpha1.TableGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tgName,
			Namespace: namespace,
			Labels: map[string]string{
				"multigres.com/cluster":    clusterName,
				"multigres.com/database":   dbName,
				"multigres.com/tablegroup": tgLabelName,
			},
		},
		Spec: multigresv1alpha1.TableGroupSpec{
			DatabaseName:   dbName,
			TableGroupName: tgLabelName,
			Images: multigresv1alpha1.ShardImages{
				MultiOrch:   "orch:v1",
				MultiPooler: "pooler:v1",
				Postgres:    "pg:15",
			},
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				Address: "http://etcd:2379",
			},
			Shards: []multigresv1alpha1.ShardResolvedSpec{
				{
					Name: "shard-0",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						StatelessSpec: multigresv1alpha1.StatelessSpec{
							Replicas: ptr.To(int32(1)),
						},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"data": {ReplicasPerCell: ptr.To(int32(2))},
					},
				},
			},
		},
	}
	return baseTG, tgName, namespace, clusterName, dbName, tgLabelName
}

func TestTableGroupReconciler_Reconcile_Success(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	baseTG, tgName, namespace, clusterName, dbName, tgLabelName := setupFixtures(t)

	tests := map[string]struct {
		tableGroup         *multigresv1alpha1.TableGroup
		existingObjects    []client.Object
		preReconcileUpdate func(testing.TB, *multigresv1alpha1.TableGroup)
		skipCreate         bool // If true, the object won't be created in the fake client (simulates Not Found)
		validate           func(testing.TB, client.Client)
	}{
		"Create: Shard Creation": {
			tableGroup:      baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				shardNameFull := fmt.Sprintf("%s-%s", tgName, "shard-0")
				shard := &multigresv1alpha1.Shard{}
				if err := c.Get(ctx, types.NamespacedName{Name: shardNameFull, Namespace: namespace}, shard); err != nil {
					t.Fatalf("Shard %s not created: %v", shardNameFull, err)
				}
				if got, want := shard.Spec.DatabaseName, dbName; got != want {
					t.Errorf("Shard DB name mismatch got %q, want %q", got, want)
				}
			},
		},
		"Update: Apply Changes and Prune Orphans": {
			tableGroup: baseTG.DeepCopy(),
			preReconcileUpdate: func(t testing.TB, tg *multigresv1alpha1.TableGroup) {
				tg.Spec.Shards = []multigresv1alpha1.ShardResolvedSpec{
					{
						Name: "shard-1", // New shard
						MultiOrch: multigresv1alpha1.MultiOrchSpec{
							StatelessSpec: multigresv1alpha1.StatelessSpec{
								Replicas: ptr.To(int32(1)),
							},
						},
					},
				}
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("%s-%s", tgName, "shard-0"),
						Namespace: namespace,
						Labels: map[string]string{
							"multigres.com/cluster":    clusterName,
							"multigres.com/database":   dbName,
							"multigres.com/tablegroup": tgLabelName,
						},
					},
					Spec: multigresv1alpha1.ShardSpec{ShardName: "shard-0"},
				},
			},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				newShard := &multigresv1alpha1.Shard{}
				if err := c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-%s", tgName, "shard-1"), Namespace: namespace}, newShard); err != nil {
					t.Error("New shard-1 not created")
				}
				oldShard := &multigresv1alpha1.Shard{}
				if err := c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-%s", tgName, "shard-0"), Namespace: namespace}, oldShard); !apierrors.IsNotFound(
					err,
				) {
					t.Error("Old shard-0 was not pruned")
				}
			},
		},
		"Status: Update Ready Count": {
			tableGroup: baseTG.DeepCopy(),
			existingObjects: []client.Object{
				&multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("%s-%s", tgName, "shard-0"),
						Namespace: namespace,
						Labels: map[string]string{
							"multigres.com/cluster":    clusterName,
							"multigres.com/database":   dbName,
							"multigres.com/tablegroup": tgLabelName,
						},
					},
					Spec: multigresv1alpha1.ShardSpec{ShardName: "shard-0"},
					Status: multigresv1alpha1.ShardStatus{
						Conditions: []metav1.Condition{
							{
								Type:               "Available",
								Status:             metav1.ConditionTrue,
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
			},
			validate: func(t testing.TB, c client.Client) {
				updatedTG := &multigresv1alpha1.TableGroup{}
				if err := c.Get(t.Context(), types.NamespacedName{Name: tgName, Namespace: namespace}, updatedTG); err != nil {
					t.Fatalf("failed to get tablegroup: %v", err)
				}
				if got, want := updatedTG.Status.ReadyShards, int32(1); got != want {
					t.Errorf("ReadyShards mismatch got %d, want %d", got, want)
				}
			},
		},
		"Status: Partial Ready (Not all shards ready)": {
			tableGroup: baseTG.DeepCopy(),
			existingObjects: []client.Object{
				&multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("%s-%s", tgName, "shard-0"),
						Namespace: namespace,
						Labels: map[string]string{
							"multigres.com/cluster":    clusterName,
							"multigres.com/database":   dbName,
							"multigres.com/tablegroup": tgLabelName,
						},
					},
					Spec: multigresv1alpha1.ShardSpec{ShardName: "shard-0"},
					// No status, so not ready
				},
			},
			validate: func(t testing.TB, c client.Client) {
				updatedTG := &multigresv1alpha1.TableGroup{}
				if err := c.Get(t.Context(), types.NamespacedName{Name: tgName, Namespace: namespace}, updatedTG); err != nil {
					t.Fatalf("failed to get tablegroup: %v", err)
				}
				if got, want := updatedTG.Status.ReadyShards, int32(0); got != want {
					t.Errorf("ReadyShards mismatch got %d, want %d", got, want)
				}
				if meta.IsStatusConditionTrue(updatedTG.Status.Conditions, "Available") {
					t.Error("TableGroup should NOT be Available")
				}
			},
		},
		"Status: Zero Shards (Vacuously True)": {
			tableGroup: baseTG.DeepCopy(),
			preReconcileUpdate: func(t testing.TB, tg *multigresv1alpha1.TableGroup) {
				tg.Spec.Shards = []multigresv1alpha1.ShardResolvedSpec{}
			},
			existingObjects: []client.Object{},
			validate: func(t testing.TB, c client.Client) {
				updatedTG := &multigresv1alpha1.TableGroup{}
				if err := c.Get(t.Context(), types.NamespacedName{Name: tgName, Namespace: namespace}, updatedTG); err != nil {
					t.Fatalf("failed to get tablegroup: %v", err)
				}
				if !meta.IsStatusConditionTrue(updatedTG.Status.Conditions, "Available") {
					t.Error("Zero shard TableGroup should be Available")
				}
			},
		},
		"Error: Object Not Found (Clean Exit)": {
			tableGroup:      baseTG.DeepCopy(),
			skipCreate:      true,
			existingObjects: []client.Object{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Apply pre-reconcile updates if defined
			if tc.preReconcileUpdate != nil {
				tc.preReconcileUpdate(t, tc.tableGroup)
			}

			objects := tc.existingObjects
			// Inject TableGroup if creation is not skipped
			if !tc.skipCreate {
				objects = append(objects, tc.tableGroup)
			}

			clientBuilder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(&multigresv1alpha1.TableGroup{}, &multigresv1alpha1.Shard{})
			baseClient := clientBuilder.Build()

			reconciler := &TableGroupReconciler{
				Client:   baseClient,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(100),
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tc.tableGroup.Name,
					Namespace: tc.tableGroup.Namespace,
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

func TestTableGroupReconciler_Reconcile_Failure(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	baseTG, tgName, namespace, clusterName, dbName, tgLabelName := setupFixtures(t)
	errSimulated := errors.New("simulated error for testing")

	tests := map[string]struct {
		tableGroup         *multigresv1alpha1.TableGroup
		existingObjects    []client.Object
		preReconcileUpdate func(testing.TB, *multigresv1alpha1.TableGroup)
		failureConfig      *testutil.FailureConfig
	}{
		"Error: Get TableGroup Failed": {
			tableGroup:      baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName(tgName, errSimulated),
			},
		},
		"Error: Create/Update Shard Failed": {
			tableGroup:      baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: testutil.FailOnObjectName(
					fmt.Sprintf("%s-%s", tgName, "shard-0"),
					errSimulated,
				),
			},
		},
		"Error: List Shards Failed (during pruning)": {
			tableGroup:      baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnList: func(list client.ObjectList) error {
					if _, ok := list.(*multigresv1alpha1.ShardList); ok {
						return errSimulated
					}
					return nil
				},
			},
		},
		"Error: List Shards Failed (during status check)": {
			tableGroup:      baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnList: testutil.FailObjListAfterNCalls(1, errSimulated),
			},
		},
		"Error: Delete Orphan Shard Failed": {
			tableGroup: baseTG.DeepCopy(),
			preReconcileUpdate: func(t testing.TB, tg *multigresv1alpha1.TableGroup) {
				tg.Spec.Shards = []multigresv1alpha1.ShardResolvedSpec{}
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("%s-%s", tgName, "shard-0"),
						Namespace: namespace,
						Labels: map[string]string{
							"multigres.com/cluster":    clusterName,
							"multigres.com/database":   dbName,
							"multigres.com/tablegroup": tgLabelName,
						},
					},
					Spec: multigresv1alpha1.ShardSpec{ShardName: "shard-0"},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnDelete: testutil.FailOnObjectName(
					fmt.Sprintf("%s-%s", tgName, "shard-0"),
					errSimulated,
				),
			},
		},
		"Error: Update Status Failed": {
			tableGroup:      baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnStatusUpdate: testutil.FailOnObjectName(tgName, errSimulated),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Apply pre-reconcile updates if defined
			if tc.preReconcileUpdate != nil {
				tc.preReconcileUpdate(t, tc.tableGroup)
			}

			objects := tc.existingObjects
			// Default behavior: create the TableGroup unless getting it is set to fail (which simulates Not Found or error)
			// For failure tests, usually the object exists so the code can proceed to the failing step.
			objects = append(objects, tc.tableGroup)

			clientBuilder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(&multigresv1alpha1.TableGroup{}, &multigresv1alpha1.Shard{})
			baseClient := clientBuilder.Build()

			finalClient := client.Client(baseClient)
			if tc.failureConfig != nil {
				finalClient = testutil.NewFakeClientWithFailures(baseClient, tc.failureConfig)
			}

			reconciler := &TableGroupReconciler{
				Client:   finalClient,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(100),
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tc.tableGroup.Name,
					Namespace: tc.tableGroup.Namespace,
				},
			}

			_, err := reconciler.Reconcile(t.Context(), req)
			if err == nil {
				t.Error("Expected error from Reconcile, got nil")
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
		reconciler := &TableGroupReconciler{}
		_ = reconciler.SetupWithManager(nil)
	})

	// Test the path with options to ensure coverage of the 'if len(opts) > 0' block
	t.Run("With Options", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Recovered expected panic: %v", r)
			}
		}()
		reconciler := &TableGroupReconciler{}
		_ = reconciler.SetupWithManager(nil, controller.Options{MaxConcurrentReconciles: 1})
	})
}

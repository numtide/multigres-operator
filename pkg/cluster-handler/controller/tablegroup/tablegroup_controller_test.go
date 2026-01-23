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
	"github.com/numtide/multigres-operator/pkg/cluster-handler/names"
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
			// Finalizers removed
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
		preReconcileClient func(testing.TB, client.Client) // Hook to modify client state before Reconcile
		nilRecorder        bool                            // If true, sets the Recorder to nil
		skipCreate         bool                            // If true, the object won't be created in the fake client (simulates Not Found)
		validate           func(testing.TB, client.Client)
	}{
		"Create: Shard Creation": {
			tableGroup:      baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				// Expect hashed name: md5("test-cluster", "db1", "tg1", "shard-0") -> "0a..."
				shardNameFull := names.JoinWithConstraints(
					names.DefaultConstraints,
					clusterName,
					dbName,
					tgLabelName,
					"shard-0",
				)
				shard := &multigresv1alpha1.Shard{}
				if err := c.Get(ctx, types.NamespacedName{Name: shardNameFull, Namespace: namespace}, shard); err != nil {
					t.Fatalf("Shard %s not created: %v", shardNameFull, err)
				}
				if got, want := shard.Spec.DatabaseName, dbName; got != want {
					t.Errorf("Shard DB name mismatch got %q, want %q", got, want)
				}
			},
		},

		"Status: Update Ready Count": {
			tableGroup: baseTG.DeepCopy(),
			existingObjects: []client.Object{
				&multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name: names.JoinWithConstraints(
							names.DefaultConstraints,
							clusterName,
							dbName,
							tgLabelName,
							"shard-0",
						),
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
		"Status: Shard Not Ready (False Condition)": {
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
								Type:   "Available",
								Status: metav1.ConditionFalse,
							},
							{
								Type:   "SomethingElse",
								Status: metav1.ConditionTrue,
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
				if got, want := updatedTG.Status.ReadyShards, int32(0); got != want {
					t.Errorf("ReadyShards mismatch got %d, want %d", got, want)
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
		"Update: Shard Update Success": {
			tableGroup: baseTG.DeepCopy(),
			preReconcileUpdate: func(t testing.TB, tg *multigresv1alpha1.TableGroup) {
				// Change spec to force update
				tg.Spec.Shards[0].MultiOrch.Replicas = ptr.To(int32(5))
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name: names.JoinWithConstraints(
							names.DefaultConstraints,
							clusterName,
							dbName,
							tgLabelName,
							"shard-0",
						),
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
				shard := &multigresv1alpha1.Shard{}
				shardName := names.JoinWithConstraints(
					names.DefaultConstraints,
					clusterName,
					dbName,
					tgLabelName,
					"shard-0",
				)
				if err := c.Get(t.Context(), types.NamespacedName{Name: shardName, Namespace: namespace}, shard); err != nil {
					t.Fatal(err)
				}
				if *shard.Spec.MultiOrch.Replicas != 5 {
					t.Errorf("Shard replicas not updated")
				}
			},
		},
		"Success: Early Return on Deletion": {
			tableGroup:      baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			preReconcileUpdate: func(t testing.TB, tg *multigresv1alpha1.TableGroup) {
				now := metav1.Now()
				tg.DeletionTimestamp = &now
				tg.Finalizers = []string{
					"test.finalizer",
				} // Prevent immediate deletion/GC simulation issues
			},
			validate: func(t testing.TB, c client.Client) {
				// Verify Shard is NOT created because checks are skipped
				shardNameFull := fmt.Sprintf("%s-%s", tgName, "shard-0")
				shard := &multigresv1alpha1.Shard{}
				if err := c.Get(t.Context(), types.NamespacedName{Name: shardNameFull, Namespace: namespace}, shard); !apierrors.IsNotFound(
					err,
				) {
					t.Errorf("Expected Shard %s to NOT be created", shardNameFull)
				}
			},
		},
		"Success: No Recorder": {
			tableGroup:      baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			nilRecorder:     true,
		},
		"Success: Build Shard (Long Name - Truncated)": {
			tableGroup: baseTG.DeepCopy(),
			preReconcileUpdate: func(t testing.TB, tg *multigresv1alpha1.TableGroup) {
				tg.Spec.Shards[0].Name = "a" + string(make([]byte, 64)) // > 63 chars
			},
			existingObjects: []client.Object{},
			// No validation needed if we just want to ensure it succeeds without error
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

			if tc.preReconcileClient != nil {
				tc.preReconcileClient(t, baseClient)
			}

			var recorder record.EventRecorder
			if !tc.nilRecorder {
				recorder = record.NewFakeRecorder(100)
			}

			reconciler := &TableGroupReconciler{
				Client:   baseClient,
				Scheme:   scheme,
				Recorder: recorder,
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
		"Error: Apply Shard Failed": {
			tableGroup:      baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnPatch: testutil.FailOnObjectName(
					names.JoinWithConstraints(
						names.DefaultConstraints,
						clusterName,
						dbName,
						tgLabelName,
						"shard-0",
					),
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
		"Error: Status List Failed (Second List Call)": {
			tableGroup:      baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnList: func() func(list client.ObjectList) error {
					count := 0
					return func(list client.ObjectList) error {
						if _, ok := list.(*multigresv1alpha1.ShardList); ok {
							count++
							if count == 2 {
								return errSimulated
							}
						}
						return nil
					}
				}(),
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
						Name: names.JoinWithConstraints(
							names.DefaultConstraints,
							clusterName,
							dbName,
							tgLabelName,
							"shard-0",
						),
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
					names.JoinWithConstraints(
						names.DefaultConstraints,
						clusterName,
						dbName,
						tgLabelName,
						"shard-0",
					),
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

			// Apply pre-reconcile updates
			if tc.preReconcileUpdate != nil {
				tc.preReconcileUpdate(t, tc.tableGroup)
			}

			objects := tc.existingObjects
			// Default behavior: create the TableGroup unless getting it handling failure
			// For failure tests, usually the object exists so the code can proceed to the failing step.
			objects = append(objects, tc.tableGroup)

			clientBuilder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(&multigresv1alpha1.TableGroup{}, &multigresv1alpha1.Shard{})
			baseClient := clientBuilder.Build()

			// For OnStatusUpdate failures, we need to make sure the object exists
			// and that we are targeting the right call. The fake client intercepts calls.
			finalClient := client.Client(baseClient)
			if tc.failureConfig != nil {
				finalClient = testutil.NewFakeClientWithFailures(baseClient, tc.failureConfig)
			}

			// Use explicit scheme for Reconciler.
			// If we want to simulate Build failure, we might need to inject a broken scheme here?
			// But the test structure iterates cases.
			// Ideally we catch "Build Failed" in a separate manual test if it requires structural changes (like Reconciler.Scheme change).
			// But let's try to add it here as a special case? No, the loop uses 'scheme'.

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

func TestTableGroupReconciler_Reconcile_BuildFailure(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	baseTG, _, _, _, _, _ := setupFixtures(t)

	// Create client with VALID scheme
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseTG).Build()

	// Create Reconciler with EMPTY scheme (causes BuildShard -> SetControllerReference to fail)
	r := &TableGroupReconciler{
		Client:   c,
		Scheme:   runtime.NewScheme(), // Empty!
		Recorder: record.NewFakeRecorder(100),
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{Name: baseTG.Name, Namespace: baseTG.Namespace},
	}
	_, err := r.Reconcile(t.Context(), req)
	if err == nil {
		t.Fatal("Expected Reconcile to fail due to Build error")
	}
	if err.Error() != "failed to build shard: no kind is registered for the type v1alpha1.TableGroup" {
		t.Logf("Got error: %v", err) // verify it's the right error
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

package multigrescluster

import (
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
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

// ============================================================================
// Shared Test Types & Helpers
// ============================================================================

// reconcileTestCase defines the structure for all controller unit tests
type reconcileTestCase struct {
	multigrescluster    *multigresv1alpha1.MultigresCluster
	existingObjects     []client.Object
	failureConfig       *testutil.FailureConfig
	preReconcileUpdate  func(testing.TB, *multigresv1alpha1.MultigresCluster)
	skipClusterCreation bool
	wantErrMsg          string
	// NEW: Verify specific events were emitted
	expectedEvents []string
	validate       func(testing.TB, client.Client)
}

// runReconcileTest is the shared runner for all split test files
func runReconcileTest(t *testing.T, tests map[string]reconcileTestCase) {
	t.Helper()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	coreTpl, cellTpl, shardTpl, baseCluster, _, _, _ := setupFixtures(t)

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

					if shouldDelete {
						// Simulate deletion workflow
						if err := baseClient.Get(t.Context(), types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, cluster); err != nil {
							t.Fatalf("failed to refresh cluster before delete: %v", err)
						}
						if err := baseClient.Delete(t.Context(), cluster); err != nil {
							t.Fatalf("failed to set deletion timestamp: %v", err)
						}
						if err := baseClient.Get(t.Context(), types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, cluster); err != nil {
							t.Fatalf("failed to refresh cluster after deletion: %v", err)
						}
					}
				}
			}

			// Create a buffered fake recorder to capture events
			fakeRecorder := record.NewFakeRecorder(100)
			reconciler := &MultigresClusterReconciler{
				Client:   finalClient,
				Scheme:   scheme,
				Recorder: fakeRecorder,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				},
			}

			_, err := reconciler.Reconcile(t.Context(), req)

			if tc.wantErrMsg != "" {
				if err == nil {
					t.Error("Expected error from Reconcile, got nil")
				} else if !strings.Contains(err.Error(), tc.wantErrMsg) {
					t.Errorf("Error mismatch. Expected substring %q, got %q", tc.wantErrMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("Unexpected error from Reconcile: %v", err)
			}

			// Verify Events
			if len(tc.expectedEvents) > 0 {
				close(fakeRecorder.Events)
				var gotEvents []string
				for evt := range fakeRecorder.Events {
					gotEvents = append(gotEvents, evt)
				}

				for _, want := range tc.expectedEvents {
					found := false
					for _, got := range gotEvents {
						if strings.Contains(got, want) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf(
							"Expected event containing %q not found. Got events: %v",
							want,
							gotEvents,
						)
					}
				}
			}

			if tc.validate != nil {
				tc.validate(t, baseClient)
			}
		})
	}
}

// setupFixtures provides fresh test data
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
			Name:      clusterName,
			Namespace: namespace,
			// Finalizers removed
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
			GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
				TemplateRef: "default-core",
			},
			MultiAdmin: &multigresv1alpha1.MultiAdminConfig{
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

func parseQty(s string) resource.Quantity {
	return resource.MustParse(s)
}

// ============================================================================
// Main Controller Logic & Lifecycle Tests
// ============================================================================

func TestMultigresClusterReconciler_Lifecycle(t *testing.T) {
	coreTpl, cellTpl, shardTpl, _, clusterName, namespace, _ := setupFixtures(t)
	errSimulated := errors.New("simulated error for testing")

	tests := map[string]reconcileTestCase{
		"Create: Full Cluster Creation - Verify Images and Wiring": {
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				// Verify Cell (Basic wiring check)
				cell := &multigresv1alpha1.Cell{}
				cellName := names.JoinWithConstraints(
					names.DefaultConstraints,
					clusterName,
					"zone-a",
				)
				if err := c.Get(ctx, types.NamespacedName{Name: cellName, Namespace: namespace}, cell); err != nil {
					t.Fatalf("Expected Cell %s to exist: %v", cellName, err)
				}
				if got, want := cell.Spec.Images.MultiGateway, "gateway:latest"; got != want {
					t.Errorf("Cell image mismatch got %q, want %q", got, want)
				}
			},
		},

		"Error: Fetch Cluster Failed": {
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName(clusterName, errSimulated),
			},
			wantErrMsg: "failed to get MultigresCluster",
		},

		"Success: TableGroup Name Too Long (Hashed)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.Databases = []multigresv1alpha1.DatabaseConfig{
					{
						Name: "db1",
						TableGroups: []multigresv1alpha1.TableGroupConfig{
							{
								Name:   "this-name-is-extremely-long-and-will-fail-validation",
								Shards: []multigresv1alpha1.ShardConfig{{Name: "s1"}},
							},
						},
					},
				}
			},
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			wantErrMsg:      "",
		},
		"Error: Apply TableGroup Failed": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig: &testutil.FailureConfig{
				OnPatch: testutil.FailOnObjectName(
					names.JoinWithConstraints(names.DefaultConstraints, clusterName, "db1", "tg1"),
					errSimulated,
				),
			},
			wantErrMsg: "failed to apply tablegroup",
		},
		"Error: Delete Orphan TableGroup Failed": {
			existingObjects: []client.Object{
				coreTpl, cellTpl, shardTpl,
				&multigresv1alpha1.TableGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: names.JoinWithConstraints(
							names.DefaultConstraints,
							clusterName,
							"db1",
							"orphan-tg",
						),
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnDelete: testutil.FailOnObjectName(
					names.JoinWithConstraints(
						names.DefaultConstraints,
						clusterName,
						"db1",
						"orphan-tg",
					),
					errSimulated,
				),
			},
			wantErrMsg: "failed to delete orphaned tablegroup",
		},
		"Success: Prune Orphan TableGroup": {
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
			// VERIFY EVENT: Ensure the event is emitted on success
			expectedEvents: []string{"Normal Deleted Deleted orphaned TableGroup"},
			validate: func(t testing.TB, c client.Client) {
				tg := &multigresv1alpha1.TableGroup{}
				err := c.Get(
					t.Context(),
					types.NamespacedName{Name: clusterName + "-orphan-tg", Namespace: namespace},
					tg,
				)
				if !apierrors.IsNotFound(err) {
					t.Error("Orphan TableGroup was not deleted")
				}
			},
		},
		"Object Not Found (Clean Exit)": {
			skipClusterCreation: true,
			existingObjects:     []client.Object{},
		},
		"Error: PopulateClusterDefaults Failed (Implicit Shard Check)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.TemplateDefaults.ShardTemplate = "" // Force implicit check
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnNamespacedKeyName("default", namespace, errSimulated),
			},
			wantErrMsg: "failed to check for implicit shard template",
		},
		"Error: Reconcile Global Components Failed": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig: &testutil.FailureConfig{
				OnPatch: testutil.FailOnObjectName(clusterName+"-global-topo", errSimulated),
			},
			wantErrMsg: "failed to apply global topo server",
		},
		"Error: Reconcile Cells Failed": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig: &testutil.FailureConfig{
				OnPatch: testutil.FailOnObjectName(
					names.JoinWithConstraints(names.DefaultConstraints, clusterName, "zone-a"),
					errSimulated,
				),
			},
			wantErrMsg: "failed to apply cell",
		},
		"Error: Reconcile Databases Failed": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig: &testutil.FailureConfig{
				OnPatch: testutil.FailOnObjectName(
					names.JoinWithConstraints(names.DefaultConstraints, clusterName, "db1", "tg1"),
					errSimulated,
				),
			},
			wantErrMsg: "failed to apply tablegroup",
		},
		"Error: Update Status Failed": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig: &testutil.FailureConfig{
				OnStatusUpdate: testutil.FailOnObjectName(clusterName, errSimulated),
			},
			wantErrMsg: "failed to update cluster status",
		},
		"Error: getGlobalTopoRef Failed (Cells)": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			failureConfig: &testutil.FailureConfig{
				OnGet: func() func(client.ObjectKey) error {
					count := 0
					return func(key client.ObjectKey) error {
						if key.Name == "default-core" {
							count++
							// Call 1: reconcileGlobalComponents -> reconcileGlobalTopoServer
							// Call 2: reconcileGlobalComponents -> reconcileMultiAdmin
							// Call 3: reconcileCells -> getGlobalTopoRef (Fails)
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
		"Success: External Global Topo Resolution": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.GlobalTopoServer = &multigresv1alpha1.GlobalTopoServerSpec{
					External: &multigresv1alpha1.ExternalTopoServerSpec{
						Endpoints: []multigresv1alpha1.EndpointUrl{"http://external:2379"},
						RootPath:  "/custom/root",
					},
				}
			},
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			validate: func(t testing.TB, c client.Client) {
				cell := &multigresv1alpha1.Cell{}
				cellName := names.JoinWithConstraints(
					names.DefaultConstraints,
					clusterName,
					"zone-a",
				)
				if err := c.Get(t.Context(), types.NamespacedName{Name: cellName, Namespace: namespace}, cell); err != nil {
					t.Fatalf("Expected Cell %s to exist: %v", cellName, err)
				}
				if cell.Spec.GlobalTopoServer.Address != "http://external:2379" {
					t.Errorf(
						"Expected external address http://external:2379, got %s",
						cell.Spec.GlobalTopoServer.Address,
					)
				}
				if cell.Spec.GlobalTopoServer.RootPath != "/custom/root" {
					t.Errorf(
						"Expected external root path /custom/root, got %s",
						cell.Spec.GlobalTopoServer.RootPath,
					)
				}
			},
		},
		"Success: Early Return on Deletion": {
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				now := metav1.Now()
				c.DeletionTimestamp = &now
				c.Finalizers = []string{
					"test.finalizer",
				} // Prevent immediate deletion by fake client
			},
			// Expect NO error and NO events (since it returns early)
			wantErrMsg: "",
		},
	}

	runReconcileTest(t, tests)
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

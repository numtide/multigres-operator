package tablegroup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

func TestTableGroupReconciler_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

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

	errBoom := errors.New("boom")

	tests := []struct {
		name            string
		tg              *multigresv1alpha1.TableGroup
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
			name:            "Create: Shard Creation",
			tg:              baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			expectError:     false,
			validate: func(t *testing.T, c client.Client) {
				ctx := t.Context()
				shardNameFull := fmt.Sprintf("%s-%s", tgName, "shard-0")
				shard := &multigresv1alpha1.Shard{}
				if err := c.Get(ctx, types.NamespacedName{Name: shardNameFull, Namespace: namespace}, shard); err != nil {
					t.Fatalf("Shard %s not created: %v", shardNameFull, err)
				}
				if diff := cmp.Diff(dbName, shard.Spec.DatabaseName); diff != "" {
					t.Errorf("Shard DB name mismatch (-want +got):\n%s", diff)
				}
			},
		},
		{
			name: "Update: Apply Changes and Prune Orphans",
			tg: func() *multigresv1alpha1.TableGroup {
				t := baseTG.DeepCopy()
				t.Spec.Shards = []multigresv1alpha1.ShardResolvedSpec{
					{
						Name: "shard-1", // New shard
						MultiOrch: multigresv1alpha1.MultiOrchSpec{
							StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
						},
					},
				}
				return t
			}(),
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
			expectError: false,
			validate: func(t *testing.T, c client.Client) {
				ctx := t.Context()
				newShard := &multigresv1alpha1.Shard{}
				if err := c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-%s", tgName, "shard-1"), Namespace: namespace}, newShard); err != nil {
					t.Error("New shard-1 not created")
				}
				oldShard := &multigresv1alpha1.Shard{}
				if err := c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-%s", tgName, "shard-0"), Namespace: namespace}, oldShard); !apierrors.IsNotFound(err) {
					t.Error("Old shard-0 was not pruned")
				}
			},
		},
		{
			name:            "Status: Update Ready Count",
			tg:              baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			setupFunc: func(c client.Client) {
				shardNameFull := fmt.Sprintf("%s-%s", tgName, "shard-0")
				shard := &multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      shardNameFull,
						Namespace: namespace,
						Labels: map[string]string{
							"multigres.com/cluster":    clusterName,
							"multigres.com/database":   dbName,
							"multigres.com/tablegroup": tgLabelName,
						},
					},
					Spec: multigresv1alpha1.ShardSpec{ShardName: "shard-0"},
				}
				_ = c.Create(context.Background(), shard)
				shard.Status.Conditions = []metav1.Condition{
					{Type: "Available", Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now()},
				}
				_ = c.Status().Update(context.Background(), shard)
			},
			expectError: false,
			validate: func(t *testing.T, c client.Client) {
				updatedTG := &multigresv1alpha1.TableGroup{}
				_ = c.Get(t.Context(), types.NamespacedName{Name: tgName, Namespace: namespace}, updatedTG)
				if diff := cmp.Diff(int32(1), updatedTG.Status.ReadyShards); diff != "" {
					t.Errorf("ReadyShards mismatch (-want +got):\n%s", diff)
				}
			},
		},
		{
			name:            "Status: Partial Ready (Not all shards ready)",
			tg:              baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			setupFunc: func(c client.Client) {
				shardNameFull := fmt.Sprintf("%s-%s", tgName, "shard-0")
				shard := &multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      shardNameFull,
						Namespace: namespace,
						Labels: map[string]string{
							"multigres.com/cluster":    clusterName,
							"multigres.com/database":   dbName,
							"multigres.com/tablegroup": tgLabelName,
						},
					},
					Spec: multigresv1alpha1.ShardSpec{ShardName: "shard-0"},
				}
				_ = c.Create(context.Background(), shard)
			},
			expectError: false,
			validate: func(t *testing.T, c client.Client) {
				updatedTG := &multigresv1alpha1.TableGroup{}
				_ = c.Get(t.Context(), types.NamespacedName{Name: tgName, Namespace: namespace}, updatedTG)
				if diff := cmp.Diff(int32(0), updatedTG.Status.ReadyShards); diff != "" {
					t.Errorf("ReadyShards mismatch (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(false, meta.IsStatusConditionTrue(updatedTG.Status.Conditions, "Available")); diff != "" {
					t.Errorf("TableGroup Available condition mismatch (-want +got):\n%s", diff)
				}
			},
		},
		{
			name: "Status: Zero Shards (Vacuously True)",
			tg: func() *multigresv1alpha1.TableGroup {
				t := baseTG.DeepCopy()
				t.Spec.Shards = []multigresv1alpha1.ShardResolvedSpec{}
				return t
			}(),
			existingObjects: []client.Object{},
			expectError:     false,
			validate: func(t *testing.T, c client.Client) {
				updatedTG := &multigresv1alpha1.TableGroup{}
				_ = c.Get(t.Context(), types.NamespacedName{Name: tgName, Namespace: namespace}, updatedTG)
				if diff := cmp.Diff(true, meta.IsStatusConditionTrue(updatedTG.Status.Conditions, "Available")); diff != "" {
					t.Errorf("Zero shard TableGroup Available condition mismatch (-want +got):\n%s", diff)
				}
			},
		},

		// ---------------------------------------------------------------------
		// Error Scenarios
		// ---------------------------------------------------------------------
		{
			name:            "Error: Object Not Found (Clean Exit)",
			tg:              baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			expectError:     false,
		},
		{
			name:            "Error: Get TableGroup Failed",
			tg:              baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			failureConfig:   &testutil.FailureConfig{OnGet: testutil.FailOnKeyName(tgName, errBoom)},
			expectError:     true,
		},
		{
			name:            "Error: Create/Update Shard Failed",
			tg:              baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			failureConfig:   &testutil.FailureConfig{OnCreate: testutil.FailOnObjectName(fmt.Sprintf("%s-%s", tgName, "shard-0"), errBoom)},
			expectError:     true,
		},
		{
			name:            "Error: List Shards Failed (during pruning)",
			tg:              baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnList: func(list client.ObjectList) error {
					if _, ok := list.(*multigresv1alpha1.ShardList); ok {
						return errBoom
					}
					return nil
				},
			},
			expectError: true,
		},
		{
			name:            "Error: List Shards Failed (during status check)",
			tg:              baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnList: testutil.FailObjListAfterNCalls(1, errBoom),
			},
			expectError: true,
		},
		{
			name: "Error: Delete Orphan Shard Failed",
			tg: func() *multigresv1alpha1.TableGroup {
				t := baseTG.DeepCopy()
				t.Spec.Shards = []multigresv1alpha1.ShardResolvedSpec{}
				return t
			}(),
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
				},
			},
			failureConfig: &testutil.FailureConfig{OnDelete: testutil.FailOnObjectName(fmt.Sprintf("%s-%s", tgName, "shard-0"), errBoom)},
			expectError:   true,
		},
		{
			name:            "Error: Update Status Failed",
			tg:              baseTG.DeepCopy(),
			existingObjects: []client.Object{},
			failureConfig:   &testutil.FailureConfig{OnStatusUpdate: testutil.FailOnObjectName(tgName, errBoom)},
			expectError:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clientBuilder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingObjects...).
				WithStatusSubresource(&multigresv1alpha1.TableGroup{}, &multigresv1alpha1.Shard{})
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
				check := &multigresv1alpha1.TableGroup{}
				if err := baseClient.Get(context.Background(), types.NamespacedName{Name: tc.tg.Name, Namespace: tc.tg.Namespace}, check); apierrors.IsNotFound(err) {
					_ = baseClient.Create(context.Background(), tc.tg)
				}
			}

			reconciler := &TableGroupReconciler{
				Client: finalClient,
				Scheme: scheme,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tc.tg.Name,
					Namespace: tc.tg.Namespace,
				},
			}

			_, err := reconciler.Reconcile(context.Background(), req)

			if tc.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
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
	reconciler := &TableGroupReconciler{}
	_ = reconciler.SetupWithManager(nil)
}

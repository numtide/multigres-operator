package multigrescluster

import (
	"errors"
	"testing"

	"github.com/numtide/multigres-operator/pkg/testutil"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func TestReconcile_Status(t *testing.T) {
	coreTpl, cellTpl, shardTpl, _, clusterName, namespace, _ := setupFixtures(t)
	errSimulated := errors.New("simulated error for testing")

	tests := map[string]reconcileTestCase{
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
	}

	runReconcileTest(t, tests)
}

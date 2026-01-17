package multigrescluster

import (
	"context"
	"errors"
	"testing"

	"github.com/numtide/multigres-operator/pkg/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func TestReconcile_Status(t *testing.T) {
	_, _, _, _, clusterName, _, _ := setupFixtures(t)
	errSimulated := errors.New("simulated error for testing")

	tests := map[string]reconcileTestCase{
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

func TestUpdateStatus_Coverage(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	// Ready Cell
	cell := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cell-1",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
		Spec: multigresv1alpha1.CellSpec{
			Name: "cell-1",
		},
		Status: multigresv1alpha1.CellStatus{
			Conditions: []metav1.Condition{
				{Type: "Available", Status: metav1.ConditionTrue},
			},
			GatewayReplicas: 1,
		},
	}

	// Ready TableGroup
	tg := &multigresv1alpha1.TableGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tg-1",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
		Spec: multigresv1alpha1.TableGroupSpec{
			DatabaseName: "db1",
		},
		Status: multigresv1alpha1.TableGroupStatus{
			ReadyShards: 1,
			TotalShards: 1,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, cell, tg).
		WithStatusSubresource(cluster, cell, tg).
		Build()

	r := &MultigresClusterReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	if err := r.updateStatus(context.Background(), cluster); err != nil {
		t.Fatalf("updateStatus failed: %v", err)
	}

	// Refresh cluster
	if err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(cluster), cluster); err != nil {
		t.Fatalf("Failed to refresh cluster: %v", err)
	}

	// Verify Available Condition
	found := false
	for _, c := range cluster.Status.Conditions {
		if c.Type == "Available" {
			found = true
			if c.Status != metav1.ConditionTrue {
				t.Errorf("Expected Available=True, got %s", c.Status)
			}
		}
	}
	if !found {
		t.Error("Available condition not found")
	}

	// Verify Cell Status Summary
	if s, ok := cluster.Status.Cells["cell-1"]; !ok || !s.Ready {
		t.Errorf("Expected cell-1 to be ready in status summary, got %v", s)
	}

	// Verify Database Status Summary
	if s, ok := cluster.Status.Databases["db1"]; !ok || s.ReadyShards != 1 {
		t.Errorf("Expected db1 to have 1 ready shard in status summary, got %v", s)
	}
}

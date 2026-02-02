package multigrescluster

import (
	"context"
	"errors"
	"testing"

	"github.com/numtide/multigres-operator/pkg/testutil"
	"k8s.io/apimachinery/pkg/api/meta"
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
				OnStatusPatch: testutil.FailOnObjectName(clusterName, errSimulated),
			},
			wantErrMsg: "failed to patch status",
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

	// Test Degraded Phase
	cDegraded := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cell-degraded",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
		Spec: multigresv1alpha1.CellSpec{Name: "cell-degraded"},
		Status: multigresv1alpha1.CellStatus{
			Phase: multigresv1alpha1.PhaseDegraded,
		},
	}

	fakeClient = fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, cell, tg, cDegraded).
		WithStatusSubresource(cluster, cell, tg, cDegraded).
		Build()

	r.Client = fakeClient
	if err := r.updateStatus(context.Background(), cluster); err != nil {
		t.Fatalf("updateStatus failed: %v", err)
	}

	if err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(cluster), cluster); err != nil {
		t.Fatalf("Failed to refresh cluster: %v", err)
	}

	if cluster.Status.Phase != multigresv1alpha1.PhaseDegraded {
		t.Errorf("Expected PhaseDegraded, got %s", cluster.Status.Phase)
	}

	// Test Progressing Phase (Default case)
	// We need a case where it's NOT Degraded AND NOT AllHealthy.
	// cDegraded is now removed or fixed.
	// Let's create a cell that is PhaseInitializing (or unknown)
	cProgressing := &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cell-prog",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
		Spec: multigresv1alpha1.CellSpec{Name: "cell-prog"},
		Status: multigresv1alpha1.CellStatus{
			Phase: multigresv1alpha1.PhaseInitializing,
		},
	}

	fakeClient = fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, cell, tg, cProgressing). // cell-1 is healthy, cell-prog is init
		WithStatusSubresource(cluster, cell, tg, cProgressing).
		Build()
	r.Client = fakeClient

	if err := r.updateStatus(context.Background(), cluster); err != nil {
		t.Fatalf("updateStatus failed: %v", err)
	}
	if err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(cluster), cluster); err != nil {
		t.Fatalf("Failed to refresh cluster: %v", err)
	}
	if cluster.Status.Phase != multigresv1alpha1.PhaseProgressing {
		t.Errorf("Expected PhaseProgressing, got %s", cluster.Status.Phase)
	}

	// Test Degraded Phase from TableGroup
	tgDegraded := &multigresv1alpha1.TableGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tg-degraded",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
		Spec: multigresv1alpha1.TableGroupSpec{
			DatabaseName:   "db1",
			TableGroupName: "tg-degraded",
		},
		Status: multigresv1alpha1.TableGroupStatus{
			Phase: multigresv1alpha1.PhaseDegraded,
		},
	}

	fakeClient = fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, cell, tg, tgDegraded).
		WithStatusSubresource(cluster, cell, tg, tgDegraded).
		Build()

	r.Client = fakeClient
	if err := r.updateStatus(context.Background(), cluster); err != nil {
		t.Fatalf("updateStatus failed: %v", err)
	}

	if err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(cluster), cluster); err != nil {
		t.Fatalf("Failed to refresh cluster: %v", err)
	}

	if cluster.Status.Phase != multigresv1alpha1.PhaseDegraded {
		t.Errorf("Expected PhaseDegraded, got %s", cluster.Status.Phase)
	}

	// Test Initializing/Progressing Phase from TableGroup (Default branch)
	tgInit := &multigresv1alpha1.TableGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tg-init",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
		Spec: multigresv1alpha1.TableGroupSpec{
			DatabaseName:   "db1",
			TableGroupName: "tg-init",
		},
		Status: multigresv1alpha1.TableGroupStatus{
			Phase: multigresv1alpha1.PhaseInitializing,
		},
	}

	fakeClient = fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, cell, tg, tgInit). // cell is healthy, tg is healthy, tgInit is init
		WithStatusSubresource(cluster, cell, tg, tgInit).
		Build()

	r.Client = fakeClient
	if err := r.updateStatus(context.Background(), cluster); err != nil {
		t.Fatalf("updateStatus failed: %v", err)
	}

	if err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(cluster), cluster); err != nil {
		t.Fatalf("Failed to refresh cluster: %v", err)
	}

	if cluster.Status.Phase != multigresv1alpha1.PhaseProgressing {
		t.Errorf("Expected PhaseProgressing, got %s", cluster.Status.Phase)
	}
}

func TestUpdateStatus_ZeroResources(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster). // No cells, no TGs
		WithStatusSubresource(cluster).
		Build()

	r := &MultigresClusterReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	if err := r.updateStatus(context.Background(), cluster); err != nil {
		t.Fatalf("updateStatus failed: %v", err)
	}

	if err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(cluster), cluster); err != nil {
		t.Fatalf("Failed to refresh cluster: %v", err)
	}

	// With 0 cells/TGs, logic:
	// anyDegraded = false
	// allHealthy = true (initial value) -> Loops skipped
	// Switch case allHealthy -> PhaseHealthy
	// But len(Cells) == 0 -> Available condition = False

	if cluster.Status.Phase != multigresv1alpha1.PhaseHealthy {
		t.Errorf("Expected PhaseHealthy (vacuously true), got %s", cluster.Status.Phase)
	}

	cond := meta.FindStatusCondition(cluster.Status.Conditions, "Available")
	if cond == nil {
		t.Fatal("Available condition missing")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("Expected Available=False (no cells), got %s", cond.Status)
	}
}

package multigrescluster

import (
	"errors"
	"strings"
	"testing"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcile_Cells(t *testing.T) {
	coreTpl, cellTpl, shardTpl, _, clusterName, namespace, _ := setupFixtures(t)
	errSimulated := errors.New("simulated error for testing")

	tests := map[string]reconcileTestCase{
		"Error: Explicit Cell Template Missing": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.Cells[0].CellTemplate = "missing-cell-tpl"
			},
			existingObjects: []client.Object{coreTpl, shardTpl},
			wantErrMsg:      "failed to resolve cell",
		},
		"Error: Resolve CellTemplate Failed": {
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName("default-cell", errSimulated),
			},
			wantErrMsg: "failed to resolve cell",
		},
		"Error: List Existing Cells Failed (Reconcile Loop)": {
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
			wantErrMsg: "failed to create cell",
		},
		"Error: Get Cell Failed (Unexpected Error)": {
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName(clusterName+"-zone-a", errSimulated),
			},
			wantErrMsg: "failed to get cell",
		},
		"Error: Update Cell Failed": {
			existingObjects: []client.Object{
				coreTpl, cellTpl, shardTpl,
				&multigresv1alpha1.Cell{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName + "-zone-a",
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName(clusterName+"-zone-a", errSimulated),
			},
			wantErrMsg: "failed to update cell",
		},
		"Error: Prune Cell Failed": {
			existingObjects: []client.Object{
				coreTpl, shardTpl,
				// Need the template to create valid cells
				&multigresv1alpha1.CellTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "default-cell", Namespace: namespace},
					Spec:       multigresv1alpha1.CellTemplateSpec{},
				},
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
		"Error: Global Topo Resolution Failed (During Cell Reconcile)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.TemplateDefaults.CoreTemplate = ""
				c.Spec.GlobalTopoServer = &multigresv1alpha1.GlobalTopoServerSpec{
					TemplateRef: "topo-fail-cells",
				}
				c.Spec.MultiAdmin = nil
			},
			existingObjects: []client.Object{
				// Need cell template
				&multigresv1alpha1.CellTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "default-cell", Namespace: namespace},
					Spec:       multigresv1alpha1.CellTemplateSpec{},
				},
				shardTpl,
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
							// Call 1: reconcileGlobalComponents -> ResolveCoreTemplate (Succeeds)
							// Call 2: reconcileCells -> getGlobalTopoRef -> ResolveCoreTemplate (Fails)
							if count == 2 {
								return errSimulated
							}
						}
						return nil
					}
				}(),
			},
			wantErrMsg: "failed to get global topo ref",
		},
		"Success: Cell Exists (Idempotency)": {
			existingObjects: []client.Object{
				coreTpl, cellTpl, shardTpl,
				&multigresv1alpha1.Cell{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName + "-zone-a",
						Namespace: namespace,
						Labels: map[string]string{
							"multigres.com/cluster": clusterName,
							"multigres.com/cell":    "zone-a",
						},
					},
					Spec: multigresv1alpha1.CellSpec{
						Name: "zone-a",
						Zone: "us-east-1a",
					},
				},
			},
		},

		"Success: Prune Orphan Cell": {
			existingObjects: []client.Object{
				coreTpl, cellTpl, shardTpl,
				&multigresv1alpha1.Cell{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName + "-orphan-zone",
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
					Spec: multigresv1alpha1.CellSpec{Name: "orphan-zone"},
				},
			},
		},
	}

	runReconcileTest(t, tests)
}

func TestReconcile_Cells_BuildFailure(t *testing.T) {
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
			// Need to disable global components to avoid them failing first (on cluster name if long? No, we use normal cluster name here)
			// But for Cells, we use Long Cell Name.
			GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
				External: &multigresv1alpha1.ExternalTopoServerSpec{
					Endpoints: []multigresv1alpha1.EndpointUrl{"http://ext:2379"},
				},
			},
			MultiAdmin: nil,
			Cells: []multigresv1alpha1.CellConfig{ // This will trigger Build failure due to long name
				{Name: strings.Repeat("a", 64), CellTemplate: "cell-tpl"},
			},
		},
	}

	tmpl := &multigresv1alpha1.CellTemplate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CellTemplate",
			APIVersion: multigresv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{Name: "cell-tpl", Namespace: "ns1"},
		Spec:       multigresv1alpha1.CellTemplateSpec{},
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
	if !contains(err.Error(), "failed to build cell") {
		// If MultiAdmin fails, we will see it here. But since we used normal cluster name and nil MultiAdmin (which likely defaults),
		// we might hit MultiAdmin build.
		// If MultiAdmin defaults, it builds.
		// Does MultiAdmin validation fail? No.
		// So MultiAdmin succeeds.
		// Then Cells run. Cell name is long. Validation fails.
		// WE SHOULD SEE "failed to build cell".
		t.Errorf("Unexpected error: %v", err)
	}
}

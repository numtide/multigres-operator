package multigrescluster

import (
	"errors"
	"strings"
	"testing"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/cluster-handler/names"
	"github.com/numtide/multigres-operator/pkg/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		"Error: Apply Cell Failed": {
			failureConfig: &testutil.FailureConfig{
				OnPatch: testutil.FailOnObjectName(
					names.JoinWithConstraints(names.DefaultConstraints, clusterName, "zone-a"),
					errSimulated,
				),
			},
			wantErrMsg: "failed to apply cell",
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
						Name: names.JoinWithConstraints(
							names.DefaultConstraints,
							clusterName,
							"zone-b",
						),
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnDelete: testutil.FailOnObjectName(
					names.JoinWithConstraints(names.DefaultConstraints, clusterName, "zone-b"),
					errSimulated,
				),
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
						Name: names.JoinWithConstraints(
							names.DefaultConstraints,
							clusterName,
							"zone-a",
						),
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
						Name: names.JoinWithConstraints(
							names.DefaultConstraints,
							clusterName,
							"orphan-zone",
						),
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
					Spec: multigresv1alpha1.CellSpec{Name: "orphan-zone"},
				},
			},
		},
		"Success: Build Cell (Name Too Long - Truncated)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				// Use normal cluster name, but put a long name in Cells config
				c.Spec.Cells = []multigresv1alpha1.CellConfig{
					{Name: strings.Repeat("a", 64), CellTemplate: "default-cell"},
				}
				// Ensure GlobalTopo doesn't fail
				c.Spec.GlobalTopoServer = &multigresv1alpha1.GlobalTopoServerSpec{
					External: &multigresv1alpha1.ExternalTopoServerSpec{
						Endpoints: []multigresv1alpha1.EndpointUrl{"http://ext:2379"},
					},
				}
				// Disable MultiAdmin to avoid distractions
				c.Spec.MultiAdmin = nil
			},
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			wantErrMsg:      "",
		},
	}

	runReconcileTest(t, tests)
}

package multigrescluster

import (
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

func TestReconcile_Global(t *testing.T) {
	coreTpl, cellTpl, shardTpl, _, clusterName, namespace, _ := setupFixtures(t)
	errSimulated := errors.New("simulated error for testing")

	tests := map[string]reconcileTestCase{
		"Create: Independent Templates (Topo vs Admin)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.TemplateDefaults.CoreTemplate = "" // clear default
				c.Spec.GlobalTopoServer = &multigresv1alpha1.GlobalTopoServerSpec{
					TemplateRef: "topo-core",
				}
				c.Spec.MultiAdmin = &multigresv1alpha1.MultiAdminConfig{
					TemplateRef: "admin-core",
				}
			},
			existingObjects: []client.Object{
				cellTpl, shardTpl,
				&multigresv1alpha1.CoreTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "topo-core", Namespace: namespace},
					Spec: multigresv1alpha1.CoreTemplateSpec{
						GlobalTopoServer: &multigresv1alpha1.TopoServerSpec{
							Etcd: &multigresv1alpha1.EtcdSpec{Image: "etcd:topo"},
						},
					},
				},
				&multigresv1alpha1.CoreTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "admin-core", Namespace: namespace},
					Spec: multigresv1alpha1.CoreTemplateSpec{
						MultiAdmin: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(5))},
					},
				},
			},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()

				ts := &multigresv1alpha1.TopoServer{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-global-topo", Namespace: namespace}, ts); err != nil {
					t.Fatal(err)
				}
				if got, want := ts.Spec.Etcd.Image, "etcd:topo"; got != want {
					t.Errorf("TopoServer image mismatch got %q, want %q", got, want)
				}

				deploy := &appsv1.Deployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-multiadmin", Namespace: namespace}, deploy); err != nil {
					t.Fatal(err)
				}
				if got, want := *deploy.Spec.Replicas, int32(5); got != want {
					t.Errorf("MultiAdmin replicas mismatch got %d, want %d", got, want)
				}
			},
		},

		"Create: Managed Etcd with RootPath": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.GlobalTopoServer = &multigresv1alpha1.GlobalTopoServerSpec{
					Etcd: &multigresv1alpha1.EtcdSpec{
						Image:    "etcd:test",
						RootPath: "/custom/root",
					},
				}
			},
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				ts := &multigresv1alpha1.TopoServer{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-global-topo", Namespace: namespace}, ts); err != nil {
					t.Fatal(err)
				}
				if got, want := ts.Spec.Etcd.RootPath, "/custom/root"; got != want {
					t.Errorf("RootPath mismatch got %q, want %q", got, want)
				}
			},
		},

		"Create: External Topo Integration": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.GlobalTopoServer = &multigresv1alpha1.GlobalTopoServerSpec{
					External: &multigresv1alpha1.ExternalTopoServerSpec{
						Endpoints: []multigresv1alpha1.EndpointUrl{"http://external-etcd:2379"},
					},
				}
			},
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			validate: func(t testing.TB, c client.Client) {
				ctx := t.Context()
				ts := &multigresv1alpha1.TopoServer{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-global-topo", Namespace: namespace}, ts); !apierrors.IsNotFound(
					err,
				) {
					t.Fatal("Global TopoServer should NOT be created for External mode")
				}
				cell := &multigresv1alpha1.Cell{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-zone-a", Namespace: namespace}, cell); err != nil {
					t.Fatal(err)
				}
				if got, want := cell.Spec.GlobalTopoServer.Address, "http://external-etcd:2379"; got != want {
					t.Errorf("External address mismatch got %q, want %q", got, want)
				}
			},
		},
		"Error: Explicit Core Template Missing (Should Fail)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.TemplateDefaults.CoreTemplate = "non-existent-template"
			},
			existingObjects: []client.Object{}, // No templates exist
			failureConfig:   nil,
			wantErrMsg:      "failed to resolve global topo",
		},
		"Error: Resolve CoreTemplate Failed": {
			existingObjects: []client.Object{coreTpl},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName("default-core", errSimulated),
			},
			wantErrMsg: "failed to resolve global topo",
		},
		"Error: Resolve Admin Template Failed (Second Call)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.TemplateDefaults.CoreTemplate = ""
				c.Spec.GlobalTopoServer = &multigresv1alpha1.GlobalTopoServerSpec{
					TemplateRef: "topo-core",
				}
				c.Spec.MultiAdmin = &multigresv1alpha1.MultiAdminConfig{
					TemplateRef: "admin-core-fail",
				}
			},
			existingObjects: []client.Object{
				cellTpl, shardTpl,
				&multigresv1alpha1.CoreTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "topo-core", Namespace: namespace},
					Spec:       multigresv1alpha1.CoreTemplateSpec{},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName("admin-core-fail", errSimulated),
			},
			wantErrMsg: "failed to resolve multiadmin",
		},
		"Error: Apply GlobalTopo Failed": {
			failureConfig: &testutil.FailureConfig{
				OnPatch: testutil.FailOnObjectName(clusterName+"-global-topo", errSimulated),
			},
			wantErrMsg: "failed to apply global topo server",
		},

		"Error: Apply MultiAdmin Failed": {
			failureConfig: &testutil.FailureConfig{
				OnPatch: testutil.FailOnObjectName(clusterName+"-multiadmin", errSimulated),
			},
			wantErrMsg: "failed to apply multiadmin deployment",
		},
		"Update: Global Components Success": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.GlobalTopoServer = &multigresv1alpha1.GlobalTopoServerSpec{
					Etcd: &multigresv1alpha1.EtcdSpec{Image: "new-etcd"},
				}
				c.Spec.MultiAdmin = &multigresv1alpha1.MultiAdminConfig{
					Spec: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(3))},
				}
			},
			existingObjects: []client.Object{
				coreTpl, cellTpl, shardTpl,
				&multigresv1alpha1.TopoServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName + "-global-topo",
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
					Spec: multigresv1alpha1.TopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{Image: "old-etcd"},
					},
				},
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName + "-multiadmin",
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(1)),
					},
				},
			},
			validate: func(t testing.TB, c client.Client) {
				ts := &multigresv1alpha1.TopoServer{}
				if err := c.Get(t.Context(), types.NamespacedName{Name: clusterName + "-global-topo", Namespace: namespace}, ts); err != nil {
					t.Fatal(err)
				}
				if ts.Spec.Etcd.Image != "new-etcd" {
					t.Errorf("TopoServer not updated")
				}
				deploy := &appsv1.Deployment{}
				if err := c.Get(t.Context(), types.NamespacedName{Name: clusterName + "-multiadmin", Namespace: namespace}, deploy); err != nil {
					t.Fatal(err)
				}
				if *deploy.Spec.Replicas != 3 {
					t.Errorf("MultiAdmin not updated")
				}
			},
		},
		"Error: Build GlobalTopo Failed (Name Too Long)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Name = strings.Repeat("a", 64)
			},
			existingObjects: []client.Object{coreTpl},
			wantErrMsg:      "failed to build global topo server",
		},
		"Error: Build MultiAdmin Failed (Name Too Long)": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Name = strings.Repeat("a", 64)
				// Ensure GlobalTopo doesn't fail first (set to External to skip build)
				c.Spec.GlobalTopoServer = &multigresv1alpha1.GlobalTopoServerSpec{
					External: &multigresv1alpha1.ExternalTopoServerSpec{
						Endpoints: []multigresv1alpha1.EndpointUrl{"http://ext:2379"},
					},
				}
			},
			existingObjects: []client.Object{coreTpl},
			wantErrMsg:      "failed to build multiadmin deployment",
		},
	}

	runReconcileTest(t, tests)
}

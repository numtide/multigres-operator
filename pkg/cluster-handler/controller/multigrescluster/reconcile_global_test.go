package multigrescluster

import (
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
		"Create: MultiAdmin with PodLabels": {
			preReconcileUpdate: func(t testing.TB, c *multigresv1alpha1.MultigresCluster) {
				c.Spec.MultiAdmin = &multigresv1alpha1.MultiAdminConfig{
					Spec: &multigresv1alpha1.StatelessSpec{
						PodLabels: map[string]string{"custom": "label"},
					},
				}
			},
			existingObjects: []client.Object{coreTpl, cellTpl, shardTpl},
			validate: func(t testing.TB, c client.Client) {
				deploy := &appsv1.Deployment{}
				if err := c.Get(t.Context(), types.NamespacedName{Name: clusterName + "-multiadmin", Namespace: namespace}, deploy); err != nil {
					t.Fatal(err)
				}
				if deploy.Spec.Template.Labels["custom"] != "label" {
					t.Error("PodLabels were not applied")
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
		"Error: Get GlobalTopo Failed (Unexpected Error)": {
			failureConfig: &testutil.FailureConfig{
				// Determine if Get fails for the TopoServer resource
				OnGet: testutil.FailOnKeyName(clusterName+"-global-topo", errSimulated),
			},
			wantErrMsg: "failed to get global topo server",
		},
		"Error: Create GlobalTopo Failed": {
			failureConfig: &testutil.FailureConfig{
				OnCreate: testutil.FailOnObjectName(clusterName+"-global-topo", errSimulated),
			},
			wantErrMsg: "failed to create global topo server",
		},
		"Error: Update GlobalTopo Failed": {
			// Pre-create the TopoServer so we hit the Update path
			existingObjects: []client.Object{
				coreTpl, cellTpl, shardTpl, // default templates
				&multigresv1alpha1.TopoServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName + "-global-topo",
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
					Spec: multigresv1alpha1.TopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{Image: "old-image"},
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName(clusterName+"-global-topo", errSimulated),
			},
			wantErrMsg: "failed to update global topo server",
		},
		"Error: Get MultiAdmin Failed (Unexpected Error)": {
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName(clusterName+"-multiadmin", errSimulated),
			},
			wantErrMsg: "failed to get multiadmin deployment",
		},
		"Error: Create MultiAdmin Failed": {
			failureConfig: &testutil.FailureConfig{
				OnCreate: testutil.FailOnObjectName(clusterName+"-multiadmin", errSimulated),
			},
			wantErrMsg: "failed to create multiadmin deployment",
		},
		"Error: Update MultiAdmin Failed": {
			// Pre-create the MultiAdmin Deployment so CreateOrUpdate goes to Update path
			existingObjects: []client.Object{
				coreTpl, cellTpl, shardTpl,
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName + "-multiadmin",
						Namespace: namespace,
						Labels:    map[string]string{"multigres.com/cluster": clusterName},
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName(clusterName+"-multiadmin", errSimulated),
			},
			wantErrMsg: "failed to update multiadmin deployment",
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
	}

	runReconcileTest(t, tests)
}

func TestReconcile_Global_BuildFailure(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	cluster := &multigresv1alpha1.MultigresCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MultigresCluster",
			APIVersion: multigresv1alpha1.GroupVersion.String(),
		},
		// Use a name > 63 characters to trigger validation error in BuildGlobalTopoServer
		ObjectMeta: metav1.ObjectMeta{
			Name:       strings.Repeat("a", 64),
			Namespace:  "ns1",
			Finalizers: []string{"multigres.com/finalizer"},
		},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
				TemplateRef: "topo-core",
			},
		},
	}

	tmpl := &multigresv1alpha1.CoreTemplate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CoreTemplate",
			APIVersion: multigresv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{Name: "topo-core", Namespace: "ns1"},
		Spec: multigresv1alpha1.CoreTemplateSpec{
			GlobalTopoServer: &multigresv1alpha1.TopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{Image: "img"},
			},
		},
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
	if !contains(err.Error(), "failed to build global topo server") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestReconcile_MultiAdmin_BuildFailure(t *testing.T) {
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
			Name:       strings.Repeat("a", 64),
			Namespace:  "ns1",
			Finalizers: []string{"multigres.com/finalizer"},
		},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
				External: &multigresv1alpha1.ExternalTopoServerSpec{
					Endpoints: []multigresv1alpha1.EndpointUrl{"http://ext:2379"},
				},
			},
			// MultiAdmin enabled by default (implicit)
		},
	}

	// Need CoreTemplate to resolve MultiAdmin (even if defaults used, resolver might fetch it)
	// But mostly defaults.

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
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
	if !contains(err.Error(), "failed to build multiadmin deployment") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[0:len(substr)] == substr
}

package multigrescluster

import (
	"context"
	"errors"
	"testing"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resolver"
	"github.com/numtide/multigres-operator/pkg/testutil"
	"github.com/numtide/multigres-operator/pkg/util/name"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestReconcileGlobal_ErrorPaths(t *testing.T) {
	scheme := setupScheme()

	t.Run("Error: Resolve Global Topo Failed", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
					TemplateRef: "non-existent-core",
				},
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}

		err := r.reconcileGlobalTopoServer(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil {
			t.Error("Expected error due to missing global topo spec, got nil")
		}
	})

	t.Run("Error: Resolve MultiAdmin Failed", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				MultiAdmin: &multigresv1alpha1.MultiAdminConfig{
					TemplateRef: "non-existent-core",
				},
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}

		err := r.reconcileMultiAdmin(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil {
			t.Error("Expected error due to missing multi admin spec, got nil")
		}
	})

	t.Run("Error: Resolve MultiAdminWeb Failed", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				MultiAdminWeb: &multigresv1alpha1.MultiAdminWebConfig{
					TemplateRef: "non-existent-core",
				},
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}

		err := r.reconcileMultiAdminWeb(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil {
			t.Error("Expected error due to missing multi admin web spec, got nil")
		}
	})

	t.Run("Error: Patch Global Topo Failed", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
					Etcd: &multigresv1alpha1.EtcdSpec{Image: "etcd"},
				},
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, cli client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				return errors.New("patch error")
			},
		}).WithObjects(cluster).Build()

		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}
		err := r.reconcileGlobalTopoServer(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil || err.Error() != "failed to apply global topo server: patch error" {
			t.Errorf("Expected 'patch error', got %v", err)
		}
	})

	t.Run("Error: Patch MultiAdmin Failed", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			// MultiAdmin is created by default unless disabled, so it should try to patch.
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, cli client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				return errors.New("patch error")
			},
		}).WithObjects(cluster).Build()

		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}
		err := r.reconcileMultiAdmin(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil || err.Error() != "failed to apply multiadmin deployment: patch error" {
			t.Errorf("Expected 'patch error', got %v", err)
		}
	})

	t.Run("Error: Patch MultiAdminWeb Failed", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			// MultiAdminWeb also defaults to enabled if image is present (which defaults handles)
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, cli client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				return errors.New("patch error")
			},
		}).WithObjects(cluster).Build()

		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}
		err := r.reconcileMultiAdminWeb(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil || err.Error() != "failed to apply multiadmin-web deployment: patch error" {
			t.Errorf("Expected 'patch error', got %v", err)
		}
	})

	t.Run("Error: Build Global Topo Failed", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
					Etcd: &multigresv1alpha1.EtcdSpec{Image: "etcd"},
				},
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
		// Use empty scheme for Reconciler
		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   runtime.NewScheme(),
			Recorder: record.NewFakeRecorder(10),
		}

		err := r.reconcileGlobalTopoServer(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil {
			t.Error("Expected error due to build failure, got nil")
		}
	})

	t.Run("Error: Build MultiAdmin Failed", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			// MultiAdmin default enabled
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
		// Use empty scheme for Reconciler
		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   runtime.NewScheme(),
			Recorder: record.NewFakeRecorder(10),
		}

		err := r.reconcileMultiAdmin(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil {
			t.Error("Expected error due to build failure, got nil")
		}
	})

	t.Run("Error: Build MultiAdminWeb Failed", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			// MultiAdminWeb default enabled
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
		// Use empty scheme for Reconciler
		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   runtime.NewScheme(),
			Recorder: record.NewFakeRecorder(10),
		}

		err := r.reconcileMultiAdminWeb(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil {
			t.Error("Expected error due to build failure, got nil")
		}
	})

	t.Run("Error: Build MultiAdmin Service Failed", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		}

		s := runtime.NewScheme()
		_ = appsv1.AddToScheme(s)
		_ = multigresv1alpha1.AddToScheme(s) // Required for SetControllerReference

		// Use limited scheme for Client so Patch fails or Build fails if it uses client scheme?
		// Build uses r.Scheme.
		// Patch uses r.Client which uses client scheme.
		// If we want Patch to fail due to missing kind, client scheme must miss it.
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(cluster).Build()

		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   s,
			Recorder: record.NewFakeRecorder(10),
		}

		err := r.reconcileMultiAdmin(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil {
			t.Error("Expected error due to build failure, got nil")
		}
	})

	t.Run("Error: Patch MultiAdmin Service Failed", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, cli client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if _, ok := obj.(*corev1.Service); ok {
					return errors.New("service patch error")
				}
				return cli.Patch(ctx, obj, patch, opts...)
			},
		}).WithObjects(cluster).Build()

		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}

		err := r.reconcileMultiAdmin(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil || err.Error() != "failed to apply multiadmin service: service patch error" {
			t.Errorf("Expected 'service patch error', got %v", err)
		}
	})

	t.Run("Error: Build MultiAdminWeb Service Failed", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		}

		s := runtime.NewScheme()
		_ = appsv1.AddToScheme(s)
		_ = multigresv1alpha1.AddToScheme(s)

		c := fake.NewClientBuilder().WithScheme(s).WithObjects(cluster).Build()
		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   s,
			Recorder: record.NewFakeRecorder(10),
		}

		err := r.reconcileMultiAdminWeb(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil {
			t.Error("Expected error due to build failure, got nil")
		}
	})

	t.Run("Error: Patch MultiAdminWeb Service Failed", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, cli client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if _, ok := obj.(*corev1.Service); ok {
					return errors.New("service patch error")
				}
				return cli.Patch(ctx, obj, patch, opts...)
			},
		}).WithObjects(cluster).Build()

		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}

		err := r.reconcileMultiAdminWeb(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil ||
			err.Error() != "failed to apply multiadmin-web service: service patch error" {
			t.Errorf("Expected 'service patch error', got %v", err)
		}
	})
}

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
				if got, want := ts.Spec.Etcd.Image, multigresv1alpha1.ImageRef("etcd:topo"); got != want {
					t.Errorf("TopoServer image mismatch got %q, want %q", got, want)
				}

				deploy := &appsv1.Deployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-multiadmin", Namespace: namespace}, deploy); err != nil {
					t.Fatal(err)
				}
				if got, want := *deploy.Spec.Replicas, int32(5); got != want {
					t.Errorf("MultiAdmin replicas mismatch got %d, want %d", got, want)
				}

				webDeploy := &appsv1.Deployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName + "-multiadmin-web", Namespace: namespace}, webDeploy); err != nil {
					t.Fatal(err)
				}
				// Default replicas is 1
				if got, want := *webDeploy.Spec.Replicas, int32(1); got != want {
					t.Errorf("MultiAdminWeb replicas mismatch got %d, want %d", got, want)
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
				// Use hashed name for Cell
				cellName := name.JoinWithConstraints(
					name.DefaultConstraints,
					clusterName,
					"zone-a",
				)
				if err := c.Get(ctx, types.NamespacedName{Name: cellName, Namespace: namespace}, cell); err != nil {
					t.Fatalf("Expected Cell %s to exist: %v", cellName, err)
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
				c.Spec.MultiAdminWeb = &multigresv1alpha1.MultiAdminWebConfig{
					Spec: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(2))},
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

				webDeploy := &appsv1.Deployment{}
				if err := c.Get(t.Context(), types.NamespacedName{Name: clusterName + "-multiadmin-web", Namespace: namespace}, webDeploy); err != nil {
					t.Fatal(err)
				}
				if *webDeploy.Spec.Replicas != 2 {
					t.Errorf("MultiAdminWeb not updated")
				}
			},
		},
	}

	runReconcileTest(t, tests)
}

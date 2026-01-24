package multigrescluster

import (
	"context"
	"errors"
	"testing"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resolver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestReconcileCells_ErrorPaths(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	t.Run("Error: Get Global Topo Ref Failed", func(t *testing.T) {
		// Use explicit invalid core template to force GetGlobalTopoRef to fail
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
					TemplateRef: "non-existent-core",
				},
				Cells: []multigresv1alpha1.CellConfig{{Name: "zone-a", Zone: "us-east-1a"}},
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}

		err := r.reconcileCells(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil {
			t.Error("Expected error due to missing global topo, got nil")
		}
	})

	t.Run("Error: Resolve Cell Failed", func(t *testing.T) {
		// Use explicit invalid cell template
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
					Etcd: &multigresv1alpha1.EtcdSpec{Image: "etcd"},
				},
				Cells: []multigresv1alpha1.CellConfig{{
					Name:         "zone-a",
					Zone:         "us-east-1a",
					CellTemplate: "non-existent-cell",
				}},
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}

		err := r.reconcileCells(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil {
			t.Error("Expected error due to missing cell template, got nil")
		}
	})

	t.Run("Error: List Existing Cells Failed", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, cli client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				return errors.New("list error")
			},
		}).WithObjects(cluster).Build()

		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}
		err := r.reconcileCells(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil || err.Error() != "failed to list existing cells: list error" {
			t.Errorf("Expected 'list error', got %v", err)
		}
	})

	t.Run("Error: Patch Cell Failed", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
					Etcd: &multigresv1alpha1.EtcdSpec{Image: "etcd"},
				},
				Cells: []multigresv1alpha1.CellConfig{
					{Name: "zone-a", Zone: "us-east-1a"},
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
		err := r.reconcileCells(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil || err.Error() != "failed to apply cell 'zone-a': patch error" {
			t.Errorf("Expected 'patch error', got %v", err)
		}
	})

	t.Run("Error: Delete Orphaned Cell Failed", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			// No cells in spec -> existing cell should be deleted
			Spec: multigresv1alpha1.MultigresClusterSpec{
				GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
					Etcd: &multigresv1alpha1.EtcdSpec{Image: "etcd"},
				},
			},
		}
		existingCell := &multigresv1alpha1.Cell{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-zone-orphan",
				Namespace: "default",
				Labels:    map[string]string{"multigres.com/cluster": "test"},
			},
			Spec: multigresv1alpha1.CellSpec{Name: "zone-orphan"},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(ctx context.Context, cli client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				return errors.New("delete error")
			},
		}).WithObjects(cluster, existingCell).Build()

		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}
		err := r.reconcileCells(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		// Note: The loop might continue or return error immediately depending on implementation.
		// Current implementation returns error immediately on delete failure.
		if err == nil ||
			err.Error() != "failed to delete orphaned cell 'test-zone-orphan': delete error" {
			t.Errorf("Expected 'delete error', got %v", err)
		}
	})

	t.Run("Error: Build Cell Failed", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
					Etcd: &multigresv1alpha1.EtcdSpec{Image: "etcd"},
				},
				Cells: []multigresv1alpha1.CellConfig{
					{Name: "zone-a", Zone: "us-east-1a"},
				},
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
		// Use empty scheme for Reconciler to cause SetControllerReference to fail
		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   runtime.NewScheme(),
			Recorder: record.NewFakeRecorder(10),
		}

		err := r.reconcileCells(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err == nil {
			t.Error("Expected error due to build failure (scheme mismatch), got nil")
		}
	})
}

func TestReconcileCells_HappyPath(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	t.Run("Happy Path: Create and Prune Cells", func(t *testing.T) {
		cluster := &multigresv1alpha1.MultigresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: multigresv1alpha1.MultigresClusterSpec{
				GlobalTopoServer: &multigresv1alpha1.GlobalTopoServerSpec{
					Etcd: &multigresv1alpha1.EtcdSpec{Image: "etcd"},
				},
				Cells: []multigresv1alpha1.CellConfig{
					{Name: "zone-new", Zone: "us-east-1a"},
				},
			},
		}
		// Existing cell that should be pruned
		orphan := &multigresv1alpha1.Cell{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-zone-old",
				Namespace: "default",
				Labels:    map[string]string{"multigres.com/cluster": "test"},
			},
			Spec: multigresv1alpha1.CellSpec{Name: "zone-old"},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, orphan).Build()
		r := &MultigresClusterReconciler{
			Client:   c,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}

		err := r.reconcileCells(
			context.Background(),
			cluster,
			resolver.NewResolver(c, "default", multigresv1alpha1.TemplateDefaults{}),
		)
		if err != nil {
			t.Fatalf("Expected happy path success, got %v", err)
		}

		// Verify "zone-new" created
		// Name is hashed but we can list by label
		cells := &multigresv1alpha1.CellList{}
		if err := c.List(context.Background(), cells); err != nil {
			t.Fatal(err)
		}
		foundNew := false
		foundOld := false
		for _, cell := range cells.Items {
			if cell.Spec.Name == "zone-new" {
				foundNew = true
			}
			if cell.Spec.Name == "zone-old" {
				foundOld = true
			}
		}
		if !foundNew {
			t.Error("Expected new cell 'zone-new' to be created")
		}
		if foundOld {
			t.Error("Expected old cell 'zone-old' to be deleted")
		}
	})
}

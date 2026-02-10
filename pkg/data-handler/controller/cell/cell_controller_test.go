package cell_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/multigres/multigres/go/common/topoclient"
	"github.com/multigres/multigres/go/common/topoclient/memorytopo"
	"github.com/multigres/multigres/go/pb/clustermetadata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"k8s.io/client-go/tools/record"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/data-handler/controller/cell"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

const (
	finalizerName = "cell.data-handler.multigres.com/finalizer"
)

func TestReconcile(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	tests := map[string]struct {
		cell            *multigresv1alpha1.Cell
		existingObjects []client.Object
		skipAddCell     bool // Don't add tc.cell to fake client (for testing NotFound)
		failureConfig   *testutil.FailureConfig
		topoSetup       func(t *testing.T, store topoclient.Store)
		wantErr         bool
		assertFunc      func(t *testing.T, c client.Client, cellObj *multigresv1alpha1.Cell, store topoclient.Store)
	}{
		"adds finalizer on first reconcile": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-a",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "memory",
					},
					TopologyReconciliation: multigresv1alpha1.TopologyReconciliation{
						RegisterCell: true,
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, cellObj *multigresv1alpha1.Cell, store topoclient.Store) {
				updatedCell := &multigresv1alpha1.Cell{}
				if err := c.Get(context.Background(), types.NamespacedName{
					Name:      cellObj.Name,
					Namespace: cellObj.Namespace,
				}, updatedCell); err != nil {
					t.Fatalf("Failed to get Cell: %v", err)
				}
				if !slices.Contains(updatedCell.Finalizers, finalizerName) {
					t.Error("Finalizer should be added")
				}
			},
		},
		"registers cell in topology": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cell",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-a",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "memory",
					},
					TopologyReconciliation: multigresv1alpha1.TopologyReconciliation{
						RegisterCell: true,
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, cellObj *multigresv1alpha1.Cell, store topoclient.Store) {
				gotCell, err := store.GetCell(context.Background(), string(cellObj.Spec.Name))
				if err != nil {
					t.Errorf("Cell should be in topology: %v", err)
					return
				}
				wantCell := &clustermetadata.Cell{
					Name:            "zone-a",
					ServerAddresses: []string{"localhost:2379"},
					Root:            "/test",
				}
				opts := cmpopts.IgnoreUnexported(clustermetadata.Cell{})
				if diff := cmp.Diff(wantCell, gotCell, opts); diff != "" {
					t.Errorf("Cell in topology mismatch (-want +got):\n%s", diff)
				}
			},
		},
		"skips registration when disabled": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cell",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-a",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "memory",
					},
					TopologyReconciliation: multigresv1alpha1.TopologyReconciliation{
						RegisterCell: false,
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, cellObj *multigresv1alpha1.Cell, store topoclient.Store) {
				_, err := store.GetCell(context.Background(), string(cellObj.Spec.Name))
				if err == nil {
					t.Error("Cell should not be in topology when registration disabled")
				}
			},
		},
		"idempotent - cell already exists": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cell",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-existing",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "memory",
					},
					TopologyReconciliation: multigresv1alpha1.TopologyReconciliation{
						RegisterCell: true,
					},
				},
			},
			topoSetup: func(t *testing.T, store topoclient.Store) {
				if err := store.CreateCell(context.Background(), "zone-existing", &clustermetadata.Cell{
					Name: "zone-existing",
				}); err != nil {
					t.Fatalf("Failed to setup topology: %v", err)
				}
			},
			assertFunc: func(t *testing.T, c client.Client, cellObj *multigresv1alpha1.Cell, store topoclient.Store) {
				_, err := store.GetCell(context.Background(), string(cellObj.Spec.Name))
				if err != nil {
					t.Errorf("Cell should still be in topology: %v", err)
				}
			},
		},
		"handles deletion with finalizer": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-cell",
					Namespace:         "default",
					Finalizers:        []string{finalizerName},
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-delete",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "memory",
					},
				},
			},
			topoSetup: func(t *testing.T, store topoclient.Store) {
				if err := store.CreateCell(context.Background(), "zone-delete", &clustermetadata.Cell{
					Name: "zone-delete",
				}); err != nil {
					t.Fatalf("Failed to setup topology: %v", err)
				}
			},
			assertFunc: func(t *testing.T, c client.Client, cellObj *multigresv1alpha1.Cell, store topoclient.Store) {
				// Verify cell removed from topology
				_, err := store.GetCell(context.Background(), string(cellObj.Spec.Name))
				if err == nil {
					t.Error("Cell should be removed from topology")
				}

				// Fake client removes objects with DeletionTimestamp and no finalizers
				// So we can't verify the finalizer was removed by Get
				// The test just ensures no error occurred
			},
		},
		"handles deletion when cell not in topology": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-cell",
					Namespace:         "default",
					Finalizers:        []string{finalizerName},
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-nonexistent",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "memory",
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, cellObj *multigresv1alpha1.Cell, store topoclient.Store) {
				// Fake client removes objects with DeletionTimestamp and no finalizers
				// The test just ensures no error occurred and cell not in topology
				_, err := store.GetCell(context.Background(), string(cellObj.Spec.Name))
				if err == nil {
					t.Error("Cell should not be in topology")
				}
			},
		},
		"handles deletion with other finalizers": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-cell",
					Namespace:         "default",
					Finalizers:        []string{"other-finalizer"},
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-a",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "memory",
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, cellObj *multigresv1alpha1.Cell, store topoclient.Store) {
				updatedCell := &multigresv1alpha1.Cell{}
				if err := c.Get(context.Background(), types.NamespacedName{
					Name:      cellObj.Name,
					Namespace: cellObj.Namespace,
				}, updatedCell); err != nil {
					t.Fatalf("Failed to get Cell: %v", err)
				}
				if slices.Contains(updatedCell.Finalizers, finalizerName) {
					t.Error("Our finalizer should not be present")
				}
			},
		},
		"handles non-existent cell": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell",
					Namespace: "default",
				},
			},
			skipAddCell: true, // Don't add to fake client to test NotFound path
		},
		"error on Get cell": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell",
					Namespace: "default",
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName("test-cell", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error on Update when adding finalizer": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-a",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "memory",
					},
					TopologyReconciliation: multigresv1alpha1.TopologyReconciliation{
						RegisterCell: true,
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-cell", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error registering cell in topology": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cell",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-a",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "invalid-impl",
					},
					TopologyReconciliation: multigresv1alpha1.TopologyReconciliation{
						RegisterCell: true,
					},
				},
			},
			wantErr: true,
		},
		"error unregistering cell during deletion": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-cell",
					Namespace:         "default",
					Finalizers:        []string{finalizerName},
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-a",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "invalid-impl",
					},
				},
			},
			wantErr: true,
		},
		"error on Update when removing finalizer": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-cell",
					Namespace:         "default",
					Finalizers:        []string{finalizerName},
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-delete",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "memory",
					},
				},
			},
			topoSetup: func(t *testing.T, store topoclient.Store) {
				if err := store.CreateCell(context.Background(), "zone-delete", &clustermetadata.Cell{
					Name: "zone-delete",
				}); err != nil {
					t.Fatalf("Failed to setup topology: %v", err)
				}
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-cell", testutil.ErrInjected),
			},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store, factory := memorytopo.NewServerAndFactory(context.Background())
			// TODO: handle store.Close() error properly
			defer func() { _ = store.Close() }()

			if tc.topoSetup != nil {
				tc.topoSetup(t, store)
			}

			var objs []client.Object
			if tc.existingObjects != nil {
				objs = tc.existingObjects
			} else if !tc.skipAddCell {
				objs = append(objs, tc.cell)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				WithStatusSubresource(&multigresv1alpha1.Cell{}).
				Build()

			c := client.Client(fakeClient)
			if tc.failureConfig != nil {
				c = testutil.NewFakeClientWithFailures(fakeClient, tc.failureConfig)
			}

			reconciler := &cell.CellReconciler{
				Client:   c,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
			}

			// Override createTopoStore for valid implementations
			if tc.cell.Spec.GlobalTopoServer.Implementation != "invalid-impl" {
				reconciler.SetCreateTopoStore(
					func(cellObj *multigresv1alpha1.Cell) (topoclient.Store, error) {
						return topoclient.NewWithFactory(
							factory,
							cellObj.Spec.GlobalTopoServer.RootPath,
							nil,
							nil,
						), nil
					},
				)
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tc.cell.Name,
					Namespace: tc.cell.Namespace,
				},
			}

			result, err := reconciler.Reconcile(context.Background(), req)

			if (err != nil) != tc.wantErr {
				t.Errorf("Reconcile() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if tc.wantErr {
				return
			}

			// Verify no requeue after is set (controller-runtime watch handles re-reconciliation)
			if result.RequeueAfter > 0 {
				t.Errorf(
					"Reconcile() should not set RequeueAfter, controller-runtime watch will trigger re-reconciliation, got %v",
					result.RequeueAfter,
				)
			}

			if tc.assertFunc != nil {
				tc.assertFunc(t, c, tc.cell, store)
			}
		})
	}
}

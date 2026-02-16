package cell

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/multigres/multigres/go/common/topoclient"
	"github.com/multigres/multigres/go/common/topoclient/memorytopo"
	"github.com/multigres/multigres/go/pb/clustermetadata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"k8s.io/client-go/tools/record"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

// TestRegisterCellInTopology tests the internal registerCellInTopology function
// which cannot be fully tested from the public API due to mock store requirements.
func TestRegisterCellInTopology(t *testing.T) {
	tests := map[string]struct {
		cell              *multigresv1alpha1.Cell
		topoSetup         func(t *testing.T, store topoclient.Store)
		mockCreateFunc    func(*multigresv1alpha1.Cell) (topoclient.Store, error)
		useDefaultCreate  bool // Use defaultCreateTopoStore instead of mocking (for error testing)
		wantCell          *clustermetadata.Cell
		wantErr           bool
		wantErrContaining string
	}{
		"creates cell in empty topology": {
			cell: &multigresv1alpha1.Cell{
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-a",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "memory",
					},
				},
			},
			wantCell: &clustermetadata.Cell{
				Name:            "zone-a",
				ServerAddresses: []string{"localhost:2379"},
				Root:            "/test",
			},
			wantErr: false,
		},
		"idempotent - cell already exists": {
			cell: &multigresv1alpha1.Cell{
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-existing",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "memory",
					},
				},
			},
			topoSetup: func(t *testing.T, store topoclient.Store) {
				if err := store.CreateCell(context.Background(), "zone-existing", &clustermetadata.Cell{
					Name:            "zone-existing",
					ServerAddresses: []string{"localhost:2379"},
					Root:            "/test",
				}); err != nil {
					t.Fatalf("Failed to setup topology: %v", err)
				}
			},
			wantCell: &clustermetadata.Cell{
				Name:            "zone-existing",
				ServerAddresses: []string{"localhost:2379"},
				Root:            "/test",
			},
			wantErr: false,
		},
		"error creating topo store": {
			cell: &multigresv1alpha1.Cell{
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-a",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "invalid-impl",
					},
				},
			},
			useDefaultCreate: true, // Use default to trigger real error from invalid implementation
			wantErr:          true,
		},
		"error on CreateCell with non-NodeExists error": {
			cell: &multigresv1alpha1.Cell{
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-timeout",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "memory",
					},
				},
			},
			mockCreateFunc: func(c *multigresv1alpha1.Cell) (topoclient.Store, error) {
				// Use a real memory store as the base, but override CreateCell
				baseStore, factory := memorytopo.NewServerAndFactory(context.Background())
				realStore := topoclient.NewWithFactory(factory, "/test-timeout", nil, nil)
				return &mockTopoStoreWrapper{
					Store: realStore,
					createCellFunc: func(ctx context.Context, cellName string, cell *clustermetadata.Cell) error {
						// Clean up the real store when test completes
						// TODO: handle baseStore.Close() error properly
						defer func() { _ = baseStore.Close() }()
						// Return a non-NodeExists error to trigger line 145
						return topoclient.TopoError{
							Code:    topoclient.Timeout,
							Message: "timeout creating cell",
						}
					},
				}, nil
			},
			wantErr:           true,
			wantErrContaining: "failed to create cell in topology",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			store, factory := memorytopo.NewServerAndFactory(context.Background())
			// TODO: handle store.Close() error properly
			defer func() { _ = store.Close() }()

			if tc.topoSetup != nil {
				tc.topoSetup(t, store)
			}

			reconciler := &CellReconciler{
				Recorder: record.NewFakeRecorder(10),
			}

			// Setup topology store creation based on test needs
			if tc.mockCreateFunc != nil {
				// Use custom mock (for testing specific error conditions)
				reconciler.createTopoStore = tc.mockCreateFunc
			} else if !tc.useDefaultCreate {
				// Use memory topo (for happy path testing)
				reconciler.createTopoStore = func(cellObj *multigresv1alpha1.Cell) (topoclient.Store, error) {
					return topoclient.NewWithFactory(factory, cellObj.Spec.GlobalTopoServer.RootPath, nil, nil), nil
				}
			}
			// If useDefaultCreate is true, leave createTopoStore nil to use defaultCreateTopoStore

			err := reconciler.registerCellInTopology(context.Background(), tc.cell)

			if (err != nil) != tc.wantErr {
				t.Errorf("registerCellInTopology() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if tc.wantErr {
				if tc.wantErrContaining != "" && err != nil {
					if !strings.Contains(err.Error(), tc.wantErrContaining) {
						t.Errorf(
							"registerCellInTopology() error = %v, want error containing %q",
							err,
							tc.wantErrContaining,
						)
					}
				}
				return
			}

			got, err := store.GetCell(context.Background(), string(tc.cell.Spec.Name))
			if err != nil {
				t.Fatalf("Failed to get cell from topology: %v", err)
			}

			opts := cmpopts.IgnoreUnexported(clustermetadata.Cell{})
			if diff := cmp.Diff(tc.wantCell, got, opts); diff != "" {
				t.Errorf("Cell in topology mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestUnregisterCellFromTopology tests the internal unregisterCellFromTopology function
// which cannot be fully tested from the public API due to mock store requirements.
func TestUnregisterCellFromTopology(t *testing.T) {
	tests := map[string]struct {
		cell              *multigresv1alpha1.Cell
		topoSetup         func(t *testing.T, store topoclient.Store)
		mockCreateFunc    func(*multigresv1alpha1.Cell) (topoclient.Store, error)
		useDefaultCreate  bool // Use defaultCreateTopoStore instead of mocking (for error testing)
		wantInTopo        bool
		wantErr           bool
		wantErrContaining string
	}{
		"removes existing cell": {
			cell: &multigresv1alpha1.Cell{
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
			wantInTopo: false,
			wantErr:    false,
		},
		"idempotent - cell does not exist": {
			cell: &multigresv1alpha1.Cell{
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-nonexistent",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "memory",
					},
				},
			},
			wantInTopo: false,
			wantErr:    false,
		},
		"error creating topo store": {
			cell: &multigresv1alpha1.Cell{
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-a",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "invalid-impl",
					},
				},
			},
			useDefaultCreate: true, // Use default to trigger real error from invalid implementation
			wantErr:          true,
		},
		"error on DeleteCell with non-NoNode error": {
			cell: &multigresv1alpha1.Cell{
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone-not-empty",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "memory",
					},
				},
			},
			mockCreateFunc: func(c *multigresv1alpha1.Cell) (topoclient.Store, error) {
				// Use a real memory store as the base, but override DeleteCell
				baseStore, factory := memorytopo.NewServerAndFactory(context.Background())
				realStore := topoclient.NewWithFactory(factory, "/test-delete", nil, nil)
				return &mockTopoStoreWrapper{
					Store: realStore,
					deleteCellFunc: func(ctx context.Context, cellName string, force bool) error {
						// Clean up the real store when test completes
						// TODO: handle baseStore.Close() error properly
						defer func() { _ = baseStore.Close() }()
						// Return a non-NoNode error to trigger line 175
						return topoclient.TopoError{
							Code:    topoclient.NodeNotEmpty,
							Message: "cell has child nodes",
						}
					},
				}, nil
			},
			wantErr:           true,
			wantErrContaining: "failed to delete cell from topology",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			store, factory := memorytopo.NewServerAndFactory(context.Background())
			// TODO: handle store.Close() error properly
			defer func() { _ = store.Close() }()

			if tc.topoSetup != nil {
				tc.topoSetup(t, store)
			}

			reconciler := &CellReconciler{
				Recorder: record.NewFakeRecorder(10),
			}

			// Setup topology store creation based on test needs
			if tc.mockCreateFunc != nil {
				// Use custom mock (for testing specific error conditions)
				reconciler.createTopoStore = tc.mockCreateFunc
			} else if !tc.useDefaultCreate {
				// Use memory topo (for happy path testing)
				reconciler.createTopoStore = func(cellObj *multigresv1alpha1.Cell) (topoclient.Store, error) {
					return topoclient.NewWithFactory(factory, cellObj.Spec.GlobalTopoServer.RootPath, nil, nil), nil
				}
			}
			// If useDefaultCreate is true, leave createTopoStore nil to use defaultCreateTopoStore

			err := reconciler.unregisterCellFromTopology(context.Background(), tc.cell)

			if (err != nil) != tc.wantErr {
				t.Errorf("unregisterCellFromTopology() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if tc.wantErr {
				if tc.wantErrContaining != "" && err != nil {
					if !strings.Contains(err.Error(), tc.wantErrContaining) {
						t.Errorf(
							"unregisterCellFromTopology() error = %v, want error containing %q",
							err,
							tc.wantErrContaining,
						)
					}
				}
				return
			}

			_, err = store.GetCell(context.Background(), string(tc.cell.Spec.Name))
			cellExists := (err == nil)

			if cellExists != tc.wantInTopo {
				t.Errorf("Cell in topology = %v, want %v", cellExists, tc.wantInTopo)
			}
		})
	}
}

// TestHandleDeletion tests the internal handleDeletion function
// which is difficult to test completely from the public API.
func TestHandleDeletion(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	tests := map[string]struct {
		cell             *multigresv1alpha1.Cell
		topoSetup        func(t *testing.T, store topoclient.Store)
		failureConfig    *testutil.FailureConfig
		useDefaultCreate bool // Use defaultCreateTopoStore instead of mocking (for error testing)
		wantErr          bool
	}{
		"no finalizer - early return": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cell",
					Namespace:  "default",
					Finalizers: []string{},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "memory",
					},
				},
			},
			wantErr: false,
		},
		"unregister error": {
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
				},
			},
			useDefaultCreate: true, // Use default to trigger real error from invalid implementation
			wantErr:          true,
		},
		"update error when removing finalizer": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cell",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
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
		"success - removes cell and finalizer": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cell",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
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
			wantErr: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			store, factory := memorytopo.NewServerAndFactory(context.Background())
			// TODO: handle store.Close() error properly
			defer func() { _ = store.Close() }()

			if tc.topoSetup != nil {
				tc.topoSetup(t, store)
			}

			baseClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.cell).
				Build()

			c := client.Client(baseClient)
			if tc.failureConfig != nil {
				c = testutil.NewFakeClientWithFailures(baseClient, tc.failureConfig)
			}

			reconciler := &CellReconciler{
				Client:   c,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
			}

			// Setup topology store creation based on test needs
			if !tc.useDefaultCreate {
				// Use memory topo (for happy path testing)
				reconciler.createTopoStore = func(cellObj *multigresv1alpha1.Cell) (topoclient.Store, error) {
					return topoclient.NewWithFactory(
						factory,
						cellObj.Spec.GlobalTopoServer.RootPath,
						nil,
						nil,
					), nil
				}
			}
			// If useDefaultCreate is true, leave createTopoStore nil to use defaultCreateTopoStore

			_, err := reconciler.handleDeletion(context.Background(), tc.cell)

			if (err != nil) != tc.wantErr {
				t.Errorf("handleDeletion() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestDefaultCreateTopoStore tests the default topology store creation function.
// This test covers code paths that cannot be tested with live etcd servers.
func TestDefaultCreateTopoStore(t *testing.T) {
	tests := map[string]struct {
		cell    *multigresv1alpha1.Cell
		wantErr bool
	}{
		"defaults to etcd when implementation empty": {
			cell: &multigresv1alpha1.Cell{
				Spec: multigresv1alpha1.CellSpec{
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "localhost:2379",
						RootPath:       "/test",
						Implementation: "",
					},
				},
			},
			wantErr: true,
		},
		"explicit etcd implementation": {
			cell: &multigresv1alpha1.Cell{
				Spec: multigresv1alpha1.CellSpec{
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "nonexistent:2379",
						RootPath:       "/test",
						Implementation: "etcd",
					},
				},
			},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			store, err := defaultCreateTopoStore(tc.cell)

			if (err != nil) != tc.wantErr {
				t.Errorf("defaultCreateTopoStore() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if err == nil {
				// TODO: handle store.Close() error properly
				defer func() { _ = store.Close() }()
			}
		})
	}
}

// TestDefaultCreateTopoStore_MemoryImplementation tests the success path with memory implementation.
// This covers the return statement in defaultCreateTopoStore that can't be tested with etcd.
func TestDefaultCreateTopoStore_MemoryImplementation(t *testing.T) {
	// Create a memory topology factory first
	_, factory := memorytopo.NewServerAndFactory(t.Context())

	// Register it
	topoclient.RegisterFactory("memory", factory)

	cell := &multigresv1alpha1.Cell{
		Spec: multigresv1alpha1.CellSpec{
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				Address:        "localhost:2379",
				RootPath:       "/test",
				Implementation: "memory",
			},
		},
	}

	store, err := defaultCreateTopoStore(cell)
	if err != nil {
		t.Fatalf("defaultCreateTopoStore() with memory implementation should succeed: %v", err)
	}

	if store == nil {
		t.Fatal("Expected non-nil store")
	}

	// TODO: handle store.Close() error properly
	defer func() { _ = store.Close() }()
}

// mockTopoStoreWrapper wraps a real topoclient.Store and allows overriding specific methods
// for testing error paths. This avoids having to implement the entire Store interface.
type mockTopoStoreWrapper struct {
	topoclient.Store
	createCellFunc func(ctx context.Context, cellName string, cell *clustermetadata.Cell) error
	deleteCellFunc func(ctx context.Context, cellName string, force bool) error
}

func (m *mockTopoStoreWrapper) CreateCell(
	ctx context.Context,
	cellName string,
	cell *clustermetadata.Cell,
) error {
	if m.createCellFunc != nil {
		return m.createCellFunc(ctx, cellName, cell)
	}
	return m.Store.CreateCell(ctx, cellName, cell)
}

func (m *mockTopoStoreWrapper) DeleteCell(ctx context.Context, cellName string, force bool) error {
	if m.deleteCellFunc != nil {
		return m.deleteCellFunc(ctx, cellName, force)
	}
	return m.Store.DeleteCell(ctx, cellName, force)
}

func TestIsTopoUnavailable(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err  error
		want bool
	}{
		"nil error":           {err: nil, want: false},
		"UNAVAILABLE error":   {err: errors.New("Code: UNAVAILABLE"), want: true},
		"no connection error": {err: errors.New("no connection available"), want: true},
		"unrelated error":     {err: errors.New("permission denied"), want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := isTopoUnavailable(tc.err); got != tc.want {
				t.Errorf("isTopoUnavailable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

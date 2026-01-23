package cell_test

import (
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/cell"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

// conditionAssertion defines expected condition state for testing
type conditionAssertion struct {
	Type   string
	Status metav1.ConditionStatus
	Reason string
}

// assertConditions verifies conditions match expectations
func assertConditions(t testing.TB, got []metav1.Condition, want ...conditionAssertion) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("condition count = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Type != w.Type {
			t.Errorf("condition[%d].Type = %q, want %q", i, g.Type, w.Type)
		}
		if g.Status != w.Status {
			t.Errorf("condition[%d].Status = %q, want %q", i, g.Status, w.Status)
		}
		if g.Reason != w.Reason {
			t.Errorf("condition[%d].Reason = %q, want %q", i, g.Reason, w.Reason)
		}
	}
}

func TestCellReconciler_Reconcile(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := map[string]struct {
		cell            *multigresv1alpha1.Cell
		existingObjects []client.Object
		failureConfig   *testutil.FailureConfig
		wantErr         bool
		wantRequeue     bool
		assertFunc      func(t *testing.T, c client.Client, cell *multigresv1alpha1.Cell)
	}{
		////----------------------------------------
		///   Success
		//------------------------------------------
		"create all resources for new Cell": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
				},
			},
			existingObjects: []client.Object{},
			assertFunc: func(t *testing.T, c client.Client, cell *multigresv1alpha1.Cell) {
				// Verify MultiGateway Deployment was created
				mgDeploy := &appsv1.Deployment{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-cell-multigateway", Namespace: "default"},
					mgDeploy); err != nil {
					t.Errorf("MultiGateway Deployment should exist: %v", err)
				}

				// Verify MultiGateway Service was created
				mgSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-cell-multigateway", Namespace: "default"},
					mgSvc); err != nil {
					t.Errorf("MultiGateway Service should exist: %v", err)
				}

				// Verify defaults
				if *mgDeploy.Spec.Replicas != int32(2) {
					t.Errorf(
						"MultiGateway Deployment replicas = %d, want %d",
						*mgDeploy.Spec.Replicas,
						int32(2),
					)
				}

				// Verify finalizer was added
				updatedCell := &multigresv1alpha1.Cell{}
				if err := c.Get(t.Context(), types.NamespacedName{Name: "test-cell", Namespace: "default"}, updatedCell); err != nil {
					t.Fatalf("Failed to get Cell: %v", err)
				}
				if !slices.Contains(updatedCell.Finalizers, "cell.multigres.com/finalizer") {
					t.Errorf("Finalizer should be added")
				}
			},
		},
		"update existing resources": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "existing-cell",
					Namespace:  "default",
					Finalizers: []string{"cell.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone2",
					Images: multigresv1alpha1.CellImages{
						MultiGateway: "custom/multigateway:v1.0.0",
					},
					MultiGateway: multigresv1alpha1.StatelessSpec{
						Replicas: ptr.To(int32(5)),
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-cell-multigateway",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(2)), // will be updated to 5
					},
					Status: appsv1.DeploymentStatus{
						Replicas:      2,
						ReadyReplicas: 2,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-cell-multigateway",
						Namespace: "default",
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, cell *multigresv1alpha1.Cell) {
				mgDeploy := &appsv1.Deployment{}
				err := c.Get(t.Context(), types.NamespacedName{
					Name:      "existing-cell-multigateway",
					Namespace: "default",
				}, mgDeploy)
				if err != nil {
					t.Fatalf("Failed to get MultiGateway Deployment: %v", err)
				}

				if *mgDeploy.Spec.Replicas != 5 {
					t.Errorf(
						"MultiGateway Deployment replicas = %d, want 5",
						*mgDeploy.Spec.Replicas,
					)
				}

				if mgDeploy.Spec.Template.Spec.Containers[0].Image != "custom/multigateway:v1.0.0" {
					t.Errorf(
						"MultiGateway image = %s, want custom/multigateway:v1.0.0",
						mgDeploy.Spec.Template.Spec.Containers[0].Image,
					)
				}
			},
		},
		"deletion with finalizer": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-cell-deletion",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					Finalizers:        []string{"cell.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone3",
				},
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.Cell{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-cell-deletion",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{"cell.multigres.com/finalizer"},
					},
					Spec: multigresv1alpha1.CellSpec{
						Name: "zone3",
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, cell *multigresv1alpha1.Cell) {
				updatedCell := &multigresv1alpha1.Cell{}
				err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-cell-deletion", Namespace: "default"},
					updatedCell)
				if err == nil {
					t.Errorf(
						"Cell object should be deleted but still exists (finalizers: %v)",
						updatedCell.Finalizers,
					)
				}
			},
		},
		"all replicas ready status": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cell-ready",
					Namespace:  "default",
					Finalizers: []string{"cell.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone4",
					MultiGateway: multigresv1alpha1.StatelessSpec{
						Replicas: ptr.To(int32(2)),
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cell-ready-multigateway",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(2)),
					},
					Status: appsv1.DeploymentStatus{
						Replicas:      2,
						ReadyReplicas: 2,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cell-ready-multigateway",
						Namespace: "default",
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, cell *multigresv1alpha1.Cell) {
				updatedCell := &multigresv1alpha1.Cell{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-cell-ready", Namespace: "default"},
					updatedCell); err != nil {
					t.Fatalf("Failed to get Cell: %v", err)
				}

				assertConditions(
					t,
					updatedCell.Status.Conditions,
					conditionAssertion{
						Type:   "Available",
						Status: metav1.ConditionTrue,
						Reason: "MultiGatewayAvailable",
					},
					conditionAssertion{
						Type:   "Ready",
						Status: metav1.ConditionTrue,
						Reason: "MultiGatewayReady",
					},
				)

				if got, want := updatedCell.Status.GatewayReplicas, int32(2); got != want {
					t.Errorf("GatewayReplicas = %d, want %d", got, want)
				}
				if got, want := updatedCell.Status.GatewayReadyReplicas, int32(2); got != want {
					t.Errorf("GatewayReadyReplicas = %d, want %d", got, want)
				}

				if !slices.Contains(updatedCell.Finalizers, "cell.multigres.com/finalizer") {
					t.Error("Finalizer should be present")
				}
			},
		},
		"not ready status - partial replicas": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cell-partial",
					Namespace:  "default",
					Finalizers: []string{"cell.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone5",
					MultiGateway: multigresv1alpha1.StatelessSpec{
						Replicas: ptr.To(int32(3)),
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cell-partial-multigateway",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(3)),
					},
					Status: appsv1.DeploymentStatus{
						Replicas:      3,
						ReadyReplicas: 2, // not all ready
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cell-partial-multigateway",
						Namespace: "default",
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, cell *multigresv1alpha1.Cell) {
				updatedCell := &multigresv1alpha1.Cell{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-cell-partial", Namespace: "default"},
					updatedCell); err != nil {
					t.Fatalf("Failed to get Cell: %v", err)
				}

				// With 2/3 replicas ready: Available=True (service is up), Ready=False (not converged)
				assertConditions(
					t,
					updatedCell.Status.Conditions,
					conditionAssertion{
						Type:   "Available",
						Status: metav1.ConditionTrue,
						Reason: "MultiGatewayAvailable",
					},
					conditionAssertion{
						Type:   "Ready",
						Status: metav1.ConditionFalse,
						Reason: "MultiGatewayNotReady",
					},
				)
			},
		},
		////----------------------------------------
		///   Error
		//------------------------------------------
		"error on status update": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnStatusUpdate: testutil.FailOnObjectName("test-cell", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error on Get MultiGateway Deployment in updateStatus (network error)": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cell-status",
					Namespace:  "default",
					Finalizers: []string{"cell.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cell-status-multigateway",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cell-status-multigateway",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				// Fail MultiGateway Deployment Get in updateStatus (after reconcile calls)
				OnGet: testutil.FailKeyAfterNCalls(2, testutil.ErrNetworkTimeout),
			},
			wantErr: true,
		},
		"error on MultiGateway Deployment create": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: func(obj client.Object) error {
					if deploy, ok := obj.(*appsv1.Deployment); ok &&
						deploy.Name == "test-cell-multigateway" {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on MultiGateway Deployment Update": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cell",
					Namespace:  "default",
					Finalizers: []string{"cell.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
					MultiGateway: multigresv1alpha1.StatelessSpec{
						Replicas: ptr.To(int32(5)),
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cell-multigateway",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(2)),
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: func(obj client.Object) error {
					if deploy, ok := obj.(*appsv1.Deployment); ok &&
						deploy.Name == "test-cell-multigateway" {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Get MultiGateway Deployment (network error)": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cell",
					Namespace:  "default",
					Finalizers: []string{"cell.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					if key.Name == "test-cell-multigateway" {
						return testutil.ErrNetworkTimeout
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on MultiGateway Service create": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok &&
						svc.Name == "test-cell-multigateway" {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on MultiGateway Service Update": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cell",
					Namespace:  "default",
					Finalizers: []string{"cell.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cell-multigateway",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cell-multigateway",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok &&
						svc.Name == "test-cell-multigateway" {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Get MultiGateway Service (network error)": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cell-svc",
					Namespace:  "default",
					Finalizers: []string{"cell.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cell-svc-multigateway",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnNamespacedKeyName(
					"test-cell-svc-multigateway",
					"default",
					testutil.ErrNetworkTimeout,
				),
			},
			wantErr: true,
		},
		"error on finalizer Update": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-cell", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"deletion error on finalizer removal": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-cell-del",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					Finalizers:        []string{"cell.multigres.com/finalizer"},
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
				},
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.Cell{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-cell-del",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{"cell.multigres.com/finalizer"},
					},
					Spec: multigresv1alpha1.CellSpec{
						Name: "zone1",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-cell-del", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error on Get Cell (network error)": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
				},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName("test-cell", testutil.ErrNetworkTimeout),
			},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Create base fake client
			baseClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingObjects...).
				WithStatusSubresource(&multigresv1alpha1.Cell{}).
				Build()

			fakeClient := client.Client(baseClient)
			// Wrap with failure injection if configured
			if tc.failureConfig != nil {
				fakeClient = testutil.NewFakeClientWithFailures(baseClient, tc.failureConfig)
			}

			reconciler := &cell.CellReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			// Create the Cell resource if not in existing objects
			cellInExisting := false
			for _, obj := range tc.existingObjects {
				if cell, ok := obj.(*multigresv1alpha1.Cell); ok && cell.Name == tc.cell.Name {
					cellInExisting = true
					break
				}
			}
			if !cellInExisting {
				err := fakeClient.Create(t.Context(), tc.cell)
				if err != nil {
					t.Fatalf("Failed to create Cell: %v", err)
				}
			}

			// Reconcile
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tc.cell.Name,
					Namespace: tc.cell.Namespace,
				},
			}

			result, err := reconciler.Reconcile(t.Context(), req)
			if (err != nil) != tc.wantErr {
				t.Errorf("Reconcile() error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			if tc.wantErr {
				return
			}

			// NOTE: Check for requeue delay when we need to support such setup.
			_ = result

			// Run custom assertions if provided
			if tc.assertFunc != nil {
				tc.assertFunc(t, fakeClient, tc.cell)
			}
		})
	}
}

func TestCellReconciler_ReconcileNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	reconciler := &cell.CellReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// Reconcile non-existent resource
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "nonexistent-cell",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(t.Context(), req)
	if err != nil {
		t.Errorf("Reconcile() should not error on NotFound, got: %v", err)
	}
	if result.RequeueAfter > 0 {
		t.Errorf("Reconcile() should not requeue on NotFound")
	}
}

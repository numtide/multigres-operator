package multiorch

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/testutil"
)

func TestMultiOrchReconciler_Reconcile(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := map[string]struct {
		multiorch       *multigresv1alpha1.MultiOrch
		existingObjects []client.Object
		failureConfig   *testutil.FailureConfig
		wantErr         bool
		wantRequeue     bool
		assertFunc      func(t *testing.T, c client.Client, multiorch *multigresv1alpha1.MultiOrch)
	}{
		////----------------------------------------
		///   Success
		//------------------------------------------
		"create all resources for new MultiOrch": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multiorch",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiOrchSpec{},
			},
			existingObjects: []client.Object{},
			assertFunc: func(t *testing.T, c client.Client, multiorch *multigresv1alpha1.MultiOrch) {
				// Verify Deployment was created
				dp := &appsv1.Deployment{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-multiorch", Namespace: "default"},
					dp); err != nil {
					t.Errorf("Deployment should exist: %v", err)
				}

				// Verify Service was created
				svc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-multiorch", Namespace: "default"},
					svc); err != nil {
					t.Errorf("Service should exist: %v", err)
				}

				// Verify defaults
				if *dp.Spec.Replicas != DefaultReplicas {
					t.Errorf(
						"Deployment replicas = %d, want %d",
						*dp.Spec.Replicas,
						DefaultReplicas,
					)
				}

				// Verify container
				wantContainer := corev1.Container{
					Name:      "multiorch",
					Image:     DefaultImage,
					Resources: corev1.ResourceRequirements{},
					Ports:     buildContainerPorts(&multigresv1alpha1.MultiOrch{}),
				}
				if diff := cmp.Diff(wantContainer, dp.Spec.Template.Spec.Containers[0]); diff != "" {
					t.Errorf("Container mismatch (-want +got):\n%s", diff)
				}

				// Verify finalizer
				updatedMultiOrch := &multigresv1alpha1.MultiOrch{}
				if err := c.Get(t.Context(), types.NamespacedName{Name: "test-multiorch", Namespace: "default"}, updatedMultiOrch); err != nil {
					t.Fatalf("Failed to get MultiOrch: %v", err)
				}
				if !slices.Contains(updatedMultiOrch.Finalizers, finalizerName) {
					t.Errorf("Finalizer should be added")
				}
			},
		},
		"update existing resources": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "existing-multiorch",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiOrchSpec{
					Replicas: int32Ptr(3),
					Image:    "custom/multiorch:v1.0.0",
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-multiorch",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: int32Ptr(1), // will be updated to 3
					},
					Status: appsv1.DeploymentStatus{
						Replicas:      1,
						ReadyReplicas: 1,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-multiorch",
						Namespace: "default",
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, multiorch *multigresv1alpha1.MultiOrch) {
				dp := &appsv1.Deployment{}
				err := c.Get(t.Context(), types.NamespacedName{
					Name:      "existing-multiorch",
					Namespace: "default",
				}, dp)
				if err != nil {
					t.Fatalf("Failed to get Deployment: %v", err)
				}

				if *dp.Spec.Replicas != 3 {
					t.Errorf("Deployment replicas = %d, want 3", *dp.Spec.Replicas)
				}

				if dp.Spec.Template.Spec.Containers[0].Image != "custom/multiorch:v1.0.0" {
					t.Errorf(
						"Deployment image = %s, want custom/multiorch:v1.0.0",
						dp.Spec.Template.Spec.Containers[0].Image,
					)
				}
			},
		},
		"multiorch with cellName": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multiorch-cell1",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiOrchSpec{
					CellName: "cell-1",
				},
			},
			existingObjects: []client.Object{},
			assertFunc: func(t *testing.T, c client.Client, multiorch *multigresv1alpha1.MultiOrch) {
				dp := &appsv1.Deployment{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "multiorch-cell1", Namespace: "default"},
					dp); err != nil {
					t.Fatalf("Failed to get Deployment: %v", err)
				}
				if dp.Labels["multigres.com/cell"] != "cell-1" {
					t.Errorf(
						"Deployment cell label = %s, want cell-1",
						dp.Labels["multigres.com/cell"],
					)
				}

				svc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "multiorch-cell1", Namespace: "default"},
					svc); err != nil {
					t.Fatalf("Failed to get Service: %v", err)
				}
				if svc.Labels["multigres.com/cell"] != "cell-1" {
					t.Errorf(
						"Service cell label = %s, want cell-1",
						svc.Labels["multigres.com/cell"],
					)
				}
			},
		},
		"deletion with finalizer": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-multiorch-deletion",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					Finalizers:        []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiOrchSpec{},
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.MultiOrch{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-multiorch-deletion",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{finalizerName},
					},
					Spec: multigresv1alpha1.MultiOrchSpec{},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, multiorch *multigresv1alpha1.MultiOrch) {
				updatedMultiOrch := &multigresv1alpha1.MultiOrch{}
				err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-multiorch-deletion", Namespace: "default"},
					updatedMultiOrch)
				if err == nil {
					t.Errorf(
						"MultiOrch object should be deleted but still exists (finalizers: %v)",
						updatedMultiOrch.Finalizers,
					)
				}
			},
		},
		"all replicas ready status": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-multiorch-ready",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiOrchSpec{
					Replicas: int32Ptr(2),
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multiorch-ready",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: int32Ptr(2),
					},
					Status: appsv1.DeploymentStatus{
						Replicas:      2,
						ReadyReplicas: 2,
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, multiorch *multigresv1alpha1.MultiOrch) {
				updatedMultiOrch := &multigresv1alpha1.MultiOrch{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-multiorch-ready", Namespace: "default"},
					updatedMultiOrch); err != nil {
					t.Fatalf("Failed to get MultiOrch: %v", err)
				}

				if !updatedMultiOrch.Status.Ready {
					t.Error("Status.Ready should be true")
				}
				if updatedMultiOrch.Status.Replicas != 2 {
					t.Errorf("Status.Replicas = %d, want 2", updatedMultiOrch.Status.Replicas)
				}
				if updatedMultiOrch.Status.ReadyReplicas != 2 {
					t.Errorf(
						"Status.ReadyReplicas = %d, want 2",
						updatedMultiOrch.Status.ReadyReplicas,
					)
				}
				if len(updatedMultiOrch.Status.Conditions) == 0 {
					t.Error("Status.Conditions should not be empty")
				} else {
					readyCondition := updatedMultiOrch.Status.Conditions[0]
					if readyCondition.Type != "Ready" {
						t.Errorf("Condition type = %s, want Ready", readyCondition.Type)
					}
					if readyCondition.Status != metav1.ConditionTrue {
						t.Errorf("Condition status = %s, want True", readyCondition.Status)
					}
				}

				if !slices.Contains(updatedMultiOrch.Finalizers, finalizerName) {
					t.Errorf("Finalizer should be present")
				}
			},
		},
		////----------------------------------------
		///   Error
		//------------------------------------------
		"error on Deployment create": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multiorch",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiOrchSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: func(obj client.Object) error {
					if _, ok := obj.(*appsv1.Deployment); ok {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Service create": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multiorch",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiOrchSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok && svc.Name == "test-multiorch" {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on status update": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multiorch",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiOrchSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnStatusUpdate: testutil.FailOnObjectName("test-multiorch", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error on Get MultiOrch": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multiorch",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiOrchSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName("test-multiorch", testutil.ErrNetworkTimeout),
			},
			wantErr: true,
		},
		"error on finalizer Update": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multiorch",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiOrchSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-multiorch", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error on Deployment Update": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-multiorch",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiOrchSpec{
					Replicas: int32Ptr(5),
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multiorch",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: int32Ptr(1),
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: func(obj client.Object) error {
					if _, ok := obj.(*appsv1.Deployment); ok {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Service Update": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-multiorch",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiOrchSpec{},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multiorch",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multiorch",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok && svc.Name == "test-multiorch" {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Get Deployment in updateStatus": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-multiorch-status",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiOrchSpec{},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multiorch-status",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				// Fail Deployment Get after first successful call
				// First Get succeeds (in reconcileDeployment)
				// Second Get fails (in updateStatus)
				OnGet: testutil.FailKeyAfterNCalls(1, testutil.ErrNetworkTimeout),
			},
			wantErr: true,
		},
		"error on Get Deployment (not NotFound)": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-multiorch",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiOrchSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					// Fail Deployment Get with non-NotFound error
					if key.Name == "test-multiorch" {
						return testutil.ErrNetworkTimeout
					}
					return nil
				},
			},
			wantErr: true,
		},
		"deletion error on finalizer removal": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-multiorch-del",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					Finalizers:        []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiOrchSpec{},
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.MultiOrch{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-multiorch-del",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{finalizerName},
					},
					Spec: multigresv1alpha1.MultiOrchSpec{},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-multiorch-del", testutil.ErrInjected),
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
				WithStatusSubresource(&multigresv1alpha1.MultiOrch{}).
				Build()

			fakeClient := client.Client(baseClient)
			// Wrap with failure injection if configured
			if tc.failureConfig != nil {
				fakeClient = testutil.NewFakeClientWithFailures(baseClient, tc.failureConfig)
			}

			reconciler := &MultiOrchReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			// Create the MultiOrch resource if not in existing objects
			multiorchInExisting := false
			for _, obj := range tc.existingObjects {
				if mo, ok := obj.(*multigresv1alpha1.MultiOrch); ok &&
					mo.Name == tc.multiorch.Name {
					multiorchInExisting = true
					break
				}
			}
			if !multiorchInExisting {
				err := fakeClient.Create(t.Context(), tc.multiorch)
				if err != nil {
					t.Fatalf("Failed to create MultiOrch: %v", err)
				}
			}

			// Reconcile
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tc.multiorch.Name,
					Namespace: tc.multiorch.Namespace,
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

			_ = result

			// Run custom assertions if provided
			if tc.assertFunc != nil {
				tc.assertFunc(t, fakeClient, tc.multiorch)
			}
		})
	}
}

func TestMultiOrchReconciler_ReconcileNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	reconciler := &MultiOrchReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// Reconcile non-existent resource
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "nonexistent-multiorch",
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

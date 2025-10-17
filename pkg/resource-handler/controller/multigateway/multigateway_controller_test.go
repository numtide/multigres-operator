package multigateway

import (
	"slices"
	"testing"

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

func TestMultiGatewayReconciler_Reconcile(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := map[string]struct {
		mg              *multigresv1alpha1.MultiGateway
		existingObjects []client.Object
		failureConfig   *testutil.FailureConfig
		// TODO: If wantErr is false but failureConfig is set, assertions may fail
		// due to failure injection. This should be addressed when we need to test
		// partial failures that don't prevent reconciliation success.
		wantErr     bool
		wantRequeue bool
		assertFunc  func(t *testing.T, c client.Client, mg *multigresv1alpha1.MultiGateway)
	}{
		////----------------------------------------
		///   Success
		//------------------------------------------
		"create all resources for new MultiGateway": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multigateway",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{},
			},
			existingObjects: []client.Object{},
			assertFunc: func(t *testing.T, c client.Client, mg *multigresv1alpha1.MultiGateway) {
				// Verify all three resources were created
				sts := &appsv1.Deployment{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-multigateway", Namespace: "default"},
					sts); err != nil {
					t.Errorf("Deployment should exist: %v", err)
				}

				svc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-multigateway", Namespace: "default"},
					svc); err != nil {
					t.Errorf("Service should exist: %v", err)
				}

				// Verify defaults and finalizer
				if *sts.Spec.Replicas != DefaultReplicas {
					t.Errorf(
						"Deployment replicas = %d, want %d",
						*sts.Spec.Replicas,
						DefaultReplicas,
					)
				}

				updatedMultiGateway := &multigresv1alpha1.MultiGateway{}
				if err := c.Get(t.Context(), types.NamespacedName{Name: "test-multigateway", Namespace: "default"}, updatedMultiGateway); err != nil {
					t.Fatalf("Failed to get MultiGateway: %v", err)
				}
				if !slices.Contains(updatedMultiGateway.Finalizers, finalizerName) {
					t.Errorf("Finalizer should be added")
				}
			},
		},
		"update existing resources": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "existing-multigateway",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{
					Replicas: int32Ptr(5),
					Image:    "foo/bar:1.2.3",
				},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-multigateway",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: int32Ptr(3), // will be updated to 5
					},
					Status: appsv1.StatefulSetStatus{
						Replicas:      3,
						ReadyReplicas: 3,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-multigateway-headless",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-multigateway",
						Namespace: "default",
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, mg *multigresv1alpha1.MultiGateway) {
				dp := &appsv1.Deployment{}
				err := c.Get(t.Context(), types.NamespacedName{
					Name:      "existing-multigateway",
					Namespace: "default",
				}, dp)
				if err != nil {
					t.Fatalf("Failed to get Deployment: %v", err)
				}

				if *dp.Spec.Replicas != 5 {
					t.Errorf("Deployment replicas = %d, want 5", *dp.Spec.Replicas)
				}

				if dp.Spec.Template.Spec.Containers[0].Image != "foo/bar:1.2.3" {
					t.Errorf(
						"Deployment image = %s, want foo/bar:1.2.3",
						dp.Spec.Template.Spec.Containers[0].Image,
					)
				}
			},
		},
		"MultiGateway with cellName": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multigateway-zone1",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{
					CellName: "zone1",
				},
			},
			existingObjects: []client.Object{},
			assertFunc: func(t *testing.T, c client.Client, mg *multigresv1alpha1.MultiGateway) {
				dp := &appsv1.Deployment{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "multigateway-zone1", Namespace: "default"},
					dp); err != nil {
					t.Fatalf("Failed to get Deployment: %v", err)
				}
				if dp.Labels["multigres.com/cell"] != "zone1" {
					t.Errorf(
						"Deployment cell label = %s, want zone1",
						dp.Labels["multigres.com/cell"],
					)
				}

				svc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "multigateway-zone1", Namespace: "default"},
					svc); err != nil {
					t.Fatalf("Failed to get Service: %v", err)
				}
				if svc.Labels["multigres.com/cell"] != "zone1" {
					t.Errorf(
						"Service cell label = %s, want zone1",
						svc.Labels["multigres.com/cell"],
					)
				}
			},
		},
		"deletion with finalizer": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-multigateway-deletion",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					Finalizers:        []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{},
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.MultiGateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-multigateway-deletion",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{finalizerName},
					},
					Spec: multigresv1alpha1.MultiGatewaySpec{},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, multigateway *multigresv1alpha1.MultiGateway) {
				updatedMultiGateway := &multigresv1alpha1.MultiGateway{}
				err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-multigateway-deletion", Namespace: "default"},
					updatedMultiGateway)
				if err == nil {
					t.Errorf(
						"MultiGateway object should be deleted but still exists (finalizers: %v)",
						updatedMultiGateway.Finalizers,
					)
				}
			},
		},
		"all replicas ready status": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-multigateway-ready",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{
					Replicas: int32Ptr(3),
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multigateway-ready",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: int32Ptr(3),
					},
					Status: appsv1.DeploymentStatus{
						Replicas:      3,
						ReadyReplicas: 3,
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, multigateway *multigresv1alpha1.MultiGateway) {
				updatedMultiGateway := &multigresv1alpha1.MultiGateway{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-multigateway-ready", Namespace: "default"},
					updatedMultiGateway); err != nil {
					t.Fatalf("Failed to get MultiGateway: %v", err)
				}

				if !updatedMultiGateway.Status.Ready {
					t.Error("Status.Ready should be true")
				}
				if updatedMultiGateway.Status.Replicas != 3 {
					t.Errorf("Status.Replicas = %d, want 3", updatedMultiGateway.Status.Replicas)
				}
				if updatedMultiGateway.Status.ReadyReplicas != 3 {
					t.Errorf(
						"Status.ReadyReplicas = %d, want 3",
						updatedMultiGateway.Status.ReadyReplicas,
					)
				}
				if len(updatedMultiGateway.Status.Conditions) == 0 {
					t.Error("Status.Conditions should not be empty")
				} else {
					readyCondition := updatedMultiGateway.Status.Conditions[0]
					if readyCondition.Type != "Ready" {
						t.Errorf("Condition type = %s, want Ready", readyCondition.Type)
					}
					if readyCondition.Status != metav1.ConditionTrue {
						t.Errorf("Condition status = %s, want True", readyCondition.Status)
					}
				}

				if !slices.Contains(updatedMultiGateway.Finalizers, finalizerName) {
					t.Errorf("Finalizer should be present")
				}
			},
		},
		////----------------------------------------
		///   Error
		//------------------------------------------
		"error on status update": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multigateway",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnStatusUpdate: testutil.FailOnObjectName(
					"test-multigateway",
					testutil.ErrInjected,
				),
			},
			wantErr: true,
		},
		"error on Get Deployment in updateStatus (network error)": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-multigateway-status",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multigateway-status",
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
		"error on Service create": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multigateway",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok && svc.Name == "test-multigateway" {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Service Update": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-multigateway",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multigateway",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multigateway-headless",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multigateway",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok && svc.Name == "test-multigateway" {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Get Service (network error)": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-multigateway-svc",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multigateway-svc",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multigateway-svc-headless",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnNamespacedKeyName(
					"test-multigateway-svc",
					"default",
					testutil.ErrNetworkTimeout,
				),
			},
			wantErr: true,
		},
		"error on Deployment create": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multigateway",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{},
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
		"error on Deployment Update": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-multigateway",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{
					Replicas: int32Ptr(5),
				},
			},
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multigateway",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: int32Ptr(3),
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
		"error on Get Deployment (network error)": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-multigateway",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					if key.Name == "test-multigateway" {
						return testutil.ErrNetworkTimeout
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on finalizer Update": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multigateway",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-multigateway", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"deletion error on finalizer removal": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-multigateway-del",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					Finalizers:        []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{},
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.MultiGateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-multigateway-del",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{finalizerName},
					},
					Spec: multigresv1alpha1.MultiGatewaySpec{},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-multigateway-del", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error on Get MultiGateway (network error)": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multigateway",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName("test-multigateway", testutil.ErrNetworkTimeout),
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
				WithStatusSubresource(&multigresv1alpha1.MultiGateway{}).
				Build()

			fakeClient := client.Client(baseClient)
			// Wrap with failure injection if configured
			if tc.failureConfig != nil {
				fakeClient = testutil.NewFakeClientWithFailures(baseClient, tc.failureConfig)
			}

			reconciler := &MultiGatewayReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			// Create the MultiGateway resource if not in existing objects
			mgInExisting := false
			for _, obj := range tc.existingObjects {
				if mg, ok := obj.(*multigresv1alpha1.MultiGateway); ok && mg.Name == tc.mg.Name {
					mgInExisting = true
					break
				}
			}
			if !mgInExisting {
				err := fakeClient.Create(t.Context(), tc.mg)
				if err != nil {
					t.Fatalf("Failed to create MultiGateway: %v", err)
				}
			}

			// Reconcile
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tc.mg.Name,
					Namespace: tc.mg.Namespace,
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
			// // Check requeue
			// if (result.RequeueAfter != 0) != tc.wantRequeue {
			// 	t.Errorf("Reconcile() result.Requeue = %v, want %v", result.RequeueAfter, tc.wantRequeue)
			// }

			// Run custom assertions if provided
			if tc.assertFunc != nil {
				tc.assertFunc(t, fakeClient, tc.mg)
			}
		})
	}
}

func TestMultiGatewayReconciler_ReconcileNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	reconciler := &MultiGatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// Reconcile non-existent resource
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "nonexistent-multigateway",
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

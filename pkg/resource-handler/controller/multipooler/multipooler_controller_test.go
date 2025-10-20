package multipooler

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

func TestMultiPoolerReconciler_Reconcile(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := map[string]struct {
		multipooler     *multigresv1alpha1.MultiPooler
		existingObjects []client.Object
		failureConfig   *testutil.FailureConfig
		// TODO: If wantErr is false but failureConfig is set, assertions may fail
		// due to failure injection. This should be addressed when we need to test
		// partial failures that don't prevent reconciliation success.
		wantErr     bool
		wantRequeue bool
		assertFunc  func(t *testing.T, c client.Client, multipooler *multigresv1alpha1.MultiPooler)
	}{
		////----------------------------------------
		///   Success
		//------------------------------------------
		"create all resources for new MultiPooler": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			existingObjects: []client.Object{},
			assertFunc: func(t *testing.T, c client.Client, multipooler *multigresv1alpha1.MultiPooler) {
				// Verify StatefulSet was created
				sts := &appsv1.StatefulSet{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-multipooler", Namespace: "default"},
					sts); err != nil {
					t.Errorf("StatefulSet should exist: %v", err)
				}

				// Verify headless Service was created
				headlessSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-multipooler-headless", Namespace: "default"},
					headlessSvc); err != nil {
					t.Errorf("Headless Service should exist: %v", err)
				}

				// Verify defaults and finalizer
				if *sts.Spec.Replicas != DefaultReplicas {
					t.Errorf(
						"StatefulSet replicas = %d, want %d",
						*sts.Spec.Replicas,
						DefaultReplicas,
					)
				}

				// Verify init containers: pgctld-init, multipooler (sidecar)
				wantInitContainers := []corev1.Container{
					buildPgctldInitContainer(&multigresv1alpha1.MultiPooler{}),
					buildMultiPoolerSidecar(&multigresv1alpha1.MultiPooler{}),
				}
				if diff := cmp.Diff(wantInitContainers, sts.Spec.Template.Spec.InitContainers); diff != "" {
					t.Errorf("InitContainers mismatch (-want +got):\n%s", diff)
				}

				// Verify main container: postgres
				wantContainers := []corev1.Container{
					buildPostgresContainer(&multigresv1alpha1.MultiPooler{}),
				}
				if diff := cmp.Diff(wantContainers, sts.Spec.Template.Spec.Containers); diff != "" {
					t.Errorf("Containers mismatch (-want +got):\n%s", diff)
				}

				updatedMultiPooler := &multigresv1alpha1.MultiPooler{}
				if err := c.Get(t.Context(), types.NamespacedName{Name: "test-multipooler", Namespace: "default"}, updatedMultiPooler); err != nil {
					t.Fatalf("Failed to get MultiPooler: %v", err)
				}
				if !slices.Contains(updatedMultiPooler.Finalizers, finalizerName) {
					t.Errorf("Finalizer should be added")
				}
			},
		},
		"update existing resources": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "existing-multipooler",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{
					Replicas: int32Ptr(3),
					MultiPooler: multigresv1alpha1.MultiPoolerContainerSpec{
						Image: "ghcr.io/multigres/multipooler:v1.0.0",
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-multipooler",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: int32Ptr(1), // will be updated to 3
					},
					Status: appsv1.StatefulSetStatus{
						Replicas:      1,
						ReadyReplicas: 1,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-multipooler-headless",
						Namespace: "default",
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, multipooler *multigresv1alpha1.MultiPooler) {
				sts := &appsv1.StatefulSet{}
				err := c.Get(t.Context(), types.NamespacedName{
					Name:      "existing-multipooler",
					Namespace: "default",
				}, sts)
				if err != nil {
					t.Fatalf("Failed to get StatefulSet: %v", err)
				}

				if *sts.Spec.Replicas != 3 {
					t.Errorf("StatefulSet replicas = %d, want 3", *sts.Spec.Replicas)
				}

				// Verify multipooler sidecar image was updated (it's in init containers)
				wantMultiPoolerSpec := &multigresv1alpha1.MultiPoolerSpec{
					MultiPooler: multigresv1alpha1.MultiPoolerContainerSpec{
						Image: "ghcr.io/multigres/multipooler:v1.0.0",
					},
				}
				wantMultiPoolerSidecar := buildMultiPoolerSidecar(&multigresv1alpha1.MultiPooler{
					Spec: *wantMultiPoolerSpec,
				})
				if diff := cmp.Diff(wantMultiPoolerSidecar, sts.Spec.Template.Spec.InitContainers[1]); diff != "" {
					t.Errorf("multipooler sidecar mismatch (-want +got):\n%s", diff)
				}
			},
		},
		"multipooler with cellName": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multipooler-cell1",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{
					CellName: "cell-1",
				},
			},
			existingObjects: []client.Object{},
			assertFunc: func(t *testing.T, c client.Client, multipooler *multigresv1alpha1.MultiPooler) {
				sts := &appsv1.StatefulSet{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "multipooler-cell1", Namespace: "default"},
					sts); err != nil {
					t.Fatalf("Failed to get StatefulSet: %v", err)
				}
				if sts.Labels["multigres.com/cell"] != "cell-1" {
					t.Errorf(
						"StatefulSet cell label = %s, want cell-1",
						sts.Labels["multigres.com/cell"],
					)
				}

				headlessSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "multipooler-cell1-headless", Namespace: "default"},
					headlessSvc); err != nil {
					t.Fatalf("Failed to get headless Service: %v", err)
				}
				if headlessSvc.Labels["multigres.com/cell"] != "cell-1" {
					t.Errorf(
						"Headless Service cell label = %s, want cell-1",
						headlessSvc.Labels["multigres.com/cell"],
					)
				}
			},
		},
		"deletion with finalizer": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-multipooler-deletion",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					Finalizers:        []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.MultiPooler{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-multipooler-deletion",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{finalizerName},
					},
					Spec: multigresv1alpha1.MultiPoolerSpec{},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, multipooler *multigresv1alpha1.MultiPooler) {
				updatedMultiPooler := &multigresv1alpha1.MultiPooler{}
				err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-multipooler-deletion", Namespace: "default"},
					updatedMultiPooler)
				if err == nil {
					t.Errorf(
						"MultiPooler object should be deleted but still exists (finalizers: %v)",
						updatedMultiPooler.Finalizers,
					)
				}
			},
		},
		"all replicas ready status": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-multipooler-ready",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{
					Replicas: int32Ptr(2),
				},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multipooler-ready",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: int32Ptr(2),
					},
					Status: appsv1.StatefulSetStatus{
						Replicas:      2,
						ReadyReplicas: 2,
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, multipooler *multigresv1alpha1.MultiPooler) {
				updatedMultiPooler := &multigresv1alpha1.MultiPooler{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-multipooler-ready", Namespace: "default"},
					updatedMultiPooler); err != nil {
					t.Fatalf("Failed to get MultiPooler: %v", err)
				}

				if !updatedMultiPooler.Status.Ready {
					t.Error("Status.Ready should be true")
				}
				if updatedMultiPooler.Status.Replicas != 2 {
					t.Errorf("Status.Replicas = %d, want 2", updatedMultiPooler.Status.Replicas)
				}
				if updatedMultiPooler.Status.ReadyReplicas != 2 {
					t.Errorf(
						"Status.ReadyReplicas = %d, want 2",
						updatedMultiPooler.Status.ReadyReplicas,
					)
				}
				if len(updatedMultiPooler.Status.Conditions) == 0 {
					t.Error("Status.Conditions should not be empty")
				} else {
					readyCondition := updatedMultiPooler.Status.Conditions[0]
					if readyCondition.Type != "Ready" {
						t.Errorf("Condition type = %s, want Ready", readyCondition.Type)
					}
					if readyCondition.Status != metav1.ConditionTrue {
						t.Errorf("Condition status = %s, want True", readyCondition.Status)
					}
				}

				if !slices.Contains(updatedMultiPooler.Finalizers, finalizerName) {
					t.Errorf("Finalizer should be present")
				}
			},
		},
		////----------------------------------------
		///   Error
		//------------------------------------------
		"error on StatefulSet create": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: func(obj client.Object) error {
					if _, ok := obj.(*appsv1.StatefulSet); ok {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on headless Service create": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok &&
						svc.Name == "test-multipooler-headless" {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on status update": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnStatusUpdate: testutil.FailOnObjectName("test-multipooler", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error on Get MultiPooler": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName("test-multipooler", testutil.ErrNetworkTimeout),
			},
			wantErr: true,
		},
		"error on finalizer Update": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-multipooler", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error on StatefulSet Update": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-multipooler",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{
					Replicas: int32Ptr(5),
				},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multipooler",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: int32Ptr(1),
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: func(obj client.Object) error {
					if _, ok := obj.(*appsv1.StatefulSet); ok {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on headless Service Update": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-multipooler",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multipooler",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multipooler-headless",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok &&
						svc.Name == "test-multipooler-headless" {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Get StatefulSet in updateStatus": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-multipooler-status",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multipooler-status",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				// Fail StatefulSet Get after first successful call
				// First Get succeeds (in reconcileStatefulSet)
				// Second Get fails (in updateStatus)
				OnGet: testutil.FailKeyAfterNCalls(1, testutil.ErrNetworkTimeout),
			},
			wantErr: true,
		},
		"error on Get StatefulSet (not NotFound)": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-multipooler",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					// Fail StatefulSet Get with non-NotFound error
					if key.Name == "test-multipooler" {
						return testutil.ErrNetworkTimeout
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Get headless Service (not NotFound)": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-multipooler",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-multipooler",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					// Fail headless Service Get with non-NotFound error
					if key.Name == "test-multipooler-headless" {
						return testutil.ErrNetworkTimeout
					}
					return nil
				},
			},
			wantErr: true,
		},
		"deletion error on finalizer removal": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-multipooler-del",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					Finalizers:        []string{finalizerName},
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.MultiPooler{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-multipooler-del",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{finalizerName},
					},
					Spec: multigresv1alpha1.MultiPoolerSpec{},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-multipooler-del", testutil.ErrInjected),
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
				WithStatusSubresource(&multigresv1alpha1.MultiPooler{}).
				Build()

			fakeClient := client.Client(baseClient)
			// Wrap with failure injection if configured
			if tc.failureConfig != nil {
				fakeClient = testutil.NewFakeClientWithFailures(baseClient, tc.failureConfig)
			}

			reconciler := &MultiPoolerReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			// Create the MultiPooler resource if not in existing objects
			multipoolerInExisting := false
			for _, obj := range tc.existingObjects {
				if mp, ok := obj.(*multigresv1alpha1.MultiPooler); ok &&
					mp.Name == tc.multipooler.Name {
					multipoolerInExisting = true
					break
				}
			}
			if !multipoolerInExisting {
				err := fakeClient.Create(t.Context(), tc.multipooler)
				if err != nil {
					t.Fatalf("Failed to create MultiPooler: %v", err)
				}
			}

			// Reconcile
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tc.multipooler.Name,
					Namespace: tc.multipooler.Namespace,
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
				tc.assertFunc(t, fakeClient, tc.multipooler)
			}
		})
	}
}

func TestMultiPoolerReconciler_ReconcileNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	reconciler := &MultiPoolerReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// Reconcile non-existent resource
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "nonexistent-multipooler",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(t.Context(), req)
	if err != nil {
		t.Errorf("Reconcile() should not error on NotFound, got: %v", err)
	}
	if result.Requeue {
		t.Errorf("Reconcile() should not requeue on NotFound")
	}
}

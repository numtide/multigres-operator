package etcd

import (
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

func TestEtcdReconciler_Reconcile(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := map[string]struct {
		etcd            *multigresv1alpha1.Etcd
		existingObjects []client.Object
		failureConfig   *testutil.FailureConfig
		// TODO: If wantErr is false but failureConfig is set, assertions may fail
		// due to failure injection. This should be addressed when we need to test
		// partial failures that don't prevent reconciliation success.
		wantErr     bool
		wantRequeue bool
		assertFunc  func(t *testing.T, c client.Client, etcd *multigresv1alpha1.Etcd)
	}{
		////----------------------------------------
		///   Success
		//------------------------------------------
		"create all resources for new Etcd": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			existingObjects: []client.Object{},
			assertFunc: func(t *testing.T, c client.Client, etcd *multigresv1alpha1.Etcd) {
				// Verify all three resources were created
				sts := &appsv1.StatefulSet{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-etcd", Namespace: "default"},
					sts); err != nil {
					t.Errorf("StatefulSet should exist: %v", err)
				}

				headlessSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-etcd-headless", Namespace: "default"},
					headlessSvc); err != nil {
					t.Errorf("Headless Service should exist: %v", err)
				}

				clientSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-etcd", Namespace: "default"},
					clientSvc); err != nil {
					t.Errorf("Client Service should exist: %v", err)
				}

				// Verify default values were applied
				// Note: Only checking replicas here - full resource validation is in statefulset_test.go
				if *sts.Spec.Replicas != DefaultReplicas {
					t.Errorf("StatefulSet replicas = %d, want default %d", *sts.Spec.Replicas, DefaultReplicas)
				}
			},
		},
		"update existing resources": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "existing-etcd",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.EtcdSpec{
					Replicas: int32Ptr(5),
					Image:    "quay.io/coreos/etcd:v3.5.15",
				},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-etcd",
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
						Name:      "existing-etcd-headless",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-etcd",
						Namespace: "default",
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, etcd *multigresv1alpha1.Etcd) {
				sts := &appsv1.StatefulSet{}
				err := c.Get(t.Context(), types.NamespacedName{
					Name:      "existing-etcd",
					Namespace: "default",
				}, sts)
				if err != nil {
					t.Fatalf("Failed to get StatefulSet: %v", err)
				}

				if *sts.Spec.Replicas != 5 {
					t.Errorf("StatefulSet replicas = %d, want 5", *sts.Spec.Replicas)
				}

				if sts.Spec.Template.Spec.Containers[0].Image != "quay.io/coreos/etcd:v3.5.15" {
					t.Errorf("StatefulSet image = %s, want quay.io/coreos/etcd:v3.5.15", sts.Spec.Template.Spec.Containers[0].Image)
				}
			},
		},
		"etcd with cellName": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "etcd-zone1",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.EtcdSpec{
					CellName: "zone1",
				},
			},
			existingObjects: []client.Object{},
			assertFunc: func(t *testing.T, c client.Client, etcd *multigresv1alpha1.Etcd) {
				sts := &appsv1.StatefulSet{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "etcd-zone1", Namespace: "default"},
					sts); err != nil {
					t.Fatalf("Failed to get StatefulSet: %v", err)
				}
				if sts.Labels["multigres.com/cell"] != "zone1" {
					t.Errorf("StatefulSet cell label = %s, want zone1", sts.Labels["multigres.com/cell"])
				}

				headlessSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "etcd-zone1-headless", Namespace: "default"},
					headlessSvc); err != nil {
					t.Fatalf("Failed to get headless Service: %v", err)
				}
				if headlessSvc.Labels["multigres.com/cell"] != "zone1" {
					t.Errorf("Headless Service cell label = %s, want zone1", headlessSvc.Labels["multigres.com/cell"])
				}

				clientSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "etcd-zone1", Namespace: "default"},
					clientSvc); err != nil {
					t.Fatalf("Failed to get client Service: %v", err)
				}
				if clientSvc.Labels["multigres.com/cell"] != "zone1" {
					t.Errorf("Client Service cell label = %s, want zone1", clientSvc.Labels["multigres.com/cell"])
				}
			},
		},
		"deletion with finalizer": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-etcd-deletion",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					Finalizers:        []string{finalizerName},
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.Etcd{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-etcd-deletion",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{finalizerName},
					},
					Spec: multigresv1alpha1.EtcdSpec{},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, etcd *multigresv1alpha1.Etcd) {
				updatedEtcd := &multigresv1alpha1.Etcd{}
				err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-etcd-deletion", Namespace: "default"},
					updatedEtcd)
				if err == nil {
					t.Errorf("Etcd object should be deleted but still exists (finalizers: %v)", updatedEtcd.Finalizers)
				}
			},
		},
		"all replicas ready status": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-etcd-ready",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.EtcdSpec{
					Replicas: int32Ptr(3),
				},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-etcd-ready",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: int32Ptr(3),
					},
					Status: appsv1.StatefulSetStatus{
						Replicas:      3,
						ReadyReplicas: 3,
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, etcd *multigresv1alpha1.Etcd) {
				updatedEtcd := &multigresv1alpha1.Etcd{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-etcd-ready", Namespace: "default"},
					updatedEtcd); err != nil {
					t.Fatalf("Failed to get Etcd: %v", err)
				}

				if !updatedEtcd.Status.Ready {
					t.Error("Status.Ready should be true")
				}
				if updatedEtcd.Status.Replicas != 3 {
					t.Errorf("Status.Replicas = %d, want 3", updatedEtcd.Status.Replicas)
				}
				if updatedEtcd.Status.ReadyReplicas != 3 {
					t.Errorf("Status.ReadyReplicas = %d, want 3", updatedEtcd.Status.ReadyReplicas)
				}
				if len(updatedEtcd.Status.Conditions) == 0 {
					t.Error("Status.Conditions should not be empty")
				} else {
					readyCondition := updatedEtcd.Status.Conditions[0]
					if readyCondition.Type != "Ready" {
						t.Errorf("Condition type = %s, want Ready", readyCondition.Type)
					}
					if readyCondition.Status != metav1.ConditionTrue {
						t.Errorf("Condition status = %s, want True", readyCondition.Status)
					}
				}
			},
		},
		////----------------------------------------
		///   Error
		//------------------------------------------
		"error on StatefulSet create": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.EtcdSpec{},
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
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok && svc.Name == "test-etcd-headless" {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on client Service create": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnCreate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok && svc.Name == "test-etcd" {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on status update": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnStatusUpdate: testutil.FailOnObjectName("test-etcd", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error on Get Etcd": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName("test-etcd", testutil.ErrNetworkTimeout),
			},
			wantErr: true,
		},
		"error on finalizer Update": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-etcd", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error on StatefulSet Update": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-etcd",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.EtcdSpec{
					Replicas: int32Ptr(5),
				},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-etcd",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: int32Ptr(3),
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
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-etcd",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-etcd",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-etcd-headless",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok && svc.Name == "test-etcd-headless" {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on client Service Update": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-etcd",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-etcd",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-etcd-headless",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-etcd",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok && svc.Name == "test-etcd" {
						return testutil.ErrInjected
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Get StatefulSet in updateStatus": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-etcd-status",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-etcd-status",
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
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-etcd",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					// Fail StatefulSet Get with non-NotFound error
					if key.Name == "test-etcd" {
						return testutil.ErrNetworkTimeout
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Get headless Service (not NotFound)": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-etcd",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-etcd",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					// Fail headless Service Get with non-NotFound error
					if key.Name == "test-etcd-headless" {
						return testutil.ErrNetworkTimeout
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on Get client Service (not NotFound)": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-etcd-svc",
					Namespace:  "default",
					Finalizers: []string{finalizerName},
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-etcd-svc",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-etcd-svc-headless",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnNamespacedKeyName("test-etcd-svc", "default", testutil.ErrNetworkTimeout),
			},
			wantErr: true,
		},
		"deletion error on finalizer removal": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-etcd-del",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					Finalizers:        []string{finalizerName},
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.Etcd{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-etcd-del",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{finalizerName},
					},
					Spec: multigresv1alpha1.EtcdSpec{},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-etcd-del", testutil.ErrInjected),
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
				WithStatusSubresource(&multigresv1alpha1.Etcd{}).
				Build()

			fakeClient := client.Client(baseClient)
			// Wrap with failure injection if configured
			if tc.failureConfig != nil {
				fakeClient = testutil.NewFakeClientWithFailures(baseClient, tc.failureConfig)
			}

			reconciler := &EtcdReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			// Create the Etcd resource if not in existing objects
			etcdInExisting := false
			for _, obj := range tc.existingObjects {
				if etcd, ok := obj.(*multigresv1alpha1.Etcd); ok && etcd.Name == tc.etcd.Name {
					etcdInExisting = true
					break
				}
			}
			if !etcdInExisting {
				err := fakeClient.Create(t.Context(), tc.etcd)
				if err != nil {
					t.Fatalf("Failed to create Etcd: %v", err)
				}
			}

			// Reconcile
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tc.etcd.Name,
					Namespace: tc.etcd.Namespace,
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
				tc.assertFunc(t, fakeClient, tc.etcd)
			}
		})
	}
}

func TestEtcdReconciler_ReconcileNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	reconciler := &EtcdReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// Reconcile non-existent resource
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "nonexistent-etcd",
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

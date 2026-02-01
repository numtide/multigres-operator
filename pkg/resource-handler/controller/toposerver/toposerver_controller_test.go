package toposerver

import (
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

func TestTopoServerReconciler_Reconcile(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := map[string]struct {
		toposerver      *multigresv1alpha1.TopoServer
		existingObjects []client.Object
		failureConfig   *testutil.FailureConfig
		// TODO: If wantErr is false but failureConfig is set, assertions may fail
		// due to failure injection. This should be addressed when we need to test
		// partial failures that don't prevent reconciliation success.
		wantErr     bool
		wantRequeue bool
		assertFunc  func(t *testing.T, c client.Client, toposerver *multigresv1alpha1.TopoServer)
	}{
		////----------------------------------------
		///   Success
		//------------------------------------------
		"create all resources for new TopoServer": {
			toposerver: &multigresv1alpha1.TopoServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toposerver",
					Namespace: "default",
					Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.TopoServerSpec{},
			},
			existingObjects: []client.Object{},
			assertFunc: func(t *testing.T, c client.Client, toposerver *multigresv1alpha1.TopoServer) {
				// Verify all three resources were created
				sts := &appsv1.StatefulSet{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-toposerver", Namespace: "default"},
					sts); err != nil {
					t.Errorf("StatefulSet should exist: %v", err)
				}

				headlessSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-toposerver-headless", Namespace: "default"},
					headlessSvc); err != nil {
					t.Errorf("Headless Service should exist: %v", err)
				}

				clientSvc := &corev1.Service{}
				if err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-toposerver", Namespace: "default"},
					clientSvc); err != nil {
					t.Errorf("Client Service should exist: %v", err)
				}

				// Verify defaults and finalizer
				if *sts.Spec.Replicas != int32(3) {
					t.Errorf(
						"StatefulSet replicas = %d, want %d",
						*sts.Spec.Replicas,
						int32(3),
					)
				}

				updatedTopoServer := &multigresv1alpha1.TopoServer{}
				if err := c.Get(t.Context(), types.NamespacedName{Name: "test-toposerver", Namespace: "default"}, updatedTopoServer); err != nil {
					t.Fatalf("Failed to get TopoServer: %v", err)
				}
				if !slices.Contains(
					updatedTopoServer.Finalizers,
					"toposerver.multigres.com/finalizer",
				) {
					t.Errorf("Finalizer should be added")
				}
			},
		},
		"update existing resources": {
			toposerver: &multigresv1alpha1.TopoServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "existing-toposerver",
					Namespace:  "default",
					Finalizers: []string{"toposerver.multigres.com/finalizer"},
					Labels:     map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.TopoServerSpec{
					Etcd: &multigresv1alpha1.EtcdSpec{
						Replicas: ptr.To(int32(5)),
						Image:    "quay.io/coreos/etcd:v3.5.15",
					},
				},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-toposerver",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: ptr.To(int32(3)), // will be updated to 5
					},
					Status: appsv1.StatefulSetStatus{
						Replicas:      3,
						ReadyReplicas: 3,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-toposerver-headless",
						Namespace: "default",
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-toposerver",
						Namespace: "default",
					},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, toposerver *multigresv1alpha1.TopoServer) {
				sts := &appsv1.StatefulSet{}
				err := c.Get(t.Context(), types.NamespacedName{
					Name:      "existing-toposerver",
					Namespace: "default",
				}, sts)
				if err != nil {
					t.Fatalf("Failed to get StatefulSet: %v", err)
				}

				if *sts.Spec.Replicas != 5 {
					t.Errorf("StatefulSet replicas = %d, want 5", *sts.Spec.Replicas)
				}

				if sts.Spec.Template.Spec.Containers[0].Image != "quay.io/coreos/etcd:v3.5.15" {
					t.Errorf(
						"StatefulSet image = %s, want quay.io/coreos/etcd:v3.5.15",
						sts.Spec.Template.Spec.Containers[0].Image,
					)
				}
			},
		},
		"deletion with finalizer": {
			toposerver: &multigresv1alpha1.TopoServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-toposerver-deletion",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					Finalizers:        []string{"toposerver.multigres.com/finalizer"},
					Labels:            map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.TopoServerSpec{},
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.TopoServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-toposerver-deletion",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{"toposerver.multigres.com/finalizer"},
						Labels: map[string]string{
							"multigres.com/cluster": "test-cluster",
						},
					},
					Spec: multigresv1alpha1.TopoServerSpec{},
				},
			},
			assertFunc: func(t *testing.T, c client.Client, toposerver *multigresv1alpha1.TopoServer) {
				updatedTopoServer := &multigresv1alpha1.TopoServer{}
				err := c.Get(t.Context(),
					types.NamespacedName{Name: "test-toposerver-deletion", Namespace: "default"},
					updatedTopoServer)
				if err == nil {
					t.Errorf(
						"TopoServer object should be deleted but still exists (finalizers: %v)",
						updatedTopoServer.Finalizers,
					)
				}
			},
		},

		////----------------------------------------
		///   Error
		//------------------------------------------
		"error on status update": {
			toposerver: &multigresv1alpha1.TopoServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toposerver",
					Namespace: "default",
					Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.TopoServerSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnStatusUpdate: testutil.FailOnObjectName("test-toposerver", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error on Get StatefulSet in updateStatus (network error)": {
			toposerver: &multigresv1alpha1.TopoServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-toposerver-status",
					Namespace:  "default",
					Finalizers: []string{"toposerver.multigres.com/finalizer"},
					Labels:     map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.TopoServerSpec{},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-toposerver-status",
						Namespace: "default",
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				// Fail StatefulSet Get in updateStatus.
				// No Gets in reconcile loop with SSA.
				OnGet: testutil.FailKeyAfterNCalls(0, testutil.ErrNetworkTimeout),
			},
			wantErr: true,
		},
		"error on client Service patch": {
			toposerver: &multigresv1alpha1.TopoServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toposerver",
					Namespace: "default",
					Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.TopoServerSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnPatch: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok && svc.Name == "test-toposerver" {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on headless Service patch": {
			toposerver: &multigresv1alpha1.TopoServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toposerver",
					Namespace: "default",
					Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.TopoServerSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnPatch: func(obj client.Object) error {
					if svc, ok := obj.(*corev1.Service); ok &&
						svc.Name == "test-toposerver-headless" {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on StatefulSet patch": {
			toposerver: &multigresv1alpha1.TopoServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toposerver",
					Namespace: "default",
					Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.TopoServerSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnPatch: func(obj client.Object) error {
					if _, ok := obj.(*appsv1.StatefulSet); ok {
						return testutil.ErrPermissionError
					}
					return nil
				},
			},
			wantErr: true,
		},
		"error on finalizer Update": {
			toposerver: &multigresv1alpha1.TopoServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toposerver",
					Namespace: "default",
					Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.TopoServerSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-toposerver", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"deletion error on finalizer removal": {
			toposerver: &multigresv1alpha1.TopoServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-toposerver-del",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					Finalizers:        []string{"toposerver.multigres.com/finalizer"},
					Labels:            map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.TopoServerSpec{},
			},
			existingObjects: []client.Object{
				&multigresv1alpha1.TopoServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-toposerver-del",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{"toposerver.multigres.com/finalizer"},
						Labels: map[string]string{
							"multigres.com/cluster": "test-cluster",
						},
					},
					Spec: multigresv1alpha1.TopoServerSpec{},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName("test-toposerver-del", testutil.ErrInjected),
			},
			wantErr: true,
		},
		"error on Get TopoServer (network error)": {
			toposerver: &multigresv1alpha1.TopoServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toposerver",
					Namespace: "default",
					Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.TopoServerSpec{},
			},
			existingObjects: []client.Object{},
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName("test-toposerver", testutil.ErrNetworkTimeout),
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
				WithStatusSubresource(&multigresv1alpha1.TopoServer{}).
				WithStatusSubresource(&appsv1.StatefulSet{}).
				Build()

			fakeClient := client.Client(baseClient)
			// Wrap with failure injection if configured
			if tc.failureConfig != nil {
				fakeClient = testutil.NewFakeClientWithFailures(baseClient, tc.failureConfig)
			}

			reconciler := &TopoServerReconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
			}

			// Create the TopoServer resource if not in existing objects
			toposerverInExisting := false
			for _, obj := range tc.existingObjects {
				if toposerver, ok := obj.(*multigresv1alpha1.TopoServer); ok &&
					toposerver.Name == tc.toposerver.Name {
					toposerverInExisting = true
					break
				}
			}
			if !toposerverInExisting {
				err := fakeClient.Create(t.Context(), tc.toposerver)
				if err != nil {
					t.Fatalf("Failed to create TopoServer: %v", err)
				}
			}

			// Reconcile
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tc.toposerver.Name,
					Namespace: tc.toposerver.Namespace,
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
				tc.assertFunc(t, fakeClient, tc.toposerver)
			}
		})
	}
}

func TestTopoServerReconciler_ReconcileNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	reconciler := &TopoServerReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
	}

	// Reconcile non-existent resource
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "nonexistent-toposerver",
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

func TestTopoServerReconciler_UpdateStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	t.Run("all_replicas_ready_status", func(t *testing.T) {
		toposerver := &multigresv1alpha1.TopoServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-toposerver-ready",
				Namespace:  "default",
				Finalizers: []string{"toposerver.multigres.com/finalizer"},
				Labels:     map[string]string{"multigres.com/cluster": "test-cluster"},
			},
			Spec: multigresv1alpha1.TopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{
					Replicas: ptr.To(int32(3)),
				},
			},
		}

		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-toposerver-ready",
				Namespace: "default",
				Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: ptr.To(int32(3)),
			},
			Status: appsv1.StatefulSetStatus{
				Replicas:      3,
				ReadyReplicas: 3,
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(toposerver, sts).
			WithStatusSubresource(toposerver, sts).
			Build()

		r := &TopoServerReconciler{
			Client:   fakeClient,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}

		if err := r.updateStatus(t.Context(), toposerver); err != nil {
			t.Fatalf("updateStatus failed: %v", err)
		}

		updatedTopoServer := &multigresv1alpha1.TopoServer{}
		if err := fakeClient.Get(t.Context(), client.ObjectKeyFromObject(toposerver), updatedTopoServer); err != nil {
			t.Fatalf("Failed to get TopoServer: %v", err)
		}

		if len(updatedTopoServer.Status.Conditions) == 0 {
			t.Error("Status.Conditions should not be empty")
		} else {
			readyCondition := updatedTopoServer.Status.Conditions[0]
			if readyCondition.Type != "Ready" {
				t.Errorf("Condition type = %s, want Ready", readyCondition.Type)
			}
			if readyCondition.Status != metav1.ConditionTrue {
				t.Errorf("Condition status = %s, want True", readyCondition.Status)
			}
		}
	})
}

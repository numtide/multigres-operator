package etcd

import (
	"context"
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
)

func TestEtcdReconciler_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := map[string]struct {
		etcd                *multigresv1alpha1.Etcd
		existingObjects     []client.Object
		wantStatefulSet     bool
		wantHeadlessService bool
		wantClientService   bool
		wantFinalizer       bool
		wantErr             bool
	}{
		"create all resources for new Etcd": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			existingObjects:     []client.Object{},
			wantStatefulSet:     true,
			wantHeadlessService: true,
			wantClientService:   true,
			wantFinalizer:       true,
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
				},
			},
			existingObjects: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-etcd",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: int32Ptr(3), // old value
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
			wantStatefulSet:     true,
			wantHeadlessService: true,
			wantClientService:   true,
			wantFinalizer:       true,
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
			existingObjects:     []client.Object{},
			wantStatefulSet:     true,
			wantHeadlessService: true,
			wantClientService:   true,
			wantFinalizer:       true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Create fake client with existing objects
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingObjects...).
				WithStatusSubresource(&multigresv1alpha1.Etcd{}).
				Build()

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
				err := fakeClient.Create(context.Background(), tc.etcd)
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

			_, err := reconciler.Reconcile(context.Background(), req)
			if (err != nil) != tc.wantErr {
				t.Errorf("Reconcile() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if tc.wantErr {
				return
			}

			// Verify StatefulSet was created
			if tc.wantStatefulSet {
				sts := &appsv1.StatefulSet{}
				err := fakeClient.Get(context.Background(), types.NamespacedName{
					Name:      tc.etcd.Name,
					Namespace: tc.etcd.Namespace,
				}, sts)
				if err != nil {
					t.Errorf("Expected StatefulSet to exist, got error: %v", err)
				}
			}

			// Verify headless Service was created
			if tc.wantHeadlessService {
				svc := &corev1.Service{}
				err := fakeClient.Get(context.Background(), types.NamespacedName{
					Name:      tc.etcd.Name + "-headless",
					Namespace: tc.etcd.Namespace,
				}, svc)
				if err != nil {
					t.Errorf("Expected headless Service to exist, got error: %v", err)
				}
			}

			// Verify client Service was created
			if tc.wantClientService {
				svc := &corev1.Service{}
				err := fakeClient.Get(context.Background(), types.NamespacedName{
					Name:      tc.etcd.Name,
					Namespace: tc.etcd.Namespace,
				}, svc)
				if err != nil {
					t.Errorf("Expected client Service to exist, got error: %v", err)
				}
			}

			// Verify finalizer was added
			if tc.wantFinalizer {
				etcd := &multigresv1alpha1.Etcd{}
				err := fakeClient.Get(context.Background(), types.NamespacedName{
					Name:      tc.etcd.Name,
					Namespace: tc.etcd.Namespace,
				}, etcd)
				if err != nil {
					t.Fatalf("Failed to get Etcd: %v", err)
				}
				if !slices.Contains(etcd.Finalizers, finalizerName) {
					t.Errorf("Expected finalizer %s to be present", finalizerName)
				}
			}
		})
	}
}

func TestEtcdReconciler_HandleDeletion(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	now := metav1.Now()

	tests := map[string]struct {
		etcd                 *multigresv1alpha1.Etcd
		wantFinalizerRemoved bool
	}{
		"remove finalizer on deletion": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-etcd",
					Namespace:         "default",
					DeletionTimestamp: &now,
					Finalizers:        []string{finalizerName},
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			wantFinalizerRemoved: true,
		},
		"no finalizer to remove": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-etcd",
					Namespace:         "default",
					DeletionTimestamp: &now,
					Finalizers:        []string{},
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			wantFinalizerRemoved: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.etcd).
				WithStatusSubresource(&multigresv1alpha1.Etcd{}).
				Build()

			reconciler := &EtcdReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tc.etcd.Name,
					Namespace: tc.etcd.Namespace,
				},
			}

			_, err := reconciler.Reconcile(context.Background(), req)
			if err != nil {
				t.Fatalf("Reconcile() unexpected error = %v", err)
			}

			// Verify finalizer state
			etcd := &multigresv1alpha1.Etcd{}
			err = fakeClient.Get(context.Background(), types.NamespacedName{
				Name:      tc.etcd.Name,
				Namespace: tc.etcd.Namespace,
			}, etcd)
			if err != nil {
				t.Fatalf("Failed to get Etcd: %v", err)
			}

			hasFinalizer := slices.Contains(etcd.Finalizers, finalizerName)
			if tc.wantFinalizerRemoved && hasFinalizer {
				t.Errorf("Expected finalizer to be removed, but it's still present")
			}
			if !tc.wantFinalizerRemoved && len(tc.etcd.Finalizers) > 0 && !hasFinalizer && slices.Contains(etcd.Finalizers, finalizerName) {
				t.Errorf("Expected finalizer to be present, but it's removed")
			}
		})
	}
}

func TestEtcdReconciler_ReconcileNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	reconciler := &EtcdReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "nonexistent-etcd",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	if err != nil {
		t.Errorf("Reconcile() should not error on NotFound, got: %v", err)
	}

	if result != (ctrl.Result{}) {
		t.Errorf("Reconcile() should return empty Result on NotFound, got: %v", result)
	}
}

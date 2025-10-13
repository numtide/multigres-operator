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
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/testutil"
)

func TestEtcdReconciler_Reconcile(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := map[string]struct {
		etcd                *multigresv1alpha1.Etcd
		existingObjects     []client.Object
		failureConfig       *testutil.FailureConfig
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

//go:build integration
// +build integration

package testutil_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/testutil"
)

// TestResourceWatcher_BeforeCreation tests that watcher can subscribe to events
// that haven't happened yet (watcher started before resource creation).
func TestResourceWatcher_BeforeCreation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		setup      func(ctx context.Context, c client.Client) error
		assertFunc func(t *testing.T, watcher *testutil.ResourceWatcher)
	}{
		"single service created": {
			setup: func(ctx context.Context, c client.Client) error {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-svc",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 80}},
					},
				}
				return c.Create(ctx, svc)
			},
			assertFunc: func(t *testing.T, watcher *testutil.ResourceWatcher) {
				// Expected object with Kubernetes defaults explicitly set
				expected := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-svc",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Protocol:   corev1.ProtocolTCP, // Kubernetes default
								Port:       80,
								TargetPort: intstr.FromInt(80), // Defaults to Port
							},
						},
						Type:            corev1.ServiceTypeClusterIP, // Kubernetes default
						SessionAffinity: corev1.ServiceAffinityNone,  // Kubernetes default
					},
				}

				// Configure watcher with comparison options
				opts := append(
					testutil.IgnoreMetaRuntimeFields(),
					testutil.IgnoreStatus(),
					testutil.IgnoreServiceRuntimeFields(),
				)
				watcher.SetCmpOpts(opts...)

				err := watcher.WaitForMatch(expected)
				if err != nil {
					t.Errorf("Failed to wait for Service: %v", err)
				}
			},
		},
		"statefulset created": {
			setup: func(ctx context.Context, c client.Client) error {
				sts := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: ptr.To(int32(2)),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "test"},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "nginx",
										Image: "nginx:latest",
									},
								},
							},
						},
					},
				}
				return c.Create(ctx, sts)
			},
			assertFunc: func(t *testing.T, watcher *testutil.ResourceWatcher) {
				expected := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: ptr.To(int32(2)),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "test"},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "nginx",
										Image: "nginx:latest",
									},
								},
							},
						},
					},
				}

				opts := append(
					testutil.IgnoreMetaRuntimeFields(),
					testutil.IgnoreStatefulSetRuntimeFields(),
					testutil.IgnorePodSpecDefaults(),
					testutil.IgnoreStatefulSetSpecDefaults(),
				)
				watcher.SetCmpOpts(opts...)

				err := watcher.WaitForMatch(expected)
				if err != nil {
					t.Errorf("Failed to wait for StatefulSet: %v", err)
				}
			},
		},
		"deployment created": {
			setup: func(ctx context.Context, c client.Client) error {
				deploy := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-deploy",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(1)),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "nginx"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "nginx"},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "nginx",
										Image: "nginx:1.25",
									},
								},
							},
						},
					},
				}
				return c.Create(ctx, deploy)
			},
			assertFunc: func(t *testing.T, watcher *testutil.ResourceWatcher) {
				expected := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-deploy",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(1)),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "nginx"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "nginx"},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:            "nginx",
										Image:           "nginx:1.25",
										ImagePullPolicy: corev1.PullIfNotPresent, // Default for non-:latest tag
									},
								},
							},
						},
					},
				}

				opts := append(
					testutil.IgnoreMetaRuntimeFields(),
					testutil.IgnoreDeploymentRuntimeFields(),
					testutil.IgnorePodSpecDefaultsExceptPullPolicy(), // Keep ImagePullPolicy for verification
					testutil.IgnoreDeploymentSpecDefaults(),
				)
				watcher.SetCmpOpts(opts...)

				err := watcher.WaitForMatch(expected)
				if err != nil {
					t.Errorf("Failed to wait for Deployment: %v", err)
				}
			},
		},
		"multiple unwatched kinds fail immediately": {
			setup: func(ctx context.Context, c client.Client) error {
				// No setup needed - we're testing validation before waiting
				return nil
			},
			assertFunc: func(t *testing.T, watcher *testutil.ResourceWatcher) {
				// Try to wait for ConfigMap and Secret which are not being watched
				err := watcher.WaitForMatch(&corev1.ConfigMap{}, &corev1.Secret{})
				if err == nil {
					t.Errorf("Expected error for unwatched kinds, but got nil")
					return
				}

				want := &testutil.ErrUnwatchedKinds{Kinds: []string{"ConfigMap", "Secret"}}
				var got *testutil.ErrUnwatchedKinds
				if !errors.As(err, &got) {
					t.Errorf("Expected ErrUnwatchedKinds, got: %T - %v", err, err)
					return
				}

				if !reflect.DeepEqual(want.Kinds, got.Kinds) {
					t.Errorf("Expected unwatched kinds = %v, got = %v", want.Kinds, got.Kinds)
				}
			},
		},
	}

	for name, tc := range tests {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			ctx := t.Context()
			mgr := testutil.SetUpEnvtestManager(t, scheme)

			watcher := testutil.NewResourceWatcher(t, ctx, mgr)
			c := mgr.GetClient()

			// Start assertion in background FIRST
			done := make(chan error, 1)
			go func() {
				defer close(done)
				tc.assertFunc(t, watcher)
			}()

			// THEN create resources (tests subscription path)
			if err := tc.setup(ctx, c); err != nil {
				t.Fatalf("Setup failed: %v", err)
			}

			// Wait for assertion to complete
			<-done
		})
	}
}

// TestResourceWatcher_AfterCreation tests that watcher correctly handles resources
// that were created before the watcher started, and subsequent updates.
func TestResourceWatcher_AfterCreation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		setup      func(ctx context.Context, c client.Client) error
		update     func(ctx context.Context, c client.Client) error
		assertFunc func(t *testing.T, watcher *testutil.ResourceWatcher)
	}{
		"service updated after watcher starts": {
			setup: func(ctx context.Context, c client.Client) error {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-svc",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 80}},
					},
				}
				return c.Create(ctx, svc)
			},
			update: func(ctx context.Context, c client.Client) error {
				svc := &corev1.Service{}
				if err := c.Get(ctx, client.ObjectKey{
					Name:      "test-svc",
					Namespace: "default",
				}, svc); err != nil {
					return err
				}

				svc.Spec.Ports[0].Port = 8080
				return c.Update(ctx, svc)
			},
			assertFunc: func(t *testing.T, watcher *testutil.ResourceWatcher) {
				// Expected object with Kubernetes defaults and updated port
				expected := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-svc",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Protocol:   corev1.ProtocolTCP, // Kubernetes default
								Port:       8080,               // Updated value
								TargetPort: intstr.FromInt(80), // Original TargetPort preserved
							},
						},
						Type:            corev1.ServiceTypeClusterIP, // Kubernetes default
						SessionAffinity: corev1.ServiceAffinityNone,  // Kubernetes default
					},
				}

				opts := append(
					testutil.IgnoreMetaRuntimeFields(),
					testutil.IgnoreStatus(),
					testutil.IgnoreServiceRuntimeFields(),
				)
				watcher.SetCmpOpts(opts...)

				err := watcher.WaitForMatch(expected)
				if err != nil {
					t.Errorf("Failed to wait for updated Service: %v", err)
				}
			},
		},
		"service deleted after watcher starts": {
			setup: func(ctx context.Context, c client.Client) error {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-svc-delete",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 80}},
					},
				}
				return c.Create(ctx, svc)
			},
			update: func(ctx context.Context, c client.Client) error {
				svc := &corev1.Service{}
				if err := c.Get(ctx, client.ObjectKey{
					Name:      "test-svc-delete",
					Namespace: "default",
				}, svc); err != nil {
					return err
				}
				return c.Delete(ctx, svc)
			},
			assertFunc: func(t *testing.T, watcher *testutil.ResourceWatcher) {
				// Wait specifically for DELETED event
				evt, err := watcher.WaitForEventType("Service", "DELETED")
				if err != nil {
					t.Fatalf("Failed to wait for Service DELETED event: %v", err)
				}

				if evt.Name != "test-svc-delete" {
					t.Errorf("Expected test-svc-delete, got %s", evt.Name)
				}

				t.Logf("Successfully detected Service deletion")
			},
		},
	}

	for name, tc := range tests {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			ctx := context.Background()
			mgr := testutil.SetUpEnvtestManager(t, scheme)
			c := mgr.GetClient()

			// Create resources FIRST
			if err := tc.setup(ctx, c); err != nil {
				t.Fatalf("Setup failed: %v", err)
			}

			// THEN start watcher (won't see initial creation)
			watcher := testutil.NewResourceWatcher(t, ctx, mgr)

			// Start assertion in background
			done := make(chan error, 1)
			go func() {
				defer close(done)
				tc.assertFunc(t, watcher)
			}()

			// Trigger update (this should be picked up by watcher)
			if err := tc.update(ctx, c); err != nil {
				t.Fatalf("Update failed: %v", err)
			}

			// Wait for assertion to complete
			<-done
		})
	}
}

// TestObj tests the generic Obj helper function.
func TestObj(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		obj      client.Object
		expected client.Object
	}{
		"Service": {
			obj: testutil.Obj[corev1.Service]("my-service", "my-namespace"),
			expected: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-service",
					Namespace: "my-namespace",
				},
			},
		},
		"StatefulSet": {
			obj: testutil.Obj[appsv1.StatefulSet]("my-sts", "default"),
			expected: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-sts",
					Namespace: "default",
				},
			},
		},
		"Deployment": {
			obj: testutil.Obj[appsv1.Deployment]("my-deploy", "kube-system"),
			expected: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-deploy",
					Namespace: "kube-system",
				},
			},
		},
		"ConfigMap": {
			obj: testutil.Obj[corev1.ConfigMap]("my-cm", "default"),
			expected: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cm",
					Namespace: "default",
				},
			},
		},
		"Secret": {
			obj: testutil.Obj[corev1.Secret]("my-secret", "default"),
			expected: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-secret",
					Namespace: "default",
				},
			},
		},
		"Pod": {
			obj: testutil.Obj[corev1.Pod]("my-pod", "default"),
			expected: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-pod",
					Namespace: "default",
				},
			},
		},
		"DaemonSet": {
			obj: testutil.Obj[appsv1.DaemonSet]("my-ds", "default"),
			expected: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-ds",
					Namespace: "default",
				},
			},
		},
		"ReplicaSet": {
			obj: testutil.Obj[appsv1.ReplicaSet]("my-rs", "default"),
			expected: &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-rs",
					Namespace: "default",
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.expected, tc.obj); diff != "" {
				t.Errorf("Obj mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestWaitForDeletion tests the WaitForDeletion function.
func TestWaitForDeletion(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		setup      func(ctx context.Context, c client.Client, watcher *testutil.ResourceWatcher) error
		delete     func(ctx context.Context, c client.Client) error
		assertFunc func(t *testing.T, watcher *testutil.ResourceWatcher)
	}{
		"single service deletion": {
			setup: func(ctx context.Context, c client.Client, watcher *testutil.ResourceWatcher) error {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "test-svc", Namespace: "default"},
					Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
				}
				if err := c.Create(ctx, svc); err != nil {
					return err
				}
				// Wait for creation to be observed
				watcher.SetCmpOpts(testutil.IgnoreMetaRuntimeFields(), testutil.IgnoreServiceRuntimeFields())
				return watcher.WaitForMatch(svc)
			},
			delete: func(ctx context.Context, c client.Client) error {
				return c.Delete(ctx, testutil.Obj[corev1.Service]("test-svc", "default"))
			},
			assertFunc: func(t *testing.T, watcher *testutil.ResourceWatcher) {
				err := watcher.WaitForDeletion(testutil.Obj[corev1.Service]("test-svc", "default"))
				if err != nil {
					t.Errorf("Failed to wait for deletion: %v", err)
				}
			},
		},
		"multiple services deletion": {
			setup: func(ctx context.Context, c client.Client, watcher *testutil.ResourceWatcher) error {
				svc1 := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "svc-1", Namespace: "default"},
					Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
				}
				svc2 := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "svc-2", Namespace: "default"},
					Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
				}
				if err := c.Create(ctx, svc1); err != nil {
					return err
				}
				if err := c.Create(ctx, svc2); err != nil {
					return err
				}
				watcher.SetCmpOpts(testutil.IgnoreMetaRuntimeFields(), testutil.IgnoreServiceRuntimeFields())
				return watcher.WaitForMatch(svc1, svc2)
			},
			delete: func(ctx context.Context, c client.Client) error {
				if err := c.Delete(ctx, testutil.Obj[corev1.Service]("svc-1", "default")); err != nil {
					return err
				}
				return c.Delete(ctx, testutil.Obj[corev1.Service]("svc-2", "default"))
			},
			assertFunc: func(t *testing.T, watcher *testutil.ResourceWatcher) {
				err := watcher.WaitForDeletion(
					testutil.Obj[corev1.Service]("svc-1", "default"),
					testutil.Obj[corev1.Service]("svc-2", "default"),
				)
				if err != nil {
					t.Errorf("Failed to wait for multiple deletions: %v", err)
				}
			},
		},
		"mixed resource types deletion": {
			setup: func(ctx context.Context, c client.Client, watcher *testutil.ResourceWatcher) error {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "my-svc", Namespace: "default"},
					Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
				}
				deploy := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "my-deploy", Namespace: "default"},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(1)),
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test"}},
							Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "nginx", Image: "nginx"}}},
						},
					},
				}
				if err := c.Create(ctx, svc); err != nil {
					return err
				}
				if err := c.Create(ctx, deploy); err != nil {
					return err
				}
				watcher.SetCmpOpts(
					testutil.IgnoreMetaRuntimeFields(),
					testutil.IgnoreServiceRuntimeFields(),
					testutil.IgnoreDeploymentRuntimeFields(),
					testutil.IgnoreDeploymentSpecDefaults(),
					testutil.IgnorePodSpecDefaults(),
				)
				return watcher.WaitForMatch(svc, deploy)
			},
			delete: func(ctx context.Context, c client.Client) error {
				if err := c.Delete(ctx, testutil.Obj[corev1.Service]("my-svc", "default")); err != nil {
					return err
				}
				return c.Delete(ctx, testutil.Obj[appsv1.Deployment]("my-deploy", "default"))
			},
			assertFunc: func(t *testing.T, watcher *testutil.ResourceWatcher) {
				err := watcher.WaitForDeletion(
					testutil.Obj[corev1.Service]("my-svc", "default"),
					testutil.Obj[appsv1.Deployment]("my-deploy", "default"),
				)
				if err != nil {
					t.Errorf("Failed to wait for mixed type deletions: %v", err)
				}
			},
		},
	}

	for name, tc := range tests {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			ctx := context.Background()
			mgr := testutil.SetUpEnvtestManager(t, scheme)
			c := mgr.GetClient()
			watcher := testutil.NewResourceWatcher(t, ctx, mgr)

			if err := tc.setup(ctx, c, watcher); err != nil {
				t.Fatalf("Setup failed: %v", err)
			}

			if err := tc.delete(ctx, c); err != nil {
				t.Fatalf("Delete failed: %v", err)
			}

			tc.assertFunc(t, watcher)
		})
	}
}

// TestWaitForDeletion_CascadingDelete tests cascading deletion with garbage collector.
//
// This test is currently skipped because envtest does not support garbage collection.
// Envtest only runs kube-apiserver and etcd, not kube-controller-manager where the
// garbage collector controller actually runs. As a result, cascading deletion via
// owner references does not work in envtest.
//
// To test cascading deletion properly, this test should be moved to a separate
// test suite that uses kind. That will be implemented in a future PR with kind-based
// integration tests.
//
// For now, we test that owner references are set correctly (which is our controller's
// responsibility), and trust that Kubernetes GC will handle the actual deletion
// (which is Kubernetes's responsibility and is well-tested upstream).
func TestWaitForDeletion_CascadingDelete(t *testing.T) {
	t.Skip("Cascading deletion not supported in envtest (requires kube-controller-manager). " +
		"This test will be moved to kind-based integration tests in a future PR. " +
		"See: https://github.com/kubernetes-sigs/controller-runtime/issues/626")

	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ctx := context.Background()
	mgr := testutil.SetUpEnvtestManager(t, scheme)
	c := mgr.GetClient()
	watcher := testutil.NewResourceWatcher(t, ctx, mgr)

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "default"},
		Data:       map[string]string{"key": "value"},
	}
	if err := c.Create(ctx, owner); err != nil {
		t.Fatalf("Failed to create owner: %v", err)
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owned-svc",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         "v1",
				Kind:               "ConfigMap",
				Name:               owner.Name,
				UID:                owner.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}},
		},
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
	}
	if err := c.Create(ctx, svc); err != nil {
		t.Fatalf("Failed to create owned service: %v", err)
	}

	// Wait for service to be created
	watcher.SetCmpOpts(testutil.IgnoreMetaRuntimeFields(), testutil.IgnoreServiceRuntimeFields())
	if err := watcher.WaitForMatch(svc); err != nil {
		t.Fatalf("Failed to wait for service creation: %v", err)
	}

	// Delete owner - should cascade to owned service
	if err := c.Delete(ctx, owner); err != nil {
		t.Fatalf("Failed to delete owner: %v", err)
	}

	// Wait for cascading deletion
	if err := watcher.WaitForDeletion(testutil.Obj[corev1.Service]("owned-svc", "default")); err != nil {
		t.Errorf("Cascading deletion failed: %v", err)
	}
}

// TestResourceWatcherEventUtilities tests event inspection utility functions.
func TestResourceWatcherEventUtilities(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ctx := context.Background()
	mgr := testutil.SetUpEnvtestManager(t, scheme)

	watcher := testutil.NewResourceWatcher(t, ctx, mgr,
		testutil.WithExtraResource(&corev1.ConfigMap{}),
	)

	c := mgr.GetClient()

	// Create multiple resources
	svc1 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-1", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
	}
	svc2 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-2", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
	}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "deploy-1", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "nginx", Image: "nginx"}}},
			},
		},
	}

	if err := c.Create(ctx, svc1); err != nil {
		t.Fatalf("Failed to create svc-1: %v", err)
	}
	if err := c.Create(ctx, svc2); err != nil {
		t.Fatalf("Failed to create svc-2: %v", err)
	}
	if err := c.Create(ctx, deploy); err != nil {
		t.Fatalf("Failed to create deploy-1: %v", err)
	}

	// Wait for all resources
	watcher.SetCmpOpts(
		testutil.IgnoreMetaRuntimeFields(),
		testutil.IgnoreServiceRuntimeFields(),
		testutil.IgnoreDeploymentRuntimeFields(),
		testutil.IgnoreDeploymentSpecDefaults(),
		testutil.IgnorePodSpecDefaults(),
	)
	if err := watcher.WaitForMatch(svc1, svc2, deploy); err != nil {
		t.Fatalf("Failed to wait for resources: %v", err)
	}

	// Test Events() returns all collected events
	events := watcher.Events()
	if len(events) < 3 {
		t.Errorf("Events() = %d events, want at least 3", len(events))
	}

	// Verify Events() contains expected resources
	foundSvc1, foundSvc2, foundDeploy := false, false, false
	for _, evt := range events {
		if evt.Name == "svc-1" && evt.Kind == "Service" && evt.Type == "ADDED" {
			foundSvc1 = true
		}
		if evt.Name == "svc-2" && evt.Kind == "Service" && evt.Type == "ADDED" {
			foundSvc2 = true
		}
		if evt.Name == "deploy-1" && evt.Kind == "Deployment" && evt.Type == "ADDED" {
			foundDeploy = true
		}
	}
	if !foundSvc1 {
		t.Error("Events() should contain ADDED event for svc-1")
	}
	if !foundSvc2 {
		t.Error("Events() should contain ADDED event for svc-2")
	}
	if !foundDeploy {
		t.Error("Events() should contain ADDED event for deploy-1")
	}

	// Test ForKind() filters by resource kind
	svcEvents := watcher.ForKind("Service")
	if len(svcEvents) < 2 {
		t.Errorf("ForKind(Service) = %d events, want at least 2 (svc-1 and svc-2)", len(svcEvents))
	}
	for _, evt := range svcEvents {
		if evt.Kind != "Service" {
			t.Errorf("ForKind(Service) returned event with Kind = %s", evt.Kind)
		}
	}

	deployEvents := watcher.ForKind("Deployment")
	if len(deployEvents) < 1 {
		t.Errorf("ForKind(Deployment) = %d events, want at least 1", len(deployEvents))
	}
	for _, evt := range deployEvents {
		if evt.Kind != "Deployment" {
			t.Errorf("ForKind(Deployment) returned event with Kind = %s", evt.Kind)
		}
	}

	// Test ForName() filters by resource name
	svc2Events := watcher.ForName("svc-2")
	if len(svc2Events) == 0 {
		t.Error("ForName(svc-2) returned no events")
	}
	for _, evt := range svc2Events {
		if evt.Name != "svc-2" {
			t.Errorf("ForName(svc-2) returned event with Name = %s", evt.Name)
		}
	}

	// Test Count() matches Events() length
	count := watcher.Count()
	if count != len(events) {
		t.Errorf("Count() = %d, len(Events()) = %d, should be equal", count, len(events))
	}

	// Test EventCh() provides direct access to event channel
	ch := watcher.EventCh()
	if ch == nil {
		t.Fatal("EventCh() returned nil")
	}

	// Verify EventCh returns same channel on multiple calls
	ch2 := watcher.EventCh()
	if ch != ch2 {
		t.Error("EventCh() should return the same channel instance")
	}

	// Create another resource and verify the event count increases
	// (can't consume from channel as that would interfere with collectEvents)
	countBefore := watcher.Count()

	svc3 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-3", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
	}
	if err := c.Create(ctx, svc3); err != nil {
		t.Fatalf("Failed to create svc-3: %v", err)
	}

	// Wait for event to be collected
	watcher.SetCmpOpts(testutil.IgnoreMetaRuntimeFields(), testutil.IgnoreServiceRuntimeFields())
	if err := watcher.WaitForMatch(svc3); err != nil {
		t.Fatalf("Failed to wait for svc-3: %v", err)
	}

	// Verify event was collected through the channel
	countAfter := watcher.Count()
	if countAfter <= countBefore {
		t.Errorf("Count after creating svc-3: %d, should be > %d (events collected via EventCh)", countAfter, countBefore)
	}

	// Verify the new event is in Events()
	foundSvc3 := false
	for _, evt := range watcher.Events() {
		if evt.Name == "svc-3" && evt.Kind == "Service" && evt.Type == "ADDED" {
			foundSvc3 = true
			break
		}
	}
	if !foundSvc3 {
		t.Error("Events() should contain ADDED event for svc-3 (received via EventCh)")
	}
}

// TestErrUnwatchedKinds_Error tests the Error() method.
func TestErrUnwatchedKinds_Error(t *testing.T) {
	err := &testutil.ErrUnwatchedKinds{
		Kinds: []string{"ConfigMap", "Secret"},
	}

	got := err.Error()
	want := "the following kinds are not being watched by this ResourceWatcher: [ConfigMap Secret]"

	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestResourceWatcher_ContextCancellation tests behavior when context is canceled.
func TestResourceWatcher_ContextCancellation(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	tests := map[string]struct {
		testFunc func(t *testing.T, watcher *testutil.ResourceWatcher) error
	}{
		"WaitForMatch": {
			testFunc: func(t *testing.T, watcher *testutil.ResourceWatcher) error {
				expected := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
					Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
				}
				return watcher.WaitForMatch(expected)
			},
		},
		"WaitForDeletion": {
			testFunc: func(t *testing.T, watcher *testutil.ResourceWatcher) error {
				return watcher.WaitForDeletion(testutil.Obj[corev1.Service]("test", "default"))
			},
		},
		"WaitForEventType": {
			testFunc: func(t *testing.T, watcher *testutil.ResourceWatcher) error {
				_, err := watcher.WaitForEventType("Service", "DELETED")
				return err
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			mgr := testutil.SetUpEnvtestManager(t, scheme)
			watcher := testutil.NewResourceWatcher(t, ctx, mgr)

			cancel() // Cancel context to trigger error paths

			err := tc.testFunc(t, watcher)
			if err == nil {
				t.Error("Function should error when context is canceled")
			}
		})
	}
}

// TestResourceWatcher_Timeouts tests timeout scenarios with 1ms timeout.
func TestResourceWatcher_Timeouts(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	tests := map[string]struct {
		testFunc func(t *testing.T, watcher *testutil.ResourceWatcher) error
	}{
		"WaitForMatch timeout": {
			testFunc: func(t *testing.T, watcher *testutil.ResourceWatcher) error {
				expected := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "nonexistent", Namespace: "default"},
					Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort},
				}
				return watcher.WaitForMatch(expected)
			},
		},
		"WaitForDeletion timeout": {
			testFunc: func(t *testing.T, watcher *testutil.ResourceWatcher) error {
				return watcher.WaitForDeletion(testutil.Obj[corev1.Service]("nonexistent", "default"))
			},
		},
		"WaitForEventType timeout": {
			testFunc: func(t *testing.T, watcher *testutil.ResourceWatcher) error {
				_, err := watcher.WaitForEventType("Service", "DELETED")
				return err
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			mgr := testutil.SetUpEnvtestManager(t, scheme)
			watcher := testutil.NewResourceWatcher(t, ctx, mgr,
				testutil.WithTimeout(1*time.Millisecond), // 1ms timeout
			)

			err := tc.testFunc(t, watcher)
			if err == nil {
				t.Error("Function should timeout")
			}
		})
	}
}

// TestResourceWatcher_UnwatchedKinds tests error for unwatched resource kinds.
func TestResourceWatcher_UnwatchedKinds(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ctx := t.Context()
	mgr := testutil.SetUpEnvtestManager(t, scheme)
	watcher := testutil.NewResourceWatcher(t, ctx, mgr)

	// Try to wait for ConfigMap which is not watched by default
	err := watcher.WaitForMatch(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	})

	if err == nil {
		t.Error("WaitForMatch() should error for unwatched kind")
	}

	var unwatchedErr *testutil.ErrUnwatchedKinds
	if !errors.As(err, &unwatchedErr) {
		t.Errorf("Error should be ErrUnwatchedKinds, got: %T", err)
	}

	if len(unwatchedErr.Kinds) != 1 || unwatchedErr.Kinds[0] != "ConfigMap" {
		t.Errorf("ErrUnwatchedKinds.Kinds = %v, want [ConfigMap]", unwatchedErr.Kinds)
	}
}

//go:build integration
// +build integration

package envtestutil_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/numtide/multigres-operator/pkg/envtestutil"
)

// TestObj tests the generic Obj helper function.
func TestObj(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		obj      client.Object
		expected client.Object
	}{
		"Service": {
			obj: envtestutil.Obj[corev1.Service]("my-service", "my-namespace"),
			expected: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-service",
					Namespace: "my-namespace",
				},
			},
		},
		"StatefulSet": {
			obj: envtestutil.Obj[appsv1.StatefulSet]("my-sts", "default"),
			expected: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-sts",
					Namespace: "default",
				},
			},
		},
		"Deployment": {
			obj: envtestutil.Obj[appsv1.Deployment]("my-deploy", "kube-system"),
			expected: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-deploy",
					Namespace: "kube-system",
				},
			},
		},
		"ConfigMap": {
			obj: envtestutil.Obj[corev1.ConfigMap]("my-cm", "default"),
			expected: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cm",
					Namespace: "default",
				},
			},
		},
		"Secret": {
			obj: envtestutil.Obj[corev1.Secret]("my-secret", "default"),
			expected: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-secret",
					Namespace: "default",
				},
			},
		},
		"Pod": {
			obj: envtestutil.Obj[corev1.Pod]("my-pod", "default"),
			expected: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-pod",
					Namespace: "default",
				},
			},
		},
		"DaemonSet": {
			obj: envtestutil.Obj[appsv1.DaemonSet]("my-ds", "default"),
			expected: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-ds",
					Namespace: "default",
				},
			},
		},
		"ReplicaSet": {
			obj: envtestutil.Obj[appsv1.ReplicaSet]("my-rs", "default"),
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
		setup      func(ctx context.Context, c client.Client, watcher *envtestutil.ResourceWatcher) error
		delete     func(ctx context.Context, c client.Client) error
		assertFunc func(t *testing.T, watcher *envtestutil.ResourceWatcher)
	}{
		"single service deletion": {
			setup: func(ctx context.Context, c client.Client, watcher *envtestutil.ResourceWatcher) error {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-svc",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 80}},
					},
				}
				if err := c.Create(ctx, svc); err != nil {
					return err
				}
				// Wait for creation to be observed
				watcher.SetCmpOpts(
					envtestutil.IgnoreMetaRuntimeFields(),
					envtestutil.IgnoreServiceRuntimeFields(),
				)
				return watcher.WaitForMatch(svc)
			},
			delete: func(ctx context.Context, c client.Client) error {
				return c.Delete(ctx, envtestutil.Obj[corev1.Service]("test-svc", "default"))
			},
			assertFunc: func(t *testing.T, watcher *envtestutil.ResourceWatcher) {
				err := watcher.WaitForDeletion(envtestutil.Obj[corev1.Service]("test-svc", "default"))
				if err != nil {
					t.Errorf("Failed to wait for deletion: %v", err)
				}
			},
		},
		"multiple services deletion": {
			setup: func(ctx context.Context, c client.Client, watcher *envtestutil.ResourceWatcher) error {
				svc1 := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-1",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 80}},
					},
				}
				svc2 := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-2",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 80}},
					},
				}
				if err := c.Create(ctx, svc1); err != nil {
					return err
				}
				if err := c.Create(ctx, svc2); err != nil {
					return err
				}
				watcher.SetCmpOpts(
					envtestutil.IgnoreMetaRuntimeFields(),
					envtestutil.IgnoreServiceRuntimeFields(),
				)
				return watcher.WaitForMatch(svc1, svc2)
			},
			delete: func(ctx context.Context, c client.Client) error {
				if err := c.Delete(ctx, envtestutil.Obj[corev1.Service]("svc-1", "default")); err != nil {
					return err
				}
				return c.Delete(ctx, envtestutil.Obj[corev1.Service]("svc-2", "default"))
			},
			assertFunc: func(t *testing.T, watcher *envtestutil.ResourceWatcher) {
				err := watcher.WaitForDeletion(
					envtestutil.Obj[corev1.Service]("svc-1", "default"),
					envtestutil.Obj[corev1.Service]("svc-2", "default"),
				)
				if err != nil {
					t.Errorf("Failed to wait for multiple deletions: %v", err)
				}
			},
		},
		"mixed resource types deletion": {
			setup: func(ctx context.Context, c client.Client, watcher *envtestutil.ResourceWatcher) error {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-svc",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 80}},
					},
				}
				deploy := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-deploy",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(1)),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "test"},
							},
							Spec: corev1.PodSpec{Containers: []corev1.Container{
								{Name: "nginx", Image: "nginx"},
							}},
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
					envtestutil.IgnoreMetaRuntimeFields(),
					envtestutil.IgnoreServiceRuntimeFields(),
					envtestutil.IgnoreDeploymentRuntimeFields(),
					envtestutil.IgnoreDeploymentSpecDefaults(),
					envtestutil.IgnorePodSpecDefaults(),
				)

				// Make sure that the resource is created beforehand.
				return watcher.WaitForMatch(svc, deploy)
			},
			delete: func(ctx context.Context, c client.Client) error {
				if err := c.Delete(ctx, envtestutil.Obj[corev1.Service]("my-svc", "default")); err != nil {
					return err
				}
				return c.Delete(ctx, envtestutil.Obj[appsv1.Deployment]("my-deploy", "default"))
			},
			assertFunc: func(t *testing.T, watcher *envtestutil.ResourceWatcher) {
				err := watcher.WaitForDeletion(
					envtestutil.Obj[corev1.Service]("my-svc", "default"),
					envtestutil.Obj[appsv1.Deployment]("my-deploy", "default"),
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
			mgr := envtestutil.SetUpEnvtestManager(t, scheme)
			c := mgr.GetClient()
			watcher := envtestutil.NewResourceWatcher(t, ctx, mgr)

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

// TestWaitForDeletion_ExistingDeletedEvent tests WaitForDeletion finding
// existing DELETED event.
func TestWaitForDeletion_ExistingDeletedEvent(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ctx := context.Background()
	mgr := envtestutil.SetUpEnvtestManager(t, scheme)
	c := mgr.GetClient()

	watcher := envtestutil.NewResourceWatcher(t, ctx, mgr)

	// Create and delete a service
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "to-delete", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
	}
	if err := c.Create(ctx, svc); err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}

	// Wait for creation
	watcher.SetCmpOpts(envtestutil.IgnoreMetaRuntimeFields(), envtestutil.IgnoreServiceRuntimeFields())
	if err := watcher.WaitForMatch(svc); err != nil {
		t.Fatalf("Failed to wait for Service creation: %v", err)
	}

	// Delete it
	if err := c.Delete(ctx, svc); err != nil {
		t.Fatalf("Failed to delete Service: %v", err)
	}

	// Wait for deletion event
	evt, err := watcher.WaitForEventType("Service", "DELETED")
	if err != nil {
		t.Fatalf("WaitForEventType() error = %v", err)
	}

	// Now wait for deletion again - should find existing event
	err = watcher.WaitForDeletion(envtestutil.Obj[corev1.Service]("to-delete", "default"))
	if err != nil {
		t.Errorf("WaitForDeletion() error = %v, want nil (should find existing event)", err)
	}

	t.Logf("Successfully found existing DELETED event: %+v", evt)
}

// TestWaitForDeletion_ContextCanceled tests WaitForDeletion when context is
// canceled.
func TestWaitForDeletion_ContextCanceled(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ctx, cancel := context.WithCancel(t.Context())
	mgr := envtestutil.SetUpEnvtestManager(t, scheme)

	watcher := envtestutil.NewResourceWatcher(t, ctx, mgr)

	// Cancel context immediately
	cancel()

	// Try to wait for deletion - should fail with watcher stopped
	err := watcher.WaitForDeletion(envtestutil.Obj[corev1.Service]("test", "default"))
	if err == nil {
		t.Error("Expected error when context is canceled")
	}

	t.Logf("Got expected error: %v", err)
}

// TestWaitForDeletion_CascadingDelete tests cascading deletion with garbage
// collector.
//
// This test is currently skipped because envtest does not support garbage
// collection. Envtest only runs kube-apiserver and etcd, not
// kube-controller-manager where the garbage collector controller actually runs.
// As a result, cascading deletion via owner references does not work in
// envtest.
//
// To test cascading deletion properly, this test should be moved to a separate
// test suite that uses kind. That will be implemented in a future PR with
// kind-based integration tests.
//
// For now, we test that owner references are set correctly (which is our
// controller's responsibility), and trust that Kubernetes GC will handle the
// actual deletion (which is Kubernetes's responsibility and is well-tested
// upstream).
func TestWaitForDeletion_CascadingDelete(t *testing.T) {
	t.Skip("Cascading deletion not supported in envtest (requires kube-controller-manager). " +
		"This test will be moved to kind-based integration tests in a future PR. " +
		"See: https://github.com/kubernetes-sigs/controller-runtime/issues/626")

	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ctx := context.Background()
	mgr := envtestutil.SetUpEnvtestManager(t, scheme)
	c := mgr.GetClient()
	watcher := envtestutil.NewResourceWatcher(t, ctx, mgr)

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
	watcher.SetCmpOpts(envtestutil.IgnoreMetaRuntimeFields(), envtestutil.IgnoreServiceRuntimeFields())
	if err := watcher.WaitForMatch(svc); err != nil {
		t.Fatalf("Failed to wait for service creation: %v", err)
	}

	// Delete owner - should cascade to owned service
	if err := c.Delete(ctx, owner); err != nil {
		t.Fatalf("Failed to delete owner: %v", err)
	}

	// Wait for cascading deletion
	if err := watcher.WaitForDeletion(envtestutil.Obj[corev1.Service]("owned-svc", "default")); err != nil {
		t.Errorf("Cascading deletion failed: %v", err)
	}
}

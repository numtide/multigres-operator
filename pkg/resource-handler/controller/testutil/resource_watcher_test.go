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
				evt, err := watcher.WaitForEventType("Service", "DELETED", 5*time.Second)
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

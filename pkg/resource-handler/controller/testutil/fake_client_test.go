package testutil

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestFakeClientWithFailures_Get(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	tests := map[string]struct {
		config  *FailureConfig
		key     client.ObjectKey
		wantErr bool
	}{
		"no failure - get succeeds": {
			config: nil,
			key: client.ObjectKey{
				Name:      "test-pod",
				Namespace: "default",
			},
			wantErr: false,
		},
		"fail on specific name": {
			config: &FailureConfig{
				OnGet: FailOnKeyName("test-pod", ErrInjected),
			},
			key: client.ObjectKey{
				Name:      "test-pod",
				Namespace: "default",
			},
			wantErr: true,
		},
		"no failure on different name": {
			config: &FailureConfig{
				OnGet: FailOnKeyName("other-pod", ErrInjected),
			},
			key: client.ObjectKey{
				Name:      "test-pod",
				Namespace: "default",
			},
			wantErr: false,
		},
		"always fail": {
			config: &FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					return ErrInjected
				},
			},
			key: client.ObjectKey{
				Name:      "test-pod",
				Namespace: "default",
			},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			baseClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pod).
				Build()

			fakeClient := NewFakeClientWithFailures(baseClient, tc.config)

			result := &corev1.Pod{}
			err := fakeClient.Get(context.Background(), tc.key, result)

			if (err != nil) != tc.wantErr {
				t.Errorf("Get() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestFakeClientWithFailures_Create(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := map[string]struct {
		config  *FailureConfig
		obj     *corev1.Pod
		wantErr bool
	}{
		"no failure - create succeeds": {
			config: nil,
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-pod",
					Namespace: "default",
				},
			},
			wantErr: false,
		},
		"fail on specific object name": {
			config: &FailureConfig{
				OnCreate: FailOnObjectName("new-pod", ErrPermissionError),
			},
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-pod",
					Namespace: "default",
				},
			},
			wantErr: true,
		},
		"no failure on different object name": {
			config: &FailureConfig{
				OnCreate: FailOnObjectName("other-pod", ErrPermissionError),
			},
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-pod",
					Namespace: "default",
				},
			},
			wantErr: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			baseClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			fakeClient := NewFakeClientWithFailures(baseClient, tc.config)

			err := fakeClient.Create(context.Background(), tc.obj)

			if (err != nil) != tc.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestFakeClientWithFailures_Update(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	tests := map[string]struct {
		config  *FailureConfig
		wantErr bool
	}{
		"no failure - update succeeds": {
			config:  nil,
			wantErr: false,
		},
		"fail on update": {
			config: &FailureConfig{
				OnUpdate: FailOnObjectName("test-pod", ErrInjected),
			},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			baseClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pod).
				Build()

			fakeClient := NewFakeClientWithFailures(baseClient, tc.config)

			err := fakeClient.Update(context.Background(), pod)

			if (err != nil) != tc.wantErr {
				t.Errorf("Update() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestFakeClientWithFailures_Delete(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	tests := map[string]struct {
		config  *FailureConfig
		wantErr bool
	}{
		"no failure - delete succeeds": {
			config:  nil,
			wantErr: false,
		},
		"fail on delete": {
			config: &FailureConfig{
				OnDelete: FailOnObjectName("test-pod", ErrInjected),
			},
			wantErr: true,
		},
		"fail on namespace": {
			config: &FailureConfig{
				OnDelete: FailOnNamespace("default", ErrPermissionError),
			},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			baseClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pod.DeepCopy()).
				Build()

			fakeClient := NewFakeClientWithFailures(baseClient, tc.config)

			err := fakeClient.Delete(context.Background(), pod)

			if (err != nil) != tc.wantErr {
				t.Errorf("Delete() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestFakeClientWithFailures_StatusUpdate(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	tests := map[string]struct {
		config  *FailureConfig
		wantErr bool
	}{
		"no failure - status update succeeds": {
			config:  nil,
			wantErr: false,
		},
		"fail on status update": {
			config: &FailureConfig{
				OnStatusUpdate: FailOnObjectName("test-pod", ErrInjected),
			},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			baseClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pod).
				WithStatusSubresource(&corev1.Pod{}).
				Build()

			fakeClient := NewFakeClientWithFailures(baseClient, tc.config)

			err := fakeClient.Status().Update(context.Background(), pod)

			if (err != nil) != tc.wantErr {
				t.Errorf("Status().Update() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestFakeClientWithFailures_List(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	tests := map[string]struct {
		config  *FailureConfig
		wantErr bool
	}{
		"no failure - list succeeds": {
			config:  nil,
			wantErr: false,
		},
		"fail on list": {
			config: &FailureConfig{
				OnList: func(list client.ObjectList) error {
					return ErrInjected
				},
			},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			baseClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pod).
				Build()

			fakeClient := NewFakeClientWithFailures(baseClient, tc.config)

			podList := &corev1.PodList{}
			err := fakeClient.List(context.Background(), podList)

			if (err != nil) != tc.wantErr {
				t.Errorf("List() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestFakeClientWithFailures_Patch(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	tests := map[string]struct {
		config  *FailureConfig
		wantErr bool
	}{
		"no failure - patch succeeds": {
			config:  nil,
			wantErr: false,
		},
		"fail on patch": {
			config: &FailureConfig{
				OnPatch: FailOnObjectName("test-pod", ErrInjected),
			},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			baseClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pod.DeepCopy()).
				Build()

			fakeClient := NewFakeClientWithFailures(baseClient, tc.config)

			patch := client.MergeFrom(pod.DeepCopy())
			err := fakeClient.Patch(context.Background(), pod, patch)

			if (err != nil) != tc.wantErr {
				t.Errorf("Patch() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestFakeClientWithFailures_DeleteAllOf(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	tests := map[string]struct {
		config  *FailureConfig
		wantErr bool
	}{
		"no failure - deleteAllOf succeeds": {
			config:  nil,
			wantErr: false,
		},
		"fail on deleteAllOf": {
			config: &FailureConfig{
				OnDeleteAllOf: func(obj client.Object) error {
					return ErrInjected
				},
			},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			baseClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pod).
				Build()

			fakeClient := NewFakeClientWithFailures(baseClient, tc.config)

			err := fakeClient.DeleteAllOf(context.Background(), &corev1.Pod{}, client.InNamespace("default"))

			if (err != nil) != tc.wantErr {
				t.Errorf("DeleteAllOf() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestFakeClientWithFailures_StatusPatch(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	tests := map[string]struct {
		config  *FailureConfig
		wantErr bool
	}{
		"no failure - status patch succeeds": {
			config:  nil,
			wantErr: false,
		},
		"fail on status patch": {
			config: &FailureConfig{
				OnStatusPatch: FailOnObjectName("test-pod", ErrInjected),
			},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			baseClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pod.DeepCopy()).
				WithStatusSubresource(&corev1.Pod{}).
				Build()

			fakeClient := NewFakeClientWithFailures(baseClient, tc.config)

			patch := client.MergeFrom(pod.DeepCopy())
			err := fakeClient.Status().Patch(context.Background(), pod, patch)

			if (err != nil) != tc.wantErr {
				t.Errorf("Status().Patch() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Parallel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	t.Run("FailOnObjectName - matching name", func(t *testing.T) {
		t.Parallel()

		fn := FailOnObjectName("test-pod", ErrInjected)
		err := fn(pod)
		if err != ErrInjected {
			t.Errorf("Expected ErrInjected, got %v", err)
		}
	})

	t.Run("FailOnObjectName - different name", func(t *testing.T) {
		t.Parallel()

		fn := FailOnObjectName("other-pod", ErrInjected)
		err := fn(pod)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
	})

	t.Run("FailOnKeyName - matching name", func(t *testing.T) {
		t.Parallel()

		fn := FailOnKeyName("test-pod", ErrInjected)
		err := fn(client.ObjectKey{Name: "test-pod", Namespace: "default"})
		if err != ErrInjected {
			t.Errorf("Expected ErrInjected, got %v", err)
		}
	})

	t.Run("FailOnKeyName - different name", func(t *testing.T) {
		t.Parallel()

		fn := FailOnKeyName("other-pod", ErrInjected)
		err := fn(client.ObjectKey{Name: "test-pod", Namespace: "default"})
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
	})

	t.Run("FailOnNamespacedKeyName - matching name and namespace", func(t *testing.T) {
		t.Parallel()

		fn := FailOnNamespacedKeyName("test-pod", "default", ErrInjected)
		err := fn(client.ObjectKey{Name: "test-pod", Namespace: "default"})
		if err != ErrInjected {
			t.Errorf("Expected ErrInjected, got %v", err)
		}
	})

	t.Run("FailOnNamespacedKeyName - matching name but different namespace", func(t *testing.T) {
		t.Parallel()

		fn := FailOnNamespacedKeyName("test-pod", "default", ErrInjected)
		err := fn(client.ObjectKey{Name: "test-pod", Namespace: "kube-system"})
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
	})

	t.Run("FailOnNamespacedKeyName - different name but matching namespace", func(t *testing.T) {
		t.Parallel()

		fn := FailOnNamespacedKeyName("test-pod", "default", ErrInjected)
		err := fn(client.ObjectKey{Name: "other-pod", Namespace: "default"})
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
	})

	t.Run("FailOnNamespace - matching namespace", func(t *testing.T) {
		t.Parallel()

		fn := FailOnNamespace("default", ErrInjected)
		err := fn(pod)
		if err != ErrInjected {
			t.Errorf("Expected ErrInjected, got %v", err)
		}
	})

	t.Run("FailOnNamespace - different namespace", func(t *testing.T) {
		t.Parallel()

		fn := FailOnNamespace("other-ns", ErrInjected)
		err := fn(pod)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
	})

	t.Run("FailAfterNCalls", func(t *testing.T) {
		t.Parallel()

		fn := FailAfterNCalls(2, ErrInjected)()

		// First call - should succeed
		if err := fn(nil); err != nil {
			t.Errorf("Call 1: expected no error, got %v", err)
		}

		// Second call - should succeed
		if err := fn(nil); err != nil {
			t.Errorf("Call 2: expected no error, got %v", err)
		}

		// Third call - should fail
		if err := fn(nil); err != ErrInjected {
			t.Errorf("Call 3: expected ErrInjected, got %v", err)
		}

		// Fourth call - should fail
		if err := fn(nil); err != ErrInjected {
			t.Errorf("Call 4: expected ErrInjected, got %v", err)
		}
	})

	t.Run("AlwaysFail with object", func(t *testing.T) {
		t.Parallel()

		fn := AlwaysFail(ErrInjected)
		err := fn(pod)
		if err != ErrInjected {
			t.Errorf("Expected ErrInjected, got %v", err)
		}
	})

	t.Run("AlwaysFail with key", func(t *testing.T) {
		t.Parallel()

		fn := AlwaysFail(ErrNetworkTimeout)
		err := fn(client.ObjectKey{Name: "test", Namespace: "default"})
		if err != ErrNetworkTimeout {
			t.Errorf("Expected ErrNetworkTimeout, got %v", err)
		}
	})
}

func TestHelperFunctions_Panic(t *testing.T) {
	t.Parallel()

	t.Run("FailOnObjectName - panics on nil object", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Errorf("Expected panic when meta.Accessor fails on nil")
			}
		}()

		fn := FailOnObjectName("test", ErrInjected)
		_ = fn(nil) // Should panic
	})

	t.Run("FailOnNamespace - panics on nil object", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Errorf("Expected panic when meta.Accessor fails on nil")
			}
		}()

		fn := FailOnNamespace("default", ErrInjected)
		_ = fn(nil) // Should panic
	})
}

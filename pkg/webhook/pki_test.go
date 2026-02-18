package webhook

import (
	"context"
	"errors"
	"strings"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func pkiScheme(tb testing.TB) *runtime.Scheme {
	tb.Helper()
	s := runtime.NewScheme()
	if err := admissionregistrationv1.AddToScheme(s); err != nil {
		tb.Fatal(err)
	}
	if err := appsv1.AddToScheme(s); err != nil {
		tb.Fatal(err)
	}
	return s
}

func TestPatchWebhookCABundle(t *testing.T) {
	t.Parallel()

	caBundle := []byte("test-ca-bundle")

	t.Run("Patches Both Webhook Configs", func(t *testing.T) {
		t.Parallel()

		mutating := &admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: MutatingWebhookName},
			Webhooks: []admissionregistrationv1.MutatingWebhook{
				{
					Name:                    "wh1.example.com",
					ClientConfig:            admissionregistrationv1.WebhookClientConfig{},
					AdmissionReviewVersions: []string{"v1"},
					SideEffects: func() *admissionregistrationv1.SideEffectClass {
						v := admissionregistrationv1.SideEffectClassNone
						return &v
					}(),
				},
			},
		}
		validating := &admissionregistrationv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: ValidatingWebhookName},
			Webhooks: []admissionregistrationv1.ValidatingWebhook{
				{
					Name:                    "wh2.example.com",
					ClientConfig:            admissionregistrationv1.WebhookClientConfig{},
					AdmissionReviewVersions: []string{"v1"},
					SideEffects: func() *admissionregistrationv1.SideEffectClass {
						v := admissionregistrationv1.SideEffectClassNone
						return &v
					}(),
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(pkiScheme(t)).
			WithObjects(mutating, validating).
			Build()

		if err := PatchWebhookCABundle(context.Background(), cl, caBundle); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify mutating
		got := &admissionregistrationv1.MutatingWebhookConfiguration{}
		if err := cl.Get(context.Background(), client.ObjectKeyFromObject(mutating), got); err != nil {
			t.Fatal(err)
		}
		if string(got.Webhooks[0].ClientConfig.CABundle) != string(caBundle) {
			t.Errorf("mutating CABundle = %q, want %q", got.Webhooks[0].ClientConfig.CABundle, caBundle)
		}

		// Verify validating
		gotV := &admissionregistrationv1.ValidatingWebhookConfiguration{}
		if err := cl.Get(context.Background(), client.ObjectKeyFromObject(validating), gotV); err != nil {
			t.Fatal(err)
		}
		if string(gotV.Webhooks[0].ClientConfig.CABundle) != string(caBundle) {
			t.Errorf("validating CABundle = %q, want %q", gotV.Webhooks[0].ClientConfig.CABundle, caBundle)
		}
	})

	t.Run("Tolerates NotFound", func(t *testing.T) {
		t.Parallel()

		cl := fake.NewClientBuilder().WithScheme(pkiScheme(t)).Build()

		if err := PatchWebhookCABundle(context.Background(), cl, caBundle); err != nil {
			t.Fatalf("expected no error for missing configs, got: %v", err)
		}
	})

	t.Run("Error: Mutating Get Failure", func(t *testing.T) {
		t.Parallel()

		cl := fake.NewClientBuilder().
			WithScheme(pkiScheme(t)).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*admissionregistrationv1.MutatingWebhookConfiguration); ok {
						return errors.New("network error")
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}).
			Build()

		err := PatchWebhookCABundle(context.Background(), cl, caBundle)
		if err == nil || !strings.Contains(err.Error(), "failed to get mutating webhook config") {
			t.Errorf("expected mutating get error, got: %v", err)
		}
	})

	t.Run("Error: Mutating Update Failure", func(t *testing.T) {
		t.Parallel()

		mutating := &admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: MutatingWebhookName},
			Webhooks: []admissionregistrationv1.MutatingWebhook{
				{
					Name:                    "wh.example.com",
					ClientConfig:            admissionregistrationv1.WebhookClientConfig{},
					AdmissionReviewVersions: []string{"v1"},
					SideEffects: func() *admissionregistrationv1.SideEffectClass {
						v := admissionregistrationv1.SideEffectClassNone
						return &v
					}(),
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(pkiScheme(t)).
			WithObjects(mutating).
			WithInterceptorFuncs(interceptor.Funcs{
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					if _, ok := obj.(*admissionregistrationv1.MutatingWebhookConfiguration); ok {
						return errors.New("update fail")
					}
					return c.Update(ctx, obj, opts...)
				},
			}).
			Build()

		err := PatchWebhookCABundle(context.Background(), cl, caBundle)
		if err == nil || !strings.Contains(err.Error(), "failed to update mutating webhook config") {
			t.Errorf("expected update error, got: %v", err)
		}
	})

	t.Run("Error: Validating Get Failure", func(t *testing.T) {
		t.Parallel()

		cl := fake.NewClientBuilder().
			WithScheme(pkiScheme(t)).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*admissionregistrationv1.ValidatingWebhookConfiguration); ok {
						return errors.New("network error")
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}).
			Build()

		err := PatchWebhookCABundle(context.Background(), cl, caBundle)
		if err == nil || !strings.Contains(err.Error(), "failed to get validating webhook config") {
			t.Errorf("expected validating get error, got: %v", err)
		}
	})

	t.Run("Error: Validating Update Failure", func(t *testing.T) {
		t.Parallel()

		validating := &admissionregistrationv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: ValidatingWebhookName},
			Webhooks: []admissionregistrationv1.ValidatingWebhook{
				{
					Name:                    "wh.example.com",
					ClientConfig:            admissionregistrationv1.WebhookClientConfig{},
					AdmissionReviewVersions: []string{"v1"},
					SideEffects: func() *admissionregistrationv1.SideEffectClass {
						v := admissionregistrationv1.SideEffectClassNone
						return &v
					}(),
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(pkiScheme(t)).
			WithObjects(validating).
			WithInterceptorFuncs(interceptor.Funcs{
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					if _, ok := obj.(*admissionregistrationv1.ValidatingWebhookConfiguration); ok {
						return errors.New("update fail")
					}
					return c.Update(ctx, obj, opts...)
				},
			}).
			Build()

		err := PatchWebhookCABundle(context.Background(), cl, caBundle)
		if err == nil || !strings.Contains(err.Error(), "failed to update validating webhook config") {
			t.Errorf("expected update error, got: %v", err)
		}
	})
}

func TestFindOperatorDeployment(t *testing.T) {
	t.Parallel()

	namespace := "test-ns"
	labels := map[string]string{"app.kubernetes.io/name": "multigres-operator"}

	t.Run("Found by Labels", func(t *testing.T) {
		t.Parallel()

		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-operator",
				Namespace: namespace,
				Labels:    labels,
			},
		}

		cl := fake.NewClientBuilder().WithScheme(pkiScheme(t)).WithObjects(dep).Build()
		got, err := FindOperatorDeployment(context.Background(), cl, namespace, labels, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil || got.Name != "my-operator" {
			t.Errorf("expected deployment 'my-operator', got %v", got)
		}
	})

	t.Run("Found by Name", func(t *testing.T) {
		t.Parallel()

		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "explicit-name",
				Namespace: namespace,
			},
		}

		cl := fake.NewClientBuilder().WithScheme(pkiScheme(t)).WithObjects(dep).Build()
		got, err := FindOperatorDeployment(context.Background(), cl, namespace, nil, "explicit-name")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil || got.Name != "explicit-name" {
			t.Errorf("expected deployment 'explicit-name', got %v", got)
		}
	})

	t.Run("Not Found Returns nil", func(t *testing.T) {
		t.Parallel()

		cl := fake.NewClientBuilder().WithScheme(pkiScheme(t)).Build()
		got, err := FindOperatorDeployment(context.Background(), cl, namespace, nil, "nonexistent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("No Labels No Name Returns nil", func(t *testing.T) {
		t.Parallel()

		cl := fake.NewClientBuilder().WithScheme(pkiScheme(t)).Build()
		got, err := FindOperatorDeployment(context.Background(), cl, namespace, nil, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("Error: Multiple Matches", func(t *testing.T) {
		t.Parallel()

		dep1 := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "op1", Namespace: namespace, Labels: labels},
		}
		dep2 := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "op2", Namespace: namespace, Labels: labels},
		}

		cl := fake.NewClientBuilder().WithScheme(pkiScheme(t)).WithObjects(dep1, dep2).Build()
		_, err := FindOperatorDeployment(context.Background(), cl, namespace, labels, "")
		if err == nil || !strings.Contains(err.Error(), "found multiple deployments") {
			t.Errorf("expected multiple match error, got: %v", err)
		}
	})

	t.Run("Error: List Failure", func(t *testing.T) {
		t.Parallel()

		cl := fake.NewClientBuilder().
			WithScheme(pkiScheme(t)).
			WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					return errors.New("list error")
				},
			}).
			Build()

		_, err := FindOperatorDeployment(context.Background(), cl, namespace, labels, "")
		if err == nil || !strings.Contains(err.Error(), "failed to list deployments by labels") {
			t.Errorf("expected list error, got: %v", err)
		}
	})

	t.Run("Error: Get by Name Failure", func(t *testing.T) {
		t.Parallel()

		cl := fake.NewClientBuilder().
			WithScheme(pkiScheme(t)).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					return errors.New("get error")
				},
			}).
			Build()

		_, err := FindOperatorDeployment(context.Background(), cl, namespace, nil, "some-name")
		if err == nil || !strings.Contains(err.Error(), "failed to get operator deployment by name") {
			t.Errorf("expected get error, got: %v", err)
		}
	})
}

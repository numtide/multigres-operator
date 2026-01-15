package cert

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/numtide/multigres-operator/pkg/testutil"
)

const (
	WebhookConfigNameMutating   = "multigres-operator-mutating-webhook-configuration"
	WebhookConfigNameValidating = "multigres-operator-validating-webhook-configuration"
)

func TestManager_EnsureCerts(t *testing.T) {
	t.Parallel()

	const (
		namespace   = "test-ns"
		serviceName = "test-svc"
	)

	expectedDNSName := serviceName + "." + namespace + ".svc"

	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = admissionregistrationv1.AddToScheme(s)

	// Helper to generate a dummy CA and a signed cert
	validCABytes, validCAKeyBytes := generateCAPEM(t)
	ca, _ := ParseCA(validCABytes, validCAKeyBytes)

	validCert := generateSignedCertPEM(
		t,
		ca,
		time.Now().Add(365*24*time.Hour),
		[]string{expectedDNSName},
	)
	expiredCert := generateSignedCertPEM(
		t,
		ca,
		time.Now().Add(-1*time.Hour),
		[]string{expectedDNSName},
	)
	nearExpiryCert := generateSignedCertPEM(
		t,
		ca,
		time.Now().Add(15*24*time.Hour),
		[]string{expectedDNSName},
	)

	otherCA, _ := GenerateCA()
	signedByOtherCACert := generateSignedCertPEM(
		t,
		otherCA,
		time.Now().Add(time.Hour),
		[]string{expectedDNSName},
	)
	signedByOtherCAValidCert := generateSignedCertPEM(
		t,
		otherCA,
		time.Now().Add(365*24*time.Hour),
		[]string{expectedDNSName},
	)

	corruptCertBody := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: []byte("this is not a valid der certificate"),
	})

	baseWebhooks := []client.Object{
		&admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: WebhookConfigNameMutating},
			Webhooks: []admissionregistrationv1.MutatingWebhook{
				{
					ClientConfig: admissionregistrationv1.WebhookClientConfig{
						Service: &admissionregistrationv1.ServiceReference{
							Name:      serviceName,
							Namespace: namespace,
						},
						CABundle: []byte("old-bundle"),
					},
				},
			},
		},
		&admissionregistrationv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: WebhookConfigNameValidating},
			Webhooks: []admissionregistrationv1.ValidatingWebhook{
				{
					ClientConfig: admissionregistrationv1.WebhookClientConfig{
						Service: &admissionregistrationv1.ServiceReference{
							Name:      serviceName,
							Namespace: namespace,
						},
						CABundle: []byte("old-bundle"),
					},
				},
			},
		},
	}

	tests := map[string]struct {
		existingObjects []client.Object
		failureConfig   *testutil.FailureConfig
		customCertDir   string
		customOptions   *Options
		wantErr         bool
		errContains     string
		wantGenerated   bool
		checkFiles      bool
	}{
		"Bootstrap: Fresh Install": {
			existingObjects: baseWebhooks,
			checkFiles:      true,
			wantGenerated:   true,
		},
		"Idempotency: Valid Secret Exists": {
			existingObjects: append([]client.Object{
				// CA Secret
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: CASecretName, Namespace: namespace},
					Data: map[string][]byte{
						"ca.crt": validCABytes,
						"ca.key": validCAKeyBytes,
					},
				},
				// Server Secret
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: ServerSecretName, Namespace: namespace},
					Type:       corev1.SecretTypeTLS,
					Data: map[string][]byte{
						"tls.crt": validCert,
						"tls.key": []byte("key"),
					},
				},
			}, baseWebhooks...),
			checkFiles:    true,
			wantGenerated: false,
		},
		"Rotation: Expired Server Cert": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: CASecretName, Namespace: namespace},
					Data: map[string][]byte{
						"ca.crt": validCABytes,
						"ca.key": validCAKeyBytes,
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: ServerSecretName, Namespace: namespace},
					Data: map[string][]byte{
						"tls.crt": expiredCert,
						"tls.key": []byte("key"),
					},
				},
			}, baseWebhooks...),
			checkFiles:    true,
			wantGenerated: true,
		},
		"Rotation: Near Expiry Server Cert": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: CASecretName, Namespace: namespace},
					Data: map[string][]byte{
						"ca.crt": validCABytes,
						"ca.key": validCAKeyBytes,
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: ServerSecretName, Namespace: namespace},
					Data: map[string][]byte{
						"tls.crt": nearExpiryCert,
						"tls.key": []byte("key"),
					},
				},
			}, baseWebhooks...),
			checkFiles:    true,
			wantGenerated: true,
		},
		"Rotation: Corrupt Cert Body": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: CASecretName, Namespace: namespace},
					Data: map[string][]byte{
						"ca.crt": validCABytes,
						"ca.key": validCAKeyBytes,
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: ServerSecretName, Namespace: namespace},
					Data: map[string][]byte{
						"tls.crt": corruptCertBody,
						"tls.key": []byte("key"),
					},
				},
			}, baseWebhooks...),
			checkFiles:    true,
			wantGenerated: true,
		},
		"Rotation: CA Near Expiry": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: CASecretName, Namespace: namespace},
					Data: map[string][]byte{
						"ca.crt": nearExpiryCert, // Using near expiry cert as CA cert
						"ca.key": validCAKeyBytes,
					},
				},
			}, baseWebhooks...),
			checkFiles:    true,
			wantGenerated: true,
		},
		"Rotation: Signed by Different CA": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: CASecretName, Namespace: namespace},
					Data: map[string][]byte{
						"ca.crt": validCABytes,
						"ca.key": validCAKeyBytes,
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: ServerSecretName, Namespace: namespace},
					Data: map[string][]byte{
						"tls.crt": signedByOtherCACert,
						"tls.key": []byte("key"),
					},
				},
			}, baseWebhooks...),
			checkFiles:    true,
			wantGenerated: true,
		},
		"Error: Get Secret Failed": {
			existingObjects: baseWebhooks,
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName(CASecretName, errors.New("injected get error")),
			},
			wantErr:     true,
			errContains: "injected get error",
		},
		"Error: Create Secret Failed": {
			existingObjects: baseWebhooks,
			failureConfig: &testutil.FailureConfig{
				OnCreate: testutil.FailOnObjectName(
					CASecretName,
					errors.New("injected create error"),
				),
			},
			wantErr:     true,
			errContains: "failed to create CA secret",
		},
		"Error: File System (Mkdir/Write)": {
			existingObjects: baseWebhooks,
			customCertDir:   "/dev/null/invalid-dir",
			wantErr:         true,
			errContains:     "mkdir",
		},
		"Error: Patch Webhooks Failed": {
			existingObjects: baseWebhooks,
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName(
					WebhookConfigNameMutating,
					errors.New("injected patch error"),
				),
			},
			wantErr:     true,
			errContains: "failed to patch webhooks",
		},
		"Error: Update Server Cert Failed": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: CASecretName, Namespace: namespace},
					Data: map[string][]byte{
						"ca.crt": validCABytes,
						"ca.key": validCAKeyBytes,
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: ServerSecretName, Namespace: namespace},
					Data:       map[string][]byte{"tls.crt": expiredCert, "tls.key": []byte("key")},
				},
			}, baseWebhooks...),
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName(
					ServerSecretName,
					errors.New("injected update error"),
				),
			},
			wantErr:     true,
			errContains: "failed to update server cert secret",
		},
		"Error: Delete Failed (Corrupt CA)": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: CASecretName, Namespace: namespace},
					Data: map[string][]byte{
						"ca.crt": []byte("corrupt"),
						"ca.key": []byte("key"),
					},
				},
			}, baseWebhooks...),
			failureConfig: &testutil.FailureConfig{
				OnDelete: testutil.FailOnObjectName(CASecretName, errors.New("delete fail")),
			},
			wantErr:     true,
			errContains: "failed to delete corrupt CA secret",
		},
		"Error: Delete Failed (Expiring CA)": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: CASecretName, Namespace: namespace},
					Data: map[string][]byte{
						"ca.crt": generateSignedCertPEM(
							t,
							ca,
							time.Now().Add(15*24*time.Hour),
							[]string{"ca"},
						),
						"ca.key": validCAKeyBytes,
					},
				},
			}, baseWebhooks...),
			failureConfig: &testutil.FailureConfig{
				OnDelete: testutil.FailOnObjectName(CASecretName, errors.New("delete fail")),
			},
			wantErr:     true,
			errContains: "failed to delete expiring CA secret",
		},
		"Error: Get Validating Webhook Failed": {
			existingObjects: baseWebhooks[:1], // Only mutating
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					if key.Name == "multigres-operator-validating-webhook-configuration" {
						return errors.New("get fail")
					}
					return nil
				},
			},
			wantErr:     true,
			errContains: "failed to get validating webhook config",
		},
		"Error: Update Validating Webhook Failed": {
			existingObjects: baseWebhooks,
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName(
					"multigres-operator-validating-webhook-configuration",
					errors.New("update fail"),
				),
			},
			wantErr:     true,
			errContains: "failed to update validating webhook config",
		},
		"Error: Deployment List Failure": {
			failureConfig: &testutil.FailureConfig{
				OnList: func(list client.ObjectList) error {
					return errors.New("list fail")
				},
			},
			wantErr:     true,
			errContains: "failed to list deployments by labels",
		},
		"Error: Delete Failed (Corrupt Server Cert)": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: ServerSecretName, Namespace: namespace},
					Data: map[string][]byte{
						"tls.crt": []byte("corrupt"),
						"tls.key": []byte("key"),
					},
				},
			}, baseWebhooks...),
			failureConfig: &testutil.FailureConfig{
				OnDelete: testutil.FailOnObjectName(ServerSecretName, errors.New("delete fail")),
			},
			wantErr:     true,
			errContains: "failed to delete corrupt server cert secret",
		},
		"Rotation: Wrong CA (Still Valid)": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: CASecretName, Namespace: namespace},
					Data: map[string][]byte{
						"ca.crt": validCABytes,
						"ca.key": validCAKeyBytes,
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: ServerSecretName, Namespace: namespace},
					Data: map[string][]byte{
						"tls.crt": signedByOtherCAValidCert,
						"tls.key": []byte("key"),
					},
				},
			}, baseWebhooks...),
			checkFiles:    true,
			wantGenerated: true,
		},
		"Rotation: Corrupt Cert Data (No Block)": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: ServerSecretName, Namespace: namespace},
					Data: map[string][]byte{
						"tls.crt": []byte("not pem"),
						"tls.key": []byte("key"),
					},
				},
			}, baseWebhooks...),
			checkFiles:    true,
			wantGenerated: true,
		},
		"Error: Delete Failed (Corrupt Server Cert Data)": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: ServerSecretName, Namespace: namespace},
					Data: map[string][]byte{
						"tls.crt": []byte("not pem"),
						"tls.key": []byte("key"),
					},
				},
			}, baseWebhooks...),
			failureConfig: &testutil.FailureConfig{
				OnDelete: testutil.FailOnObjectName(ServerSecretName, errors.New("delete fail")),
			},
			wantErr:     true,
			errContains: "failed to delete corrupt server cert secret",
		},
		"Error: Delete Failed (Unparseable Server Cert)": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: CASecretName, Namespace: namespace},
					Data: map[string][]byte{
						"ca.crt": validCABytes,
						"ca.key": validCAKeyBytes,
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: ServerSecretName, Namespace: namespace},
					Data: map[string][]byte{
						"tls.crt": corruptCertBody,
						"tls.key": []byte("key"),
					},
				},
			}, baseWebhooks...),
			failureConfig: &testutil.FailureConfig{
				OnDelete: testutil.FailOnObjectName(ServerSecretName, errors.New("delete fail")),
			},
			wantErr:     true,
			errContains: "failed to delete unparseable server cert secret",
		},
		"Error: Controller Ref Failed": {
			existingObjects: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "op",
						Namespace: namespace,
						Labels:    map[string]string{"app": "op"},
						// No UID -> SetControllerReference might fail?
						// Actually fake client might auto-assign UID.
					},
				},
			},
			failureConfig: &testutil.FailureConfig{
				OnGet: func(key client.ObjectKey) error {
					// We want findOperatorDeployment to find it, but SetControllerReference to fail.
					// We can make m.Client.Scheme() return nil in CertRotator if we had control.
					return nil
				},
			},
			wantErr:     true,
			errContains: "failed to set controller reference",
		},
		"Success: Owner Not Found (No-op)": {
			existingObjects: baseWebhooks,
			// No deployment exists, label selector won't match anything.
			// setOwner should return nil (no error).
			wantErr: false,
		},
		"Success: Found by Name": {
			customOptions: &Options{
				OperatorDeployment: "op-by-name",
				// OperatorLabelSelector is nil by default in struct, ensuring we skip label search
			},
			existingObjects: append([]client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "op-by-name",
						Namespace: namespace,
					},
				},
			}, baseWebhooks...),
			checkFiles:    true,
			wantGenerated: true,
		},
		"Success: Not Found by Name": {
			customOptions: &Options{
				OperatorDeployment:    "missing-deployment",
				OperatorLabelSelector: nil, // Clear selector
			},
			wantErr: false,
		},
		"Success: No Webhooks (Skip Patch)": {
			existingObjects: []client.Object{}, // No webhooks
			// Default options find operator logic (mocked elsewhere? or defaults work)
			// Wait, findOperatorDeployment logic requires deployment?
			// If not found, setOwner returns nil.
			// setOwner is called inside ensureCA and ensureServerCert.
			// If owner not found, secret is created without owner ref.
			// This is valid test.
			// patchWebhooks will list webhooks. If none found, get returns NotFound -> skipped.
			// Coverage for "if !errors.IsNotFound(err)" branch?
			// The code:
			// if err := m.Client.Get(..., mutating); err != nil {
			//    if !errors.IsNotFound(err) { return err }
			// }
			// So "NotFound" is the success (skip) path.
			// To cover it, we just need Get to return NotFound.
			// Which happens if objects are missing.
			wantErr: false,
		},
		"Recreation: Corrupt CA Secret": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: CASecretName, Namespace: namespace},
					Data: map[string][]byte{
						"ca.crt": []byte("corrupt-pem-data"),
						"ca.key": validCAKeyBytes,
					},
				},
			}, baseWebhooks...),
			checkFiles:    true,
			wantGenerated: true,
		},
		// ... (previous cases)

		"Error: Update Mutating Webhook Failed": {
			existingObjects: baseWebhooks,
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName(
					WebhookConfigNameMutating,
					errors.New("update fail"),
				),
			},
			wantErr:     true,
			errContains: "failed to update mutating webhook config",
		},
		// ... (previous cases)

		"Error: Server Cert Owner Ref Failed": {
			existingObjects: append([]client.Object{
				// CA exists, so ensureCA succeeds and skips setOwner
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: CASecretName, Namespace: namespace},
					Data: map[string][]byte{
						"ca.crt": validCABytes,
						"ca.key": validCAKeyBytes,
					},
				},
				// Deployment exists but List will fail
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "op",
						Namespace: namespace,
						Labels:    map[string]string{"app": "op"},
					},
				},
			}, baseWebhooks...),
			failureConfig: &testutil.FailureConfig{
				OnList: func(list client.ObjectList) error {
					return errors.New("list fail")
				},
			},
			wantErr:     true,
			errContains: "failed to list deployments", // Error comes from findOperatorDeployment -> setOwner
		},
		// ... (previous cases)

		"Error: Create Server Secret Failed": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: CASecretName, Namespace: namespace},
					Data: map[string][]byte{
						"ca.crt": validCABytes,
						"ca.key": validCAKeyBytes,
					},
				},
			}, baseWebhooks...),
			failureConfig: &testutil.FailureConfig{
				OnCreate: testutil.FailOnObjectName(
					ServerSecretName,
					errors.New("server create fail"),
				),
			},
			wantErr:     true,
			errContains: "failed to create server cert secret",
		},
		"Error: Get Server Secret Failed": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: CASecretName, Namespace: namespace},
					Data: map[string][]byte{
						"ca.crt": validCABytes,
						"ca.key": validCAKeyBytes,
					},
				},
			}, baseWebhooks...),
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName(ServerSecretName, errors.New("server get fail")),
			},
			wantErr:     true,
			errContains: "failed to get server cert secret",
		},
		"Error: Get Mutating Webhook Failed": {
			existingObjects: baseWebhooks,
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName(
					WebhookConfigNameMutating,
					errors.New("mutating get fail"),
				),
			},
			wantErr:     true,
			errContains: "failed to get mutating webhook config",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tc.existingObjects...).
				Build()

			var cl client.Client = fakeClient
			if tc.failureConfig != nil {
				cl = testutil.NewFakeClientWithFailures(fakeClient, tc.failureConfig)
			}

			certDir := tc.customCertDir
			if certDir == "" {
				certDir = t.TempDir()
			}

			// FIX: Pre-populate files for "Existing Secret" scenarios.
			// This prevents waitForKubelet from hanging when no API update occurs.
			for _, obj := range tc.existingObjects {
				if s, ok := obj.(*corev1.Secret); ok && s.Name == ServerSecretName {
					// We only write if the dir is valid to avoid breaking the "File System Error" test
					if _, err := os.Stat(certDir); err == nil {
						_ = os.WriteFile(
							filepath.Join(certDir, CertFileName),
							s.Data["tls.crt"],
							0o644,
						)
						_ = os.WriteFile(
							filepath.Join(certDir, KeyFileName),
							s.Data["tls.key"],
							0o600,
						)
					}
				}
			}

			// Mock Kubelet for *updates* during the test
			hookClient := &mockKubeletClient{
				Client:    cl,
				CertDir:   certDir,
				badScheme: name == "Error: Controller Ref Failed",
			}

			opts := Options{
				Namespace:             namespace,
				CertDir:               certDir,
				ServiceName:           serviceName,
				OperatorLabelSelector: map[string]string{"app": "op"},
			}
			if tc.customOptions != nil {
				opts = *tc.customOptions
				opts.Namespace = namespace
				opts.CertDir = certDir
				opts.ServiceName = serviceName
				// If customOptions has cleared selector, it will be cleared.
			}

			mgr := NewManager(hookClient, record.NewFakeRecorder(10), opts)

			err := mgr.Bootstrap(t.Context())

			if tc.wantErr {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf(
						"Error message mismatch. Got: %v, Want substring: %s",
						err,
						tc.errContains,
					)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tc.checkFiles {
				if _, err := os.Stat(filepath.Join(certDir, CertFileName)); os.IsNotExist(err) {
					t.Errorf("Cert file not found at %s", CertFileName)
				}
			}

			if tc.wantGenerated {
				secret := &corev1.Secret{}
				_ = fakeClient.Get(
					t.Context(),
					types.NamespacedName{Name: ServerSecretName, Namespace: namespace},
					secret,
				)

				var original []byte
				for _, obj := range tc.existingObjects {
					if s, ok := obj.(*corev1.Secret); ok && s.Name == ServerSecretName {
						original = s.Data["tls.crt"]
						break
					}
				}
				if len(original) > 0 && bytes.Equal(secret.Data["tls.crt"], original) {
					t.Error("Expected rotation, but cert did not change")
				}
			}
		})
	}
}

// mockKubeletClient intercepts Secret updates and writes them to disk to simulate Kubelet volume projection
type mockKubeletClient struct {
	client.Client
	CertDir   string
	badScheme bool
}

func (m *mockKubeletClient) Scheme() *runtime.Scheme {
	if m.badScheme {
		return runtime.NewScheme()
	}
	return m.Client.Scheme()
}

func (m *mockKubeletClient) Create(
	ctx context.Context,
	obj client.Object,
	opts ...client.CreateOption,
) error {
	err := m.Client.Create(ctx, obj, opts...)
	if err == nil {
		m.syncToDisk(obj)
	}
	return err
}

func (m *mockKubeletClient) Update(
	ctx context.Context,
	obj client.Object,
	opts ...client.UpdateOption,
) error {
	err := m.Client.Update(ctx, obj, opts...)
	if err == nil {
		m.syncToDisk(obj)
	}
	return err
}

func (m *mockKubeletClient) Patch(
	ctx context.Context,
	obj client.Object,
	patch client.Patch,
	opts ...client.PatchOption,
) error {
	err := m.Client.Patch(ctx, obj, patch, opts...)
	if err == nil {
		m.syncToDisk(obj)
	}
	return err
}

func (m *mockKubeletClient) syncToDisk(obj client.Object) {
	if secret, ok := obj.(*corev1.Secret); ok && secret.Name == ServerSecretName {
		_ = os.WriteFile(filepath.Join(m.CertDir, CertFileName), secret.Data["tls.crt"], 0o644)
		_ = os.WriteFile(filepath.Join(m.CertDir, KeyFileName), secret.Data["tls.key"], 0o600)
	}
}

func generateCAPEM(tb testing.TB) ([]byte, []byte) {
	tb.Helper()
	ca, err := GenerateCA()
	if err != nil {
		tb.Fatal(err)
	}
	return ca.CertPEM, ca.KeyPEM
}

func generateSignedCertPEM(
	tb testing.TB,
	ca *CAArtifacts,
	expiry time.Time,
	dnsNames []string,
) []byte {
	tb.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		tb.Fatal(err)
	}

	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "server"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     expiry,
		DNSNames:     dnsNames,
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, ca.Cert, &priv.PublicKey, ca.Key)
	if err != nil {
		tb.Fatal(err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestManager_Misc(t *testing.T) {
	t.Parallel() // Misc can be parallel

	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = admissionregistrationv1.AddToScheme(s)

	namespace := "default"
	cl := fake.NewClientBuilder().WithScheme(s).Build()

	t.Run("Start Loop", func(t *testing.T) {
		t.Parallel()
		timeoutCtx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()

		mgr := NewManager(cl, nil, Options{
			Namespace:        namespace,
			RotationInterval: 10 * time.Millisecond,
		})

		_ = mgr.Start(timeoutCtx)
	})

	t.Run("Start Loop: Default Interval", func(t *testing.T) {
		t.Parallel()
		timeoutCtx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		defer cancel()

		mgr := NewManager(cl, nil, Options{
			Namespace: namespace,
			// Interval will be Hour
		})

		_ = mgr.Start(timeoutCtx)
	})

	t.Run("setOwner: found multiple deployments", func(t *testing.T) {
		t.Parallel()
		objs := []client.Object{
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "op1",
					Namespace: namespace,
					Labels:    map[string]string{"app": "op"},
				},
			},
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "op2",
					Namespace: namespace,
					Labels:    map[string]string{"app": "op"},
				},
			},
		}
		clFail := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
		mgr := NewManager(clFail, nil, Options{
			Namespace:             namespace,
			OperatorLabelSelector: map[string]string{"app": "op"},
		})
		err := mgr.setOwner(t.Context(), &corev1.Secret{})
		if err == nil || !strings.Contains(err.Error(), "found multiple deployments") {
			t.Errorf("Expected multiple deployments error, got: %v", err)
		}
	})

	t.Run("setOwner: get by name failure", func(t *testing.T) {
		t.Parallel()
		clFail := testutil.NewFakeClientWithFailures(cl, &testutil.FailureConfig{
			OnGet: func(key client.ObjectKey) error { return errors.New("get fail") },
		})
		mgr := NewManager(clFail, nil, Options{
			Namespace:          namespace,
			OperatorDeployment: "op",
		})
		err := mgr.setOwner(t.Context(), &corev1.Secret{})
		if err == nil ||
			!strings.Contains(err.Error(), "failed to get operator deployment by name") {
			t.Errorf("Expected get failure, got: %v", err)
		}
	})

	t.Run("waitForKubelet: mismatch log", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, CertFileName), []byte("wrong"), 0o644)

		mgr := NewManager(cl, nil, Options{
			Namespace: namespace,
			CertDir:   dir,
		})

		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer cancel()

		_ = mgr.waitForKubelet(ctx, []byte("expected"))
	})
}

func TestManager_EntropyFailures(t *testing.T) {
	// Not parallel - modifies global rand.Reader
	oldReader := rand.Reader
	defer func() { rand.Reader = oldReader }()

	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	// Base objects
	namespace := "test-ns"

	// We need 100% coverage, so we target:
	// 1. ensureCA -> GenerateCA failure
	// 2. ensureServerCert -> GenerateServerCert failure (creation)
	// 3. ensureServerCert -> GenerateServerCert failure (rotation)

	t.Run("ensureCA: GenerateCA Failure", func(t *testing.T) {
		// We want GenerateCA to fail.
		// GenerateCA calls GenerateKey then CreateCertificate.
		// If we use errorReader, GenerateKey fails immediately.
		rand.Reader = errorReader{}

		mgr := NewManager(
			fake.NewClientBuilder().WithScheme(s).Build(),
			nil,
			Options{Namespace: namespace},
		)

		err := mgr.reconcilePKI(t.Context())
		if err == nil || !strings.Contains(err.Error(), "failed to generate CA") {
			t.Errorf("Expected generate CA error, got %v", err)
		}
	})

	t.Run("ensureServerCert: GenerateServerCert Failure (Creation)", func(t *testing.T) {
		// CA exists, but Server Cert missing.
		// GenerateServerCert should be called.
		// We want it to fail.

		// Setup CA
		rand.Reader = oldReader // Need valid CA
		caArt, _ := GenerateCA()
		caSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: CASecretName, Namespace: namespace},
			Data:       map[string][]byte{"ca.crt": caArt.CertPEM, "ca.key": caArt.KeyPEM},
		}

		cl := fake.NewClientBuilder().WithScheme(s).WithObjects(caSecret).Build()
		mgr := NewManager(cl, nil, Options{Namespace: namespace, ServiceName: "svc"})

		// We use stack inspection to fail GenerateServerCert -> GenerateKey (or CreateCertificate)
		// Failing GenerateKey is easier/faster.
		// GenerateServerCert calls ecdsa.GenerateKey.
		// We can match "ecdsa.GenerateKey".
		// But GenerateCA (if called) also calls it. But here GenerateCA is not called.

		rand.Reader = &functionTargetedReader{
			failOnCaller: "ecdsa.GenerateKey",
			delegate:     oldReader,
		}

		err := mgr.reconcilePKI(t.Context())
		if err == nil || !strings.Contains(err.Error(), "failed to generate server cert") {
			t.Errorf("Expected server cert gen error, got %v", err)
		}
	})

	t.Run("ensureServerCert: GenerateServerCert Failure (Rotation)", func(t *testing.T) {
		// CA exists. Server Cert exists but is expired.
		// Rotation triggered -> GenerateServerCert called.

		rand.Reader = oldReader
		caArt, _ := GenerateCA()

		// Generate expired server cert
		priv, _ := rsa.GenerateKey(rand.Reader, 2048)
		tmpl := x509.Certificate{
			SerialNumber: big.NewInt(2),
			Subject:      pkix.Name{CommonName: "server"},
			NotBefore:    time.Now().Add(-2 * time.Hour),
			NotAfter:     time.Now().Add(-1 * time.Hour), // Expired
			DNSNames:     []string{"svc.test-ns.svc"},
		}
		der, _ := x509.CreateCertificate(rand.Reader, &tmpl, caArt.Cert, &priv.PublicKey, caArt.Key)
		expiredPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

		caSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: CASecretName, Namespace: namespace},
			Data:       map[string][]byte{"ca.crt": caArt.CertPEM, "ca.key": caArt.KeyPEM},
		}
		srvSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: ServerSecretName, Namespace: namespace},
			Data:       map[string][]byte{"tls.crt": expiredPEM, "tls.key": []byte("key")},
		}

		cl := fake.NewClientBuilder().WithScheme(s).WithObjects(caSecret, srvSecret).Build()
		mgr := NewManager(cl, nil, Options{Namespace: namespace, ServiceName: "svc"})

		rand.Reader = &functionTargetedReader{
			failOnCaller: "ecdsa.GenerateKey",
			delegate:     oldReader,
		}

		err := mgr.reconcilePKI(t.Context())
		if err == nil || !strings.Contains(err.Error(), "failed to generate new server cert") {
			t.Errorf("Expected server cert rotation error, got %v", err)
		}
	})
}

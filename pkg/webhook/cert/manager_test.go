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
	// t.Parallel() // Parallel disabled to avoid race conditions on global hooks

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
			errContains: "failed to ensure CA",
		},
		"Error: File System (Mkdir/Write)": {
			existingObjects: baseWebhooks,
			customCertDir:   "/dev/null/invalid-dir",
			wantErr:         true,
			errContains:     "mkdir",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
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
				Client:  cl,
				CertDir: certDir,
			}

			mgr := NewManager(hookClient, record.NewFakeRecorder(10), Options{
				Namespace:   namespace,
				CertDir:     certDir,
				ServiceName: serviceName,
			})

			err := mgr.Bootstrap(context.Background())

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
					context.Background(),
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
	CertDir string
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

func generateCAPEM(t *testing.T) ([]byte, []byte) {
	t.Helper()
	ca, err := GenerateCA()
	if err != nil {
		t.Fatal(err)
	}
	return ca.CertPEM, ca.KeyPEM
}

func generateSignedCertPEM(
	t *testing.T,
	ca *CAArtifacts,
	expiry time.Time,
	dnsNames []string,
) []byte {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

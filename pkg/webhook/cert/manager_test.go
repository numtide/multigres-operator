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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestManager_EnsureCerts(t *testing.T) {
	t.Parallel()

	const (
		namespace   = "test-ns"
		serviceName = "test-svc"
	)

	// Register schemes
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = admissionregistrationv1.AddToScheme(s)

	validCert := generateCertPEM(t, time.Now().Add(365*24*time.Hour))
	expiredCert := generateCertPEM(t, time.Now().Add(-1*time.Hour))
	nearExpiryCert := generateCertPEM(t, time.Now().Add(15*24*time.Hour)) // 15 days left (threshold is 30)

	// Fixtures
	webhookConfigs := []client.Object{
		&admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: WebhookConfigNameMutating},
			Webhooks: []admissionregistrationv1.MutatingWebhook{
				{ClientConfig: admissionregistrationv1.WebhookClientConfig{CABundle: []byte("old-bundle")}},
			},
		},
		&admissionregistrationv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: WebhookConfigNameValidating},
			Webhooks: []admissionregistrationv1.ValidatingWebhook{
				{ClientConfig: admissionregistrationv1.WebhookClientConfig{CABundle: []byte("old-bundle")}},
			},
		},
	}

	tests := map[string]struct {
		existingObjects []client.Object
		mockConfig      *mockConfig
		checkFiles      bool
		wantGenerated   bool // If true, expects a NEW cert to be generated (different from existing)
		wantErr         bool
		errorMsg        string
	}{
		"Bootstrap: Fresh Install": {
			existingObjects: webhookConfigs,
			checkFiles:      true,
		},
		"Idempotency: Valid Secret Exists": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: namespace},
					Data: map[string][]byte{
						"tls.crt": validCert,
						"tls.key": []byte("key"),
						"ca.crt":  []byte("ca-cert"),
					},
				},
			}, webhookConfigs...),
			checkFiles:    true,
			wantGenerated: false,
		},
		"Rotation: Expired Cert": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: namespace},
					Data: map[string][]byte{
						"tls.crt": expiredCert,
						"tls.key": []byte("key"),
						"ca.crt":  []byte("old-ca"),
					},
				},
			}, webhookConfigs...),
			checkFiles:    true,
			wantGenerated: true,
		},
		"Rotation: Near Expiry Cert": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: namespace},
					Data: map[string][]byte{
						"tls.crt": nearExpiryCert,
						"tls.key": []byte("key"),
						"ca.crt":  []byte("old-ca"),
					},
				},
			}, webhookConfigs...),
			checkFiles:    true,
			wantGenerated: true,
		},
		"Error: Get Secret Failed": {
			existingObjects: webhookConfigs,
			mockConfig:      &mockConfig{failGet: true, failGetKey: SecretName},
			wantErr:         true,
			errorMsg:        "failed to ensure cert secret",
		},
		"Error: Create Secret Failed": {
			existingObjects: webhookConfigs,
			mockConfig:      &mockConfig{failCreate: true},
			wantErr:         true,
			errorMsg:        "failed to create cert secret",
		},
		"Error: Update Secret Failed (during rotation)": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: namespace},
					Data: map[string][]byte{
						"tls.crt": expiredCert,
						"tls.key": []byte("key"),
						"ca.crt":  []byte("old-ca"),
					},
				},
			}, webhookConfigs...),
			mockConfig: &mockConfig{failUpdate: true, failUpdateKey: SecretName},
			wantErr:    true,
			errorMsg:   "failed to update cert secret",
		},
		"Error: Patch Webhook Config Failed": {
			existingObjects: webhookConfigs,
			mockConfig:      &mockConfig{failUpdate: true, failUpdateKey: WebhookConfigNameMutating},
			wantErr:         true,
			errorMsg:        "failed to patch webhook configurations",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Create a temp dir for each test
			certDir := t.TempDir()

			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tc.existingObjects...).
				Build()

			var cl client.Client = fakeClient
			if tc.mockConfig != nil {
				cl = &mockClient{Client: fakeClient, config: *tc.mockConfig}
			}

			mgr := NewManager(cl, Options{
				ServiceName: serviceName,
				Namespace:   namespace,
				CertDir:     certDir,
			})

			err := mgr.EnsureCerts(context.Background())
			if tc.wantErr {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if tc.errorMsg != "" && err.Error() != "" {
					if diff := cmp.Diff(tc.errorMsg, err.Error(), containsSubstring()); diff != "" {
						t.Errorf("Error message mismatch (-want +got):\n%s", diff)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Validations
			if tc.checkFiles {
				// Check files verify on disk
				if _, err := os.Stat(filepath.Join(certDir, CertFileName)); os.IsNotExist(err) {
					t.Errorf("Cert file not created")
				}
				if _, err := os.Stat(filepath.Join(certDir, KeyFileName)); os.IsNotExist(err) {
					t.Errorf("Key file not created")
				}

				// Verify Webhook Configs Patched
				mutating := &admissionregistrationv1.MutatingWebhookConfiguration{}
				if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: WebhookConfigNameMutating}, mutating); err != nil {
					t.Fatal(err)
				}
				if len(mutating.Webhooks[0].ClientConfig.CABundle) == 0 || bytes.Equal(mutating.Webhooks[0].ClientConfig.CABundle, []byte("old-bundle")) {
					t.Error("MutatingWebhookConfiguration CA bundle was not updated")
				}
			}

			if tc.wantGenerated {
				// Verify that the cert in the secret is DIFFERENT from the input
				secret := &corev1.Secret{}
				_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: SecretName, Namespace: namespace}, secret)

				// Find the original input cert
				var original []byte
				for _, obj := range tc.existingObjects {
					if s, ok := obj.(*corev1.Secret); ok && s.Name == SecretName {
						original = s.Data["tls.crt"]
						break
					}
				}

				if bytes.Equal(secret.Data["tls.crt"], original) {
					t.Error("Expected certificate to be rotated (regenerated), but it matches the existing one")
				}
			}
		})
	}
}

// --- Helpers ---

func generateCertPEM(t *testing.T, expiry time.Time) []byte {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     expiry,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// containsSubstring is a custom cmp comparator for error messages
func containsSubstring() cmp.Option {
	return cmp.FilterValues(func(x, y string) bool {
		return true
	}, cmp.Comparer(func(want, got string) bool {
		// Simple substring check
		return len(got) >= len(want) && (got == want || (len(want) > 0 && contains(got, want)))
	}))
}

func contains(s, substr string) bool {
	// Simple implementation to avoid strings package import if desired, but strings is standard
	// Since we are in tests, using standard library is fine.
	importStrings := "strings"
	_ = importStrings
	// Actually we can just use the real implementation logic or import strings
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- Mock Client ---

type mockConfig struct {
	failGet       bool
	failGetKey    string
	failCreate    bool
	failUpdate    bool
	failUpdateKey string
}

type mockClient struct {
	client.Client
	config mockConfig
}

func (m *mockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if m.config.failGet && (m.config.failGetKey == "" || m.config.failGetKey == key.Name) {
		return errors.New("simulated get error")
	}
	return m.Client.Get(ctx, key, obj, opts...)
}

func (m *mockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if m.config.failCreate {
		return errors.New("simulated create error")
	}
	return m.Client.Create(ctx, obj, opts...)
}

func (m *mockClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if m.config.failUpdate {
		if m.config.failUpdateKey == "" || obj.GetName() == m.config.failUpdateKey {
			return errors.New("simulated update error")
		}
	}
	return m.Client.Update(ctx, obj, opts...)
}

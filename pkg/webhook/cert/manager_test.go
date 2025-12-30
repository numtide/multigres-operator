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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/numtide/multigres-operator/pkg/testutil"
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

	// Helpers to generate cert bytes for setup
	validCert := generateCertPEM(t, time.Now().Add(365*24*time.Hour))
	expiredCert := generateCertPEM(t, time.Now().Add(-1*time.Hour))
	nearExpiryCert := generateCertPEM(t, time.Now().Add(15*24*time.Hour))

	// Helper to generate corrupt cert (valid PEM header, invalid body)
	corruptCertBody := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: []byte("this is not a valid der certificate"),
	})

	// Base fixtures
	baseWebhooks := []client.Object{
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
		failureConfig   *testutil.FailureConfig
		// Inject a failing RNG to test generation failure
		injectFailingRNG bool
		// If set, we use this as CertDir. If empty, we create a temp dir.
		customCertDir string
		// Expectations
		wantErr       bool
		errContains   string
		wantGenerated bool // If true, expects a NEW cert to be generated (different from existing)
		checkFiles    bool // Check if files exist on disk
	}{
		"Bootstrap: Fresh Install": {
			existingObjects: baseWebhooks,
			checkFiles:      true,
		},
		"Idempotency: Valid Secret Exists": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: namespace},
					Type:       corev1.SecretTypeTLS,
					Data: map[string][]byte{
						"tls.crt": validCert,
						"tls.key": []byte("key"),
						"ca.crt":  []byte("ca-cert"),
						"ca.key":  []byte("ca-key"),
					},
				},
			}, baseWebhooks...),
			checkFiles:    true,
			wantGenerated: false,
		},
		"Idempotency: Up-to-Date Webhooks (No Patch)": {
			existingObjects: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: namespace},
					Type:       corev1.SecretTypeTLS,
					Data: map[string][]byte{
						"tls.crt": validCert,
						"tls.key": []byte("key"),
						"ca.crt":  []byte("matching-ca"),
						"ca.key":  []byte("ca-key"),
					},
				},
				&admissionregistrationv1.MutatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: WebhookConfigNameMutating},
					Webhooks: []admissionregistrationv1.MutatingWebhook{
						{ClientConfig: admissionregistrationv1.WebhookClientConfig{CABundle: []byte("matching-ca")}},
					},
				},
				&admissionregistrationv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: WebhookConfigNameValidating},
					Webhooks: []admissionregistrationv1.ValidatingWebhook{
						{ClientConfig: admissionregistrationv1.WebhookClientConfig{CABundle: []byte("matching-ca")}},
					},
				},
			},
			checkFiles:    true,
			wantGenerated: false,
			// Ensure we don't try to update
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName(WebhookConfigNameMutating, errors.New("should not happen")),
			},
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
			}, baseWebhooks...),
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
			}, baseWebhooks...),
			checkFiles:    true,
			wantGenerated: true,
		},
		"Rotation: Empty Cert Data (isValid fails)": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: namespace},
					Data: map[string][]byte{
						"tls.crt": []byte(""),
						"tls.key": []byte("key"),
					},
				},
			}, baseWebhooks...),
			checkFiles:    true,
			wantGenerated: true,
		},
		"Rotation: Invalid PEM (isValid fails - decode)": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: namespace},
					Data: map[string][]byte{
						"tls.crt": []byte("NOT PEM DATA"),
						"tls.key": []byte("key"),
					},
				},
			}, baseWebhooks...),
			checkFiles:    true,
			wantGenerated: true,
		},
		"Rotation: Corrupt Cert Body (isValid fails - parse)": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: namespace},
					Data: map[string][]byte{
						"tls.crt": corruptCertBody,
						"tls.key": []byte("key"),
					},
				},
			}, baseWebhooks...),
			checkFiles:    true,
			wantGenerated: true,
		},
		"Error: Generation Failed (Fault Injection)": {
			existingObjects:  baseWebhooks,
			injectFailingRNG: true,
			wantErr:          true,
			errContains:      "failed to ensure cert secret: failed to generate CA",
		},
		"Patch: Webhooks Missing (Ignore)": {
			existingObjects: []client.Object{},
			checkFiles:      true,
			wantGenerated:   true,
		},
		"Error: Get Secret Failed": {
			existingObjects: baseWebhooks,
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName(SecretName, errors.New("injected get error")),
			},
			wantErr:     true,
			errContains: "failed to ensure cert secret",
		},
		"Error: Create Secret Failed": {
			existingObjects: baseWebhooks,
			failureConfig: &testutil.FailureConfig{
				OnCreate: testutil.FailOnObjectName(SecretName, errors.New("injected create error")),
			},
			wantErr:     true,
			errContains: "failed to create cert secret",
		},
		"Error: Update Secret Failed": {
			existingObjects: append([]client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: namespace},
					Data: map[string][]byte{
						"tls.crt": expiredCert,
						"tls.key": []byte("key"),
					},
				},
			}, baseWebhooks...),
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName(SecretName, errors.New("injected update error")),
			},
			wantErr:     true,
			errContains: "failed to update cert secret",
		},
		"Error: File System (Mkdir/Write)": {
			existingObjects: baseWebhooks,
			customCertDir:   "/dev/null/invalid-dir",
			wantErr:         true,
			errContains:     "failed to write certs to disk",
		},
		"Error: Patch MutatingWebhook Failed": {
			existingObjects: baseWebhooks,
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName(WebhookConfigNameMutating, errors.New("injected webhook update error")),
			},
			wantErr:     true,
			errContains: "failed to patch webhook configurations",
		},
		"Error: Patch ValidatingWebhook Failed": {
			existingObjects: baseWebhooks,
			failureConfig: &testutil.FailureConfig{
				OnUpdate: testutil.FailOnObjectName(WebhookConfigNameValidating, errors.New("injected webhook update error")),
			},
			wantErr:     true,
			errContains: "failed to patch webhook configurations",
		},
		"Error: Get MutatingWebhook Failed": {
			existingObjects: baseWebhooks,
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName(WebhookConfigNameMutating, errors.New("injected webhook get error")),
			},
			wantErr:     true,
			errContains: "failed to patch webhook configurations",
		},
		"Error: Get ValidatingWebhook Failed": {
			existingObjects: baseWebhooks,
			failureConfig: &testutil.FailureConfig{
				OnGet: testutil.FailOnKeyName(WebhookConfigNameValidating, errors.New("injected webhook get error")),
			},
			wantErr:     true,
			errContains: "failed to patch webhook configurations",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Setup Client
			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tc.existingObjects...).
				Build()

			var cl client.Client = fakeClient
			if tc.failureConfig != nil {
				cl = testutil.NewFakeClientWithFailures(fakeClient, tc.failureConfig)
			}

			// Setup Dir
			certDir := tc.customCertDir
			if certDir == "" {
				certDir = t.TempDir()
			}

			mgr := NewManager(cl, Options{
				ServiceName: serviceName,
				Namespace:   namespace,
				CertDir:     certDir,
			})

			// INJECT FAILING RNG HERE
			if tc.injectFailingRNG {
				mgr.rng = &failReader{}
			}

			err := mgr.EnsureCerts(context.Background())

			// Error Assertion
			if tc.wantErr {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("Error message mismatch. Got: %v, Want substring: %s", err, tc.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Success Assertions
			if tc.checkFiles {
				if _, err := os.Stat(filepath.Join(certDir, CertFileName)); os.IsNotExist(err) {
					t.Errorf("Cert file not created at %s", CertFileName)
				}
				if _, err := os.Stat(filepath.Join(certDir, KeyFileName)); os.IsNotExist(err) {
					t.Errorf("Key file not created at %s", KeyFileName)
				}

				// Check CA Bundle Injection IF webhooks existed
				if len(tc.existingObjects) > 0 {
					mutating := &admissionregistrationv1.MutatingWebhookConfiguration{}
					err := fakeClient.Get(context.Background(), types.NamespacedName{Name: WebhookConfigNameMutating}, mutating)
					if err == nil {
						if len(mutating.Webhooks) > 0 && bytes.Equal(mutating.Webhooks[0].ClientConfig.CABundle, []byte("old-bundle")) {
							t.Error("MutatingWebhookConfiguration CA bundle was NOT updated")
						}
					}
				}
			}

			if tc.wantGenerated {
				// Verify secret content changed
				secret := &corev1.Secret{}
				_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: SecretName, Namespace: namespace}, secret)

				// Find original cert from inputs
				var original []byte
				for _, obj := range tc.existingObjects {
					if s, ok := obj.(*corev1.Secret); ok && s.Name == SecretName {
						original = s.Data["tls.crt"]
						break
					}
				}

				if bytes.Equal(secret.Data["tls.crt"], original) {
					t.Error("Expected certificate to be rotated (changed), but it matches the existing one")
				}
			}
		})
	}
}

// generateCertPEM creates a valid self-signed cert for testing expiry logic
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

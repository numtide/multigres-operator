package cert

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;list;watch;update;patch

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// SecretName is the name of the Secret where we store the generated certs.
	SecretName = "multigres-webhook-certs" //nolint:gosec // Not a credential, just a name

	// WebhookConfigNameMutating is the standard name of our mutating webhook config.
	WebhookConfigNameMutating = "multigres-mutating-webhook"
	// WebhookConfigNameValidating is the standard name of our validating webhook config.
	WebhookConfigNameValidating = "multigres-validating-webhook"

	// CertFileName is the name of the certificate file expected by controller-runtime.
	CertFileName = "tls.crt"
	// KeyFileName is the name of the key file expected by controller-runtime.
	KeyFileName = "tls.key"

	// RotationThreshold is the buffer period before expiration when we should rotate the cert.
	// If the cert expires in less than 30 days, we regenerate it.
	RotationThreshold = 30 * 24 * time.Hour
)

// Options configuration for the certificate manager.
type Options struct {
	// ServiceName is the name of the Service that points to the webhook.
	ServiceName string
	// Namespace is the namespace where the operator (and Service) is running.
	Namespace string
	// CertDir is the directory where the certificates should be written for the server to use.
	CertDir string
}

// Manager handles the lifecycle of the webhook certificates.
type Manager struct {
	Client  client.Client
	Options Options
	// rng is the source of randomness. Defaults to crypto/rand.Reader.
	// We keep this internal but mutable via internal constructors/tests if needed.
	rng io.Reader
}

// NewManager creates a new certificate manager.
func NewManager(c client.Client, opts Options) *Manager {
	return &Manager{
		Client:  c,
		Options: opts,
		rng:     rand.Reader, // Secure default
	}
}

// EnsureCerts checks for existing certificates, generates them if missing or expired,
// writes them to disk, and injects the CA bundle into the WebhookConfigurations.
func (m *Manager) EnsureCerts(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("webhook-cert-manager")
	logger.Info("ensuring webhook certificates exist and are valid")

	// 1. Ensure the Secret exists and has valid, non-expired certs
	artifacts, err := m.ensureSecret(ctx)
	if err != nil {
		return fmt.Errorf("failed to ensure cert secret: %w", err)
	}

	// 2. Write certs to disk so the webhook server can start
	if err := m.writeCertsToDisk(ctx, artifacts); err != nil {
		return fmt.Errorf("failed to write certs to disk: %w", err)
	}

	// 3. Patch the WebhookConfigurations with the CA Bundle
	if err := m.patchWebhookConfigurations(ctx, artifacts.CACertPEM); err != nil {
		return fmt.Errorf("failed to patch webhook configurations: %w", err)
	}

	logger.Info("webhook certificates successfully configured")
	return nil
}

// ensureSecret fetches the cert secret. It validates the certificate's expiration.
// If missing or expiring soon, it generates new artifacts and updates/creates the Secret.
func (m *Manager) ensureSecret(ctx context.Context) (*Artifacts, error) {
	logger := log.FromContext(ctx)
	secret := &corev1.Secret{}
	err := m.Client.Get(
		ctx,
		types.NamespacedName{Name: SecretName, Namespace: m.Options.Namespace},
		secret,
	)

	secretFound := false
	if err == nil {
		secretFound = true
		artifacts := &Artifacts{
			CACertPEM:     secret.Data["ca.crt"],
			CAKeyPEM:      secret.Data["ca.key"],
			ServerCertPEM: secret.Data["tls.crt"],
			ServerKeyPEM:  secret.Data["tls.key"],
		}

		if m.isValid(artifacts) {
			logger.Info("existing webhook certificates are valid")
			return artifacts, nil
		}

		logger.Info(
			"existing webhook certificates are missing, expired, or near expiration; rotating",
		)
		// Fall through to regeneration
	} else if !errors.IsNotFound(err) {
		return nil, err
	}

	// Generate new artifacts
	commonName := fmt.Sprintf("%s.%s.svc", m.Options.ServiceName, m.Options.Namespace)
	dnsNames := []string{
		m.Options.ServiceName,
		fmt.Sprintf("%s.%s", m.Options.ServiceName, m.Options.Namespace),
		commonName,
		commonName + ".cluster.local",
	}

	logger.Info("generating new self-signed certificates", "commonName", commonName)
	// PASSING m.rng HERE IS THE KEY CHANGE
	artifacts, genErr := GenerateSelfSignedArtifacts(m.rng, commonName, dnsNames)
	if genErr != nil {
		return nil, genErr
	}

	// Create or Update the Secret
	secret.ObjectMeta = metav1.ObjectMeta{
		Name:      SecretName,
		Namespace: m.Options.Namespace,
	}
	secret.Type = corev1.SecretTypeTLS
	secret.Data = map[string][]byte{
		"tls.crt": artifacts.ServerCertPEM,
		"tls.key": artifacts.ServerKeyPEM,
		"ca.crt":  artifacts.CACertPEM,
		"ca.key":  artifacts.CAKeyPEM,
	}

	if secretFound {
		if updateErr := m.Client.Update(ctx, secret); updateErr != nil {
			return nil, fmt.Errorf("failed to update cert secret: %w", updateErr)
		}
	} else {
		if createErr := m.Client.Create(ctx, secret); createErr != nil {
			return nil, fmt.Errorf("failed to create cert secret: %w", createErr)
		}
	}

	return artifacts, nil
}

// isValid checks if the artifacts contain valid PEM data and if the certificate
// is far enough from expiration.
func (m *Manager) isValid(a *Artifacts) bool {
	if len(a.ServerCertPEM) == 0 || len(a.ServerKeyPEM) == 0 {
		return false
	}

	// Parse the cert to check expiration
	block, _ := pem.Decode(a.ServerCertPEM)
	if block == nil {
		return false
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}

	// Check if expired or expiring soon (within RotationThreshold)
	if time.Now().Add(RotationThreshold).After(cert.NotAfter) {
		return false
	}

	return true
}

func (m *Manager) writeCertsToDisk(ctx context.Context, artifacts *Artifacts) error {
	logger := log.FromContext(ctx)

	// Ensure directory exists
	if err := os.MkdirAll(m.Options.CertDir, 0o755); err != nil {
		return err
	}

	certPath := filepath.Join(m.Options.CertDir, CertFileName)
	keyPath := filepath.Join(m.Options.CertDir, KeyFileName)

	logger.Info("writing certificates to disk", "dir", m.Options.CertDir)

	// Write Cert (0644)
	if err := os.WriteFile(certPath, artifacts.ServerCertPEM, 0o644); err != nil { //nolint:gosec // Cert is public
		return err
	}

	// Write Key (0600 - strict permissions)
	if err := os.WriteFile(keyPath, artifacts.ServerKeyPEM, 0o600); err != nil {
		return err
	}

	return nil
}

func (m *Manager) patchWebhookConfigurations(ctx context.Context, caCert []byte) error {
	logger := log.FromContext(ctx)

	// 1. Patch MutatingWebhookConfiguration
	mutatingCfg := &admissionregistrationv1.MutatingWebhookConfiguration{}
	err := m.Client.Get(ctx, types.NamespacedName{Name: WebhookConfigNameMutating}, mutatingCfg)
	if err == nil {
		updated := false
		for i := range mutatingCfg.Webhooks {
			if string(mutatingCfg.Webhooks[i].ClientConfig.CABundle) != string(caCert) {
				mutatingCfg.Webhooks[i].ClientConfig.CABundle = caCert
				updated = true
			}
		}
		if updated {
			logger.Info(
				"patching CA bundle",
				"kind",
				"MutatingWebhookConfiguration",
				"name",
				WebhookConfigNameMutating,
			)
			if err := m.Client.Update(ctx, mutatingCfg); err != nil {
				return err
			}
		}
	} else if !errors.IsNotFound(err) {
		return err
	}

	// 2. Patch ValidatingWebhookConfiguration
	validatingCfg := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	err = m.Client.Get(ctx, types.NamespacedName{Name: WebhookConfigNameValidating}, validatingCfg)
	if err == nil {
		updated := false
		for i := range validatingCfg.Webhooks {
			if string(validatingCfg.Webhooks[i].ClientConfig.CABundle) != string(caCert) {
				validatingCfg.Webhooks[i].ClientConfig.CABundle = caCert
				updated = true
			}
		}
		if updated {
			logger.Info(
				"patching CA bundle",
				"kind",
				"ValidatingWebhookConfiguration",
				"name",
				WebhookConfigNameValidating,
			)
			if err := m.Client.Update(ctx, validatingCfg); err != nil {
				return err
			}
		}
	} else if !errors.IsNotFound(err) {
		return err
	}

	return nil
}

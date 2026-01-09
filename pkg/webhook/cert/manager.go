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

	// CertFileName is the name of the certificate file expected by controller-runtime.
	CertFileName = "tls.crt"
	// KeyFileName is the name of the key file expected by controller-runtime.
	KeyFileName = "tls.key"

	// RotationThreshold is the buffer period before expiration when we should rotate the cert.
	RotationThreshold = 30 * 24 * time.Hour

	// OperatorPodLabelKey identifies the controller manager pod/service
	OperatorPodLabelKey = "control-plane"
	// OperatorPodLabelValue identifies the controller manager pod/service
	OperatorPodLabelValue = "controller-manager"
)

// Options configuration for the certificate manager.
type Options struct {
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
	rng io.Reader
}

// NewManager creates a new certificate manager.
func NewManager(c client.Client, opts Options) *Manager {
	return &Manager{
		Client:  c,
		Options: opts,
		rng:     rand.Reader,
	}
}

// EnsureCerts checks for existing certificates, generates them if missing or expired,
// writes them to disk, and injects the CA bundle into the WebhookConfigurations.
func (m *Manager) EnsureCerts(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("webhook-cert-manager")
	logger.Info("ensuring webhook certificates exist and are valid")

	// 1. Auto-Discovery: Find the Service Name
	serviceName, err := m.findMyService(ctx)
	if err != nil {
		return fmt.Errorf("failed to discover webhook service: %w", err)
	}
	logger.Info("Discovered webhook service", "name", serviceName)

	// 2. Ensure the Secret exists and has valid, non-expired certs
	artifacts, err := m.ensureSecret(ctx, serviceName)
	if err != nil {
		return fmt.Errorf("failed to ensure cert secret: %w", err)
	}

	// 3. Write certs to disk so the webhook server can start
	if err := m.writeCertsToDisk(ctx, artifacts); err != nil {
		return fmt.Errorf("failed to write certs to disk: %w", err)
	}

	// 4. Patch the WebhookConfigurations with the CA Bundle
	if err := m.patchWebhookConfigurations(ctx, serviceName, artifacts.CACertPEM); err != nil {
		return fmt.Errorf("failed to patch webhook configurations: %w", err)
	}

	logger.Info("webhook certificates successfully configured")
	return nil
}

// findMyService looks for a Service in the operator's namespace that targets the operator's labels.
func (m *Manager) findMyService(ctx context.Context) (string, error) {
	svcList := &corev1.ServiceList{}
	if err := m.Client.List(ctx, svcList, client.InNamespace(m.Options.Namespace)); err != nil {
		return "", err
	}

	for _, svc := range svcList.Items {
		// Check if the service selector matches our hardcoded operator labels
		if val, ok := svc.Spec.Selector[OperatorPodLabelKey]; ok && val == OperatorPodLabelValue {
			// CRITICAL FIX: Distinguish between Webhook Service and Metrics Service.
			// Both select the same pods, but the Webhook Service targets port 9443 (container) or exposes 443 (service).
			// The Metrics Service targets 8443.
			for _, port := range svc.Spec.Ports {
				if port.TargetPort.IntVal == 9443 || port.Port == 443 {
					return svc.Name, nil
				}
			}
		}
	}

	return "", fmt.Errorf(
		"no webhook service found in namespace %s with selector %s=%s targeting port 9443",
		m.Options.Namespace,
		OperatorPodLabelKey,
		OperatorPodLabelValue,
	)
}

// ensureSecret fetches the cert secret. It validates the certificate's expiration.
// If missing or expiring soon, it generates new artifacts and updates/creates the Secret.
func (m *Manager) ensureSecret(ctx context.Context, serviceName string) (*Artifacts, error) {
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

		// Ensure the existing cert is valid for the DISCOVERED service name.
		// If we previously generated a cert for the "metrics" service by mistake, we must rotate it now.
		if m.isValid(artifacts, serviceName) {
			logger.Info("existing webhook certificates are valid")
			return artifacts, nil
		}

		logger.Info(
			"existing webhook certificates are missing, expired, or invalid for current service; rotating",
		)
		// Fall through to regeneration
	} else if !errors.IsNotFound(err) {
		return nil, err
	}

	// Generate new artifacts using the discovered service name
	commonName := fmt.Sprintf("%s.%s.svc", serviceName, m.Options.Namespace)
	dnsNames := []string{
		serviceName,
		fmt.Sprintf("%s.%s", serviceName, m.Options.Namespace),
		commonName,
		commonName + ".cluster.local",
	}

	logger.Info("generating new self-signed certificates", "commonName", commonName)
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

// isValid checks validity AND expiration
func (m *Manager) isValid(a *Artifacts, expectedServiceName string) bool {
	if len(a.ServerCertPEM) == 0 || len(a.ServerKeyPEM) == 0 {
		return false
	}

	block, _ := pem.Decode(a.ServerCertPEM)
	if block == nil {
		return false
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}

	// Check Expiration
	if time.Now().Add(RotationThreshold).After(cert.NotAfter) {
		return false
	}

	// Check if the cert was generated for the correct service
	// (Simple check: does the first DNS name match?)
	if len(cert.DNSNames) > 0 && cert.DNSNames[0] != expectedServiceName {
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

func (m *Manager) patchWebhookConfigurations(
	ctx context.Context,
	serviceName string,
	caCert []byte,
) error {
	logger := log.FromContext(ctx)

	// 1. Patch MutatingWebhookConfigurations
	mutatingList := &admissionregistrationv1.MutatingWebhookConfigurationList{}
	if err := m.Client.List(ctx, mutatingList); err != nil {
		return err
	}

	for _, cfg := range mutatingList.Items {
		if m.isConfigurationTargetingService(cfg.Webhooks, serviceName) {
			if err := m.patchObject(ctx, &cfg, caCert); err != nil {
				return err
			}
			logger.Info(
				"patched CA bundle",
				"kind",
				"MutatingWebhookConfiguration",
				"name",
				cfg.Name,
			)
		}
	}

	// 2. Patch ValidatingWebhookConfigurations
	validatingList := &admissionregistrationv1.ValidatingWebhookConfigurationList{}
	if err := m.Client.List(ctx, validatingList); err != nil {
		return err
	}

	for _, cfg := range validatingList.Items {
		if m.isConfigurationTargetingServiceValidating(cfg.Webhooks, serviceName) {
			if err := m.patchObject(ctx, &cfg, caCert); err != nil {
				return err
			}
			logger.Info(
				"patched CA bundle",
				"kind",
				"ValidatingWebhookConfiguration",
				"name",
				cfg.Name,
			)
		}
	}

	return nil
}

// Helper to check if a webhook config belongs to us
func (m *Manager) isConfigurationTargetingService(
	webhooks []admissionregistrationv1.MutatingWebhook,
	serviceName string,
) bool {
	for _, w := range webhooks {
		if w.ClientConfig.Service != nil &&
			w.ClientConfig.Service.Name == serviceName &&
			w.ClientConfig.Service.Namespace == m.Options.Namespace {
			return true
		}
	}
	return false
}

// Overload for Validating
func (m *Manager) isConfigurationTargetingServiceValidating(
	webhooks []admissionregistrationv1.ValidatingWebhook,
	serviceName string,
) bool {
	for _, w := range webhooks {
		if w.ClientConfig.Service != nil &&
			w.ClientConfig.Service.Name == serviceName &&
			w.ClientConfig.Service.Namespace == m.Options.Namespace {
			return true
		}
	}
	return false
}

// Generic patcher
func (m *Manager) patchObject(ctx context.Context, obj client.Object, caBundle []byte) error {
	base := obj.DeepCopyObject().(client.Object)
	updated := false

	switch r := obj.(type) {
	case *admissionregistrationv1.MutatingWebhookConfiguration:
		for i := range r.Webhooks {
			if string(r.Webhooks[i].ClientConfig.CABundle) != string(caBundle) {
				r.Webhooks[i].ClientConfig.CABundle = caBundle
				updated = true
			}
		}
	case *admissionregistrationv1.ValidatingWebhookConfiguration:
		for i := range r.Webhooks {
			if string(r.Webhooks[i].ClientConfig.CABundle) != string(caBundle) {
				r.Webhooks[i].ClientConfig.CABundle = caBundle
				updated = true
			}
		}
	}

	if updated {
		return m.Client.Patch(ctx, obj, client.MergeFrom(base))
	}
	return nil
}

package cert

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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// SecretName is the name of the Secret where we store the generated certs.
	SecretName = "multigres-webhook-certs"

	// CertFileName is the name of the certificate file.
	CertFileName = "tls.crt"
	// KeyFileName is the name of the key file.
	KeyFileName = "tls.key"

	// RotationThreshold is the buffer period before expiration when we should rotate the cert.
	// Best Practice: 30 days allows ample time for retry loops if the cluster is unstable.
	RotationThreshold = 30 * 24 * time.Hour
)

// Options configuration for the certificate manager.
type Options struct {
	Namespace          string
	ServiceName        string
	CertDir            string
	OperatorDeployment string // Name of the deployment to own the secret
}

// CertRotator manages the lifecycle of the webhook certificates.
// It implements controller-runtime's manager.Runnable interface.
type CertRotator struct {
	Client  client.Client
	Options Options
	rng     io.Reader
}

// NewManager creates a new certificate rotator.
func NewManager(c client.Client, opts Options) *CertRotator {
	return &CertRotator{
		Client:  c,
		Options: opts,
		// CRITICAL FIX: Initialize with crypto/rand.Reader to prevent nil pointer panic
		rng: rand.Reader,
	}
}

// Bootstrap is called BEFORE the Manager starts. It uses a direct client
// to ensure certificates exist on disk so the Webhook Server can bind to the port.
func (m *CertRotator) Bootstrap(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("cert-bootstrap")
	logger.Info("bootstrapping webhook certificates")
	return m.ensureCerts(ctx)
}

// Start implements manager.Runnable. It runs a background loop to rotate certificates.
func (m *CertRotator) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("cert-rotation")
	logger.Info("starting certificate rotation loop")

	// Check every hour
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.ensureCerts(ctx); err != nil {
				logger.Error(err, "failed to rotate certificates")
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// ensureCerts checks state, generates if needed, writes to disk, and patches k8s.
func (m *CertRotator) ensureCerts(ctx context.Context) error {
	logger := log.FromContext(ctx)

	// 1. Fetch Secret
	secret := &corev1.Secret{}
	err := m.Client.Get(
		ctx,
		types.NamespacedName{Name: SecretName, Namespace: m.Options.Namespace},
		secret,
	)

	var artifacts *Artifacts
	shouldRotate := false

	if errors.IsNotFound(err) {
		logger.Info("certificate secret not found, generating new one")
		shouldRotate = true
	} else if err != nil {
		return fmt.Errorf("failed to get certificate secret: %w", err)
	} else {
		// Secret exists, check validity
		artifacts = &Artifacts{
			CACertPEM:     secret.Data["ca.crt"],
			CAKeyPEM:      secret.Data["ca.key"],
			ServerCertPEM: secret.Data["tls.crt"],
			ServerKeyPEM:  secret.Data["tls.key"],
		}

		if m.isExpiringOrInvalid(artifacts) {
			logger.Info("certificates are expiring or invalid, rotating")
			shouldRotate = true
		}
	}

	// 2. Generate if needed
	if shouldRotate {
		commonName := fmt.Sprintf("%s.%s.svc", m.Options.ServiceName, m.Options.Namespace)
		dnsNames := []string{
			m.Options.ServiceName,
			fmt.Sprintf("%s.%s", m.Options.ServiceName, m.Options.Namespace),
			commonName,
			commonName + ".cluster.local",
		}

		var genErr error
		artifacts, genErr = GenerateSelfSignedArtifacts(m.rng, commonName, dnsNames)
		if genErr != nil {
			return genErr
		}

		// Save to Secret (with OwnerRef)
		if err := m.saveSecret(ctx, artifacts); err != nil {
			return err
		}
	}

	// 3. Always write to disk (Handling the "Wait for Kubelet" race condition)
	// We overwrite the files directly so the webhook server picks them up immediately.
	if err := m.writeToDisk(artifacts); err != nil {
		return err
	}

	// 4. Always patch Webhook Configurations to ensure CA Bundle is correct
	if err := m.patchWebhooks(ctx, artifacts.CACertPEM); err != nil {
		return err
	}

	return nil
}

func (m *CertRotator) isExpiringOrInvalid(a *Artifacts) bool {
	if len(a.ServerCertPEM) == 0 || len(a.ServerKeyPEM) == 0 {
		return true
	}

	block, _ := pem.Decode(a.ServerCertPEM)
	if block == nil {
		return true
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true
	}

	// Check Expiration
	if time.Now().Add(RotationThreshold).After(cert.NotAfter) {
		return true
	}

	// Check DNS name match (Simple check)
	expectedName := fmt.Sprintf("%s.%s.svc", m.Options.ServiceName, m.Options.Namespace)
	found := false
	for _, name := range cert.DNSNames {
		if name == expectedName {
			found = true
			break
		}
	}
	return !found
}

func (m *CertRotator) saveSecret(ctx context.Context, artifacts *Artifacts) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SecretName,
			Namespace: m.Options.Namespace,
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": artifacts.ServerCertPEM,
			"tls.key": artifacts.ServerKeyPEM,
			"ca.crt":  artifacts.CACertPEM,
			"ca.key":  artifacts.CAKeyPEM,
		},
	}

	// Create or Update
	op, err := controllerutil.CreateOrUpdate(ctx, m.Client, secret, func() error {
		secret.Data = map[string][]byte{
			"tls.crt": artifacts.ServerCertPEM,
			"tls.key": artifacts.ServerKeyPEM,
			"ca.crt":  artifacts.CACertPEM,
			"ca.key":  artifacts.CAKeyPEM,
		}
		// Attempt to set OwnerReference for Garbage Collection
		if m.Options.OperatorDeployment != "" {
			dep := &appsv1.Deployment{}
			if err := m.Client.Get(ctx, types.NamespacedName{Name: m.Options.OperatorDeployment, Namespace: m.Options.Namespace}, dep); err == nil {
				// We ignore errors here (e.g., if RBAC prevents reading deployments),
				// as it's an optimization, not a hard requirement.
				_ = controllerutil.SetControllerReference(dep, secret, m.Client.Scheme())
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to sync cert secret: %w", err)
	}
	log.FromContext(ctx).Info("synced certificate secret", "operation", op)
	return nil
}

func (m *CertRotator) writeToDisk(artifacts *Artifacts) error {
	if err := os.MkdirAll(m.Options.CertDir, 0o755); err != nil {
		return err
	}

	certPath := filepath.Join(m.Options.CertDir, CertFileName)
	keyPath := filepath.Join(m.Options.CertDir, KeyFileName)

	if err := os.WriteFile(certPath, artifacts.ServerCertPEM, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, artifacts.ServerKeyPEM, 0o600); err != nil {
		return err
	}
	return nil
}

func (m *CertRotator) patchWebhooks(ctx context.Context, caCert []byte) error {
	logger := log.FromContext(ctx)

	// Fallback strategy: Patch specific named configs found in standard manifests
	configs := []struct {
		Kind string
		Name string
	}{
		{"MutatingWebhookConfiguration", "multigres-operator-mutating-webhook-configuration"},
		{"ValidatingWebhookConfiguration", "multigres-operator-validating-webhook-configuration"},
	}

	for _, c := range configs {
		var obj client.Object
		if c.Kind == "MutatingWebhookConfiguration" {
			obj = &admissionregistrationv1.MutatingWebhookConfiguration{}
		} else {
			obj = &admissionregistrationv1.ValidatingWebhookConfiguration{}
		}

		if err := m.Client.Get(ctx, types.NamespacedName{Name: c.Name}, obj); err != nil {
			if errors.IsNotFound(err) {
				continue // Webhook might be disabled or not installed
			}
			return err
		}

		base := obj.DeepCopyObject().(client.Object)
		updated := false

		// Naive patch: update ALL hooks in this config.
		// Since this config belongs to the operator, this is safe.
		switch r := obj.(type) {
		case *admissionregistrationv1.MutatingWebhookConfiguration:
			for i := range r.Webhooks {
				if string(r.Webhooks[i].ClientConfig.CABundle) != string(caCert) {
					r.Webhooks[i].ClientConfig.CABundle = caCert
					updated = true
				}
			}
		case *admissionregistrationv1.ValidatingWebhookConfiguration:
			for i := range r.Webhooks {
				if string(r.Webhooks[i].ClientConfig.CABundle) != string(caCert) {
					r.Webhooks[i].ClientConfig.CABundle = caCert
					updated = true
				}
			}
		}

		if updated {
			if err := m.Client.Patch(ctx, obj, client.MergeFrom(base)); err != nil {
				return fmt.Errorf("failed to patch %s: %w", c.Name, err)
			}
			logger.Info("patched CA bundle", "config", c.Name)
		}
	}

	return nil
}

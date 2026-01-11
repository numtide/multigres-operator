package cert

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// CASecretName stores the Authority. NEVER mounted to the pod.
	CASecretName = "multigres-operator-ca-secret"
	// ServerSecretName stores the Leaf certs. Mounted to the pod.
	ServerSecretName = "multigres-webhook-certs"

	CertFileName = "tls.crt"
	KeyFileName  = "tls.key"

	// Rotation buffer: 30 days
	RotationThreshold = 30 * 24 * time.Hour
)

type Options struct {
	Namespace             string
	ServiceName           string
	CertDir               string
	OperatorDeployment    string
	OperatorLabelSelector map[string]string
}

type CertRotator struct {
	Client   client.Client
	Recorder record.EventRecorder
	Options  Options
}

func NewManager(c client.Client, recorder record.EventRecorder, opts Options) *CertRotator {
	return &CertRotator{
		Client:   c,
		Recorder: recorder,
		Options:  opts,
	}
}

// Bootstrap runs at startup to ensure PKI is healthy before the webhook server listens.
func (m *CertRotator) Bootstrap(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("pki-bootstrap")
	logger.Info("bootstrapping PKI infrastructure")

	if err := os.MkdirAll(m.Options.CertDir, 0o755); err != nil {
		return err
	}

	return m.reconcilePKI(ctx)
}

// Start kicks off the background rotation loop.
func (m *CertRotator) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("pki-rotation")
	logger.Info("starting PKI rotation loop")

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.reconcilePKI(ctx); err != nil {
				logger.Error(err, "periodic PKI reconciliation failed")
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// reconcilePKI is the main control loop.
// 1. Ensure CA is valid.
// 2. Ensure Server Cert is valid and signed by CA.
// 3. Inject CA into Webhooks.
// 4. Wait for Kubelet sync.
func (m *CertRotator) reconcilePKI(ctx context.Context) error {
	logger := log.FromContext(ctx)

	// --- Step 1: Handle Root CA ---
	caArtifacts, err := m.ensureCA(ctx)
	if err != nil {
		return fmt.Errorf("failed to ensure CA: %w", err)
	}

	// --- Step 2: Handle Server Certificate ---
	serverCertPEM, err := m.ensureServerCert(ctx, caArtifacts)
	if err != nil {
		return fmt.Errorf("failed to ensure server cert: %w", err)
	}

	// --- Step 3: Wait for File Propagation ---
	// This ensures the webhook server reads the *new* files before we tell API server to talk to us.
	if err := m.waitForKubelet(ctx, serverCertPEM); err != nil {
		return fmt.Errorf("timeout waiting for kubelet to update secrets: %w", err)
	}

	// --- Step 4: Patch Webhook Configurations ---
	if err := m.patchWebhooks(ctx, caArtifacts.CertPEM); err != nil {
		return fmt.Errorf("failed to patch webhooks: %w", err)
	}

	logger.V(1).Info("PKI reconciliation complete")
	return nil
}

func (m *CertRotator) ensureCA(ctx context.Context) (*CAArtifacts, error) {
	logger := log.FromContext(ctx)
	secret := &corev1.Secret{}
	err := m.Client.Get(
		ctx,
		types.NamespacedName{Name: CASecretName, Namespace: m.Options.Namespace},
		secret,
	)

	generate := false
	if errors.IsNotFound(err) {
		logger.Info("CA secret not found, generating new one")
		generate = true
	} else if err != nil {
		return nil, err
	} else {
		// Validate existing CA
		certPEM := secret.Data["ca.crt"]
		keyPEM := secret.Data["ca.key"]

		if len(certPEM) == 0 || len(keyPEM) == 0 {
			generate = true
		} else {
			// Parse and Check Expiry
			ca, err := ParseCA(certPEM, keyPEM)
			if err != nil {
				logger.Error(err, "existing CA is corrupt, rotating")
				generate = true
			} else {
				if time.Now().Add(RotationThreshold).After(ca.Cert.NotAfter) {
					logger.Info("CA is expiring, rotating")
					generate = true
				} else {
					return ca, nil // Valid
				}
			}
		}
	}

	if generate {
		newCA, err := GenerateCA()
		if err != nil {
			return nil, err
		}

		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      CASecretName,
				Namespace: m.Options.Namespace,
			},
			Type: corev1.SecretTypeOpaque, // CA secret is just data, not necessarily TLS type
			Data: map[string][]byte{
				"ca.crt": newCA.CertPEM,
				"ca.key": newCA.KeyPEM,
			},
		}

		// Set ownership
		if err := m.setOwner(ctx, secret); err != nil {
			return nil, err
		}

		if _, err := controllerutil.CreateOrUpdate(ctx, m.Client, secret, func() error {
			secret.Data = map[string][]byte{
				"ca.crt": newCA.CertPEM,
				"ca.key": newCA.KeyPEM,
			}
			return nil
		}); err != nil {
			return nil, err
		}

		m.recorderEvent(secret, "Normal", "Rotated", "Created new CA certificate")
		return newCA, nil
	}

	return nil, fmt.Errorf("unreachable in ensureCA")
}

func (m *CertRotator) ensureServerCert(ctx context.Context, ca *CAArtifacts) ([]byte, error) {
	logger := log.FromContext(ctx)
	secret := &corev1.Secret{}
	err := m.Client.Get(
		ctx,
		types.NamespacedName{Name: ServerSecretName, Namespace: m.Options.Namespace},
		secret,
	)

	generate := false
	if errors.IsNotFound(err) {
		generate = true
	} else if err != nil {
		return nil, err
	} else {
		// Check validity and if signed by current CA
		certPEM := secret.Data["tls.crt"]
		if len(certPEM) == 0 {
			generate = true
		} else {
			block, _ := pem.Decode(certPEM)
			if block == nil {
				generate = true
			} else {
				cert, err := x509.ParseCertificate(block.Bytes)
				if err != nil {
					generate = true
				} else {
					// Check Expiry
					if time.Now().Add(RotationThreshold).After(cert.NotAfter) {
						logger.Info("Server cert expiring, rotating")
						generate = true
					}
					// Check if signed by current CA
					if err := cert.CheckSignatureFrom(ca.Cert); err != nil {
						logger.Info("Server cert not signed by current CA, rotating")
						generate = true
					}
				}
			}
		}
	}

	if !generate {
		return secret.Data["tls.crt"], nil
	}

	// Generate new
	commonName := fmt.Sprintf("%s.%s.svc", m.Options.ServiceName, m.Options.Namespace)
	dnsNames := []string{
		m.Options.ServiceName,
		fmt.Sprintf("%s.%s", m.Options.ServiceName, m.Options.Namespace),
		commonName,
		commonName + ".cluster.local",
	}

	srv, err := GenerateServerCert(ca, commonName, dnsNames)
	if err != nil {
		return nil, err
	}

	secret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServerSecretName,
			Namespace: m.Options.Namespace,
		},
		Type: corev1.SecretTypeTLS,
	}

	if err := m.setOwner(ctx, secret); err != nil {
		return nil, err
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, m.Client, secret, func() error {
		secret.Data = map[string][]byte{
			"tls.crt": srv.CertPEM,
			"tls.key": srv.KeyPEM,
			"ca.crt":  ca.CertPEM, // Helpful to have CA in here too for client verification if needed
		}
		return nil
	}); err != nil {
		return nil, err
	}

	m.recorderEvent(secret, "Normal", "Rotated", "Rotated webhook server certificate")
	return srv.CertPEM, nil
}

func (m *CertRotator) waitForKubelet(ctx context.Context, expectedCertPEM []byte) error {
	logger := log.FromContext(ctx)
	// Exponential backoff to wait for Kubelet
	// Start fast (100ms) and backoff up to 2m total wait.
	backoff := wait.Backoff{
		Duration: 100 * time.Millisecond,
		Factor:   2,
		Jitter:   0.1,
		Steps:    12, // 100ms -> 200 -> 400 ... ~3-4 minutes max
	}

	return wait.ExponentialBackoff(backoff, func() (bool, error) {
		certPath := filepath.Join(m.Options.CertDir, CertFileName)
		diskBytes, err := os.ReadFile(certPath)
		if err != nil {
			// File might not exist yet if pod is just starting
			logger.V(1).Info("Waiting for certificate file", "path", certPath, "err", err)
			return false, nil
		}

		if string(diskBytes) == string(expectedCertPEM) {
			return true, nil
		}

		logger.V(1).Info("Certificate on disk does not match Secret yet")
		return false, nil
	})
}

func (m *CertRotator) setOwner(ctx context.Context, secret *corev1.Secret) error {
	logger := log.FromContext(ctx)

	dep, err := m.findOperatorDeployment(ctx)
	if err != nil {
		// Don't fail if we can't find it (e.g. running locally), but log it
		logger.V(1).Info("Could not determine operator deployment for GC", "error", err)
		return nil
	}

	if dep == nil {
		return nil
	}

	return controllerutil.SetControllerReference(dep, secret, m.Client.Scheme())
}

// findOperatorDeployment locates the deployment running this operator.
func (m *CertRotator) findOperatorDeployment(ctx context.Context) (*appsv1.Deployment, error) {
	// 1. Try Robust Label Selection (Preferred)
	if len(m.Options.OperatorLabelSelector) > 0 {
		list := &appsv1.DeploymentList{}
		err := m.Client.List(ctx, list,
			client.InNamespace(m.Options.Namespace),
			client.MatchingLabels(m.Options.OperatorLabelSelector),
		)
		if err != nil {
			return nil, err
		}

		if len(list.Items) == 1 {
			return &list.Items[0], nil
		} else if len(list.Items) > 1 {
			return nil, fmt.Errorf("found multiple deployments matching selector %v", m.Options.OperatorLabelSelector)
		}
		// If 0 found, fall through to legacy name check
	}

	// 2. Try Specific Name (Fallback)
	if m.Options.OperatorDeployment != "" {
		dep := &appsv1.Deployment{}
		err := m.Client.Get(
			ctx,
			types.NamespacedName{
				Name:      m.Options.OperatorDeployment,
				Namespace: m.Options.Namespace,
			},
			dep,
		)
		if err != nil {
			if errors.IsNotFound(err) {
				return nil, nil
			}
			return nil, err
		}
		return dep, nil
	}

	return nil, nil
}

func (m *CertRotator) patchWebhooks(ctx context.Context, caCert []byte) error {
	logger := log.FromContext(ctx)

	configs := []struct {
		Kind client.Object
		Name string
	}{
		{
			&admissionregistrationv1.MutatingWebhookConfiguration{},
			"multigres-operator-mutating-webhook-configuration",
		},
		{
			&admissionregistrationv1.ValidatingWebhookConfiguration{},
			"multigres-operator-validating-webhook-configuration",
		},
	}

	for _, c := range configs {
		obj := c.Kind.DeepCopyObject().(client.Object)
		if err := m.Client.Get(ctx, types.NamespacedName{Name: c.Name}, obj); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}

		// Create copy for diffing
		oldObj := obj.DeepCopyObject().(client.Object)
		updated := false

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
			logger.Info("Patching webhook configuration with new CA", "config", c.Name)
			if err := m.Client.Patch(ctx, obj, client.MergeFrom(oldObj)); err != nil {
				return fmt.Errorf("failed to patch %s: %w", c.Name, err)
			}
		}
	}
	return nil
}

// recorderEvent emits a Kubernetes event.
func (m *CertRotator) recorderEvent(obj client.Object, eventType, reason, message string) {
	if m.Recorder != nil {
		m.Recorder.Event(obj, eventType, reason, message)
	}
}

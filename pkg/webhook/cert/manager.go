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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// CASecretName stores the Authority. NEVER mounted to the pod.
	CASecretName = "multigres-operator-ca-secret" //nolint:gosec // K8s resource name, not a credential
	// ServerSecretName stores the Leaf certs. Mounted to the pod.
	ServerSecretName = "multigres-webhook-certs" //nolint:gosec // K8s resource name, not a credential

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
	RotationInterval      time.Duration
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
	logger := log.FromContext(ctx)
	logger.Info("bootstrapping PKI")

	if err := os.MkdirAll(m.Options.CertDir, 0o750); err != nil {
		return fmt.Errorf("failed to create cert directory: %w", err)
	}

	return m.reconcilePKI(ctx)
}

func (m *CertRotator) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("pki-rotation")
	logger.Info("starting PKI rotation loop")

	interval := m.Options.RotationInterval
	if interval == 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
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
func (m *CertRotator) reconcilePKI(ctx context.Context) error {
	ca, err := m.ensureCA(ctx)
	if err != nil {
		return err
	}

	serverCertPEM, err := m.ensureServerCert(ctx, ca)
	if err != nil {
		return err
	}

	if err := m.patchWebhooks(ctx, ca.CertPEM); err != nil {
		return fmt.Errorf("failed to patch webhooks: %w", err)
	}

	return m.waitForKubelet(ctx, serverCertPEM)
}

func (m *CertRotator) ensureCA(ctx context.Context) (*CAArtifacts, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CASecretName,
			Namespace: m.Options.Namespace,
		},
	}

	if err := m.Client.Get(ctx, types.NamespacedName{Name: CASecretName, Namespace: m.Options.Namespace}, secret); err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get CA secret: %w", err)
		}

		artifacts, err := GenerateCA()
		if err != nil {
			return nil, fmt.Errorf("failed to generate CA: %w", err)
		}

		secret.Data = map[string][]byte{
			"ca.crt": artifacts.CertPEM,
			"ca.key": artifacts.KeyPEM,
		}

		if err := m.setOwner(ctx, secret); err != nil {
			return nil, fmt.Errorf("failed to set owner for CA secret: %w", err)
		}

		if err := m.Client.Create(ctx, secret); err != nil {
			return nil, fmt.Errorf("failed to create CA secret: %w", err)
		}

		m.recorderEvent(secret, "Normal", "Generated", "Generated new CA certificate")
		return artifacts, nil
	}

	// Found secret, validate it
	artifacts, err := ParseCA(secret.Data["ca.crt"], secret.Data["ca.key"])
	if err != nil {
		// If corrupt, recreate
		log.FromContext(ctx).Error(err, "CA secret is corrupt, recreating")
		if err := m.Client.Delete(ctx, secret); err != nil {
			return nil, fmt.Errorf("failed to delete corrupt CA secret: %w", err)
		}
		return m.ensureCA(ctx)
	}

	// Check if near expiry
	if time.Until(artifacts.Cert.NotAfter) < RotationThreshold {
		log.FromContext(ctx).Info("CA is near expiry, rotating")
		if err := m.Client.Delete(ctx, secret); err != nil {
			return nil, fmt.Errorf("failed to delete expiring CA secret: %w", err)
		}
		return m.ensureCA(ctx)
	}

	return artifacts, nil
}

func (m *CertRotator) ensureServerCert(ctx context.Context, ca *CAArtifacts) ([]byte, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServerSecretName,
			Namespace: m.Options.Namespace,
		},
	}

	dnsNames := []string{
		fmt.Sprintf("%s.%s.svc", m.Options.ServiceName, m.Options.Namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", m.Options.ServiceName, m.Options.Namespace),
	}

	if err := m.Client.Get(ctx, types.NamespacedName{Name: ServerSecretName, Namespace: m.Options.Namespace}, secret); err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get server cert secret: %w", err)
		}

		artifacts, err := GenerateServerCert(ca, m.Options.ServiceName, dnsNames)
		if err != nil {
			return nil, fmt.Errorf("failed to generate server cert: %w", err)
		}

		secret.Data = map[string][]byte{
			"tls.crt": artifacts.CertPEM,
			"tls.key": artifacts.KeyPEM,
		}

		if err := m.setOwner(ctx, secret); err != nil {
			return nil, fmt.Errorf("failed to set owner for server cert secret: %w", err)
		}

		if err := m.Client.Create(ctx, secret); err != nil {
			return nil, fmt.Errorf("failed to create server cert secret: %w", err)
		}

		m.recorderEvent(secret, "Normal", "Generated", "Generated new webhook server certificate")
		return artifacts.CertPEM, nil
	}

	// Validate existing
	certBlock, _ := pem.Decode(secret.Data["tls.crt"])
	if certBlock == nil {
		log.FromContext(ctx).Error(nil, "server cert secret is corrupt, recreating")
		if err := m.Client.Delete(ctx, secret); err != nil {
			return nil, fmt.Errorf("failed to delete corrupt server cert secret: %w", err)
		}
		return m.ensureServerCert(ctx, ca)
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to parse server cert, recreating")
		if err := m.Client.Delete(ctx, secret); err != nil {
			return nil, fmt.Errorf("failed to delete unparseable server cert secret: %w", err)
		}
		return m.ensureServerCert(ctx, ca)
	}

	// Check if near expiry OR if CA changed
	needsRotation := time.Until(cert.NotAfter) < RotationThreshold
	if !needsRotation {
		// Check if signed by current CA
		if err := cert.CheckSignatureFrom(ca.Cert); err != nil {
			log.FromContext(ctx).Info("server cert was not signed by current CA, rotating")
			needsRotation = true
		}
	}

	if needsRotation {
		srv, err := GenerateServerCert(ca, m.Options.ServiceName, dnsNames)
		if err != nil {
			return nil, fmt.Errorf("failed to generate new server cert: %w", err)
		}
		secret.Data = map[string][]byte{
			"tls.crt": srv.CertPEM,
			"tls.key": srv.KeyPEM,
		}
		if err := m.Client.Update(ctx, secret); err != nil {
			return nil, fmt.Errorf("failed to update server cert secret: %w", err)
		}
		m.recorderEvent(secret, "Normal", "Rotated", "Rotated webhook server certificate")
		return srv.CertPEM, nil
	}

	return secret.Data["tls.crt"], nil
}

func (m *CertRotator) waitForKubelet(ctx context.Context, expectedCertPEM []byte) error {
	logger := log.FromContext(ctx)
	// Exponential backoff to wait for Kubelet
	// Start fast (100ms) and backoff up to 2m total wait.
	return wait.PollUntilContextTimeout(
		ctx,
		100*time.Millisecond,
		2*time.Minute,
		true,
		func(ctx context.Context) (bool, error) {
			certPath := filepath.Join(m.Options.CertDir, CertFileName)
			diskBytes, err := os.ReadFile(certPath) //nolint:gosec // path is from trusted config
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
		},
	)
}

func (m *CertRotator) setOwner(ctx context.Context, secret *corev1.Secret) error {
	dep, err := m.findOperatorDeployment(ctx)
	if err != nil {
		return err
	}
	if dep == nil {
		return nil
	}

	if err := controllerutil.SetControllerReference(dep, secret, m.Client.Scheme()); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}
	return nil
}

func (m *CertRotator) findOperatorDeployment(ctx context.Context) (*appsv1.Deployment, error) {
	// 1. Try by label selector
	if len(m.Options.OperatorLabelSelector) > 0 {
		list := &appsv1.DeploymentList{}
		if err := m.Client.List(ctx, list, client.InNamespace(m.Options.Namespace), client.MatchingLabels(m.Options.OperatorLabelSelector)); err != nil {
			return nil, fmt.Errorf("failed to list deployments by labels: %w", err)
		}
		if len(list.Items) > 1 {
			return nil, fmt.Errorf("found multiple deployments matching operator labels")
		}
		if len(list.Items) == 1 {
			return &list.Items[0], nil
		}
	}

	// 2. Try by explicit name
	if m.Options.OperatorDeployment != "" {
		dep := &appsv1.Deployment{}
		if err := m.Client.Get(ctx, types.NamespacedName{Name: m.Options.OperatorDeployment, Namespace: m.Options.Namespace}, dep); err != nil {
			if errors.IsNotFound(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("failed to get operator deployment by name: %w", err)
		}
		return dep, nil
	}

	return nil, nil
}

func (m *CertRotator) patchWebhooks(ctx context.Context, caBundle []byte) error {
	// Mutating
	mutating := &admissionregistrationv1.MutatingWebhookConfiguration{}
	mutName := "multigres-operator-mutating-webhook-configuration"
	if err := m.Client.Get(ctx, types.NamespacedName{Name: mutName}, mutating); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get mutating webhook config: %w", err)
		}
	} else {
		for i := range mutating.Webhooks {
			mutating.Webhooks[i].ClientConfig.CABundle = caBundle
		}
		if err := m.Client.Update(ctx, mutating); err != nil {
			return fmt.Errorf("failed to update mutating webhook config: %w", err)
		}
	}

	// Validating
	validating := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	valName := "multigres-operator-validating-webhook-configuration"
	if err := m.Client.Get(ctx, types.NamespacedName{Name: valName}, validating); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get validating webhook config: %w", err)
		}
	} else {
		for i := range validating.Webhooks {
			validating.Webhooks[i].ClientConfig.CABundle = caBundle
		}
		if err := m.Client.Update(ctx, validating); err != nil {
			return fmt.Errorf("failed to update validating webhook config: %w", err)
		}
	}

	return nil
}

func (m *CertRotator) recorderEvent(object runtime.Object, eventtype, reason, message string) {
	if m.Recorder != nil && object != nil {
		m.Recorder.AnnotatedEventf(object, nil, eventtype, reason, message)
	}
}

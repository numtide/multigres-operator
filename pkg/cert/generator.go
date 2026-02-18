package cert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

const (
	// Organization is the default organization name used in generated certificates.
	Organization = "Multigres Operator"
	// CAValidityDuration is the duration the CA certificate is valid for (10 years).
	CAValidityDuration = 10 * 365 * 24 * time.Hour
	// ServerValidityDuration is the duration the server certificate is valid for (1 year).
	ServerValidityDuration = 365 * 24 * time.Hour
)

// CAArtifacts holds the Certificate Authority keys and PEM-encoded data.
type CAArtifacts struct {
	Cert    *x509.Certificate
	Key     *ecdsa.PrivateKey
	CertPEM []byte
	KeyPEM  []byte
}

// ServerArtifacts holds the server certificate PEM-encoded data.
type ServerArtifacts struct {
	CertPEM []byte
	KeyPEM  []byte
}

// serverCertConfig holds the resolved configuration for server cert generation.
type serverCertConfig struct {
	extKeyUsages []x509.ExtKeyUsage
}

// ServerCertOption configures optional behavior for GenerateServerCert.
type ServerCertOption func(*serverCertConfig)

// WithExtKeyUsages overrides the default extended key usage for the generated
// server certificate. The default is [x509.ExtKeyUsageServerAuth].
// Use this when mutual TLS is needed (e.g., pgBackRest requires both
// ServerAuth and ClientAuth).
func WithExtKeyUsages(usages ...x509.ExtKeyUsage) ServerCertOption {
	return func(cfg *serverCertConfig) {
		cfg.extKeyUsages = usages
	}
}

// internal variables for mocking in tests
var (
	marshalECPrivateKey = x509.MarshalECPrivateKey
	parseCertificate    = x509.ParseCertificate
)

// GenerateCA creates a new self-signed Root CA using ECDSA P-256.
func GenerateCA() (*CAArtifacts, error) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA private key: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "Multigres Operator CA",
			Organization: []string{Organization},
		},
		NotBefore: time.Now().Add(-1 * time.Hour),
		NotAfter:  time.Now().Add(CAValidityDuration),
		KeyUsage:  x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	derBytes, err := x509.CreateCertificate(
		rand.Reader,
		&template,
		&template,
		&privKey.PublicKey,
		privKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	caCert, err := parseCertificate(derBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse generated CA: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	keyBytes, err := marshalECPrivateKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal CA key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	return &CAArtifacts{
		Cert:    caCert,
		Key:     privKey,
		CertPEM: certPEM,
		KeyPEM:  keyPEM,
	}, nil
}

// GenerateServerCert creates a leaf certificate signed by the provided CA.
// By default, the certificate includes only x509.ExtKeyUsageServerAuth.
// Use WithExtKeyUsages to override this (e.g., for mutual TLS).
func GenerateServerCert(
	ca *CAArtifacts,
	commonName string,
	dnsNames []string,
	opts ...ServerCertOption,
) (*ServerArtifacts, error) {
	if ca == nil {
		return nil, fmt.Errorf("CA artifacts cannot be nil")
	}

	cfg := serverCertConfig{
		extKeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate server private key: %w", err)
	}

	// Serial number should be unique. In a real PKI we'd track this,
	// but for ephemeral K8s secrets using a large random int is standard practice.
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, _ := rand.Int(rand.Reader, serialNumberLimit)

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{Organization},
		},
		DNSNames:    dnsNames,
		NotBefore:   time.Now().Add(-1 * time.Hour),
		NotAfter:    time.Now().Add(ServerValidityDuration),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: cfg.extKeyUsages,
	}

	if ip := net.ParseIP(commonName); ip != nil {
		template.IPAddresses = append(template.IPAddresses, ip)
	}

	derBytes, err := x509.CreateCertificate(
		rand.Reader,
		&template,
		ca.Cert,
		&privKey.PublicKey,
		ca.Key,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to sign server certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	keyBytes, err := marshalECPrivateKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal server key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	return &ServerArtifacts{
		CertPEM: certPEM,
		KeyPEM:  keyPEM,
	}, nil
}

// ParseCA decodes PEM data back into crypto objects for signing usage.
func ParseCA(certPEM, keyPEM []byte) (*CAArtifacts, error) {
	// Parse Cert
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode CA cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA cert: %w", err)
	}

	// Parse Key
	block, _ = pem.Decode(keyPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode CA key PEM")
	}
	// We optimistically try EC, then fallback to PKCS8 if needed, strictly P-256 for us.
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		// Fallback for older keys or PKCS8 wrapping
		if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
			switch k := k.(type) {
			case *ecdsa.PrivateKey:
				key = k
			default:
				return nil, fmt.Errorf("found non-ECDSA private key type in CA secret")
			}
		} else {
			return nil, fmt.Errorf("failed to parse CA private key: %w", err)
		}
	}

	return &CAArtifacts{
		Cert:    cert,
		Key:     key,
		CertPEM: certPEM,
		KeyPEM:  keyPEM,
	}, nil
}

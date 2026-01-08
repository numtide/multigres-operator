package cert

import (
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"time"
)

const (
	// RSAKeySize is the bit size for the RSA keys.
	RSAKeySize = 2048
	// Organization is the organization name used in the certificates.
	Organization = "Multigres Operator"
	// CAValidityDuration is the duration the CA certificate is valid for (10 years).
	CAValidityDuration = 10 * 365 * 24 * time.Hour
	// ServerValidityDuration is the duration the Server certificate is valid for (1 year).
	ServerValidityDuration = 365 * 24 * time.Hour
)

// Artifacts holds the generated certificate and key data in PEM format.
type Artifacts struct {
	CACertPEM     []byte
	CAKeyPEM      []byte
	ServerCertPEM []byte
	ServerKeyPEM  []byte
}

// GenerateSelfSignedArtifacts generates a complete set of self-signed artifacts:
// 1. A new Root CA.
// 2. A Server Certificate signed by that CA for the given Common Name and DNS names.
// It accepts a random number generator (rng) to allow for secure testing/fault injection.
func GenerateSelfSignedArtifacts(
	rng io.Reader,
	commonName string,
	dnsNames []string,
) (*Artifacts, error) {
	// 1. Generate CA (returns PEMs and the parsed object for signing)
	caCertPEM, caKeyPEM, caCert, caPrivKey, err := generateCA(rng)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA: %w", err)
	}

	// 2. Generate Server Certificate signed by CA (uses parsed object directly)
	serverCertPEM, serverKeyPEM, err := generateServerCert(
		rng,
		commonName,
		dnsNames,
		caCert,
		caPrivKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate server certificate: %w", err)
	}

	return &Artifacts{
		CACertPEM:     caCertPEM,
		CAKeyPEM:      caKeyPEM,
		ServerCertPEM: serverCertPEM,
		ServerKeyPEM:  serverKeyPEM,
	}, nil
}

func generateCA(rng io.Reader) ([]byte, []byte, *x509.Certificate, *rsa.PrivateKey, error) {
	privKey, err := rsa.GenerateKey(rng, RSAKeySize)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "Multigres Operator CA",
			Organization: []string{Organization},
		},
		NotBefore: time.Now().Add(-1 * time.Hour), // Backdate for clock skew
		NotAfter:  time.Now().Add(CAValidityDuration),
		KeyUsage:  x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	derBytes, err := x509.CreateCertificate(rng, &template, &template, &privKey.PublicKey, privKey)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// Parse it back to get the structural representation needed for signing the next cert
	caCert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	caKeyPEM := pem.EncodeToMemory(
		&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privKey)},
	)

	return caCertPEM, caKeyPEM, caCert, privKey, nil
}

func generateServerCert(
	rng io.Reader,
	commonName string,
	dnsNames []string,
	caCert *x509.Certificate,
	caKey *rsa.PrivateKey,
) ([]byte, []byte, error) {
	// Generate Server Key
	privKey, err := rsa.GenerateKey(rng, RSAKeySize)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(2), // Simple serial for self-signed
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{Organization},
		},
		DNSNames:    dnsNames,
		NotBefore:   time.Now().Add(-1 * time.Hour),
		NotAfter:    time.Now().Add(ServerValidityDuration),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	// Add IP SANs if the common name looks like an IP (unlikely for K8s svc, but good practice)
	if ip := net.ParseIP(commonName); ip != nil {
		template.IPAddresses = append(template.IPAddresses, ip)
	}

	derBytes, err := x509.CreateCertificate(rng, &template, caCert, &privKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(
		&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privKey)},
	)

	return certPEM, keyPEM, nil
}

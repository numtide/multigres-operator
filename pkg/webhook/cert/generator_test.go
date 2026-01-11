package cert

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"testing"
)

func TestGenerateArtifacts(t *testing.T) {
	t.Parallel()

	commonName := "test-service.test-ns.svc"
	dnsNames := []string{"test-service", "test-service.test-ns.svc"}

	// 1. Test CA Generation
	caArtifacts, err := GenerateCA()
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}
	if caArtifacts == nil {
		t.Fatal("GenerateCA() returned nil artifacts")
	}

	// Verify CA Certificate
	caBlock, _ := pem.Decode(caArtifacts.CertPEM)
	if caBlock == nil || caBlock.Type != "CERTIFICATE" {
		t.Errorf("Failed to decode CA PEM: %v", string(caArtifacts.CertPEM))
	}

	if !caArtifacts.Cert.IsCA {
		t.Error("Generated CA cert is not marked as CA")
	}

	// 2. Test Server Certificate Generation
	serverArtifacts, err := GenerateServerCert(caArtifacts, commonName, dnsNames)
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}
	if serverArtifacts == nil {
		t.Fatal("GenerateServerCert() returned nil artifacts")
	}

	// Verify Server Certificate
	serverBlock, _ := pem.Decode(serverArtifacts.CertPEM)
	if serverBlock == nil || serverBlock.Type != "CERTIFICATE" {
		t.Errorf("Failed to decode Server PEM")
	}

	serverCert, err := x509.ParseCertificate(serverBlock.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse Server Cert: %v", err)
	}

	if serverCert.IsCA {
		t.Error("Server cert should not be marked as CA")
	}

	// 3. Verify Signature (Chain of Trust)
	if err := serverCert.CheckSignatureFrom(caArtifacts.Cert); err != nil {
		t.Errorf("Server cert is not signed by the generated CA: %v", err)
	}
}

func TestGenerateServerCert_IPAddress(t *testing.T) {
	t.Parallel()
	commonName := "192.168.1.1"
	dnsNames := []string{"example.com"}

	ca, err := GenerateCA()
	if err != nil {
		t.Fatal(err)
	}

	artifacts, err := GenerateServerCert(ca, commonName, dnsNames)
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	block, _ := pem.Decode(artifacts.CertPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse cert: %v", err)
	}

	if len(cert.IPAddresses) != 1 || !cert.IPAddresses[0].Equal(net.ParseIP("192.168.1.1")) {
		t.Errorf("IP Address mismatch")
	}
}

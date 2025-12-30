package cert

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"testing"
	"time"
)

func TestGenerateSelfSignedArtifacts(t *testing.T) {
	t.Parallel()

	commonName := "test-service.test-ns.svc"
	dnsNames := []string{"test-service", "test-service.test-ns.svc"}

	artifacts, err := GenerateSelfSignedArtifacts(commonName, dnsNames)
	if err != nil {
		t.Fatalf("GenerateSelfSignedArtifacts() error = %v", err)
	}
	if artifacts == nil {
		t.Fatal("GenerateSelfSignedArtifacts() returned nil artifacts")
	}

	// 1. Verify CA Certificate
	caBlock, _ := pem.Decode(artifacts.CACertPEM)
	if caBlock == nil || caBlock.Type != "CERTIFICATE" {
		t.Errorf("Failed to decode CA PEM: %v", string(artifacts.CACertPEM))
	}

	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse CA Cert: %v", err)
	}

	if !caCert.IsCA {
		t.Error("Generated CA cert is not marked as CA")
	}
	if got, want := caCert.Subject.CommonName, "Multigres Operator CA"; got != want {
		t.Errorf("CA CommonName got %q, want %q", got, want)
	}
	if !caCert.NotAfter.After(time.Now().Add(9 * 365 * 24 * time.Hour)) {
		t.Error("CA validity period is too short (< 9 years)")
	}

	// 2. Verify CA Key
	caKeyBlock, _ := pem.Decode(artifacts.CAKeyPEM)
	if caKeyBlock == nil || caKeyBlock.Type != "RSA PRIVATE KEY" {
		t.Errorf("Failed to decode CA Key PEM")
	}
	if _, err := x509.ParsePKCS1PrivateKey(caKeyBlock.Bytes); err != nil {
		t.Errorf("Failed to parse CA Private Key: %v", err)
	}

	// 3. Verify Server Certificate
	serverBlock, _ := pem.Decode(artifacts.ServerCertPEM)
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
	if got, want := serverCert.Subject.CommonName, commonName; got != want {
		t.Errorf("Server CommonName got %q, want %q", got, want)
	}

	// Check DNS Names
	foundDNS := false
	for _, dns := range serverCert.DNSNames {
		if dns == "test-service" {
			foundDNS = true
			break
		}
	}
	if !foundDNS {
		t.Errorf("Server cert missing DNS name 'test-service', got: %v", serverCert.DNSNames)
	}

	// Verify Server Cert is signed by CA
	if err := serverCert.CheckSignatureFrom(caCert); err != nil {
		t.Errorf("Server cert is not signed by the generated CA: %v", err)
	}

	// 4. Verify Server Key
	serverKeyBlock, _ := pem.Decode(artifacts.ServerKeyPEM)
	if serverKeyBlock == nil || serverKeyBlock.Type != "RSA PRIVATE KEY" {
		t.Errorf("Failed to decode Server Key PEM")
	}
	serverKey, err := x509.ParsePKCS1PrivateKey(serverKeyBlock.Bytes)
	if err != nil {
		t.Errorf("Failed to parse Server Private Key: %v", err)
	}
	if serverKey.N.BitLen() < 2048 {
		t.Errorf("Server Key bit length too short: %d", serverKey.N.BitLen())
	}
}

func TestGenerateSelfSignedArtifacts_IPAddress(t *testing.T) {
	t.Parallel()

	commonName := "192.168.1.1"
	dnsNames := []string{"example.com"}

	artifacts, err := GenerateSelfSignedArtifacts(commonName, dnsNames)
	if err != nil {
		t.Fatalf("GenerateSelfSignedArtifacts() error = %v", err)
	}

	block, _ := pem.Decode(artifacts.ServerCertPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse cert: %v", err)
	}

	if len(cert.IPAddresses) != 1 {
		t.Errorf("Expected 1 IP Address, got %d", len(cert.IPAddresses))
	}
	if !cert.IPAddresses[0].Equal(net.ParseIP("192.168.1.1")) {
		t.Errorf("IP Address mismatch, got %v", cert.IPAddresses[0])
	}
}

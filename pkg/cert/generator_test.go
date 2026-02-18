package cert

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGenerator_Logic(t *testing.T) {
	t.Parallel()

	// Helpers (accept testing.TB)
	decodeCert := func(tb testing.TB, pemData []byte) *x509.Certificate {
		tb.Helper()
		block, _ := pem.Decode(pemData)
		if block == nil {
			tb.Fatalf("failed to decode PEM")
			return nil
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			tb.Fatalf("failed to parse certificate: %v", err)
		}
		return cert
	}

	// Fixtures
	caArtifacts, err := GenerateCA("")
	if err != nil {
		t.Fatalf("setup failed: GenerateCA error = %v", err)
	}

	type input struct {
		ca         *CAArtifacts
		commonName string
		dnsNames   []string
		opts       []ServerCertOption
	}

	tests := map[string]struct {
		input    input
		validate func(testing.TB, *ServerArtifacts)
		wantErr  bool
	}{
		"Happy Path: Generate CA": {
			validate: func(tb testing.TB, _ *ServerArtifacts) {
				cert := decodeCert(tb, caArtifacts.CertPEM)
				if !cert.IsCA {
					tb.Error("Expected CA cert to have IsCA=true")
				}
				if got, want := cert.Subject.CommonName, "Multigres Operator CA"; got != want {
					tb.Errorf("CommonName mismatch: got %q, want %q", got, want)
				}
			},
		},
		"Happy Path: Generate Server Cert": {
			input: input{
				ca:         caArtifacts,
				commonName: "test-svc.ns.svc",
				dnsNames:   []string{"test-svc", "test-svc.ns.svc"},
			},
			validate: func(tb testing.TB, arts *ServerArtifacts) {
				cert := decodeCert(tb, arts.CertPEM)
				if cert.IsCA {
					tb.Error("Expected server cert to NOT be CA")
				}
				if got, want := cert.Subject.CommonName, "test-svc.ns.svc"; got != want {
					tb.Errorf("CN mismatch: got %q, want %q", got, want)
				}
				if diff := cmp.Diff(
					cert.DNSNames,
					[]string{"test-svc", "test-svc.ns.svc"},
				); diff != "" {
					tb.Errorf("DNSNames mismatch (-got +want):\n%s", diff)
				}
				// Verify chain
				if err := cert.CheckSignatureFrom(caArtifacts.Cert); err != nil {
					tb.Errorf("Signature verification failed: %v", err)
				}
				// Verify default ExtKeyUsage is ServerAuth only
				if len(cert.ExtKeyUsage) != 1 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
					tb.Errorf("Expected default ExtKeyUsage [ServerAuth], got %v", cert.ExtKeyUsage)
				}
			},
		},
		"Happy Path: Server Cert with IP": {
			input: input{
				ca:         caArtifacts,
				commonName: "192.168.1.1",
				dnsNames:   []string{"example.com"},
			},
			validate: func(tb testing.TB, arts *ServerArtifacts) {
				cert := decodeCert(tb, arts.CertPEM)
				if len(cert.IPAddresses) != 1 ||
					!cert.IPAddresses[0].Equal(net.ParseIP("192.168.1.1")) {
					tb.Errorf("Expected IP 192.168.1.1, got %v", cert.IPAddresses)
				}
			},
		},
		"Happy Path: Custom ExtKeyUsages (Mutual TLS)": {
			input: input{
				ca:         caArtifacts,
				commonName: "pgbackrest-server",
				dnsNames:   []string{"pgbackrest.ns.svc"},
				opts: []ServerCertOption{
					WithExtKeyUsages(x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth),
				},
			},
			validate: func(tb testing.TB, arts *ServerArtifacts) {
				cert := decodeCert(tb, arts.CertPEM)
				if len(cert.ExtKeyUsage) != 2 {
					tb.Fatalf("Expected 2 ExtKeyUsages, got %d", len(cert.ExtKeyUsage))
				}
				if cert.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
					tb.Errorf(
						"Expected first ExtKeyUsage to be ServerAuth, got %v",
						cert.ExtKeyUsage[0],
					)
				}
				if cert.ExtKeyUsage[1] != x509.ExtKeyUsageClientAuth {
					tb.Errorf(
						"Expected second ExtKeyUsage to be ClientAuth, got %v",
						cert.ExtKeyUsage[1],
					)
				}
			},
		},
		"Happy Path: Single ExtKeyUsage Override": {
			input: input{
				ca:         caArtifacts,
				commonName: "client-only",
				dnsNames:   []string{"client.ns.svc"},
				opts: []ServerCertOption{
					WithExtKeyUsages(x509.ExtKeyUsageClientAuth),
				},
			},
			validate: func(tb testing.TB, arts *ServerArtifacts) {
				cert := decodeCert(tb, arts.CertPEM)
				if len(cert.ExtKeyUsage) != 1 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
					tb.Errorf("Expected ExtKeyUsage [ClientAuth], got %v", cert.ExtKeyUsage)
				}
			},
		},
		"Error: Nil CA": {
			input: input{
				ca: nil, // Trigger error
			},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Skip execution for CA-only test case
			if name == "Happy Path: Generate CA" {
				tc.validate(t, nil)
				return
			}

			arts, err := GenerateServerCert(
				tc.input.ca,
				tc.input.commonName,
				tc.input.dnsNames,
				tc.input.opts...)
			if tc.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tc.validate != nil {
				tc.validate(t, arts)
			}
		})
	}
}

func TestParseCA_Logic(t *testing.T) {
	t.Parallel()

	ca, _ := GenerateCA("")

	tests := map[string]struct {
		certBytes []byte
		keyBytes  []byte
		wantErr   string
	}{
		"Make Parsable": {
			certBytes: ca.CertPEM,
			keyBytes:  ca.KeyPEM,
		},
		"Error: Empty Cert": {
			certBytes: []byte(""),
			keyBytes:  ca.KeyPEM,
			wantErr:   "failed to decode CA cert PEM",
		},
		"Error: Empty Key": {
			certBytes: ca.CertPEM,
			keyBytes:  []byte(""),
			wantErr:   "failed to decode CA key PEM",
		},
		"Error: Invalid Cert Content": {
			certBytes: pem.EncodeToMemory(
				&pem.Block{Type: "CERTIFICATE", Bytes: []byte("garbage")},
			),
			keyBytes: ca.KeyPEM,
			wantErr:  "failed to parse CA cert",
		},
		"Error: Invalid Key Content": {
			certBytes: ca.CertPEM,
			keyBytes: pem.EncodeToMemory(
				&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte("garbage")},
			),
			wantErr: "failed to parse CA private key",
		},
		"Success: PKCS8 Key Support": {
			certBytes: ca.CertPEM,
			keyBytes: func() []byte {
				// Convert to PKCS8
				k, _ := x509.MarshalPKCS8PrivateKey(ca.Key)
				return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: k})
			}(),
		},
		"Error: Non-ECDSA Key": {
			certBytes: ca.CertPEM,
			keyBytes: func() []byte {
				k, _ := rsa.GenerateKey(rand.Reader, 2048)
				b, _ := x509.MarshalPKCS8PrivateKey(k)
				return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b})
			}(),
			wantErr: "found non-ECDSA private key type",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseCA(tc.certBytes, tc.keyBytes)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("Error mismatch. Got %q, want substring %q", err.Error(), tc.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if got == nil {
					t.Fatal("Expected artifacts, got nil")
				}
			}
		})
	}
}

// errorReader always fails reading
type errorReader struct{}

func (e errorReader) Read(p []byte) (n int, err error) {
	return 0, fmt.Errorf("entropy error")
}

// functionTargetedReader fails if the call stack contains a specific function name
type functionTargetedReader struct {
	failOnCaller string
	delegate     io.Reader
}

func (r *functionTargetedReader) Read(p []byte) (n int, err error) {
	pc := make([]uintptr, 50)
	nCallers := runtime.Callers(2, pc)
	frames := runtime.CallersFrames(pc[:nCallers])

	for {
		frame, more := frames.Next()
		if strings.Contains(frame.Function, r.failOnCaller) {
			return 0, fmt.Errorf("simulated failure for %s", r.failOnCaller)
		}
		if !more {
			break
		}
	}
	return r.delegate.Read(p)
}

func TestGenerator_EntropyFailures(t *testing.T) {
	// Not parallel - modifies global rand.Reader
	oldReader := rand.Reader
	defer func() { rand.Reader = oldReader }()

	t.Run("GenerateCA Key Failure", func(t *testing.T) {
		rand.Reader = errorReader{}
		_, err := GenerateCA("")
		if err == nil || !strings.Contains(err.Error(), "failed to generate CA private key") {
			t.Errorf("Expected key gen error, got %v", err)
		}
	})

	t.Run("GenerateServerCert Key Failure", func(t *testing.T) {
		rand.Reader = oldReader // Need valid reader for CA gen
		ca, _ := GenerateCA("")

		rand.Reader = errorReader{}
		_, err := GenerateServerCert(ca, "foo", nil)
		if err == nil || !strings.Contains(err.Error(), "failed to generate server private key") {
			t.Errorf("Expected key gen error, got %v", err)
		}
	})

	t.Run("GenerateCA: failure (cert)", func(t *testing.T) {
		rand.Reader = &functionTargetedReader{
			failOnCaller: "x509.CreateCertificate",
			delegate:     oldReader,
		}
		_, err := GenerateCA("")
		if err == nil || !strings.Contains(err.Error(), "failed to create CA certificate") {
			t.Errorf("Expected cert creation error, got %v", err)
		}
	})

	t.Run("GenerateServerCert: failure (cert)", func(t *testing.T) {
		rand.Reader = oldReader
		ca, _ := GenerateCA("")

		rand.Reader = &functionTargetedReader{
			failOnCaller: "x509.CreateCertificate",
			delegate:     oldReader,
		}
		_, err := GenerateServerCert(ca, "foo", nil)
		if err == nil || !strings.Contains(err.Error(), "failed to sign server certificate") {
			t.Errorf("Expected signing error, got %v", err)
		}
	})
}

func TestGenerator_MockFailures(t *testing.T) {
	// Restore original functions after test
	defer func() {
		parseCertificate = x509.ParseCertificate
		marshalECPrivateKey = x509.MarshalECPrivateKey
	}()

	t.Run("GenerateCA: ParseCertificate Failure", func(t *testing.T) {
		parseCertificate = func(der []byte) (*x509.Certificate, error) {
			return nil, fmt.Errorf("mock parse error")
		}

		_, err := GenerateCA("")
		if err == nil || !strings.Contains(err.Error(), "failed to parse generated CA") {
			t.Errorf("Expected parse error, got %v", err)
		}
	})

	t.Run("GenerateCA: Marshal Key Failure", func(t *testing.T) {
		parseCertificate = x509.ParseCertificate
		marshalECPrivateKey = func(key *ecdsa.PrivateKey) ([]byte, error) {
			return nil, fmt.Errorf("mock marshal error")
		}

		_, err := GenerateCA("")
		if err == nil || !strings.Contains(err.Error(), "failed to marshal CA key") {
			t.Errorf("Expected marshal error, got %v", err)
		}
	})
}

func TestGenerator_MockFailures_ServerCert(t *testing.T) {
	defer func() {
		marshalECPrivateKey = x509.MarshalECPrivateKey
	}()

	// Setup valid CA with REAL functions
	marshalECPrivateKey = x509.MarshalECPrivateKey
	ca, _ := GenerateCA("")

	t.Run("GenerateServerCert: Marshal Key Failure", func(t *testing.T) {
		marshalECPrivateKey = func(key *ecdsa.PrivateKey) ([]byte, error) {
			return nil, fmt.Errorf("mock marshal error")
		}

		_, err := GenerateServerCert(ca, "foo", nil)
		if err == nil || !strings.Contains(err.Error(), "failed to marshal server key") {
			t.Errorf("Expected marshal error, got %v", err)
		}
	})
}

package hybrid_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/node/hybrid"
	"github.com/aws/eks-hybrid/internal/validation"
)

func TestHybridNodeProvider_ValidateCertificateIfExists(t *testing.T) {
	tests := []struct {
		name          string
		setupCert     func(t *testing.T, certPath string, ca []byte) error
		nodeConfig    *api.NodeConfig
		expectedErr   string
		expectNoError bool
		expectSkipped bool // For date validation errors and no cert errors
	}{
		{
			name: "no certificate exists - should be skipped",
			setupCert: func(t *testing.T, certPath string, ca []byte) error {
				// Don't create any certificate file
				return nil
			},
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						CertificateAuthority: []byte("test-ca"),
					},
				},
			},
			expectSkipped: true,
		},
		{
			name: "valid certificate with matching CA",
			setupCert: func(t *testing.T, certPath string, ca []byte) error {
				return createValidCertificate(t, certPath, ca)
			},
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						CertificateAuthority: nil, // Will be set by test
					},
				},
			},
			expectNoError: true,
		},
		{
			name: "valid certificate without CA validation",
			setupCert: func(t *testing.T, certPath string, ca []byte) error {
				return createValidCertificate(t, certPath, nil)
			},
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						CertificateAuthority: nil,
					},
				},
			},
			expectNoError: true,
		},
		{
			name: "expired certificate - should be skipped",
			setupCert: func(t *testing.T, certPath string, ca []byte) error {
				return createExpiredCertificate(t, certPath, ca)
			},
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						CertificateAuthority: nil, // Will be set by test
					},
				},
			},
			expectSkipped: true,
		},
		{
			name: "certificate not yet valid - should be skipped",
			setupCert: func(t *testing.T, certPath string, ca []byte) error {
				return createFutureCertificate(t, certPath, ca)
			},
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						CertificateAuthority: nil, // Will be set by test
					},
				},
			},
			expectSkipped: true,
		},
		{
			name: "invalid certificate format",
			setupCert: func(t *testing.T, certPath string, ca []byte) error {
				return os.WriteFile(certPath, []byte("invalid certificate data"), 0o644)
			},
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						CertificateAuthority: []byte("test-ca"),
					},
				},
			},
			expectedErr: "validating kubelet certificate",
		},
		{
			name: "certificate with wrong CA",
			setupCert: func(t *testing.T, certPath string, ca []byte) error {
				// Create certificate with different CA
				wrongCA := generateCA(t)
				return createValidCertificate(t, certPath, wrongCA)
			},
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						CertificateAuthority: nil, // Will be set by test
					},
				},
			},
			expectedErr: "certificate is not valid for the current cluster",
		},
		{
			name: "certificate file read error",
			setupCert: func(t *testing.T, certPath string, ca []byte) error {
				// Create a directory instead of a file to cause read error
				return os.Mkdir(certPath, 0o755)
			},
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						CertificateAuthority: []byte("test-ca"),
					},
				},
			},
			expectedErr: "validating kubelet certificate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()

			// Create temporary directory for test certificates
			tempDir, err := os.MkdirTemp("", "cert-test-*")
			g.Expect(err).NotTo(HaveOccurred())
			defer os.RemoveAll(tempDir)

			certPath := filepath.Join(tempDir, "kubelet-server.crt")

			// Generate CA for tests that need it
			var ca []byte
			var caKey *rsa.PrivateKey
			if tt.nodeConfig.Spec.Cluster.CertificateAuthority == nil &&
				(tt.name == "valid certificate with matching CA" ||
					tt.name == "expired certificate - should be skipped" ||
					tt.name == "certificate not yet valid - should be skipped" ||
					tt.name == "certificate with wrong CA") {
				ca, caKey = generateCAWithKey(t)
				tt.nodeConfig.Spec.Cluster.CertificateAuthority = ca
			}

			// Setup certificate based on test case
			if tt.name == "valid certificate with matching CA" {
				err = createValidCertificateWithCA(t, certPath, ca, caKey)
			} else {
				err = tt.setupCert(t, certPath, ca)
			}
			g.Expect(err).NotTo(HaveOccurred())

			// Create HybridNodeProvider with custom cert path
			np, err := hybrid.NewHybridNodeProvider(
				tt.nodeConfig,
				[]string{}, // Don't skip any validations
				zap.NewNop(),
				hybrid.WithCertPath(certPath),
			)
			g.Expect(err).NotTo(HaveOccurred())

			// Cast to concrete type to access ValidateCertificateIfExists method
			hnp := np.(*hybrid.HybridNodeProvider)

			// Create mock informer to capture validation calls
			informer := &mockInformer{}

			// Call the function under test
			err = hnp.ValidateCertificateIfExists(ctx, informer, tt.nodeConfig)

			// Verify results
			if tt.expectNoError {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(informer.startingCalled).To(BeTrue())
				g.Expect(informer.doneCalled).To(BeTrue())
				g.Expect(informer.doneError).To(BeNil())
			} else if tt.expectSkipped {
				// For date validation errors and no cert errors, function should return nil
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(informer.startingCalled).To(BeTrue())
				g.Expect(informer.doneCalled).To(BeTrue())
				// The original error should be passed to Done, but function returns nil
				g.Expect(informer.doneError).NotTo(BeNil())
			} else {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.expectedErr))
				g.Expect(informer.startingCalled).To(BeTrue())
				g.Expect(informer.doneCalled).To(BeTrue())
				g.Expect(informer.doneError).NotTo(BeNil())
			}

			// Verify informer was called with correct parameters
			g.Expect(informer.startingName).To(Equal("kubelet-cert-validation"))
			g.Expect(informer.startingMessage).To(Equal("Validating kubelet server certificate"))
			g.Expect(informer.doneName).To(Equal("kubelet-cert-validation"))
		})
	}
}

func TestHybridNodeProvider_ValidateCertificateIfExists_RemediationMessages(t *testing.T) {
	tests := []struct {
		name           string
		setupCert      func(t *testing.T, certPath string) error
		expectedRemedy string
	}{
		{
			name: "invalid format error includes remediation",
			setupCert: func(t *testing.T, certPath string) error {
				return os.WriteFile(certPath, []byte("invalid certificate data"), 0o644)
			},
			expectedRemedy: "Delete the kubelet server certificate file",
		},
		{
			name: "certificate with wrong CA includes remediation",
			setupCert: func(t *testing.T, certPath string) error {
				wrongCA := generateCA(t)
				return createValidCertificate(t, certPath, wrongCA)
			},
			expectedRemedy: "Please remove the kubelet server certificate file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()

			// Create temporary directory for test certificates
			tempDir, err := os.MkdirTemp("", "cert-test-*")
			g.Expect(err).NotTo(HaveOccurred())
			defer os.RemoveAll(tempDir)

			certPath := filepath.Join(tempDir, "kubelet-server.crt")

			// Setup certificate
			err = tt.setupCert(t, certPath)
			g.Expect(err).NotTo(HaveOccurred())

			nodeConfig := &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						CertificateAuthority: generateCA(t),
					},
				},
			}

			// Create HybridNodeProvider with custom cert path
			np, err := hybrid.NewHybridNodeProvider(
				nodeConfig,
				[]string{},
				zap.NewNop(),
				hybrid.WithCertPath(certPath),
			)
			g.Expect(err).NotTo(HaveOccurred())

			// Cast to concrete type to access ValidateCertificateIfExists method
			hnp := np.(*hybrid.HybridNodeProvider)

			informer := &mockInformer{}

			// Call the function under test
			err = hnp.ValidateCertificateIfExists(ctx, informer, nodeConfig)

			// Verify error contains remediation
			g.Expect(err).To(HaveOccurred())
			g.Expect(validation.IsRemediable(err)).To(BeTrue())
			g.Expect(validation.Remediation(err)).To(ContainSubstring(tt.expectedRemedy))
		})
	}
}

// mockInformer implements validation.Informer for testing
type mockInformer struct {
	startingCalled  bool
	startingName    string
	startingMessage string
	doneCalled      bool
	doneName        string
	doneError       error
}

func (m *mockInformer) Starting(ctx context.Context, name, message string) {
	m.startingCalled = true
	m.startingName = name
	m.startingMessage = message
}

func (m *mockInformer) Done(ctx context.Context, name string, err error) {
	m.doneCalled = true
	m.doneName = name
	m.doneError = err
}

// Helper functions for creating test certificates

// generateCAWithKey returns both the CA certificate and private key for testing
func generateCAWithKey(t *testing.T) ([]byte, *rsa.PrivateKey) {
	// Generate CA private key
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate CA key: %v", err)
	}

	// Create CA certificate template
	caTemplate := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test CA"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// Create CA certificate
	caCertDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("Failed to create CA certificate: %v", err)
	}

	// Encode CA certificate to PEM
	caCertPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caCertDER,
	})

	return caCertPEM, caKey
}

func generateCA(t *testing.T) []byte {
	ca, _ := generateCAWithKey(t)
	return ca
}

func createValidCertificateWithCA(t *testing.T, certPath string, ca []byte, caKey *rsa.PrivateKey) error {
	// Generate private key for the certificate
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate key: %w", err)
	}

	// Parse CA certificate
	caBlock, _ := pem.Decode(ca)
	if caBlock == nil {
		return fmt.Errorf("failed to decode CA certificate")
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"Test Kubelet"},
		},
		Issuer:      caCert.Subject,
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:    []string{"localhost"},
	}

	// Create certificate signed by CA
	certDER, err := x509.CreateCertificate(rand.Reader, &template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	return os.WriteFile(certPath, certPEM, 0o644)
}

func createValidCertificate(t *testing.T, certPath string, ca []byte) error {
	// Generate private key
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate key: %w", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"Test Kubelet"},
		},
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:    []string{"localhost"},
	}

	var certDER []byte
	if ca != nil {
		// For this function, we'll create a self-signed certificate that claims to be issued by the CA
		// but is actually self-signed. This will cause CA validation to fail, which is what we want
		// for the "certificate with wrong CA" test case.
		caBlock, _ := pem.Decode(ca)
		if caBlock == nil {
			return fmt.Errorf("failed to decode CA certificate")
		}
		caCert, err := x509.ParseCertificate(caBlock.Bytes)
		if err != nil {
			return fmt.Errorf("failed to parse CA certificate: %w", err)
		}

		// Set the issuer to the CA but sign with our own key (self-signed)
		// This will create a certificate that claims to be issued by the CA but isn't actually signed by it
		template.Issuer = caCert.Subject
		certDER, err = x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
		if err != nil {
			return fmt.Errorf("failed to create certificate: %w", err)
		}
	} else {
		// Create self-signed certificate
		certDER, err = x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
		if err != nil {
			return fmt.Errorf("failed to create certificate: %w", err)
		}
	}

	// Encode certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	return os.WriteFile(certPath, certPEM, 0o644)
}

func createExpiredCertificate(t *testing.T, certPath string, ca []byte) error {
	// Generate private key
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate key: %w", err)
	}

	// Create expired certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject: pkix.Name{
			Organization: []string{"Test Kubelet Expired"},
		},
		NotBefore:   time.Now().Add(-48 * time.Hour), // Started 2 days ago
		NotAfter:    time.Now().Add(-24 * time.Hour), // Expired 1 day ago
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	return os.WriteFile(certPath, certPEM, 0o644)
}

func createFutureCertificate(t *testing.T, certPath string, ca []byte) error {
	// Generate private key
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate key: %w", err)
	}

	// Create future certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(4),
		Subject: pkix.Name{
			Organization: []string{"Test Kubelet Future"},
		},
		NotBefore:   time.Now().Add(24 * time.Hour), // Starts tomorrow
		NotAfter:    time.Now().Add(48 * time.Hour), // Expires day after tomorrow
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	return os.WriteFile(certPath, certPEM, 0o644)
}

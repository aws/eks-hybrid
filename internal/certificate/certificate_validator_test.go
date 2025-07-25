package certificate

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/eks-hybrid/internal/validation"
)

func TestIsDateValidationError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "clock skew error",
			err:      &CertClockSkewError{baseError{message: "test"}},
			expected: true,
		},
		{
			name:     "expired cert error",
			err:      &CertExpiredError{baseError{message: "test"}},
			expected: true,
		},
		{
			name:     "other error",
			err:      &CertNotFoundError{baseError{message: "test"}},
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDateValidationError(tt.err); got != tt.expected {
				t.Errorf("IsDateValidationError() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsNoCertError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "cert not found error",
			err:      &CertNotFoundError{baseError{message: "test"}},
			expected: true,
		},
		{
			name:     "other error",
			err:      &CertExpiredError{baseError{message: "test"}},
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNoCertError(tt.err); got != tt.expected {
				t.Errorf("IsNoCertError() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBaseError(t *testing.T) {
	tests := []struct {
		name           string
		message        string
		cause          error
		expectedString string
	}{
		{
			name:           "with cause",
			message:        "test message",
			cause:          errors.New("cause error"),
			expectedString: "test message: cause error",
		},
		{
			name:           "without cause",
			message:        "test message",
			cause:          nil,
			expectedString: "test message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &baseError{
				message: tt.message,
				cause:   tt.cause,
			}

			if got := err.Error(); got != tt.expectedString {
				t.Errorf("Error() = %v, want %v", got, tt.expectedString)
			}

			if got := err.Unwrap(); got != tt.cause {
				t.Errorf("Unwrap() = %v, want %v", got, tt.cause)
			}
		})
	}
}

// Helper function to create test certificates
func createTestCertificate(notBefore, notAfter time.Time) ([]byte, []byte, error) {
	// Create a CA certificate
	caPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	caTemplate := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "Test CA",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caBytes, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return nil, nil, err
	}

	caPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caBytes,
	})

	// Create a server certificate signed by the CA
	serverPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	serverTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			CommonName: "Test Server",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	serverBytes, err := x509.CreateCertificate(rand.Reader, &serverTemplate, &caTemplate, &serverPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return nil, nil, err
	}

	serverPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: serverBytes,
	})

	return caPEM, serverPEM, nil
}

func TestValidate(t *testing.T) {
	tempDir := t.TempDir()

	// Create test certificates
	now := time.Now()
	validCA, validCert, err := createTestCertificate(now.Add(-1*time.Hour), now.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("Failed to create valid certificate: %v", err)
	}

	futureCA, futureCert, err := createTestCertificate(now.Add(1*time.Hour), now.Add(25*time.Hour))
	if err != nil {
		t.Fatalf("Failed to create future certificate: %v", err)
	}

	expiredCA, expiredCert, err := createTestCertificate(now.Add(-25*time.Hour), now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to create expired certificate: %v", err)
	}

	// Create another CA for invalid CA test
	otherCA, _, err := createTestCertificate(now.Add(-1*time.Hour), now.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("Failed to create other CA: %v", err)
	}

	// Write certificates to files
	validCertPath := filepath.Join(tempDir, "valid.crt")
	if err := os.WriteFile(validCertPath, validCert, 0o644); err != nil {
		t.Fatalf("Failed to write valid certificate: %v", err)
	}

	futureCertPath := filepath.Join(tempDir, "future.crt")
	if err := os.WriteFile(futureCertPath, futureCert, 0o644); err != nil {
		t.Fatalf("Failed to write future certificate: %v", err)
	}

	expiredCertPath := filepath.Join(tempDir, "expired.crt")
	if err := os.WriteFile(expiredCertPath, expiredCert, 0o644); err != nil {
		t.Fatalf("Failed to write expired certificate: %v", err)
	}

	invalidFormatPath := filepath.Join(tempDir, "invalid.crt")
	if err := os.WriteFile(invalidFormatPath, []byte("not a valid certificate"), 0o644); err != nil {
		t.Fatalf("Failed to write invalid certificate: %v", err)
	}

	nonExistentPath := filepath.Join(tempDir, "nonexistent.crt")

	// Create a path that can't be read
	unreadablePath := filepath.Join(tempDir, "unreadable.crt")
	if err := os.WriteFile(unreadablePath, validCert, 0o644); err != nil {
		t.Fatalf("Failed to write unreadable certificate: %v", err)
	}
	// On Unix systems, we can make it unreadable
	if err := os.Chmod(unreadablePath, 0o000); err != nil {
		t.Logf("Could not change permissions for unreadable test: %v", err)
	}

	tests := []struct {
		name      string
		certPath  string
		ca        []byte
		wantError bool
		errorType interface{}
	}{
		{
			name:      "valid certificate with CA",
			certPath:  validCertPath,
			ca:        validCA,
			wantError: false,
		},
		{
			name:      "valid certificate without CA",
			certPath:  validCertPath,
			ca:        nil,
			wantError: false,
		},
		{
			name:      "certificate not found",
			certPath:  nonExistentPath,
			ca:        validCA,
			wantError: true,
			errorType: &CertNotFoundError{},
		},
		{
			name:      "certificate with future validity",
			certPath:  futureCertPath,
			ca:        futureCA,
			wantError: true,
			errorType: &CertClockSkewError{},
		},
		{
			name:      "expired certificate",
			certPath:  expiredCertPath,
			ca:        expiredCA,
			wantError: true,
			errorType: &CertExpiredError{},
		},
		{
			name:      "invalid certificate format",
			certPath:  invalidFormatPath,
			ca:        validCA,
			wantError: true,
			errorType: &CertInvalidFormatError{},
		},
		{
			name:      "certificate with wrong CA",
			certPath:  validCertPath,
			ca:        otherCA,
			wantError: true,
			errorType: &CertInvalidCAError{},
		},
		{
			name:      "invalid CA format",
			certPath:  validCertPath,
			ca:        []byte("not a valid CA"),
			wantError: true,
			errorType: &CertParseCAError{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.certPath, tt.ca)

			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if tt.wantError && tt.errorType != nil {
				if err == nil {
					t.Errorf("Expected error of type %T, got nil", tt.errorType)
					return
				}

				switch tt.errorType.(type) {
				case *CertNotFoundError:
					if _, ok := err.(*CertNotFoundError); !ok {
						t.Errorf("Expected error of type %T, got %T", tt.errorType, err)
					}
				case *CertClockSkewError:
					if _, ok := err.(*CertClockSkewError); !ok {
						t.Errorf("Expected error of type %T, got %T", tt.errorType, err)
					}
				case *CertExpiredError:
					if _, ok := err.(*CertExpiredError); !ok {
						t.Errorf("Expected error of type %T, got %T", tt.errorType, err)
					}
				case *CertInvalidFormatError:
					if _, ok := err.(*CertInvalidFormatError); !ok {
						t.Errorf("Expected error of type %T, got %T", tt.errorType, err)
					}
				case *CertInvalidCAError:
					if _, ok := err.(*CertInvalidCAError); !ok {
						t.Errorf("Expected error of type %T, got %T", tt.errorType, err)
					}
				case *CertParseCAError:
					if _, ok := err.(*CertParseCAError); !ok {
						t.Errorf("Expected error of type %T, got %T", tt.errorType, err)
					}
				}
			}
		})
	}

	// Skip the unreadable file test if we couldn't set permissions
	if fi, err := os.Stat(unreadablePath); err == nil && fi.Mode().Perm()&0o400 == 0 {
		t.Run("unreadable certificate", func(t *testing.T) {
			err := Validate(unreadablePath, validCA)
			if err == nil {
				t.Errorf("Expected error for unreadable certificate, got nil")
				return
			}
			if _, ok := err.(*CertReadError); !ok {
				t.Errorf("Expected error of type *CertReadError, got %T", err)
			}
		})
	}
}

func TestAddKubeletRemediation(t *testing.T) {
	certPath := "/path/to/cert.pem"

	tests := []struct {
		name           string
		err            error
		expectedPrefix string
		containsText   string
	}{
		{
			name:           "CertNotFoundError",
			err:            &CertNotFoundError{baseError{message: "test"}},
			expectedPrefix: "validating kubelet certificate: test",
			containsText:   "Kubelet certificate will be created",
		},
		{
			name:           "CertFileError",
			err:            &CertFileError{baseError{message: "test"}},
			expectedPrefix: "validating kubelet certificate: test",
			containsText:   "Kubelet certificate will be created",
		},
		{
			name:           "CertReadError",
			err:            &CertReadError{baseError{message: "test"}},
			expectedPrefix: "validating kubelet certificate: test",
			containsText:   "Kubelet certificate will be created",
		},
		{
			name:           "CertInvalidFormatError",
			err:            &CertInvalidFormatError{baseError{message: "test"}},
			expectedPrefix: "validating kubelet certificate: test",
			containsText:   "Delete the kubelet server certificate file",
		},
		{
			name:           "CertClockSkewError",
			err:            &CertClockSkewError{baseError{message: "test"}},
			expectedPrefix: "validating kubelet certificate: test",
			containsText:   "Verify the system time is correct",
		},
		{
			name:           "CertExpiredError",
			err:            &CertExpiredError{baseError{message: "test"}},
			expectedPrefix: "validating kubelet certificate: test",
			containsText:   "Delete the kubelet server certificate file",
		},
		{
			name:           "CertParseCAError",
			err:            &CertParseCAError{baseError{message: "test"}},
			expectedPrefix: "validating kubelet certificate: test",
			containsText:   "Ensure the cluster CA certificate is valid",
		},
		{
			name:           "CertInvalidCAError",
			err:            &CertInvalidCAError{baseError{message: "test"}},
			expectedPrefix: "validating kubelet certificate: test",
			containsText:   "Please remove the kubelet server certificate file",
		},
		{
			name:           "other error",
			err:            errors.New("generic error"),
			expectedPrefix: "validating kubelet certificate: generic error",
			containsText:   "", // No remediation for generic errors
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AddKubeletRemediation(certPath, tt.err)

			if result == nil {
				t.Fatalf("AddKubeletRemediation() returned nil")
			}

			errStr := result.Error()
			// Check if the error message contains the original error message
			if !contains(errStr, tt.err.Error()) {
				t.Errorf("AddKubeletRemediation() error message does not contain original error message.\nGot: %s\nExpected to contain: %s", errStr, tt.err.Error())
			}

			if tt.expectedPrefix != "" && errStr[:len(tt.expectedPrefix)] != tt.expectedPrefix {
				t.Errorf("Error does not start with expected prefix.\nGot: %s\nWant prefix: %s", errStr, tt.expectedPrefix)
			}

			// Check for remediation text
			if tt.containsText != "" {
				remediable, ok := result.(validation.Remediable)
				if !ok {
					t.Errorf("Expected error to implement validation.Remediable, got %T", result)
					return
				}

				remediation := remediable.Remediation()
				if remediation == "" {
					t.Errorf("Expected remediation text, got empty string")
				}

				if tt.containsText != "" && !contains(remediation, tt.containsText) {
					t.Errorf("Remediation text does not contain expected content.\nGot: %s\nExpected to contain: %s", remediation, tt.containsText)
				}
			}
		})
	}
}

// Helper function to check if a string contains another string
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

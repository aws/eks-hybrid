package kubelet

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"
)

// CertValidationError represents a kubelet certificate validation error with remediation information
type CertValidationError struct {
	message          string
	remediation      string
	cause            error
	isDateValidation bool
	noCert           bool
}

func (e *CertValidationError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.message, e.cause)
	}
	return e.message
}

func (e *CertValidationError) Unwrap() error {
	return e.cause
}

func (e *CertValidationError) Remediation() string {
	return e.remediation
}

func IsDateValidationError(err error) bool {
	var v *CertValidationError
	return errors.As(err, &v) && v.isDateValidation
}

func IsNoCertError(err error) bool {
	var v *CertValidationError
	return errors.As(err, &v) && v.noCert
}

// ValidateKubeletCert checks if there is an existing kubelet certificate and validates it against the cluster's CA
func ValidateKubeletCert(certPath string, ca []byte) error {
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		// Return an error for no cert, but one that can be identified
		return &CertValidationError{
			message:     "no kubelet certificate found",
			remediation: "Kubelet certificate will be created when the kubelet is able to authenticate with the API server. Check previous authentication remediation advice.",
			noCert:      true,
		}
	} else if err != nil {
		return &CertValidationError{
			message:     "checking kubelet certificate",
			remediation: "Kubelet certificate will be created when the kubelet is able to authenticate with the API server. Check previous authentication remediation advice.",
			cause:       err,
		}
	}

	certData, err := os.ReadFile(certPath)
	if err != nil {
		return &CertValidationError{
			message:     "reading kubelet certificate",
			remediation: "Kubelet certificate will be created when the kubelet is able to authenticate with the API server. Check previous authentication remediation advice.",
			cause:       err,
		}
	}

	block, _ := pem.Decode(certData)
	if block == nil {
		return &CertValidationError{
			message:     "parsing kubelet certificate",
			remediation: fmt.Sprintf("Delete the kubelet server certificate file %s and restart kubelet", certPath),
		}
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return &CertValidationError{
			message:     "parsing kubelet certificate",
			remediation: fmt.Sprintf("Delete the kubelet server certificate file %s and restart kubelet", certPath),
			cause:       err,
		}
	}

	now := time.Now()
	if now.Before(cert.NotBefore) {
		return &CertValidationError{
			message:          "kubelet server certificate is not yet valid",
			remediation:      "Verify the system time is correct and restart the kubelet.",
			isDateValidation: true,
		}
	}

	if now.After(cert.NotAfter) {
		return &CertValidationError{
			message:          "kubelet server certificate has expired",
			remediation:      fmt.Sprintf("Delete the kubelet server certificate file %s and restart kubelet. Validate `serverTLSBootstrap` is true in the kubelet config /etc/kubernetes/kubelet/config.json to automatically rotate the certificate.", certPath),
			isDateValidation: true,
		}
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(ca) {
		return &CertValidationError{
			message:     "parsing cluster CA certificate",
			remediation: "Ensure the cluster CA certificate is valid",
		}
	}

	opts := x509.VerifyOptions{
		Roots:       caPool,
		CurrentTime: now,
	}

	if _, err := cert.Verify(opts); err != nil {
		return &CertValidationError{
			message:     "kubelet certificate is not valid for the current cluster",
			remediation: fmt.Sprintf("Please remove the kubelet server certificate file %s or use \"--skip kubelet-cert-validation\" if this is expected", certPath),
			cause:       err,
		}
	}

	return nil
}

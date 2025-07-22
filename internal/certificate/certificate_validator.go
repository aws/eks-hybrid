package certificate

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/aws/eks-hybrid/internal/validation"
)

const KubeletCertValidation = "kubelet-cert-validation"

type baseError struct {
	message string
	cause   error
}

func (e *baseError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.message, e.cause)
	}
	return e.message
}

func (e *baseError) Unwrap() error {
	return e.cause
}

type CertNotFoundError struct {
	baseError
}

type CertFileError struct {
	baseError
}

type CertReadError struct {
	baseError
}

type CertInvalidFormatError struct {
	baseError
}

type CertClockSkewError struct {
	baseError
}

type CertExpiredError struct {
	baseError
}

type CertParseCAError struct {
	baseError
}

type CertInvalidCAError struct {
	baseError
}

func IsDateValidationError(err error) bool {
	var clockSkew *CertClockSkewError
	var expiredCrt *CertExpiredError
	return errors.As(err, &clockSkew) || errors.As(err, &expiredCrt)
}

func IsNoCertError(err error) bool {
	var notCrtFound *CertNotFoundError
	return errors.As(err, &notCrtFound)
}

// Validate checks if there is an existing certificate and validates it against the provided CA
func Validate(certPath string, ca []byte) error {
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		// Return an error for no cert, but one that can be identified
		return &CertNotFoundError{baseError{message: "no certificate found", cause: err}}
	} else if err != nil {
		return &CertFileError{baseError{message: "checking certificate", cause: err}}
	}

	certData, err := os.ReadFile(certPath)
	if err != nil {
		return &CertReadError{baseError{message: "reading certificate", cause: err}}
	}

	block, _ := pem.Decode(certData)
	if block == nil {
		return &CertInvalidFormatError{baseError{message: "parsing certificate"}}
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return &CertInvalidFormatError{baseError{message: "parsing certificate", cause: err}}
	}

	now := time.Now()
	if now.Before(cert.NotBefore) {
		return &CertClockSkewError{baseError{message: "server certificate is not yet valid"}}
	}

	if now.After(cert.NotAfter) {
		return &CertExpiredError{baseError{message: "server certificate has expired"}}
	}

	if len(ca) > 0 {
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(ca) {
			return &CertParseCAError{baseError{message: "parsing cluster CA certificate"}}
		}

		opts := x509.VerifyOptions{
			Roots:       caPool,
			CurrentTime: now,
		}

		if _, err := cert.Verify(opts); err != nil {
			return &CertInvalidCAError{baseError{message: "certificate is not valid for the current cluster", cause: err}}
		}
	}

	return nil
}

// AddKubeletRemediation adds kubelet-specific remediation messages based on error type
func AddKubeletRemediation(certPath string, err error) error {
	errWithContext := fmt.Errorf("validating kubelet certificate: %w", err)

	switch err.(type) {
	case *CertNotFoundError, *CertFileError, *CertReadError:
		return validation.WithRemediation(errWithContext, "Kubelet certificate will be created when the kubelet is able to authenticate with the API server. Check previous authentication remediation advice.")
	case *CertInvalidFormatError:
		return validation.WithRemediation(errWithContext, fmt.Sprintf("Delete the kubelet server certificate file %s and restart kubelet", certPath))
	case *CertClockSkewError:
		return validation.WithRemediation(errWithContext, "Verify the system time is correct and restart the kubelet.")
	case *CertExpiredError:
		return validation.WithRemediation(errWithContext, fmt.Sprintf("Delete the kubelet server certificate file %s and restart kubelet. Validate `serverTLSBootstrap` is true in the kubelet config /etc/kubernetes/kubelet/config.json to automatically rotate the certificate", certPath))
	case *CertParseCAError:
		return validation.WithRemediation(errWithContext, "Ensure the cluster CA certificate is valid")
	case *CertInvalidCAError:
		return validation.WithRemediation(errWithContext, fmt.Sprintf("Please remove the kubelet server certificate file %s or use \"--skip %s\" if this is expected", certPath, KubeletCertValidation))
	}

	return errWithContext
}

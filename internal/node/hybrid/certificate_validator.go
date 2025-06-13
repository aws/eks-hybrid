package hybrid

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"
)

// CertificateType represents the type of certificate being validated
type CertificateType string

const (
	CertTypeKubelet CertificateType = "kubelet"            // kubelet certificate
	CertTypeIAMRA   CertificateType = "iam-roles-anywhere" // IAM Roles Anywhere certificate
)

// ValidationError represents a kubelet certificate validation error with remediation information
type ValidationError struct {
	message          string
	remediation      string
	cause            error
	isDateValidation bool
	noCert           bool
}

type RemediationMessages struct {
	noCert            string
	invalidFormat     string
	clockSkewDetected string
	expired           string
	invalidCA         string
}

func (e *ValidationError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.message, e.cause)
	}
	return e.message
}

func (e *ValidationError) Unwrap() error {
	return e.cause
}

func (e *ValidationError) Remediation() string {
	return e.remediation
}

func IsDateValidationError(err error) bool {
	var v *ValidationError
	return errors.As(err, &v) && v.isDateValidation
}

func IsNoCertError(err error) bool {
	var v *ValidationError
	return errors.As(err, &v) && v.noCert
}

func getRemediationMessage(certPath string, certType CertificateType) RemediationMessages {
	switch certType {
	case CertTypeKubelet:
		return RemediationMessages{
			noCert:            "Kubelet certificate will be created when the kubelet is able to authenticate with the API server. Check previous authentication remediation advice.",
			invalidFormat:     fmt.Sprintf("Delete the kubelet server certificate file %s and restart kubelet", certPath),
			clockSkewDetected: "Verify the system time is correct and restart the kubelet.",
			expired:           fmt.Sprintf("Delete the kubelet server certificate file %s and restart kubelet. Validate `serverTLSBootstrap` is true in the kubelet config /etc/kubernetes/kubelet/config.json to automatically rotate the certificate.", certPath),
			invalidCA:         fmt.Sprintf("Please remove the kubelet server certificate file %s or use \"--skip %s\" if this is expected", certPath, kubeletCertValidation),
		}

	case CertTypeIAMRA:
		return RemediationMessages{
			noCert:            fmt.Sprintf("Certificate file not found at %s. Please provide a valid IAM Roles Anywhere certificate.", certPath),
			invalidFormat:     fmt.Sprintf("The IAM Roles Anywhere certificate at %s is not in a valid format. Please provide a valid certificate.", certPath),
			clockSkewDetected: fmt.Sprintf("Verify the system time. Current system time is behind IAM Roles Anywhere certificate's start time at %s", certPath),
			expired:           fmt.Sprintf("IAM Roles Anywhere certificate at %s has expired. Please provide a valid, non-expired certificate.", certPath),
			invalidCA:         fmt.Sprintf("IAM Roles Anywhere certificate at %s is not signed by a trusted Certificate Authority. Please provide a certificate signed by a valid CA.", certPath),
		}

	default:
		return RemediationMessages{
			noCert:            fmt.Sprintf("Certificate file not found at %s. Please provide a valid certificate.", certPath),
			invalidFormat:     fmt.Sprintf("The certificate at %s is not in a valid format. Please provide a valid certificate.", certPath),
			clockSkewDetected: fmt.Sprintf("Verify the system time. Current system time is behind certificate's start time at %s", certPath),
			expired:           fmt.Sprintf("Certificate at %s has expired. Please provide a valid, non-expired certificate.", certPath),
			invalidCA:         fmt.Sprintf("Certificate at %s is not signed by a trusted Certificate Authority. Please provide a certificate signed by a valid CA.", certPath),
		}
	}
}

// ValidateCertificate checks if there is an existing certificate and validates it against the provided CA
func ValidateCertificate(certPath string, ca []byte, certType CertificateType) error {
	remediation := getRemediationMessage(certPath, certType)
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		// Return an error for no cert, but one that can be identified
		return &ValidationError{
			message:     fmt.Sprintf("no %s certificate found", certType),
			remediation: remediation.noCert,
			noCert:      true,
		}
	} else if err != nil {
		return &ValidationError{
			message:     fmt.Sprintf("checking %s certificate", certType),
			remediation: remediation.noCert,
			cause:       err,
		}
	}

	certData, err := os.ReadFile(certPath)
	if err != nil {
		return &ValidationError{
			message:     fmt.Sprintf("reading %s certificate", certType),
			remediation: remediation.noCert,
			cause:       err,
		}
	}

	block, _ := pem.Decode(certData)
	if block == nil {
		return &ValidationError{
			message:     fmt.Sprintf("parsing %s certificate", certType),
			remediation: remediation.invalidFormat,
		}
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return &ValidationError{
			message:     fmt.Sprintf("parsing %s certificate", certType),
			remediation: remediation.invalidFormat,
			cause:       err,
		}
	}

	now := time.Now()
	if now.Before(cert.NotBefore) {
		return &ValidationError{
			message:          fmt.Sprintf("%s server certificate is not yet valid", certType),
			remediation:      remediation.clockSkewDetected,
			isDateValidation: true,
		}
	}

	if now.After(cert.NotAfter) {
		return &ValidationError{
			message:          fmt.Sprintf("%s server certificate has expired", certType),
			remediation:      remediation.expired,
			isDateValidation: true,
		}
	}

	if len(ca) > 0 {
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(ca) {
			return &ValidationError{
				message:     "parsing cluster CA certificate",
				remediation: "Ensure the cluster CA certificate is valid",
			}
		}

		opts := x509.VerifyOptions{
			Roots:       caPool,
			CurrentTime: now,
		}

		if _, err := cert.Verify(opts); err != nil {
			return &ValidationError{
				message:     fmt.Sprintf("%s certificate is not valid for the current cluster", certType),
				remediation: remediation.invalidCA,
				cause:       err,
			}
		}
	}

	return nil
}

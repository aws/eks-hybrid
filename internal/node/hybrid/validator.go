package hybrid

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/util/file"
	"github.com/aws/eks-hybrid/internal/validation"
)

const (
	// https://docs.aws.amazon.com/systems-manager/latest/APIReference/API_CreateActivation.html#systemsmanager-CreateActivation-response-ActivationId
	ssmActivationIDPattern   = `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	ssmActivationCodePattern = `^.{20,250}$`
)

func extractFlagValue(args []string, flag string) string {
	flagPrefix := "--" + flag + "="
	var flagValue string

	// get last instance of flag value if it exists
	for _, arg := range args {
		if strings.HasPrefix(arg, flagPrefix) {
			flagValue = strings.TrimPrefix(arg, flagPrefix)
		}
	}

	return flagValue
}

func (hnp *HybridNodeProvider) withHybridValidators() {
	hnp.validator = func(cfg *api.NodeConfig) error {
		if cfg.Spec.Cluster.Name == "" {
			return fmt.Errorf("Name is missing in cluster configuration")
		}
		if cfg.Spec.Cluster.Region == "" {
			return fmt.Errorf("Region is missing in cluster configuration")
		}
		if hostnameOverride := extractFlagValue(cfg.Spec.Kubelet.Flags, hostnameOverrideFlag); hostnameOverride != "" {
			return fmt.Errorf("hostname-override kubelet flag is not supported for hybrid nodes but found override: %s", hostnameOverride)
		}
		if !cfg.IsIAMRolesAnywhere() && !cfg.IsSSM() {
			return fmt.Errorf("Either IAMRolesAnywhere or SSM must be provided for hybrid node configuration")
		}
		if cfg.IsIAMRolesAnywhere() && cfg.IsSSM() {
			return fmt.Errorf("Only one of IAMRolesAnywhere or SSM must be provided for hybrid node configuration")
		}
		if cfg.IsIAMRolesAnywhere() {
			if err := validateRolesAnywhereNode(cfg); err != nil {
				return err
			}
		}
		if cfg.IsSSM() {
			if cfg.Spec.Hybrid.SSM.ActivationCode == "" {
				return fmt.Errorf("ActivationCode is missing in hybrid ssm configuration")
			}
			if cfg.Spec.Hybrid.SSM.ActivationID == "" {
				return fmt.Errorf("ActivationID is missing in hybrid ssm configuration")
			}

			// Compile the activation code pattern
			reCode, err := regexp.Compile(ssmActivationCodePattern)
			if err != nil {
				return fmt.Errorf("internal error: invalid ActivationCode pattern: %v", err)
			}
			// Check if ActivationCode matches the pattern
			if !reCode.MatchString(cfg.Spec.Hybrid.SSM.ActivationCode) {
				return fmt.Errorf("invalid ActivationCode format: %s. Must be 20-250 characters", cfg.Spec.Hybrid.SSM.ActivationCode)
			}

			// Compile the regex patterns
			reID, err := regexp.Compile(ssmActivationIDPattern)
			if err != nil {
				return fmt.Errorf("internal error: invalid ActivationID pattern: %v", err)
			}
			// Check if ActivationID matches the pattern
			if !reID.MatchString(cfg.Spec.Hybrid.SSM.ActivationID) {
				return fmt.Errorf("invalid ActivationID format: %s. Must be in format: ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", cfg.Spec.Hybrid.SSM.ActivationID)
			}
		}
		return nil
	}
}

func (hnp *HybridNodeProvider) ValidateConfig() error {
	hnp.logger.Info("Validating configuration...")
	if err := hnp.validator(hnp.nodeConfig); err != nil {
		return err
	}
	return nil
}

func validateRolesAnywhereNode(node *api.NodeConfig) error {
	if node.Spec.Hybrid.IAMRolesAnywhere.RoleARN == "" {
		return fmt.Errorf("RoleARN is missing in hybrid iam roles anywhere configuration")
	}
	if node.Spec.Hybrid.IAMRolesAnywhere.ProfileARN == "" {
		return fmt.Errorf("ProfileARN is missing in hybrid iam roles anywhere configuration")
	}
	if node.Spec.Hybrid.IAMRolesAnywhere.TrustAnchorARN == "" {
		return fmt.Errorf("TrustAnchorARN is missing in hybrid iam roles anywhere configuration")
	}
	if node.Spec.Hybrid.IAMRolesAnywhere.NodeName == "" {
		return fmt.Errorf("NodeName can't be empty in hybrid iam roles anywhere configuration")
	}
	if len(node.Spec.Hybrid.IAMRolesAnywhere.NodeName) > 64 {
		return fmt.Errorf("NodeName can't be longer than 64 characters in hybrid iam roles anywhere configuration")
	}

	// IAM roles anywhere certificate validation
	if node.Spec.Hybrid.IAMRolesAnywhere.CertificatePath == "" {
		return fmt.Errorf("CertificatePath is missing in hybrid iam roles anywhere configuration")
	}
	if !file.Exists(node.Spec.Hybrid.IAMRolesAnywhere.CertificatePath) {
		return fmt.Errorf("IAM Roles Anywhere certificate %s not found", node.Spec.Hybrid.IAMRolesAnywhere.CertificatePath)
	}
	if err := ValidateCertificate(node.Spec.Hybrid.IAMRolesAnywhere.CertificatePath, nil); err != nil {
		return addIAMRARemediation(node.Spec.Hybrid.IAMRolesAnywhere.CertificatePath, err)
	}

	// IAM roles anywhere key validation
	if node.Spec.Hybrid.IAMRolesAnywhere.PrivateKeyPath == "" {
		return fmt.Errorf("PrivateKeyPath is missing in hybrid iam roles anywhere configuration")
	}
	if !file.Exists(node.Spec.Hybrid.IAMRolesAnywhere.PrivateKeyPath) {
		return fmt.Errorf("IAM Roles Anywhere private key %s not found", node.Spec.Hybrid.IAMRolesAnywhere.PrivateKeyPath)
	}

	return nil
}

// addIAMRARemediation adds IAM Role Anywhere specific remediation messages based on error type
func addIAMRARemediation(certPath string, err error) error {
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		return err
	}

	errWithContext := fmt.Errorf("error in validating iam-roles-anywhere certificate: %w", err)
	switch validationErr.ErrorType() {
	case ErrorNoCert, ErrorCertFile, ErrorReadFile:
		return validation.WithRemediation(errWithContext, fmt.Sprintf("Certificate file not found at %s. Please provide a valid IAM Roles Anywhere certificate.", certPath))
	case ErrorInvalidFormat:
		return validation.WithRemediation(errWithContext, fmt.Sprintf("The IAM Roles Anywhere certificate at %s is not in a valid format. Please provide a valid certificate.", certPath))
	case ErrorClockSkewDetected:
		return validation.WithRemediation(errWithContext, fmt.Sprintf("Verify the system time. Current system time is behind IAM Roles Anywhere certificate's start time at %s", certPath))
	case ErrorExpired:
		return validation.WithRemediation(errWithContext, fmt.Sprintf("IAM Roles Anywhere certificate at %s has expired. Please provide a valid, non-expired certificate.", certPath))
	case ErrorParseCA:
		return validation.WithRemediation(errWithContext, "Ensure the iam-roles-anywhere CA certificate is valid")
	case ErrorInvalidCA:
		return validation.WithRemediation(errWithContext, fmt.Sprintf("IAM Roles Anywhere certificate at %s is not signed by a trusted Certificate Authority. Please provide a certificate signed by a valid CA.", certPath))
	default:
		return validation.WithRemediation(errWithContext, "IAM Roles Anywhere certificate error")
	}
}

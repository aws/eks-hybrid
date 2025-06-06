package hybrid

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/zap"

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
	hnp.logger.Debug("Validating configuration...")
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
	if node.Spec.Hybrid.IAMRolesAnywhere.CertificatePath == "" {
		return fmt.Errorf("CertificatePath is missing in hybrid iam roles anywhere configuration")
	}
	if node.Spec.Hybrid.IAMRolesAnywhere.PrivateKeyPath == "" {
		return fmt.Errorf("PrivateKeyPath is missing in hybrid iam roles anywhere configuration")
	}

	if !file.Exists(node.Spec.Hybrid.IAMRolesAnywhere.CertificatePath) {
		return fmt.Errorf("IAM Roles Anywhere certificate %s not found", node.Spec.Hybrid.IAMRolesAnywhere.CertificatePath)
	}

	if !file.Exists(node.Spec.Hybrid.IAMRolesAnywhere.PrivateKeyPath) {
		return fmt.Errorf("IAM Roles Anywhere private key %s not found", node.Spec.Hybrid.IAMRolesAnywhere.PrivateKeyPath)
	}

	return nil
}

// Hybrid Node Config Validator returns a validation for hybrid node configuration
func NewHybridNodeConfigValidator(node *api.NodeConfig, log *zap.Logger) validation.Validation[*api.NodeConfig] {
	return validation.New("hybrid-node-config", func(ctx context.Context, informer validation.Informer, nodeConfig *api.NodeConfig) error {
		var err error
		informer.Starting(ctx, "hybrid-node-config", "Validating hybrid node configuration")
		defer func() {
			informer.Done(ctx, "hybrid-node-config", err)
		}()

		hybridNodeProvider, err := NewHybridNodeProvider(node, []string{}, log)
		if err != nil {
			err = validation.WithRemediation(err, "Ensure hybrid node has correct aws configuration")
		}

		if err = hybridNodeProvider.ValidateConfig(); err != nil {
			err = validation.WithRemediation(err, "Ensure correct information in hybrid node configuration file (file://nodeConfig.yaml)")
			return err
		}

		return nil
	})
}

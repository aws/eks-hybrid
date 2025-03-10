package hybrid

import (
	"fmt"
	"strings"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/util/file"
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

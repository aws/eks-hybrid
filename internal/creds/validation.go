package creds

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/iamrolesanywhere"
	"github.com/aws/eks-hybrid/internal/ssm"
	"github.com/aws/eks-hybrid/internal/system"
	"github.com/aws/eks-hybrid/internal/validation"
)

func Validations(config aws.Config, node *api.NodeConfig) []validation.Validation[*api.NodeConfig] {
	if node.IsSSM() {
		return []validation.Validation[*api.NodeConfig]{
			validation.New("ssm-api-network", ssm.NewAccessValidator(config).Run),
		}
	}
	if node.IsIAMRolesAnywhere() {
		return []validation.Validation[*api.NodeConfig]{
			validation.New("iam-ra-api-network", iamrolesanywhere.NewAccessValidator(config).Run),
		}
	}

	return nil
}

func ValidateCredentialProvider(provider CredentialProvider, osName, osVersion string) error {
	if provider == IamRolesAnywhereCredentialProvider {
		majorOsVersion, err := getMajorVersion(osVersion)
		if err != nil {
			return err
		}

		// Both RHEL8 and Ubuntu 20 have older version of glibc which iam roles anywhere credential helper doesn't work with
		// Until we have a fix for that, we will validate and avoid these os version combinations
		// https://github.com/aws/rolesanywhere-credential-helper/issues/90
		if (osName == system.RhelOsName && majorOsVersion == "8") || (osName == system.UbuntuOsName && majorOsVersion == "20") {
			return fmt.Errorf("iam-ra credential provider is not supported on %s %s based operating systems. Please use ssm credential provider", osName, osVersion)
		}
	}
	return nil
}

func getMajorVersion(version string) (string, error) {
	parts := strings.Split(version, ".")
	if len(parts) > 0 {
		return parts[0], nil
	}
	return "", fmt.Errorf("failed to parse input version: %s", version)
}

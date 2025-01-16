package creds

import (
	"fmt"

	"golang.org/x/mod/semver"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/system"
	"github.com/aws/eks-hybrid/internal/tracker"
)

type CredentialProvider string

const (
	SsmCredentialProvider              CredentialProvider = "ssm"
	IamRolesAnywhereCredentialProvider CredentialProvider = "iam-ra"
)

func GetCredentialProvider(credProcess string) (CredentialProvider, error) {
	switch credProcess {
	case string(SsmCredentialProvider):
		return SsmCredentialProvider, nil
	case string(IamRolesAnywhereCredentialProvider):
		return IamRolesAnywhereCredentialProvider, nil
	default:
		return "", fmt.Errorf("invalid credential process provided. Valid options are ssm and iam-ra")
	}
}

func GetCredentialProviderFromNodeConfig(nodeCfg *api.NodeConfig) (CredentialProvider, error) {
	if nodeCfg.IsSSM() {
		return SsmCredentialProvider, nil
	} else if nodeCfg.IsIAMRolesAnywhere() {
		return IamRolesAnywhereCredentialProvider, nil
	}
	return "", fmt.Errorf("no credential process provided in nodeConfig")
}

func GetCredentialProviderFromInstalledArtifacts(artifacts *tracker.InstalledArtifacts) (CredentialProvider, error) {
	if artifacts.Ssm {
		return SsmCredentialProvider, nil
	} else if artifacts.IamRolesAnywhere {
		return IamRolesAnywhereCredentialProvider, nil
	}
	return "", fmt.Errorf("no credential process found in installed artifacts")
}

func ValidateCredentialProvider(provider CredentialProvider) error {
	osName, osVersion := system.GetOsNameWithVersion()
	majorOsVersion := semver.Major("v" + osVersion)

	if (osName == system.RhelOsName || osName == system.RockyOsName) && majorOsVersion == "v8" &&
		provider == IamRolesAnywhereCredentialProvider {
		return fmt.Errorf("iam-ra credential provider is not supported on %s %s based operating systems. Please use ssm credential provider", osName, osVersion)
	}
	return nil
}

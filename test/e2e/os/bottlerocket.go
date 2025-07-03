package os

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/constants"
)

const (
	sshUser                    = "ec2-user"
	iamRaSetupBootstrapCommand = "eks-hybrid-iam-ra-setup"
	iamRaCertificatePath       = "/root/.aws/node.crt"
	iamRaKeyPath               = "/root/.aws/node.key"
	ssmSetupBootstrapCommand   = "eks-hybrid-ssm-setup"
	awsSigningHelperBinary     = "aws_signing_helper"
)

//go:embed testdata/bottlerocket/settings.toml
var brSettingsToml []byte

var brVariantForComputeType = map[e2e.ComputeType]string{
	e2e.CPUInstance: "aws-k8s-%s",
	e2e.GPUInstance: "aws-k8s-%s-nvidia",
}

type brSettingsTomlInitData struct {
	e2e.UserDataInput
	AdminContainerUserData  string
	AWSConfig               string
	ClusterCertificate      string
	HybridContainerUserData string
	IamRA                   bool
}

type BottleRocket struct {
	amiArchitecture string
	architecture    architecture
}

func NewBottleRocket() *BottleRocket {
	br := new(BottleRocket)
	br.amiArchitecture = x8664Arch
	br.architecture = amd64
	return br
}

func NewBottleRocketARM() *BottleRocket {
	br := new(BottleRocket)
	br.amiArchitecture = arm64Arch
	br.architecture = arm64
	return br
}

func (a BottleRocket) Name() string {
	return "bottlerocket-" + a.architecture.String()
}

func (a BottleRocket) InstanceType(region string, instanceSize e2e.InstanceSize, computeType e2e.ComputeType) string {
	return getInstanceTypeFromRegionAndArch(region, a.architecture, instanceSize, computeType)
}

func (a BottleRocket) AMIName(ctx context.Context, awsConfig aws.Config, kubernetesVersion string, computeType e2e.ComputeType) (string, error) {
	amiId, err := getAmiIDFromSSM(ctx, ssm.NewFromConfig(awsConfig), fmt.Sprintf("/aws/service/bottlerocket/%s/%s/latest/image_id", fmt.Sprintf(brVariantForComputeType[computeType], kubernetesVersion), a.amiArchitecture))
	return *amiId, err
}

func (a BottleRocket) BuildUserData(userDataInput e2e.UserDataInput) ([]byte, error) {
	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}
	sshData := map[string]interface{}{
		"user":          sshUser,
		"password-hash": userDataInput.RootPasswordHash,
		"ssh": map[string][]string{
			"authorized-keys": {
				strings.TrimSuffix(userDataInput.PublicKey, "\n"),
			},
		},
	}

	jsonData, err := json.Marshal(sshData)
	if err != nil {
		return nil, err
	}
	sshKey := base64.StdEncoding.EncodeToString([]byte(jsonData))

	awsConfig := ""
	bootstrapContainerCommand := ""
	if userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere != nil {
		var certificate, key string
		for _, file := range userDataInput.Files {
			if file.Path == constants.RolesAnywhereCertPath {
				certificate = strings.ReplaceAll(file.Content, "\\n", "\n")
			}
			if file.Path == constants.RolesAnywhereKeyPath {
				key = strings.ReplaceAll(file.Content, "\\n", "\n")
			}
		}
		bootstrapContainerCommand = fmt.Sprintf("%s --certificate='%s' --key='%s'", iamRaSetupBootstrapCommand, certificate, key)
		awsConfig = fmt.Sprintf(`
[default]
credential_process = %s credential-process --certificate %s --private-key %s --profile-arn %s --role-arn %s --trust-anchor-arn %s --role-session-name %s
`, awsSigningHelperBinary, iamRaCertificatePath, iamRaKeyPath, userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere.ProfileARN, userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere.RoleARN, userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere.TrustAnchorARN, userDataInput.HostName)
	} else if userDataInput.NodeadmConfig.Spec.Hybrid.SSM != nil {
		bootstrapContainerCommand = fmt.Sprintf("%s --activation-id=%q --activation-code=%q --region=%q", ssmSetupBootstrapCommand, userDataInput.NodeadmConfig.Spec.Hybrid.SSM.ActivationID, userDataInput.NodeadmConfig.Spec.Hybrid.SSM.ActivationCode, userDataInput.Region)
	}
	data := brSettingsTomlInitData{
		UserDataInput:           userDataInput,
		AdminContainerUserData:  sshKey,
		AWSConfig:               base64.StdEncoding.EncodeToString([]byte(awsConfig)),
		ClusterCertificate:      base64.StdEncoding.EncodeToString(userDataInput.ClusterCert),
		IamRA:                   userDataInput.NodeadmConfig.Spec.Hybrid.SSM == nil,
		HybridContainerUserData: base64.StdEncoding.EncodeToString([]byte(bootstrapContainerCommand)),
	}

	return executeTemplate(brSettingsToml, data)
}

// IsBottlerocket returns true if the given name is a Bottlerocket OS name.
func IsBottlerocket(name string) bool {
	return strings.HasPrefix(name, constants.BottlerocketOsName)
}

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

//go:embed testdata/bottlerocket/settings.toml
var brSettingsToml []byte

type brSettingsTomlInitData struct {
	e2e.UserDataInput
	NodeadmUrl              string
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

func (a BottleRocket) InstanceType(region string, instanceSize e2e.InstanceSize) string {
	return getInstanceTypeFromRegionAndArch(region, a.architecture, instanceSize)
}

func (a BottleRocket) AMIName(ctx context.Context, awsConfig aws.Config, kubernetesVersion string) (string, error) {
	amiId, err := getAmiIDFromSSM(ctx, ssm.NewFromConfig(awsConfig), fmt.Sprintf("/bottlerocket/aws-k8s-%s/%s/latest/image_id", kubernetesVersion, a.amiArchitecture))
	return *amiId, err
}

func (a BottleRocket) BuildUserData(userDataInput e2e.UserDataInput) ([]byte, error) {
	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}
	sshData := map[string]interface{}{
		"user":          "ec2-user",
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
		bootstrapContainerCommand = fmt.Sprintf("eks-hybrid-iam-ra-setup --certificate='%s' --key='%s'", certificate, key)
		awsConfig = fmt.Sprintf(`
[default]
region = us-west-2
credential_process = aws_signing_helper credential-process --certificate /root/.aws/node.crt --private-key /root/.aws/node.key --profile-arn %s --role-arn %s --trust-anchor-arn %s --role-session-name %s
`, userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere.ProfileARN, userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere.RoleARN, userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere.TrustAnchorARN, userDataInput.HostName)
	}
	if userDataInput.NodeadmConfig.Spec.Hybrid.SSM != nil {
		bootstrapContainerCommand = fmt.Sprintf("eks-hybrid-ssm-setup --activation-id=%q --activation-code=%q --region=%q --enable-credentials-file=%t", userDataInput.NodeadmConfig.Spec.Hybrid.SSM.ActivationID, userDataInput.NodeadmConfig.Spec.Hybrid.SSM.ActivationCode, userDataInput.Region, userDataInput.NodeadmConfig.Spec.Hybrid.EnableCredentialsFile)
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
	return strings.HasPrefix(name, "bottlerocket")
}

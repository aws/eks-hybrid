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
)

//go:embed testdata/bottlerocket/settings.toml
var brSettingsToml []byte

//go:embed testdata/bottlerocket/files.txt
var filesData []byte

type brSettingsTomlInitData struct {
	e2e.UserDataInput
	NodeadmUrl               string
	AdminContainerUserData   string
	AWSConfig                string
	ClusterCertificate       string
	HybridContainerUserData  string
	ControlContainerUserData string
	IamRA                    bool
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
	return "br-" + a.architecture.String()
}

func (a BottleRocket) InstanceType(region string) string {
	return getInstanceTypeFromRegionAndArch(region, a.architecture)
}

func (a BottleRocket) AMIName(ctx context.Context, awsConfig aws.Config) (string, error) {
	amiId, err := getAmiIDFromSSM(ctx, ssm.NewFromConfig(awsConfig), "/aws/service/bottlerocket/aws-k8s-1.31/"+a.amiArchitecture+"/latest/image_id")
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
	if userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere != nil {
		awsConfig = fmt.Sprintf(`
		[default]
			region = us-west-2
			credential_process = aws_signing_helper credential-process --certificate /var/lib/eks-hybrid/roles-anywhere/pki/node.crt --private-key /var/lib/eks-hybrid/roles-anywhere/pki/node.key --profile-arn %s --role-arn %s --trust-anchor-arn %s --role-session-name %s
	`, userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere.ProfileARN, userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere.RoleARN, userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere.TrustAnchorARN, userDataInput.HostName)
	}
	ssmData := ""
	if userDataInput.NodeadmConfig.Spec.Hybrid.SSM != nil {

		ssmConfigData := map[string]interface{}{
			"ssm": map[string]string{
				"activation-id":   userDataInput.NodeadmConfig.Spec.Hybrid.SSM.ActivationID,
				"activation-code": userDataInput.NodeadmConfig.Spec.Hybrid.SSM.ActivationCode,
				"region":          userDataInput.Region,
			},
		}

		jsonData, err = json.Marshal(ssmConfigData)
		if err != nil {
			return nil, err
		}
		ssmData = base64.StdEncoding.EncodeToString([]byte(jsonData))
	}
	data := brSettingsTomlInitData{
		UserDataInput:            userDataInput,
		NodeadmUrl:               userDataInput.NodeadmUrls.AMD,
		AdminContainerUserData:   sshKey,
		AWSConfig:                base64.StdEncoding.EncodeToString([]byte(awsConfig)),
		ClusterCertificate:       base64.StdEncoding.EncodeToString(userDataInput.ClusterCert),
		ControlContainerUserData: ssmData,
		IamRA:                    userDataInput.NodeadmConfig.Spec.Hybrid.SSM == nil,
	}

	if a.architecture.arm() {
		data.NodeadmUrl = userDataInput.NodeadmUrls.ARM
	}

	cloudInitData, err := executeTemplate(filesData, data)
	if err != nil {
		return nil, err
	}
	data.HybridContainerUserData = base64.StdEncoding.EncodeToString([]byte(cloudInitData))

	return executeTemplate(brSettingsToml, data)
}

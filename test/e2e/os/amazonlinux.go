package os

import (
	"context"
	_ "embed"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/aws/eks-hybrid/test/e2e"
)

//go:embed testdata/amazonlinux/2023/cloud-init.txt
var al23CloudInit []byte

type amazonLinuxCloudInitData struct {
	e2e.UserDataInput
	NodeadmUrl string
}

type AmazonLinux2023 struct {
	amiArchitecture string
	architecture    architecture
}

func NewAmazonLinux2023AMD() *AmazonLinux2023 {
	al := new(AmazonLinux2023)
	al.amiArchitecture = x8664Arch
	al.architecture = amd64
	return al
}

func NewAmazonLinux2023ARM() *AmazonLinux2023 {
	al := new(AmazonLinux2023)
	al.amiArchitecture = arm64Arch
	al.architecture = arm64
	return al
}

func (a AmazonLinux2023) Name() string {
	return "al23-" + a.architecture.String()
}

func (a AmazonLinux2023) InstanceType(region string, instanceSize e2e.InstanceSize, computeType e2e.ComputeType) string {
	return getInstanceTypeFromRegionAndArch(region, a.architecture, instanceSize, computeType)
}

func (a AmazonLinux2023) AMIName(ctx context.Context, awsConfig aws.Config, _ string, _ e2e.ComputeType) (string, error) {
	return getAmiIDFromSSM(ctx, ssm.NewFromConfig(awsConfig), "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-"+a.amiArchitecture)
}

func (a AmazonLinux2023) BuildUserData(_ context.Context, userDataInput e2e.UserDataInput) ([]byte, error) {
	nodeadmConfigYaml, err := generateNodeadmConfigYaml(userDataInput.NodeadmConfig)
	if err != nil {
		return nil, err
	}
	userDataInput.NodeadmConfigYaml = nodeadmConfigYaml

	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}

	data := amazonLinuxCloudInitData{
		UserDataInput: userDataInput,
		NodeadmUrl:    userDataInput.NodeadmUrls.AMD,
	}

	if a.architecture.arm() {
		data.NodeadmUrl = userDataInput.NodeadmUrls.ARM
	}

	return executeTemplate(al23CloudInit, data)
}

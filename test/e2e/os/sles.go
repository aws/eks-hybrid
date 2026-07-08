package os

import (
	"context"
	_ "embed"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/aws/eks-hybrid/test/e2e"
)

// SUSE AMI ids for public images are under this SSM parameter tree.
// aws ssm get-parameter --name /aws/service/suse/sles/<sku>/<arch>/latest
const (
	sles15SP7SKU = "15-sp7"
	sles16SKU    = "16.0"
)

const (
	slesSsmAgentAMD = "https://s3.amazonaws.com/ec2-downloads-windows/SSMAgent/latest/linux_amd64/amazon-ssm-agent.rpm"
	slesSsmAgentARM = "https://s3.amazonaws.com/ec2-downloads-windows/SSMAgent/latest/linux_arm64/amazon-ssm-agent.rpm"
)

//go:embed testdata/sles/15-sp7/cloud-init.txt
var sles15SP7CloudInit []byte

//go:embed testdata/sles/16.0/cloud-init.txt
var sles16CloudInit []byte

type slesCloudInitData struct {
	e2e.UserDataInput
	NodeadmUrl  string
	SSMAgentURL string
}

type SLES15SP7 struct {
	amiArchitecture string
	architecture    architecture
}

func NewSLES15SP7AMD() *SLES15SP7 {
	s := new(SLES15SP7)
	s.amiArchitecture = x8664Arch
	s.architecture = amd64
	return s
}

func NewSLES15SP7ARM() *SLES15SP7 {
	s := new(SLES15SP7)
	s.amiArchitecture = arm64Arch
	s.architecture = arm64
	return s
}

func (s SLES15SP7) Name() string {
	return "sles15sp7-" + s.architecture.String()
}

func (s SLES15SP7) InstanceType(region string, instanceSize e2e.InstanceSize, computeType e2e.ComputeType) string {
	return getInstanceTypeFromRegionAndArch(region, s.architecture, instanceSize, computeType)
}

func (s SLES15SP7) AMIName(ctx context.Context, awsConfig aws.Config, _ string, _ e2e.ComputeType) (string, error) {
	return getAmiIDFromSSM(ctx, ssm.NewFromConfig(awsConfig), "/aws/service/suse/sles/"+sles15SP7SKU+"/"+s.amiArchitecture+"/latest")
}

func (s SLES15SP7) BuildUserData(_ context.Context, userDataInput e2e.UserDataInput) ([]byte, error) {
	nodeadmConfigYaml, err := generateNodeadmConfigYaml(userDataInput.NodeadmConfig)
	if err != nil {
		return nil, err
	}
	userDataInput.NodeadmConfigYaml = nodeadmConfigYaml

	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}

	data := slesCloudInitData{
		UserDataInput: userDataInput,
		NodeadmUrl:    userDataInput.NodeadmUrls.AMD,
		SSMAgentURL:   slesSsmAgentAMD,
	}

	if s.architecture.arm() {
		data.NodeadmUrl = userDataInput.NodeadmUrls.ARM
		data.SSMAgentURL = slesSsmAgentARM
	}

	return executeTemplate(sles15SP7CloudInit, data)
}

type SLES16 struct {
	amiArchitecture string
	architecture    architecture
}

func NewSLES16AMD() *SLES16 {
	s := new(SLES16)
	s.amiArchitecture = x8664Arch
	s.architecture = amd64
	return s
}

func NewSLES16ARM() *SLES16 {
	s := new(SLES16)
	s.amiArchitecture = arm64Arch
	s.architecture = arm64
	return s
}

func (s SLES16) Name() string {
	return "sles16-" + s.architecture.String()
}

func (s SLES16) InstanceType(region string, instanceSize e2e.InstanceSize, computeType e2e.ComputeType) string {
	return getInstanceTypeFromRegionAndArch(region, s.architecture, instanceSize, computeType)
}

func (s SLES16) AMIName(ctx context.Context, awsConfig aws.Config, _ string, _ e2e.ComputeType) (string, error) {
	return getAmiIDFromSSM(ctx, ssm.NewFromConfig(awsConfig), "/aws/service/suse/sles/"+sles16SKU+"/"+s.amiArchitecture+"/latest")
}

func (s SLES16) BuildUserData(_ context.Context, userDataInput e2e.UserDataInput) ([]byte, error) {
	nodeadmConfigYaml, err := generateNodeadmConfigYaml(userDataInput.NodeadmConfig)
	if err != nil {
		return nil, err
	}
	userDataInput.NodeadmConfigYaml = nodeadmConfigYaml

	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}

	data := slesCloudInitData{
		UserDataInput: userDataInput,
		NodeadmUrl:    userDataInput.NodeadmUrls.AMD,
		SSMAgentURL:   slesSsmAgentAMD,
	}

	if s.architecture.arm() {
		data.NodeadmUrl = userDataInput.NodeadmUrls.ARM
		data.SSMAgentURL = slesSsmAgentARM
	}

	return executeTemplate(sles16CloudInit, data)
}

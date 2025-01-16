package os

import (
	"context"
	_ "embed"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/aws/eks-hybrid/test/e2e"
)

const awsMarketplaceAccount = "679593333241"

//go:embed testdata/rhel/8/cloud-init.txt
var rocky8CloudInit []byte

//go:embed testdata/rhel/9/cloud-init.txt
var rocky9CloudInit []byte

type Rocky8 struct {
	amiArchitecture string
	architecture    architecture
}

func NewRocky8AMD() *Rocky8 {
	return &Rocky8{
		amiArchitecture: x8664Arch,
		architecture:    amd64,
	}
}

func NewRocky8ARM() *Rocky8 {
	return &Rocky8{
		amiArchitecture: arm64Arch,
		architecture:    arm64,
	}
}

func (r Rocky8) Name() string {
	return "rocky8-" + r.architecture.String()
}

func (r Rocky8) InstanceType(region string) string {
	return getInstanceTypeFromRegionAndArch(region, r.architecture)
}

func (r Rocky8) AMIName(ctx context.Context, awsConfig aws.Config) (string, error) {
	return findLatestImage(ctx, ec2.NewFromConfig(awsConfig), awsMarketplaceAccount, "Rocky-8*", r.amiArchitecture)
}

func (r Rocky8) BuildUserData(userDataInput e2e.UserDataInput) ([]byte, error) {
	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}

	// Rocky is based on RHEL. The following cloud-init templating works for Rocky
	data := rhelCloudInitData{
		UserDataInput: userDataInput,
		NodeadmUrl:    userDataInput.NodeadmUrls.AMD,
		SSMAgentURL:   rhelSsmAgentAMD,
	}

	if r.architecture.arm() {
		data.NodeadmUrl = userDataInput.NodeadmUrls.ARM
		data.SSMAgentURL = rhelSsmAgentARM
	}

	return executeTemplate(rocky8CloudInit, data)
}

type Rocky9 struct {
	amiArchitecture string
	architecture    architecture
}

func NewRocky9AMD() *Rocky9 {
	return &Rocky9{
		amiArchitecture: x8664Arch,
		architecture:    amd64,
	}
}

func NewRocky9ARM() *Rocky9 {
	return &Rocky9{
		amiArchitecture: arm64Arch,
		architecture:    arm64,
	}
}

func (r Rocky9) Name() string {
	return "rocky9-" + r.architecture.String()
}

func (r Rocky9) InstanceType(region string) string {
	return getInstanceTypeFromRegionAndArch(region, r.architecture)
}

func (r Rocky9) AMIName(ctx context.Context, awsConfig aws.Config) (string, error) {
	return findLatestImage(ctx, ec2.NewFromConfig(awsConfig), awsMarketplaceAccount, "Rocky-9*", r.amiArchitecture)
}

func (r Rocky9) BuildUserData(userDataInput e2e.UserDataInput) ([]byte, error) {
	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}

	// Rocky is based on RHEL. The following cloud-init templating works for Rocky
	data := rhelCloudInitData{
		UserDataInput: userDataInput,
		NodeadmUrl:    userDataInput.NodeadmUrls.AMD,
		SSMAgentURL:   rhelSsmAgentAMD,
	}

	if r.architecture.arm() {
		data.NodeadmUrl = userDataInput.NodeadmUrls.ARM
		data.SSMAgentURL = rhelSsmAgentARM
	}

	return executeTemplate(rocky9CloudInit, data)
}

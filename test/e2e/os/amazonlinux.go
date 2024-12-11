package os

import (
	"context"
	_ "embed"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ssm"

	"github.com/aws/eks-hybrid/test/e2e"
)

//go:embed testdata/amazonlinux/2023/cloud-init.txt
var al23CloudInit []byte

type amazonLinuxCloudInitData struct {
	e2e.UserDataInput
	NodeadmUrl string
}

type AmazonLinux2023 struct {
	Architecture string
}

func NewAmazonLinux2023AMD() *AmazonLinux2023 {
	al := new(AmazonLinux2023)
	al.Architecture = x8664Arch
	return al
}

func NewAmazonLinux2023ARM() *AmazonLinux2023 {
	al := new(AmazonLinux2023)
	al.Architecture = arm64Arch
	return al
}

func (a AmazonLinux2023) Name() string {
	if a.Architecture == x8664Arch {
		return "al23-amd64"
	}
	return "al23-arm64"
}

func (a AmazonLinux2023) InstanceType(region string) string {
	return getInstanceTypeFromRegionAndArch(region, normalizeArch(a.Architecture))
}

func (a AmazonLinux2023) AMIName(ctx context.Context, awsSession *session.Session) (string, error) {
	amiId, err := getAmiIDFromSSM(ctx, ssm.New(awsSession), "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-"+a.Architecture)
	return *amiId, err
}

func (a AmazonLinux2023) BuildUserData(userDataInput e2e.UserDataInput) ([]byte, error) {
	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}

	data := amazonLinuxCloudInitData{
		UserDataInput: userDataInput,
		NodeadmUrl:    userDataInput.NodeadmUrls.AMD,
	}

	if a.Architecture == arm64Arch {
		data.NodeadmUrl = userDataInput.NodeadmUrls.ARM
	}

	return executeTemplate(al23CloudInit, data)
}

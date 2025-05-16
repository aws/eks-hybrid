package os

import (
	"context"
	_ "embed"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/commands"
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
	genericOS       *GenericLinuxOS
}

func NewAmazonLinux2023AMD() *AmazonLinux2023 {
	al := new(AmazonLinux2023)
	al.amiArchitecture = x8664Arch
	al.architecture = amd64
	al.genericOS = NewGenericLinuxOS("al23", amd64)
	return al
}

func NewAmazonLinux2023ARM() *AmazonLinux2023 {
	al := new(AmazonLinux2023)
	al.amiArchitecture = arm64Arch
	al.architecture = arm64
	al.genericOS = NewGenericLinuxOS("al23", arm64)
	return al
}

func (a AmazonLinux2023) Name() string {
	return "al23-" + a.architecture.String()
}

func (a AmazonLinux2023) InstanceType(region string, instanceSize e2e.InstanceSize) string {
	return getInstanceTypeFromRegionAndArch(region, a.architecture, instanceSize)
}

func (a AmazonLinux2023) AMIName(ctx context.Context, awsConfig aws.Config, _ string) (string, error) {
	amiId, err := getAmiIDFromSSM(ctx, ssm.NewFromConfig(awsConfig), "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-"+a.amiArchitecture)
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

	if a.architecture.arm() {
		data.NodeadmUrl = userDataInput.NodeadmUrls.ARM
	}

	return executeTemplate(al23CloudInit, data)
}

// RebootInstance reboots an AmazonLinux2023 instance
func (a AmazonLinux2023) RebootInstance(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	return a.genericOS.RebootInstance(ctx, runner, instanceIP)
}

// CollectLogs collects logs from an AmazonLinux2023 instance
func (a AmazonLinux2023) CollectLogs(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, logBundleUrl string) error {
	return a.genericOS.CollectLogs(ctx, runner, instanceIP, logBundleUrl)
}

// Uninstall uninstalls Kubernetes components from an AmazonLinux2023 instance
func (a AmazonLinux2023) Uninstall(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	return a.genericOS.Uninstall(ctx, runner, instanceIP)
}

// GetNodeadmVersion returns the nodeadm version for AmazonLinux2023
func (a AmazonLinux2023) GetNodeadmVersion(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) (string, error) {
	return a.genericOS.GetNodeadmVersion(ctx, runner, instanceIP)
}

// RunNodeadmDebug runs nodeadm debug on an AmazonLinux2023 instance
func (a AmazonLinux2023) RunNodeadmDebug(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	return a.genericOS.RunNodeadmDebug(ctx, runner, instanceIP)
}

// ShouldRunNodeadmDebug indicates whether nodeadm debug should be run for AmazonLinux2023
func (a AmazonLinux2023) ShouldRunNodeadmDebug() bool {
	return a.genericOS.ShouldRunNodeadmDebug()
}

// Upgrade upgrades Kubernetes components on an AmazonLinux2023 instance
func (a AmazonLinux2023) Upgrade(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, kubernetesVersion string) error {
	return a.genericOS.Upgrade(ctx, runner, instanceIP, kubernetesVersion)
}

func (a AmazonLinux2023) PodIdentityAgentDaemonsetName() string {
	return a.genericOS.PodIdentityAgentDaemonsetName()
}

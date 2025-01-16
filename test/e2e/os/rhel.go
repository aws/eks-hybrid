package os

import (
	"context"
	_ "embed"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/aws/eks-hybrid/test/e2e"
)

const rhelAWSAccount = "309956199498"

//go:embed testdata/rhel/8/cloud-init.txt
var rhel8CloudInit []byte

//go:embed testdata/rhel/9/cloud-init.txt
var rhel9CloudInit []byte

type rhelCloudInitData struct {
	e2e.UserDataInput
	NodeadmUrl        string
	NodeadmInitScript string
	RhelUsername      string
	RhelPassword      string
	SSMAgentURL       string
}

type RedHat8 struct {
	rhelUsername    string
	rhelPassword    string
	amiArchitecture string
	architecture    architecture
}

const (
	rhelSsmAgentAMD = "https://s3.amazonaws.com/ec2-downloads-windows/SSMAgent/latest/linux_amd64/amazon-ssm-agent.rpm"
	rhelSsmAgentARM = "https://s3.amazonaws.com/ec2-downloads-windows/SSMAgent/latest/linux_arm64/amazon-ssm-agent.rpm"
)

func NewRedHat8AMD(rhelUsername, rhelPassword string) *RedHat8 {
	rh8 := new(RedHat8)
	rh8.rhelUsername = rhelUsername
	rh8.rhelPassword = rhelPassword
	rh8.amiArchitecture = x8664Arch
	rh8.architecture = amd64
	return rh8
}

func NewRedHat8ARM(rhelUsername, rhelPassword string) *RedHat8 {
	rh8 := new(RedHat8)
	rh8.rhelUsername = rhelUsername
	rh8.rhelPassword = rhelPassword
	rh8.amiArchitecture = arm64Arch
	rh8.architecture = arm64
	return rh8
}

func (r RedHat8) Name() string {
	return "rhel8-" + r.architecture.String()
}

func (r RedHat8) InstanceType(region string) string {
	return getInstanceTypeFromRegionAndArch(region, r.architecture)
}

func (r RedHat8) AMIName(ctx context.Context, awsConfig aws.Config) (string, error) {
	// there is no rhel ssm parameter
	// aws ec2 describe-images --owners 309956199498 --query 'sort_by(Images, &CreationDate)[-1].[ImageId]' --filters "Name=name,Values=RHEL-8*" "Name=architecture,Values=x86_64" --region us-west-2
	return findLatestImage(ctx, ec2.NewFromConfig(awsConfig), rhelAWSAccount, "RHEL-8*", r.amiArchitecture)
}

func (r RedHat8) BuildUserData(userDataInput e2e.UserDataInput) ([]byte, error) {
	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}

	data := rhelCloudInitData{
		UserDataInput: userDataInput,
		NodeadmUrl:    userDataInput.NodeadmUrls.AMD,
		RhelUsername:  r.rhelUsername,
		RhelPassword:  r.rhelPassword,
	}

	if r.architecture.arm() {
		data.NodeadmUrl = userDataInput.NodeadmUrls.ARM
	}

	return executeTemplate(rhel8CloudInit, data)
}

type RedHat9 struct {
	rhelUsername    string
	rhelPassword    string
	amiArchitecture string
	architecture    architecture
}

func NewRedHat9AMD(rhelUsername, rhelPassword string) *RedHat9 {
	rh9 := new(RedHat9)
	rh9.rhelUsername = rhelUsername
	rh9.rhelPassword = rhelPassword
	rh9.amiArchitecture = x8664Arch
	rh9.architecture = amd64
	return rh9
}

func NewRedHat9ARM(rhelUsername, rhelPassword string) *RedHat9 {
	rh9 := new(RedHat9)
	rh9.rhelUsername = rhelUsername
	rh9.rhelPassword = rhelPassword
	rh9.amiArchitecture = arm64Arch
	rh9.architecture = arm64
	return rh9
}

func (r RedHat9) Name() string {
	return "rhel9-" + r.architecture.String()
}

func (r RedHat9) InstanceType(region string) string {
	return getInstanceTypeFromRegionAndArch(region, r.architecture)
}

func (r RedHat9) AMIName(ctx context.Context, awsConfig aws.Config) (string, error) {
	// there is no rhel ssm parameter
	// aws ec2 describe-images --owners 309956199498 --query 'sort_by(Images, &CreationDate)[-1].[ImageId]' --filters "Name=name,Values=RHEL-9*" "Name=architecture,Values=x86_64" --region us-west-2
	return findLatestImage(ctx, ec2.NewFromConfig(awsConfig), rhelAWSAccount, "RHEL-9*", r.amiArchitecture)
}

func (r RedHat9) BuildUserData(userDataInput e2e.UserDataInput) ([]byte, error) {
	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}

	data := rhelCloudInitData{
		UserDataInput: userDataInput,
		NodeadmUrl:    userDataInput.NodeadmUrls.AMD,
		RhelUsername:  r.rhelUsername,
		RhelPassword:  r.rhelPassword,
		SSMAgentURL:   rhelSsmAgentAMD,
	}

	if r.architecture.arm() {
		data.NodeadmUrl = userDataInput.NodeadmUrls.ARM
		data.SSMAgentURL = rhelSsmAgentARM
	}

	return executeTemplate(rhel9CloudInit, data)
}

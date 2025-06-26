package os

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"golang.org/x/mod/semver"

	"github.com/aws/eks-hybrid/test/e2e"
)

const (
	rhelAWSAccount        = "309956199498"
	rhelSSMAgentProxyPath = "/etc/systemd/system/amazon-ssm-agent.service.d/http-proxy.conf"
)

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
	ContainerdSource  string
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

func (r RedHat8) InstanceType(region string, instanceSize e2e.InstanceSize, computeType e2e.ComputeType) string {
	return getInstanceTypeFromRegionAndArch(region, r.architecture, instanceSize, computeType)
}

func (r RedHat8) AMIName(ctx context.Context, awsConfig aws.Config, _ string) (string, error) {
	// there is no rhel ssm parameter
	// aws ec2 describe-images --owners 309956199498 --query 'sort_by(Images, &CreationDate)[-1].[ImageId]' --filters "Name=name,Values=RHEL-8*" "Name=architecture,Values=x86_64" --region us-west-2
	return findLatestImage(ctx, ec2.NewFromConfig(awsConfig), "RHEL-8*", r.amiArchitecture)
}

func (r RedHat8) BuildUserData(userDataInput e2e.UserDataInput) ([]byte, error) {
	nodeadmConfigYaml, err := generateNodeadmConfigYaml(userDataInput.NodeadmConfig)
	if err != nil {
		return nil, err
	}
	userDataInput.NodeadmConfigYaml = nodeadmConfigYaml

	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}

	if err := addSystemdProxyConfig(&userDataInput, rhelSSMAgentProxyPath); err != nil {
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
	rhelUsername     string
	rhelPassword     string
	amiArchitecture  string
	architecture     architecture
	containerdSource string
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

func NewRedHat9NoDockerSource(rhelUsername, rhelPassword string) *RedHat9 {
	rh9 := new(RedHat9)
	rh9.rhelUsername = rhelUsername
	rh9.rhelPassword = rhelPassword
	rh9.amiArchitecture = x8664Arch
	rh9.architecture = amd64
	rh9.containerdSource = "none"
	return rh9
}

func (r RedHat9) Name() string {
	name := "rhel9-" + r.architecture.String()
	if r.containerdSource == "none" {
		name += "-source-none"
	}
	return name
}

func (r RedHat9) InstanceType(region string, instanceSize e2e.InstanceSize, computeType e2e.ComputeType) string {
	return getInstanceTypeFromRegionAndArch(region, r.architecture, instanceSize, computeType)
}

func (r RedHat9) AMIName(ctx context.Context, awsConfig aws.Config, _ string) (string, error) {
	// there is no rhel ssm parameter
	// aws ec2 describe-images --owners 309956199498 --query 'sort_by(Images, &CreationDate)[-1].[ImageId]' --filters "Name=name,Values=RHEL-9*" "Name=architecture,Values=x86_64" --region us-west-2
	return findLatestImage(ctx, ec2.NewFromConfig(awsConfig), "RHEL-9*", r.amiArchitecture)
}

func (r RedHat9) BuildUserData(userDataInput e2e.UserDataInput) ([]byte, error) {
	nodeadmConfigYaml, err := generateNodeadmConfigYaml(userDataInput.NodeadmConfig)
	if err != nil {
		return nil, err
	}
	userDataInput.NodeadmConfigYaml = nodeadmConfigYaml

	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}

	if err := addSystemdProxyConfig(&userDataInput, rhelSSMAgentProxyPath); err != nil {
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

	data.ContainerdSource = "docker"
	if r.containerdSource == "none" {
		data.ContainerdSource = "none"
	}

	return executeTemplate(rhel9CloudInit, data)
}

// AMI represents an ec2 Image.
type AMI struct {
	ID        string
	CreatedAt time.Time
	Version   string
}

// findLatestImage returns the most recent redhat image matching the amiPrefix and and arch
// this assumes that the return ami names follow the pattern `{amiPrefix}.<minor>.<patch?>_`
// ex: amiPrefix: RHEL-8*, amiName: RHEL-8.8.0_HVM-20250116-x86_64-2339-Hourly2-GP3 or RHEL-8.8_HVM-20250116-x86_64-2339-Hourly2-GP3
func findLatestImage(ctx context.Context, client *ec2.Client, amiPrefix, arch string) (string, error) {
	var latestAMI AMI

	in := &ec2.DescribeImagesInput{
		Owners:     []string{rhelAWSAccount},
		Filters:    []types.Filter{{Name: aws.String("name"), Values: []string{amiPrefix}}, {Name: aws.String("architecture"), Values: []string{arch}}},
		MaxResults: aws.Int32(100),
	}

	for {
		l, err := client.DescribeImages(ctx, in)
		if err != nil {
			return "", err
		}

		if paginationDone(in, l) {
			break
		}

		for _, i := range l.Images {
			created, err := time.Parse(time.RFC3339Nano, *i.CreationDate)
			if err != nil {
				return "", err
			}
			name := string(*i.Name)
			versionStr := "v" + name[strings.Index(name, "-")+1:strings.Index(name, "_")]

			if !semver.IsValid(versionStr) {
				return "", fmt.Errorf("parsing version from ami name %s", name)
			}

			compared := semver.Compare(versionStr, latestAMI.Version)
			// newer version or same version with a newer createdAt
			if compared > 0 ||
				(compared == 0 && created.Compare(latestAMI.CreatedAt) > 0) {
				latestAMI = AMI{
					ID:        *i.ImageId,
					CreatedAt: created,
					Version:   versionStr,
				}
			}
		}

		in.NextToken = l.NextToken

		if in.NextToken == nil {
			break
		}
	}

	return latestAMI.ID, nil
}

func paginationDone(in *ec2.DescribeImagesInput, out *ec2.DescribeImagesOutput) bool {
	// When filters are used, they are applied on the client side per page
	// This function helps go through all the pages to make sure if filtered
	// result shows up in any one of the pages
	return out.NextToken == nil || (in.NextToken != nil && in.NextToken == out.NextToken)
}

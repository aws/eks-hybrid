package os

import (
	"context"
	_ "embed"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"

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
	RhelUsername string
	RhelPassword string
	Architecture string
}

const (
	rhelSsmAgentAMD = "https://s3.amazonaws.com/ec2-downloads-windows/SSMAgent/latest/linux_amd64/amazon-ssm-agent.rpm"
	rhelSsmAgentARM = "https://s3.amazonaws.com/ec2-downloads-windows/SSMAgent/latest/linux_arm64/amazon-ssm-agent.rpm"
)

func NewRedHat8AMD(rhelUsername, rhelPassword string) *RedHat8 {
	rh8 := new(RedHat8)
	rh8.RhelUsername = rhelUsername
	rh8.RhelPassword = rhelPassword
	rh8.Architecture = x8664Arch
	return rh8
}

func NewRedHat8ARM(rhelUsername, rhelPassword string) *RedHat8 {
	rh8 := new(RedHat8)
	rh8.RhelUsername = rhelUsername
	rh8.RhelPassword = rhelPassword
	rh8.Architecture = arm64Arch
	return rh8
}

func (r RedHat8) Name() string {
	if r.Architecture == x8664Arch {
		return "rhel8-amd64"
	}
	return "rhel8-arm64"
}

func (r RedHat8) InstanceType(region string) string {
	return getInstanceTypeFromRegionAndArch(region, normalizeArch(r.Architecture))
}

func (r RedHat8) AMIName(ctx context.Context, awsSession *session.Session) (string, error) {
	// there is no rhel ssm parameter
	// aws ec2 describe-images --owners 309956199498 --query 'sort_by(Images, &CreationDate)[-1].[ImageId]' --filters "Name=name,Values=RHEL-8*" "Name=architecture,Values=x86_64" --region us-west-2
	return findLatestImage(ec2.New(awsSession), "RHEL-8*", r.Architecture)
}

func (r RedHat8) BuildUserData(userDataInput e2e.UserDataInput) ([]byte, error) {
	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}

	data := rhelCloudInitData{
		UserDataInput: userDataInput,
		NodeadmUrl:    userDataInput.NodeadmUrls.AMD,
		RhelUsername:  r.RhelUsername,
		RhelPassword:  r.RhelPassword,
	}

	if r.Architecture == arm64Arch {
		data.NodeadmUrl = userDataInput.NodeadmUrls.ARM
	}

	return executeTemplate(rhel8CloudInit, data)
}

type RedHat9 struct {
	RhelUsername string
	RhelPassword string
	Architecture string
}

func NewRedHat9AMD(rhelUsername, rhelPassword string) *RedHat9 {
	rh9 := new(RedHat9)
	rh9.RhelUsername = rhelUsername
	rh9.RhelPassword = rhelPassword
	rh9.Architecture = x8664Arch
	return rh9
}

func NewRedHat9ARM(rhelUsername, rhelPassword string) *RedHat9 {
	rh9 := new(RedHat9)
	rh9.RhelUsername = rhelUsername
	rh9.RhelPassword = rhelPassword
	rh9.Architecture = arm64Arch
	return rh9
}

func (r RedHat9) Name() string {
	if r.Architecture == x8664Arch {
		return "rhel9-amd64"
	}
	return "rhel9-arm64"
}

func (r RedHat9) InstanceType(region string) string {
	return getInstanceTypeFromRegionAndArch(region, normalizeArch(r.Architecture))
}

func (r RedHat9) AMIName(ctx context.Context, awsSession *session.Session) (string, error) {
	// there is no rhel ssm parameter
	// aws ec2 describe-images --owners 309956199498 --query 'sort_by(Images, &CreationDate)[-1].[ImageId]' --filters "Name=name,Values=RHEL-9*" "Name=architecture,Values=x86_64" --region us-west-2
	return findLatestImage(ec2.New(awsSession), "RHEL-9*", r.Architecture)
}

func (r RedHat9) BuildUserData(userDataInput e2e.UserDataInput) ([]byte, error) {
	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}

	data := rhelCloudInitData{
		UserDataInput: userDataInput,
		NodeadmUrl:    userDataInput.NodeadmUrls.AMD,
		RhelUsername:  r.RhelUsername,
		RhelPassword:  r.RhelPassword,
		SSMAgentURL:   rhelSsmAgentAMD,
	}

	if r.Architecture == arm64Arch {
		data.NodeadmUrl = userDataInput.NodeadmUrls.ARM
		data.SSMAgentURL = rhelSsmAgentARM
	}

	return executeTemplate(rhel9CloudInit, data)
}

// AMI represents an ec2 Image.
type AMI struct {
	ID        string
	CreatedAt time.Time
}

// findLatestImage returns the most recent redhat image matching the amiPrefix and and arch
func findLatestImage(client *ec2.EC2, amiPrefix, arch string) (string, error) {
	var latestAMI AMI

	in := &ec2.DescribeImagesInput{
		Owners:     []*string{aws.String(rhelAWSAccount)},
		Filters:    []*ec2.Filter{{Name: aws.String("name"), Values: []*string{aws.String(amiPrefix)}}, {Name: aws.String("architecture"), Values: []*string{aws.String(arch)}}},
		MaxResults: aws.Int64(100),
	}

	for {
		l, err := client.DescribeImages(in)
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
			if created.Compare(latestAMI.CreatedAt) > 0 {
				latestAMI = AMI{
					ID:        *i.ImageId,
					CreatedAt: created,
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
	return (in.NextToken != nil && in.NextToken == out.NextToken) || len(out.Images) == 0
}

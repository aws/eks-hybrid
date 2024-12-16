package ec2

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/aws/smithy-go"

	"github.com/aws/eks-hybrid/test/e2e/constants"
)

// instanceConfig holds the configuration for the EC2 instance.
type InstanceConfig struct {
	ClusterName        string
	InstanceName       string
	AmiID              string
	InstanceType       string
	InstanceProfileARN string
	VolumeSize         int32
	UserData           []byte
	SubnetID           string
	SecurityGroupID    string
}

type Instance struct {
	ID string
	IP string
}

func (e *InstanceConfig) Create(ctx context.Context, ec2Client *ec2.Client, ssmClient *ssm.SSM) (Instance, error) {
	instances, err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("tag:Name"),
				Values: []string{e.InstanceName},
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: []string{"running", "pending"},
			},
		},
	})
	if err != nil {
		return Instance{}, fmt.Errorf("describing EC2 instances: %w", err)
	}

	if len(instances.Reservations) > 1 {
		return Instance{}, fmt.Errorf("more than one reservation for instances with the name %s found", e.InstanceName)
	}

	if len(instances.Reservations) == 1 && len(instances.Reservations[0].Instances) > 1 {
		return Instance{}, fmt.Errorf("more than one instance with the name %s found", e.InstanceName)
	}

	if len(instances.Reservations) == 1 && len(instances.Reservations[0].Instances) == 1 {
		return Instance{
			ID: *instances.Reservations[0].Instances[0].InstanceId,
			IP: *instances.Reservations[0].Instances[0].PrivateIpAddress,
		}, nil
	}

	userDataEncoded := base64.StdEncoding.EncodeToString(e.UserData)

	runResult, err := ec2Client.RunInstances(ctx, &ec2.RunInstancesInput{
		ImageId:      aws.String(e.AmiID),
		InstanceType: types.InstanceType(e.InstanceType),
		MinCount:     aws.Int32(1),
		MaxCount:     aws.Int32(1),
		IamInstanceProfile: &types.IamInstanceProfileSpecification{
			Arn: aws.String(e.InstanceProfileARN),
		},
		BlockDeviceMappings: []types.BlockDeviceMapping{
			{
				DeviceName: aws.String("/dev/sda1"),
				Ebs: &types.EbsBlockDevice{
					VolumeSize: aws.Int32(e.VolumeSize),
				},
			},
		},
		NetworkInterfaces: []types.InstanceNetworkInterfaceSpecification{
			{
				DeviceIndex: aws.Int32(0),
				SubnetId:    aws.String(e.SubnetID),
				Groups:      []string{e.SecurityGroupID},
			},
		},
		UserData: aws.String(userDataEncoded),
		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeInstance,
				Tags: []types.Tag{
					{
						Key:   aws.String("Name"),
						Value: aws.String(e.InstanceName),
					},
					{
						Key:   aws.String(constants.TestClusterTagKey),
						Value: aws.String(e.ClusterName),
					},
				},
			},
		},
		MetadataOptions: &types.InstanceMetadataOptionsRequest{
			HttpTokens:   types.HttpTokensStateRequired,
			HttpEndpoint: types.InstanceMetadataEndpointStateEnabled,
		},
	}, func(o *ec2.Options) {
		o.Retryer = newDefaultRunEC2Retrier()
	})
	if err != nil {
		return Instance{}, fmt.Errorf("could not create hybrid EC2 instance: %w", err)
	}

	return Instance{ID: *runResult.Instances[0].InstanceId, IP: *runResult.Instances[0].PrivateIpAddress}, nil
}

func DeleteEC2Instance(ctx context.Context, client *ec2.Client, instanceID string) error {
	terminateInstanceInput := &ec2.TerminateInstancesInput{
		InstanceIds: []string{instanceID},
	}

	if _, err := client.TerminateInstances(ctx, terminateInstanceInput); err != nil {
		return err
	}
	return nil
}

func newDefaultRunEC2Retrier() runInstanceRetrier {
	return runInstanceRetrier{
		Standard:   retry.NewStandard(),
		maxRetries: 60,
		backoff:    2 * time.Second,
	}
}

type runInstanceRetrier struct {
	*retry.Standard
	maxRetries int
	backoff    time.Duration
}

func (c runInstanceRetrier) IsErrorRetryable(err error) bool {
	if c.Standard.IsErrorRetryable(err) {
		return true
	}

	var awsErr smithy.APIError
	if ok := errors.As(err, &awsErr); ok {
		// We retry invalid instance profile errors because sometimes there is a delay between creating
		// the instance profile and that profile being available in EC2. We trust that if this error comes
		// back, it's just an eventual consistency issue and not that our setup code is not creating the
		// instance profile correctly.
		// The error message can be:
		// - Invalid IAM Instance Profile name
		// - Invalid IAM Instance Profile ARN
		// Depending if the input uses the name or the ARN in the params.
		if awsErr.ErrorCode() == "InvalidParameterValue" && strings.Contains(awsErr.ErrorMessage(), "Invalid IAM Instance Profile") {
			return true
		}
	}

	return false
}

func (c runInstanceRetrier) MaxAttempts() int {
	return c.maxRetries
}

func (c runInstanceRetrier) RetryDelay(attempt int, err error) (time.Duration, error) {
	return c.backoff, nil
}

func RebootEC2Instance(ctx context.Context, client *ec2.Client, instanceID string) error {
	rebootInstanceInput := &ec2.RebootInstancesInput{
		InstanceIds: []string{instanceID},
	}

	if _, err := client.RebootInstances(ctx, rebootInstanceInput); err != nil {
		return err
	}
	return nil
}

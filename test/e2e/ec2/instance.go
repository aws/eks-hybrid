package ec2

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/constants"
	e2eErrors "github.com/aws/eks-hybrid/test/e2e/errors"
)

const nodeRunningTimeout = 5 * time.Minute

// instanceConfig holds the configuration for the EC2 instance.
type InstanceConfig struct {
	ClusterName  string
	InstanceName string
	AmiID        string
	// InstanceTypes is an ordered list of instance types. They are attempted in
	// order and the first one that launches is used.
	InstanceTypes      []string
	InstanceProfileARN string
	VolumeSize         int32
	UserData           []byte
	SubnetID           string
	SecurityGroupID    string
	OS                 string
}

type Instance struct {
	ID   string
	Name string
	IP   string
}

// skipInstanceTypeErrorsRetryer wraps a retryer to make instance type related
// errors non retryable.
type skipInstanceTypeErrorsRetryer struct {
	aws.Retryer
}

func (r *skipInstanceTypeErrorsRetryer) IsErrorRetryable(err error) bool {
	if e2eErrors.IsInvalidInstanceTypeParameter(err) {
		return false
	}
	return r.Retryer.IsErrorRetryable(err)
}

func (e *InstanceConfig) Create(ctx context.Context, ec2Client *ec2.Client, ssmClient *ssm.Client) (Instance, error) {
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

	var userDataBuffer bytes.Buffer
	gzWriter := gzip.NewWriter(&userDataBuffer)
	if _, err := gzWriter.Write(e.UserData); err != nil {
		return Instance{}, fmt.Errorf("gzipping user data: %w", err)
	}
	if err := gzWriter.Close(); err != nil {
		return Instance{}, fmt.Errorf("gzipping user data: %w", err)
	}
	userDataEncoded := base64.StdEncoding.EncodeToString(userDataBuffer.Bytes())

	if len(e.InstanceTypes) == 0 {
		return Instance{}, fmt.Errorf("no instance types provided for instance %s", e.InstanceName)
	}

	// Launch the first instance type that works. Skip to the next type only when the
	// launch failed because of the type itself. Anything else, like a permission or subnet problem,
	// would fail the same way for every type, so return it right away.
	logger := logr.FromContextOrDiscard(ctx)

	var attemptErrs []error
	for _, instanceType := range e.InstanceTypes {
		instance, err := e.runInstance(ctx, ec2Client, instanceType, userDataEncoded)
		if err == nil {
			logger.Info("Created EC2 instance", "instanceID", instance.ID, "instanceType", instanceType)
			return instance, nil
		}

		if !e2eErrors.IsInstanceTypeUnavailable(err) {
			return Instance{}, fmt.Errorf("could not create hybrid EC2 instance with instance type %s: %w", instanceType, err)
		}

		logger.Info("Instance type unavailable, trying next candidate", "instanceType", instanceType, "error", err.Error())
		attemptErrs = append(attemptErrs, fmt.Errorf("%s: %w", instanceType, err))
	}

	return Instance{}, fmt.Errorf("could not create hybrid EC2 instance, none of the instance types %v are available: %w",
		e.InstanceTypes, errors.Join(attemptErrs...))
}

// runInstance launches a single EC2 instance with the given instance type.
func (e *InstanceConfig) runInstance(ctx context.Context, ec2Client *ec2.Client, instanceType, userDataEncoded string) (Instance, error) {
	runResult, err := ec2Client.RunInstances(ctx, &ec2.RunInstancesInput{
		ImageId:      aws.String(e.AmiID),
		InstanceType: types.InstanceType(instanceType),
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
					{
						Key:   aws.String(constants.OSArchTagKey),
						Value: aws.String(e.OS),
					},
				},
			},
		},
		MetadataOptions: &types.InstanceMetadataOptionsRequest{
			HttpTokens:   types.HttpTokensStateRequired,
			HttpEndpoint: types.InstanceMetadataEndpointStateEnabled,
		},
	}, func(o *ec2.Options) {
		// InvalidParameterValue is retried persistently to wait out IAM propagation, but
		// when it refers to the instance type it will never succeed, so it is excluded to
		// avoid spending all 60 attempts before falling back to the next type.
		clientRetryer := o.Retryer
		persistentInvalidParameterValueRetryer := retry.AddWithMaxAttempts(retry.AddWithErrorCodes(clientRetryer, "InvalidParameterValue"), 60)
		o.Retryer = &skipInstanceTypeErrorsRetryer{Retryer: persistentInvalidParameterValueRetryer}
	})
	if err != nil {
		return Instance{}, fmt.Errorf("could not create hybrid EC2 instance: %w", err)
	}

	return Instance{
		ID:   *runResult.Instances[0].InstanceId,
		Name: e.InstanceName,
		IP:   *runResult.Instances[0].PrivateIpAddress,
	}, nil
}

// DisableSourceDestCheck disables the source/destination check for the given instance.
func DisableSourceDestCheck(ctx context.Context, ec2Client *ec2.Client, instanceID string) error {
	_, err := ec2Client.ModifyInstanceAttribute(ctx, &ec2.ModifyInstanceAttributeInput{
		InstanceId: aws.String(instanceID),
		SourceDestCheck: &types.AttributeBooleanValue{
			Value: aws.Bool(false),
		},
	})
	if err != nil {
		return fmt.Errorf("disabling source/dest check for instance %s: %w", instanceID, err)
	}
	return nil
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

// DeleteRoutesForInstance deletes the routes entries that point to the instance from
// the tables associated with the given subnet.
func DeleteRoutesForInstance(ctx context.Context, ec2Client *ec2.Client, subnetID, instanceID string) error {
	routeTables, err := ec2Client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("association.subnet-id"),
				Values: []string{subnetID},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("describing route tables: %w", err)
	}

	for _, routeTable := range routeTables.RouteTables {
		for _, route := range routeTable.Routes {
			if route.InstanceId == nil || *route.InstanceId != instanceID {
				continue
			}

			_, err := ec2Client.DeleteRoute(ctx, &ec2.DeleteRouteInput{
				RouteTableId:         routeTable.RouteTableId,
				DestinationCidrBlock: route.DestinationCidrBlock,
			})
			if err != nil && !e2eErrors.IsAwsError(err, "InvalidRoute.NotFound") {
				return fmt.Errorf("deleting route for instance %s: %w", instanceID, err)
			}
		}
	}

	return nil
}

func WaitForEC2InstanceRunning(ctx context.Context, ec2Client *ec2.Client, instanceID string) error {
	logger := logr.FromContextOrDiscard(ctx)

	logger.Info("Starting to wait for EC2 instance running state", "instanceID", instanceID, "timeout", nodeRunningTimeout)
	if err := LogEC2InstanceDescribe(ctx, ec2Client, instanceID, logger); err != nil {
		logger.Error(err, "Failed to log initial instance state", "instanceID", instanceID)
	}

	waiter := ec2.NewInstanceRunningWaiter(ec2Client, func(isowo *ec2.InstanceRunningWaiterOptions) {
		isowo.MinDelay = 1 * time.Second
	})

	err := waiter.Wait(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{instanceID}}, nodeRunningTimeout)

	logger.Info("Finished waiting for EC2 instance", "instanceID", instanceID, "success", err == nil)
	if finalErr := LogEC2InstanceDescribe(ctx, ec2Client, instanceID, logger); finalErr != nil {
		logger.Error(finalErr, "Failed to log final instance state", "instanceID", instanceID)
	}

	return err
}

func IsEC2InstanceImpaired(ctx context.Context, ec2Client *ec2.Client, instanceID string) (bool, error) {
	describeStatusOutput, err := ec2Client.DescribeInstanceStatus(ctx, &ec2.DescribeInstanceStatusInput{
		InstanceIds:         []string{instanceID},
		IncludeAllInstances: aws.Bool(true),
	})
	if err != nil {
		return false, fmt.Errorf("describing instance status %s: %w", instanceID, err)
	}

	if len(describeStatusOutput.InstanceStatuses) == 0 {
		return false, fmt.Errorf("no instance status found with ID %s", instanceID)
	}

	instanceStatus := describeStatusOutput.InstanceStatuses[0]
	if instanceStatus.SystemStatus.Status == types.SummaryStatusOk && instanceStatus.InstanceStatus.Status == types.SummaryStatusOk {
		return false, nil
	}

	for _, status := range instanceStatus.SystemStatus.Details {
		if status.Name == types.StatusNameReachability && status.Status != types.StatusTypePassed {
			return true, nil
		}
	}
	for _, status := range instanceStatus.InstanceStatus.Details {
		if status.Name == types.StatusNameReachability && status.Status != types.StatusTypePassed {
			return true, nil
		}
	}
	return false, nil
}

func LogEC2InstanceDescribe(ctx context.Context, ec2Client *ec2.Client, instanceID string, logger logr.Logger) error {
	instances, ec2Err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if ec2Err != nil {
		return fmt.Errorf("describing instance %s: %w", instanceID, ec2Err)
	}

	if len(instances.Reservations) == 0 || len(instances.Reservations[0].Instances) == 0 {
		return fmt.Errorf("no instance found with ID %s", instanceID)
	}
	logger.Info("Instance details", "instanceID", instanceID, "describeInstanceResponse", instances.Reservations[0].Instances)

	describeStatusOutput, err := ec2Client.DescribeInstanceStatus(ctx, &ec2.DescribeInstanceStatusInput{
		InstanceIds:         []string{instanceID},
		IncludeAllInstances: aws.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("describing instance status %s: %w", instanceID, err)
	}
	if len(describeStatusOutput.InstanceStatuses) == 0 {
		return fmt.Errorf("no instance status found with ID %s", instanceID)
	}
	logger.Info("Instance status", "instanceID", instanceID, "describeInstanceStatusResponse", describeStatusOutput.InstanceStatuses)
	return nil
}

func GetAvailabilityZonesForInstanceType(ctx context.Context, ec2Client *ec2.Client, instanceType string) ([]string, error) {
	availabilityZonesForInstanceType := []string{}
	describeInstanceTypeOfferingsOutput, err := ec2Client.DescribeInstanceTypeOfferings(ctx, &ec2.DescribeInstanceTypeOfferingsInput{
		LocationType: types.LocationTypeAvailabilityZone,
		Filters: []types.Filter{
			{
				Name:   aws.String("instance-type"),
				Values: []string{instanceType},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("describing available offerings for instance type %s: %w", instanceType, err)
	}

	for _, offering := range describeInstanceTypeOfferingsOutput.InstanceTypeOfferings {
		availabilityZonesForInstanceType = append(availabilityZonesForInstanceType, *offering.Location)
	}

	return availabilityZonesForInstanceType, nil
}

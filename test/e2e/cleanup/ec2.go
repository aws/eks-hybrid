package cleanup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	"github.com/go-logr/logr"
)

const (
	waitTimeout = 300 * time.Second
)

type EC2Cleaner struct {
	EC2             *ec2.Client
	ResourceTagging *resourcegroupstaggingapi.Client
	Logger          logr.Logger
}

func shouldTerminateInstance(instance types.Instance, input FilterInput) bool {
	if instance.State.Name == types.InstanceStateNameTerminated {
		return false
	}

	var tags []Tag
	for _, tag := range instance.Tags {
		tags = append(tags, Tag{
			Key:   *tag.Key,
			Value: *tag.Value,
		})
	}

	resource := ResourceWithTags{
		ID:           *instance.InstanceId,
		CreationTime: aws.ToTime(instance.LaunchTime),
		Tags:         tags,
	}
	return shouldDeleteResource(resource, input)
}

func (e *EC2Cleaner) ListTaggedInstances(ctx context.Context, input FilterInput) ([]string, error) {
	tagger := &ResourceTagger{
		ResourceTagging: e.ResourceTagging,
		ClusterName:     input.ClusterName,
	}

	instanceARNs, err := tagger.GetTaggedResources(ctx, "ec2:instance")
	if err != nil {
		return nil, fmt.Errorf("getting tagged EC2 instances: %w", err)
	}

	if len(instanceARNs) == 0 {
		return nil, nil
	}

	var instanceIDs []string
	for _, arn := range instanceARNs {
		parts := strings.Split(arn, "/")
		instanceIDs = append(instanceIDs, parts[len(parts)-1])
	}

	resp, err := e.EC2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: instanceIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("describing instances: %w", err)
	}

	var instancesToTerminate []string
	for _, reservation := range resp.Reservations {
		for _, instance := range reservation.Instances {
			if shouldTerminateInstance(instance, input) {
				instancesToTerminate = append(instancesToTerminate, *instance.InstanceId)
			}
		}
	}
	return instancesToTerminate, nil
}

func (e *EC2Cleaner) DeleteInstances(ctx context.Context, instanceIDs []string) error {
	if len(instanceIDs) == 0 {
		return nil
	}

	e.Logger.Info("Terminating EC2 instances", "instanceIDs", instanceIDs)

	_, err := e.EC2.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: instanceIDs,
	})
	if err != nil {
		return fmt.Errorf("terminating instances: %w", err)
	}

	waiter := ec2.NewInstanceTerminatedWaiter(e.EC2)
	if err := waiter.Wait(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: instanceIDs,
	}, waitTimeout); err != nil {
		return fmt.Errorf("waiting for instances to terminate: %w", err)
	}

	return nil
}

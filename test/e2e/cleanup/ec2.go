package cleanup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	"github.com/go-logr/logr"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/aws/eks-hybrid/test/e2e/constants"
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
	var clusterTagValue string
	for _, tag := range instance.Tags {
		if *tag.Key == constants.TestClusterTagKey {
			clusterTagValue = *tag.Value
			break
		}
	}

	if clusterTagValue == "" {
		return false
	}

	if instance.State.Name == types.InstanceStateNameTerminated ||
		instance.State.Name == types.InstanceStateNameShuttingDown {
		return false
	}

	// For exact cluster name match, terminate regardless of age
	if input.ClusterName != "" {
		return clusterTagValue == input.ClusterName
	}

	// For all clusters or prefix match, check instance age
	if input.AllClusters || (input.ClusterNamePrefix != "" && strings.HasPrefix(clusterTagValue, input.ClusterNamePrefix)) {
		instanceAge := time.Since(aws.ToTime(instance.LaunchTime))
		return instanceAge > input.InstanceAgeThreshold
	}

	return false
}

func (e *EC2Cleaner) DeleteTaggedInstances(ctx context.Context, input FilterInput) error {
	e.Logger.Info("Deleting tagged EC2 instances")

	tagger := &ResourceTagger{
		ResourceTagging: e.ResourceTagging,
		ClusterName:     input.ClusterName,
	}

	instanceARNs, err := tagger.GetTaggedResources(ctx, "ec2:instance")
	if err != nil {
		return fmt.Errorf("getting tagged EC2 instances: %w", err)
	}

	if len(instanceARNs) == 0 {
		return nil
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
		return fmt.Errorf("describing instances: %w", err)
	}

	var instancesToTerminate []string
	for _, reservation := range resp.Reservations {
		for _, instance := range reservation.Instances {
			if shouldTerminateInstance(instance, input) {
				instancesToTerminate = append(instancesToTerminate, *instance.InstanceId)
			}
		}
	}

	if len(instancesToTerminate) == 0 {
		return nil
	}

	e.Logger.Info("Terminating EC2 instances", "instanceIDs", instancesToTerminate)

	if input.DryRun {
		e.Logger.Info("Dry run, skipping instance termination")
		return nil
	}

	_, err = e.EC2.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: instancesToTerminate,
	})
	if err != nil {
		return fmt.Errorf("terminating instances: %w", err)
	}

	waiter := ec2.NewInstanceTerminatedWaiter(e.EC2)
	if err := waiter.Wait(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: instancesToTerminate,
	}, waitTimeout); err != nil {
		return fmt.Errorf("waiting for instances to terminate: %w", err)
	}

	return nil
}

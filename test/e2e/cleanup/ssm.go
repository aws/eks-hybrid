package cleanup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/smithy-go"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/constants"
)

const managedInstanceResourceType = "ManagedInstance"

type SSMCleaner struct {
	SSM          *ssm.Client
	Filter       FilterInput
	ActivationID string
}

func shouldDeleteActivation(activation *types.Activation, input FilterInput) bool {
	var clusterTagValue string
	for _, tag := range activation.Tags {
		if *tag.Key == constants.TestClusterTagKey {
			clusterTagValue = *tag.Value
			break
		}
	}
	if clusterTagValue == "" {
		return false
	}

	// For exact cluster name match, delete regardless of age
	if input.ClusterName != "" {
		return clusterTagValue == input.ClusterName
	}

	// For all clusters or prefix match, check stack age
	if input.AllClusters || (input.ClusterNamePrefix != "" && strings.HasPrefix(clusterTagValue, input.ClusterNamePrefix)) {
		stackAge := time.Since(aws.ToTime(activation.CreatedDate))
		return stackAge > input.InstanceAgeThreshold
	}

	return false
}

func (s *SSMCleaner) DeleteActivations(ctx context.Context, logger logr.Logger) error {
	logger.Info("Deleting SSM activations")

	input := &ssm.DescribeActivationsInput{}
	if s.ActivationID != "" {
		input.Filters = []types.DescribeActivationsFilter{
			{
				FilterKey:    types.DescribeActivationsFilterKeysActivationIds,
				FilterValues: []string{s.ActivationID},
			},
		}
	}
	paginator := ssm.NewDescribeActivationsPaginator(s.SSM, input)

	var activationIDs []string
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("listing SSM activations: %w", err)
		}

		for _, activation := range output.ActivationList {
			if s.ActivationID != "" || shouldDeleteActivation(&activation, s.Filter) {
				activationIDs = append(activationIDs, *activation.ActivationId)
			}
		}
	}

	for _, activationID := range activationIDs {
		logger.Info("Deleting activation", "activationId", activationID)

		if s.Filter.DryRun {
			logger.Info("Dry run, skipping activation deletion")
			continue
		}

		_, err := s.SSM.DeleteActivation(ctx, &ssm.DeleteActivationInput{
			ActivationId: aws.String(activationID),
		})
		if err != nil {
			if IsAWSError(err, "InvalidActivation") {
				logger.Info("SSM activation already deleted", "activationId", activationID)
				continue
			}
			return fmt.Errorf("deleting SSM activation: %w", err)
		}
	}

	return nil
}

func shouldDeleteManagedInstance(instance *types.InstanceInformation, tags []types.Tag, input FilterInput) bool {
	var clusterTagValue string
	for _, tag := range tags {
		if *tag.Key == constants.TestClusterTagKey {
			clusterTagValue = *tag.Value
			break
		}
	}
	if clusterTagValue == "" {
		return false
	}

	// For exact cluster name match, delete regardless of age
	if input.ClusterName != "" {
		return clusterTagValue == input.ClusterName
	}

	// For all clusters or prefix match, check stack age
	if input.AllClusters || (input.ClusterNamePrefix != "" && strings.HasPrefix(clusterTagValue, input.ClusterNamePrefix)) {
		instanceAge := time.Since(aws.ToTime(instance.LastPingDateTime))
		return instanceAge > input.InstanceAgeThreshold
	}

	return false
}

func (s *SSMCleaner) DeleteManagedInstances(ctx context.Context, logger logr.Logger) error {
	logger.Info("Deleting SSM managed instances")

	input := &ssm.DescribeInstanceInformationInput{}
	if s.ActivationID != "" {
		input.Filters = []types.InstanceInformationStringFilter{
			{
				Key:    aws.String("ActivationIds"),
				Values: []string{s.ActivationID},
			},
		}
	} else if s.Filter.ClusterName != "" {
		input.Filters = []types.InstanceInformationStringFilter{
			{
				Key:    aws.String("tag:" + constants.TestClusterTagKey),
				Values: []string{s.Filter.ClusterName},
			},
		}
	} else {
		input.Filters = []types.InstanceInformationStringFilter{
			{
				Key:    aws.String("tag-key"),
				Values: []string{constants.TestClusterTagKey},
			},
		}
	}

	paginator := ssm.NewDescribeInstanceInformationPaginator(s.SSM, input)

	var instanceIDs []string
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("listing SSM managed instances: %w", err)
		}
		for _, instance := range output.InstanceInformationList {
			if instance.ResourceType != managedInstanceResourceType {
				continue
			}
			output, err := s.SSM.ListTagsForResource(ctx, &ssm.ListTagsForResourceInput{
				ResourceId:   aws.String(*instance.InstanceId),
				ResourceType: types.ResourceTypeForTaggingManagedInstance,
			})
			if err != nil && !IsAWSError(err, "InvalidResourceId") {
				return fmt.Errorf("getting tags for managed instance %s: %w", *instance.InstanceId, err)
			}
			if s.ActivationID != "" || shouldDeleteManagedInstance(&instance, output.TagList, s.Filter) {
				instanceIDs = append(instanceIDs, *instance.InstanceId)
			}
		}
	}

	for _, instanceID := range instanceIDs {
		logger.Info("Deregistering managed instance", "instanceId", instanceID)
		if s.Filter.DryRun {
			logger.Info("Dry run, skipping deregistration")
			continue
		}
		_, err := s.SSM.DeregisterManagedInstance(ctx, &ssm.DeregisterManagedInstanceInput{
			InstanceId: aws.String(instanceID),
		})
		if err != nil {
			if IsAWSError(err, "InvalidInstanceId") {
				logger.Info("Managed instance already deregistered", "instanceId", instanceID)
				continue
			}
			return fmt.Errorf("deregistering managed instance: %w", err)
		}
	}

	return nil
}

func IsAWSError(err error, code string) bool {
	var awsErr smithy.APIError
	ok := errors.As(err, &awsErr)
	return err != nil && ok && awsErr.ErrorCode() == code
}

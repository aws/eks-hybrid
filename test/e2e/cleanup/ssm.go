package cleanup

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/smithy-go"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/constants"
)

const managedInstanceResourceType = "ManagedInstance"

type SSMCleaner struct {
	ssm *ssm.Client
}

func NewSSMCleaner(ssm *ssm.Client) *SSMCleaner {
	return &SSMCleaner{
		ssm: ssm,
	}
}

func (s *SSMCleaner) ListActivationsForNode(ctx context.Context, nodeName string) ([]string, error) {
	return s.listActivations(ctx, func(activation *types.Activation) bool {
		return *activation.DefaultInstanceName == nodeName
	})
}

func (s *SSMCleaner) ListActivations(ctx context.Context, filterInput FilterInput) ([]string, error) {
	return s.listActivations(ctx, func(activation *types.Activation) bool {
		return shouldDeleteActivation(activation, filterInput)
	})
}

func (s *SSMCleaner) listActivations(ctx context.Context, shouldDelete func(*types.Activation) bool) ([]string, error) {
	paginator := ssm.NewDescribeActivationsPaginator(s.ssm, &ssm.DescribeActivationsInput{})

	var activationIDs []string
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing SSM activations: %w", err)
		}

		for _, activation := range output.ActivationList {
			if shouldDelete(&activation) {
				activationIDs = append(activationIDs, *activation.ActivationId)
			}
		}
	}

	return activationIDs, nil
}

func (s *SSMCleaner) DeleteActivations(ctx context.Context, activationIDs []string, logger logr.Logger) error {
	for _, activationID := range activationIDs {
		logger.Info("Deleting activation", "activationId", activationID)

		_, err := s.ssm.DeleteActivation(ctx, &ssm.DeleteActivationInput{
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

func (s *SSMCleaner) ListManagedInstancesByActivationID(ctx context.Context, activationIDs ...string) ([]string, error) {
	input := &ssm.DescribeInstanceInformationInput{
		Filters: []types.InstanceInformationStringFilter{
			{
				Key:    aws.String("ActivationIds"),
				Values: activationIDs,
			},
		},
	}

	return s.listManagedInstances(ctx, input, func(instance *types.InstanceInformation, tags []types.Tag) bool {
		return slices.Contains(activationIDs, *instance.ActivationId)
	})
}

func (s *SSMCleaner) ListManagedInstances(ctx context.Context, filterInput FilterInput) ([]string, error) {
	// These filters are mostly just to limit the number of resources returned
	// the source of truth for filtering is done in shouldDeleteManagedInstance
	input := &ssm.DescribeInstanceInformationInput{}
	if filterInput.ClusterName != "" {
		input.Filters = []types.InstanceInformationStringFilter{
			{
				Key:    aws.String("tag:" + constants.TestClusterTagKey),
				Values: []string{filterInput.ClusterName},
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

	return s.listManagedInstances(ctx, input, func(instance *types.InstanceInformation, tags []types.Tag) bool {
		return shouldDeleteManagedInstance(instance, tags, filterInput)
	})
}

func (s *SSMCleaner) listManagedInstances(ctx context.Context, input *ssm.DescribeInstanceInformationInput, shouldDelete func(*types.InstanceInformation, []types.Tag) bool) ([]string, error) {
	var instanceIDs []string

	paginator := ssm.NewDescribeInstanceInformationPaginator(s.ssm, input)

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing SSM managed instances: %w", err)
		}
		for _, instance := range output.InstanceInformationList {
			if instance.ResourceType != managedInstanceResourceType {
				continue
			}
			output, err := s.ssm.ListTagsForResource(ctx, &ssm.ListTagsForResourceInput{
				ResourceId:   aws.String(*instance.InstanceId),
				ResourceType: types.ResourceTypeForTaggingManagedInstance,
			})
			if err != nil && !IsAWSError(err, "InvalidResourceId") {
				return nil, fmt.Errorf("getting tags for managed instance %s: %w", *instance.InstanceId, err)
			}

			if shouldDelete(&instance, output.TagList) {
				instanceIDs = append(instanceIDs, *instance.InstanceId)
			}
		}
	}

	return instanceIDs, nil
}

func (s *SSMCleaner) DeleteManagedInstances(ctx context.Context, instanceIDs []string, logger logr.Logger) error {
	for _, instanceID := range instanceIDs {
		logger.Info("Deregistering managed instance", "instanceId", instanceID)
		_, err := s.ssm.DeregisterManagedInstance(ctx, &ssm.DeregisterManagedInstanceInput{
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

func shouldDeleteManagedInstance(instance *types.InstanceInformation, tags []types.Tag, input FilterInput) bool {
	var customTags []Tag
	for _, tag := range tags {
		customTags = append(customTags, Tag{
			Key:   *tag.Key,
			Value: *tag.Value,
		})
	}

	resource := ResourceWithTags{
		ID:           *instance.InstanceId,
		CreationTime: aws.ToTime(instance.LastPingDateTime),
		Tags:         customTags,
	}

	return shouldDeleteResource(resource, input)
}

func shouldDeleteActivation(activation *types.Activation, input FilterInput) bool {
	var tags []Tag
	for _, tag := range activation.Tags {
		tags = append(tags, Tag{
			Key:   *tag.Key,
			Value: *tag.Value,
		})
	}

	resource := ResourceWithTags{
		ID:           *activation.ActivationId,
		CreationTime: aws.ToTime(activation.CreatedDate),
		Tags:         tags,
	}
	return shouldDeleteResource(resource, input)
}

func IsAWSError(err error, code string) bool {
	var awsErr smithy.APIError
	ok := errors.As(err, &awsErr)
	return err != nil && ok && awsErr.ErrorCode() == code
}

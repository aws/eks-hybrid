package cleanup

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/smithy-go"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/constants"
)

const managedInstanceResourceType = "ManagedInstance"

type SSMCleaner struct {
	ssm             *ssm.Client
	resourceTagging *resourcegroupstaggingapi.Client
}

func NewSSMCleaner(ssm *ssm.Client, resourceTagging *resourcegroupstaggingapi.Client) *SSMCleaner {
	return &SSMCleaner{
		ssm:             ssm,
		resourceTagging: resourceTagging,
	}
}

func (s *SSMCleaner) ListActivations(ctx context.Context, filterInput FilterInput) ([]string, error) {
	paginator := ssm.NewDescribeActivationsPaginator(s.ssm, &ssm.DescribeActivationsInput{})

	var activationIDs []string
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing SSM activations: %w", err)
		}

		for _, activation := range output.ActivationList {
			if shouldDeleteActivation(&activation, filterInput) {
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

func (s *SSMCleaner) ListManagedInstancesByActivationID(ctx context.Context, activationID string) ([]string, error) {
	input := &ssm.DescribeInstanceInformationInput{
		Filters: []types.InstanceInformationStringFilter{
			{
				Key:    aws.String("ActivationIds"),
				Values: []string{activationID},
			},
		},
	}

	return s.listManagedInstances(ctx, input, func(instance *types.InstanceInformation, tags []types.Tag) bool {
		return *instance.ActivationId == activationID
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

func (s *SSMCleaner) ListParameters(ctx context.Context, filterInput FilterInput) ([]string, error) {
	tagger := &ResourceTagger{
		ResourceTagging: s.resourceTagging,
		ClusterName:     filterInput.ClusterName,
	}

	parameterARNs, err := tagger.GetTaggedResources(ctx, "ssm:parameter")
	if err != nil {
		return nil, fmt.Errorf("getting tagged parameters: %w", err)
	}

	var parameterNames []string
	for _, parameterARN := range parameterARNs {
		parameterName := extractParameterName(parameterARN)

		output, err := s.ssm.DescribeParameters(ctx, &ssm.DescribeParametersInput{
			ParameterFilters: []types.ParameterStringFilter{
				{
					Key:    aws.String("Name"),
					Values: []string{parameterName},
				},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("describing SSM parameter %s: %w", parameterName, err)
		}

		if len(output.Parameters) == 0 {
			continue
		}

		parameter := output.Parameters[0]

		tags, err := s.ssm.ListTagsForResource(ctx, &ssm.ListTagsForResourceInput{
			ResourceId:   aws.String(*parameter.Name),
			ResourceType: types.ResourceTypeForTaggingParameter,
		})
		if err != nil && !IsAWSError(err, "InvalidResourceId") {
			return nil, fmt.Errorf("getting tags for SSM parameter %s: %w", *parameter.Name, err)
		}

		if shouldDeleteParameter(parameter, tags.TagList, filterInput) {
			parameterNames = append(parameterNames, *parameter.Name)
		}
	}

	return parameterNames, nil
}

func (s *SSMCleaner) DeleteParameters(ctx context.Context, parameterNames []string, logger logr.Logger) error {
	for _, parameterName := range parameterNames {
		logger.Info("Deleting SSM parameter", "parameterName", parameterName)

		_, err := s.ssm.DeleteParameter(ctx, &ssm.DeleteParameterInput{
			Name: aws.String(parameterName),
		})
		if err != nil && IsAWSError(err, "ParameterNotFound") {
			logger.Info("SSM parameter already deleted", "parameterName", parameterName)
			continue
		}
		if err != nil {
			return fmt.Errorf("deleting SSM parameter %s: %w", parameterName, err)
		}
	}

	return nil
}

// arn format: arn:aws:ssm:us-west-2:<account-id>:parameter/ec2/keypair/key-07c7c4ed2454cdc2f
func extractParameterName(parameterARN string) string {
	parts := strings.Split(parameterARN, ":")
	if len(parts) < 6 {
		return ""
	}

	return strings.TrimPrefix(parts[5], "parameter")
}

func shouldDeleteManagedInstance(instance *types.InstanceInformation, tags []types.Tag, filterInput FilterInput) bool {
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

	return shouldDeleteResource(resource, FilterInput{
		ClusterName:          filterInput.ClusterName,
		AllClusters:          filterInput.AllClusters,
		InstanceAgeThreshold: filterInput.InstanceAgeThreshold,
		DryRun:               filterInput.DryRun,
	})
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

func shouldDeleteParameter(parameter types.ParameterMetadata, tags []types.Tag, input FilterInput) bool {
	customTags := []Tag{}
	for _, tag := range tags {
		customTags = append(customTags, Tag{
			Key:   *tag.Key,
			Value: *tag.Value,
		})
	}

	resource := ResourceWithTags{
		ID:           *parameter.Name,
		CreationTime: aws.ToTime(parameter.LastModifiedDate),
		Tags:         customTags,
	}

	return shouldDeleteResource(resource, input)
}

func IsAWSError(err error, code string) bool {
	var awsErr smithy.APIError
	ok := errors.As(err, &awsErr)
	return err != nil && ok && awsErr.ErrorCode() == code
}

package cleanup

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/errors"
)

const managedInstanceResourceType = "ManagedInstance"

type SSMCleaner struct {
	ssm           *ssm.Client
	taggingClient *ResourceTaggingClient
	logger        logr.Logger
}

func NewSSMCleaner(ssm *ssm.Client, taggingClient *ResourceTaggingClient, logger logr.Logger) *SSMCleaner {
	return &SSMCleaner{
		ssm:           ssm,
		taggingClient: taggingClient,
		logger:        logger,
	}
}

func (s *SSMCleaner) ListActivationsForNode(ctx context.Context, nodeName, clusterName string) ([]string, error) {
	filterInput := FilterInput{ClusterName: clusterName}
	return s.listActivations(ctx, func(activation *types.Activation) bool {
		return shouldDeleteActivation(activation, filterInput) && aws.ToString(activation.DefaultInstanceName) == nodeName
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

func (s *SSMCleaner) DeleteActivation(ctx context.Context, activationID string) error {
	_, err := s.ssm.DeleteActivation(ctx, &ssm.DeleteActivationInput{
		ActivationId: aws.String(activationID),
	})
	if err != nil && errors.IsType(err, &types.InvalidActivation{}) {
		s.logger.Info("SSM activation already deleted", "activationId", activationID)
		return nil
	}

	if err != nil {
		return fmt.Errorf("deleting SSM activation: %w", err)
	}
	s.logger.Info("Deleted activation", "activationId", activationID)
	return nil
}

func (s *SSMCleaner) ListManagedInstancesByActivationID(ctx context.Context, activationIDs ...string) ([]string, error) {
	var instanceIDs []string

	paginator := ssm.NewDescribeInstanceInformationPaginator(s.ssm, &ssm.DescribeInstanceInformationInput{
		Filters: []types.InstanceInformationStringFilter{
			{
				Key:    aws.String("ActivationIds"),
				Values: activationIDs,
			},
		},
	})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing SSM managed instances: %w", err)
		}
		for _, instance := range output.InstanceInformationList {
			if instance.ResourceType != managedInstanceResourceType {
				continue
			}

			if slices.Contains(activationIDs, *instance.ActivationId) {
				instanceIDs = append(instanceIDs, *instance.InstanceId)
			}
		}
	}

	return instanceIDs, nil
}

// ex: arn:aws:ssm:us-west-2:736510011942:managed-instance/mi-0f2d0b4c974837b23
func managedInstanceIDFromARN(arn string) string {
	parts := strings.Split(arn, "/")
	return parts[len(parts)-1]
}

func (s *SSMCleaner) ListManagedInstances(ctx context.Context, filterInput FilterInput) ([]string, error) {
	// we use resourcetaggingapi here since ssm list managed-instance api does not return tags for the instances
	// this means we end up having to make a number of listtagsforresource api requests just to determine that
	// we do not wish to delete a given instance
	// resourcetaggingapi allows us to retrieve only the instances we want based on the filterinput
	// and then we can make one additional request to describeinstanceinformation to get the creation times
	resourceARNs, err := s.taggingClient.GetResourcesWithClusterTag(ctx, "ssm:managed-instance", filterInput)
	if err != nil {
		return nil, fmt.Errorf("listing SSM managed instances: %w", err)
	}

	instanceIDToTags := map[string][]Tag{}
	instanceIDs := []string{}
	for resourceARN, tags := range resourceARNs {
		instanceID := managedInstanceIDFromARN(resourceARN)
		instanceIDToTags[instanceID] = tags
		instanceIDs = append(instanceIDs, instanceID)
	}

	paginator := ssm.NewDescribeInstanceInformationPaginator(s.ssm, &ssm.DescribeInstanceInformationInput{
		Filters: []types.InstanceInformationStringFilter{
			{
				Key:    aws.String("InstanceIds"),
				Values: instanceIDs,
			},
		},
	})

	instanceIDs = []string{}
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing SSM managed instances: %w", err)
		}

		for _, instance := range output.InstanceInformationList {
			if instance.ResourceType != managedInstanceResourceType {
				continue
			}

			tags := instanceIDToTags[*instance.InstanceId]
			if !shouldDeleteManagedInstance(&instance, tags, filterInput) {
				continue
			}

			instanceIDs = append(instanceIDs, *instance.InstanceId)
		}
	}

	return instanceIDs, nil
}

func (s *SSMCleaner) DeleteManagedInstance(ctx context.Context, instanceID string) error {
	_, err := s.ssm.DeregisterManagedInstance(ctx, &ssm.DeregisterManagedInstanceInput{
		InstanceId: aws.String(instanceID),
	})
	if err != nil && errors.IsType(err, &types.InvalidInstanceId{}) {
		s.logger.Info("Managed instance already deregistered", "instanceId", instanceID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("deregistering managed instance: %w", err)
	}

	s.logger.Info("Deregistered managed instance", "instanceId", instanceID)
	return nil
}

// ex: "ResourceARN": "arn:aws:ssm:us-west-2:<account>:parameter/ec2/keypair/key-03a251c976f96388d"
func parameterNameFromARN(arn string) string {
	parts := strings.Split(arn, ":")
	part := parts[len(parts)-1]
	return strings.TrimPrefix(part, "parameter")
}

func (s *SSMCleaner) ListParameters(ctx context.Context, filterInput FilterInput) ([]string, error) {
	// we use resourcetaggingapi here since ssm list parameters api does not support filters
	// and does not return tags for the parameters, requiring additional api calls to listtagsforresource
	// just to determine this is not a parameter we want to delete
	// resourcetaggingapi allows us to retrieve only the parameters we want based on the filterinput
	// and then we can make one additional request to get all the parameters details
	resourceARNs, err := s.taggingClient.GetResourcesWithClusterTag(ctx, "ssm:parameter", filterInput)
	if err != nil {
		return nil, fmt.Errorf("listing SSM parameters: %w", err)
	}

	parameterNameToTags := map[string][]Tag{}
	parameterNames := []string{}
	for resourceARN, tags := range resourceARNs {
		parameterName := parameterNameFromARN(resourceARN)
		parameterNameToTags[parameterName] = tags
		parameterNames = append(parameterNames, parameterName)
	}

	parameters, err := s.ssm.GetParameters(ctx, &ssm.GetParametersInput{
		Names: parameterNames,
	})
	if err != nil {
		return nil, fmt.Errorf("listing SSM parameters: %w", err)
	}

	parameterNames = []string{}
	for _, parameter := range parameters.Parameters {
		tags := parameterNameToTags[*parameter.Name]
		if !shouldDeleteParameter(parameter, tags, filterInput) {
			continue
		}

		parameterNames = append(parameterNames, *parameter.Name)
	}

	return parameterNames, nil
}

func (s *SSMCleaner) DeleteParameter(ctx context.Context, parameterName string) error {
	_, err := s.ssm.DeleteParameter(ctx, &ssm.DeleteParameterInput{
		Name: aws.String(parameterName),
	})
	if err != nil && errors.IsType(err, &types.ParameterNotFound{}) {
		s.logger.Info("SSM parameter already deleted", "parameterName", parameterName)
		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting SSM parameter %s: %w", parameterName, err)
	}
	s.logger.Info("Deleted SSM parameter", "parameterName", parameterName)
	return nil
}

func shouldDeleteManagedInstance(instance *types.InstanceInformation, tags []Tag, input FilterInput) bool {
	resource := ResourceWithTags{
		ID:           *instance.InstanceId,
		CreationTime: aws.ToTime(instance.RegistrationDate),
		Tags:         tags,
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

func shouldDeleteParameter(parameter types.Parameter, tags []Tag, input FilterInput) bool {
	resource := ResourceWithTags{
		ID:           *parameter.Name,
		CreationTime: aws.ToTime(parameter.LastModifiedDate),
		Tags:         tags,
	}

	return shouldDeleteResource(resource, input)
}

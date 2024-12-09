package cluster

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/wait"
)

//go:embed cfn-templates/setup-cfn.yaml
var setupTemplateBody []byte

type VpcConfig struct {
	VpcID         string
	PublicSubnet  string
	PrivateSubnet string
	SecurityGroup string
}

type ResourcesStackOutput struct {
	ClusterRole          string
	VPCPeeringConnection string
	ClusterVpcConfig     VpcConfig
	HybridNodeVpcConfig  VpcConfig
}

const (
	stackWaitTimeout  = 2 * time.Minute
	stackWaitInterval = 10 * time.Second
)

func getArchitectureStackName(clusterName string) string {
	return fmt.Sprintf("EKSHybridCI-Arch-%s", clusterName)
}

func (t *TestResources) DeployStack(ctx context.Context, client *cloudformation.Client, logger logr.Logger) (*ResourcesStackOutput, error) {
	stackName := getArchitectureStackName(t.ClusterName)
	resp, err := client.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
		StackName: aws.String(stackName),
	})
	if err != nil && !e2e.IsErrorType(err, &types.StackInstanceNotFoundException{}) {
		return nil, fmt.Errorf("looking for hybrid nodes cfn stack: %w", err)
	}

	params := []types.Parameter{
		{
			ParameterKey:   aws.String("ClusterName"),
			ParameterValue: aws.String(t.ClusterName),
		},
		{
			ParameterKey:   aws.String("ClusterRegion"),
			ParameterValue: aws.String(t.ClusterRegion),
		},
		{
			ParameterKey:   aws.String("ClusterVPCCidr"),
			ParameterValue: aws.String(t.ClusterNetwork.VpcCidr),
		},
		{
			ParameterKey:   aws.String("ClusterPublicSubnetCidr"),
			ParameterValue: aws.String(t.ClusterNetwork.PublicSubnetCidr),
		},
		{
			ParameterKey:   aws.String("ClusterPrivateSubnetCidr"),
			ParameterValue: aws.String(t.ClusterNetwork.PrivateSubnetCidr),
		},
		{
			ParameterKey:   aws.String("HybridNodeVPCCidr"),
			ParameterValue: aws.String(t.HybridNetwork.VpcCidr),
		},
		{
			ParameterKey:   aws.String("HybridNodePublicSubnetCidr"),
			ParameterValue: aws.String(t.HybridNetwork.PublicSubnetCidr),
		},
		{
			ParameterKey:   aws.String("HybridNodePrivateSubnetCidr"),
			ParameterValue: aws.String(t.HybridNetwork.PrivateSubnetCidr),
		},
		{
			ParameterKey:   aws.String("HybridNodePodCidr"),
			ParameterValue: aws.String(t.HybridNetwork.PodCidr),
		},
		{
			ParameterKey:   aws.String("TestClusterTagKey"),
			ParameterValue: aws.String(constants.TestClusterTagKey),
		},
	}

	if resp == nil || resp.Stacks == nil {
		logger.Info("Creating hybrid nodes setup stack", "stackName", stackName)
		_, err = client.CreateStack(ctx, &cloudformation.CreateStackInput{
			StackName:    aws.String(stackName),
			TemplateBody: aws.String(string(setupTemplateBody)),
			Parameters:   params,
			Capabilities: []types.Capability{
				types.CapabilityCapabilityIam,
				types.CapabilityCapabilityNamedIam,
			},
			Tags: []types.Tag{{
				Key:   aws.String(constants.TestClusterTagKey),
				Value: aws.String(t.ClusterName),
			}},
		})
		if err != nil {
			return nil, fmt.Errorf("creating hybrid nodes setup cfn stack: %w", err)
		}

		logger.Info("Waiting for hybrid nodes setup stack to be created", "stackName", stackName)

		err = waitForStackOperation(ctx, client, stackName)
		if err != nil {
			return nil, fmt.Errorf("waiting for hybrid nodes setup cfn stack: %w", err)
		}
	} else {
		logger.Info("Updating hybrid nodes setup stack", "stackName", stackName)
		_, err = client.UpdateStack(ctx, &cloudformation.UpdateStackInput{
			StackName: aws.String(stackName),
			Capabilities: []types.Capability{
				types.CapabilityCapabilityIam,
				types.CapabilityCapabilityNamedIam,
			},
			TemplateBody: aws.String(string(setupTemplateBody)),
			Parameters:   params,
		})
		if err != nil {
			// Handle "No updates are to be performed"
			if strings.Contains(err.Error(), "No updates are to be performed") {
				logger.Info("No changes detected in the stack, no update necessary.")
			} else {
				return nil, fmt.Errorf("updating CloudFormation stack: %w", err)
			}
		}

		logger.Info("Waiting for hybrid nodes setup stack to be updated", "stackName", stackName)
		err = waitForStackOperation(ctx, client, stackName)
		if err != nil {
			return nil, fmt.Errorf("waiting for hybrid nodes setup cfn stack: %w", err)
		}
	}

	resp, err = client.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
		StackName: aws.String(stackName),
	})
	if err != nil {
		return nil, fmt.Errorf("describing hybrid nodes setup cfn stack: %w", err)
	}

	result := &ResourcesStackOutput{}
	// extract relevant stack outputs
	for _, output := range resp.Stacks[0].Outputs {
		switch aws.ToString(output.OutputKey) {
		case "ClusterRole":
			result.ClusterRole = *output.OutputValue
		case "ClusterVPC":
			result.ClusterVpcConfig.VpcID = *output.OutputValue
		case "ClusterVPCPublicSubnet":
			result.ClusterVpcConfig.PublicSubnet = *output.OutputValue
		case "ClusterVPCPrivateSubnet":
			result.ClusterVpcConfig.PrivateSubnet = *output.OutputValue
		case "ClusterSecurityGroup":
			result.ClusterVpcConfig.SecurityGroup = *output.OutputValue
		case "HybridNodeVPC":
			result.HybridNodeVpcConfig.VpcID = *output.OutputValue
		case "HybridNodeVPCPublicSubnet":
			result.HybridNodeVpcConfig.PublicSubnet = *output.OutputValue
		case "HybridNodeVPCPrivateSubnet":
			result.HybridNodeVpcConfig.PrivateSubnet = *output.OutputValue
		case "HybridNodeSecurityGroup":
			result.HybridNodeVpcConfig.SecurityGroup = *output.OutputValue
		case "VPCPeeringConnection":
			result.VPCPeeringConnection = *output.OutputValue
		}
	}

	logger.Info("E2E resources stack deployed successfully", "stackName", stackName)
	return result, nil
}

func waitForStackOperation(ctx context.Context, client *cloudformation.Client, stackName string) error {
	err := wait.PollUntilContextTimeout(ctx, stackWaitInterval, stackWaitTimeout, true, func(ctx context.Context) (bool, error) {
		stackOutput, err := client.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
			StackName: aws.String(stackName),
		})
		if err != nil {
			if e2e.IsErrorType(err, &types.StackInstanceNotFoundException{}) {
				return true, nil
			}
			return false, err
		}

		stackStatus := stackOutput.Stacks[0].StackStatus
		switch stackStatus {
		case types.StackStatusCreateComplete, types.StackStatusUpdateComplete, types.StackStatusDeleteComplete:
			return true, nil
		case types.StackStatusCreateInProgress, types.StackStatusUpdateInProgress, types.StackStatusDeleteInProgress, types.StackStatusUpdateCompleteCleanupInProgress:
			return false, nil
		default:
			return false, fmt.Errorf("stack %s failed with status: %s", stackName, stackStatus)
		}
	})

	return err
}

func (c *CleanupResources) DeleteStack(ctx context.Context, client *cloudformation.Client, logger logr.Logger) error {
	stackName := getArchitectureStackName(c.ClusterName)
	_, err := client.DeleteStack(ctx, &cloudformation.DeleteStackInput{
		StackName: aws.String(stackName),
	})
	if err != nil {
		return fmt.Errorf("deleting hybrid nodes setup cfn stack: %w", err)
	}
	err = waitForStackOperation(ctx, client, stackName)
	if err != nil {
		return fmt.Errorf("waiting for hybrid nodes setup cfn stack to be deleted: %w", err)
	}

	logger.Info("E2E resources stack deleted successfully", "stackName", stackName)
	return nil
}

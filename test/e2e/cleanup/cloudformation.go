package cleanup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/errors"
)

type FilterInput struct {
	ClusterName          string
	ClusterNamePrefix    string
	AllClusters          bool
	InstanceAgeThreshold time.Duration
	DryRun               bool
}

type CFNStackCleanup struct {
	CFN             *cloudformation.Client
	ResourceTagging *resourcegroupstaggingapi.Client
	Logger          logr.Logger
}

type CFNStack struct {
	StackName   string
	ClusterName string
}

func getClusterTagValue(stack *types.Stack) string {
	var clusterTagValue string
	for _, tag := range stack.Tags {
		if *tag.Key == constants.TestClusterTagKey {
			clusterTagValue = *tag.Value
			break
		}
	}
	return clusterTagValue
}

func shouldDeleteStack(stack *types.Stack, input FilterInput) bool {
	clusterTagValue := getClusterTagValue(stack)
	if clusterTagValue == "" {
		return false
	}

	stackName := *stack.StackName

	// For exact cluster name match, delete regardless of age
	if input.ClusterName != "" {
		return stackName == input.ClusterName
	}

	// For all clusters or prefix match, check stack age
	if input.AllClusters || (input.ClusterNamePrefix != "" && strings.HasPrefix(clusterTagValue, input.ClusterNamePrefix)) {
		stackAge := time.Since(aws.ToTime(stack.CreationTime))
		return stackAge > input.InstanceAgeThreshold
	}

	return false
}

// ListCredentialStacks lists all the credential stacks for a given cluster
// credentials stacks start with EKSHybridCI but not EKSHybridCI-Arch
func (c *CFNStackCleanup) ListCredentialStacks(ctx context.Context, input FilterInput) ([]CFNStack, error) {
	return c.listStacks(ctx, input, func(stackName string) bool {
		return strings.HasPrefix(stackName, constants.TestCredentialsStackNamePrefix) &&
			!strings.HasPrefix(stackName, constants.TestArchitectureStackNamePrefix)
	})
}

// ListArchitectureStacks lists all the architecture stacks for a given cluster
// architecture stacks start with EKSHybridCI-Arch
func (c *CFNStackCleanup) ListArchitectureStacks(ctx context.Context, input FilterInput) ([]CFNStack, error) {
	return c.listStacks(ctx, input, func(stackName string) bool {
		return strings.HasPrefix(stackName, constants.TestArchitectureStackNamePrefix)
	})
}

func (c *CFNStackCleanup) listStacks(ctx context.Context, input FilterInput, shouldInclude func(string) bool) ([]CFNStack, error) {
	tagger := &ResourceTagger{
		ResourceTagging: c.ResourceTagging,
		ClusterName:     input.ClusterName,
	}

	stackARNs, err := tagger.GetTaggedResources(ctx, "cloudformation:stack")
	if err != nil {
		return nil, fmt.Errorf("getting tagged stack ARNs: %w", err)
	}

	var stacks []CFNStack
	for _, stackARN := range stackARNs {
		output, err := c.CFN.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
			StackName: &stackARN,
		})
		if err != nil && errors.IsType(err, &types.StackNotFoundException{}) {
			c.Logger.Info("Stack already deleted", "stack", stackARN)
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("describing stack %s: %w", stackARN, err)
		}
		if len(output.Stacks) == 0 {
			continue
		}

		stackName := *output.Stacks[0].StackName

		if !shouldInclude(stackName) {
			continue
		}

		if shouldDeleteStack(&output.Stacks[0], input) {
			stacks = append(stacks, CFNStack{
				StackName:   stackName,
				ClusterName: getClusterTagValue(&output.Stacks[0]),
			})
		}
	}

	return stacks, nil
}

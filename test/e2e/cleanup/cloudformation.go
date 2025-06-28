package cleanup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/cfn"
	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/errors"
)

const (
	stackRetryDelay      = 5 * time.Second
	stackDeletionTimeout = 15 * time.Minute
)

type CFNStackCleanup struct {
	cfnClient *cloudformation.Client
	logger    logr.Logger
}

func NewCFNStackCleanup(cfnClient *cloudformation.Client, logger logr.Logger) *CFNStackCleanup {
	return &CFNStackCleanup{
		cfnClient: cfnClient,
		logger:    logger,
	}
}

// ListCredentialStacks lists all the credential stacks for a given cluster
// credentials stacks start with EKSHybridCI but not EKSHybridCI-Arch
func (c *CFNStackCleanup) ListCredentialStacks(ctx context.Context, input FilterInput) ([]string, error) {
	return c.listStacks(ctx, input, func(stackName string) bool {
		return strings.HasPrefix(stackName, constants.TestCredentialsStackNamePrefix) &&
			!strings.HasPrefix(stackName, constants.TestArchitectureStackNamePrefix)
	})
}

// ListArchitectureStacks lists all the architecture stacks for a given cluster
// architecture stacks start with EKSHybridCI-Arch
func (c *CFNStackCleanup) ListArchitectureStacks(ctx context.Context, input FilterInput) ([]string, error) {
	return c.listStacks(ctx, input, func(stackName string) bool {
		return strings.HasPrefix(stackName, constants.TestArchitectureStackNamePrefix)
	})
}

func (c *CFNStackCleanup) DeleteStack(ctx context.Context, stackName string) error {
	// we retry to handle the case where the stack is in a failed state
	// and we need to force delete it
	for range 3 {
		// wait for any pending stack operations to stablize before triggering the delete
		// This will cover pre-existing in-progress operations which would block the deletion as well as waiting after triggering the delete
		if err := cfn.WaitForStackOperation(ctx, c.cfnClient, stackName, stackRetryDelay, stackDeletionTimeout); err != nil {
			c.logger.Error(err, "failed while waiting for stack operation to stabilize, proceeding with deletion", "stackName", stackName)
		}
		describeStackInput := &cloudformation.DescribeStacksInput{
			StackName: aws.String(stackName),
		}
		stackOutput, err := c.cfnClient.DescribeStacks(ctx, describeStackInput)
		if err != nil && errors.IsCFNStackNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("describing hybrid nodes cfn stack before deletion: %w", err)
		}

		input := &cloudformation.DeleteStackInput{
			StackName:    aws.String(stackName),
			DeletionMode: types.DeletionModeStandard,
		}

		if stackOutput.Stacks[0].StackStatus == types.StackStatusDeleteFailed {
			input.DeletionMode = types.DeletionModeForceDeleteStack
		}

		c.logger.Info("Deleting hybrid nodes cfn stack with deletion mode", "stackName", stackName, "deletionMode", input.DeletionMode)
		_, err = c.cfnClient.DeleteStack(ctx, input)
		if err != nil && errors.IsCFNStackNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("deleting hybrid nodes cfn stack: %w", err)
		}

	}
	return fmt.Errorf("failed to delete hybrid nodes cfn stack: %s", stackName)
}

func (c *CFNStackCleanup) listStacks(ctx context.Context, input FilterInput, wantName func(string) bool) ([]string, error) {
	// all status except for StackStatusDeleteComplete
	paginator := cloudformation.NewListStacksPaginator(c.cfnClient, &cloudformation.ListStacksInput{
		StackStatusFilter: []types.StackStatus{
			types.StackStatusCreateInProgress,
			types.StackStatusCreateFailed,
			types.StackStatusCreateComplete,
			types.StackStatusRollbackInProgress,
			types.StackStatusRollbackFailed,
			types.StackStatusRollbackComplete,
			types.StackStatusDeleteInProgress,
			types.StackStatusDeleteFailed,
			types.StackStatusUpdateInProgress,
			types.StackStatusUpdateCompleteCleanupInProgress,
			types.StackStatusUpdateComplete,
			types.StackStatusUpdateFailed,
			types.StackStatusUpdateRollbackInProgress,
			types.StackStatusUpdateRollbackFailed,
			types.StackStatusUpdateRollbackCompleteCleanupInProgress,
			types.StackStatusUpdateRollbackComplete,
			types.StackStatusReviewInProgress,
			types.StackStatusImportInProgress,
			types.StackStatusImportComplete,
			types.StackStatusImportRollbackInProgress,
			types.StackStatusImportRollbackFailed,
			types.StackStatusImportRollbackComplete,
		},
	})

	var stacks []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describing instances: %w", err)
		}
		for _, stack := range page.StackSummaries {
			if !wantName(*stack.StackName) {
				continue
			}

			describeStackInput := &cloudformation.DescribeStacksInput{
				StackName: stack.StackName,
			}
			stackOutput, err := c.cfnClient.DescribeStacks(ctx, describeStackInput)
			if err != nil && errors.IsCFNStackNotFound(err) {
				// skipping log since we are possiblying checking stacks we do not
				// intend to delete
				continue
			}

			if err != nil {
				return nil, fmt.Errorf("describing stack %s: %w", *stack.StackName, err)
			}

			if len(stackOutput.Stacks) == 0 {
				return nil, fmt.Errorf("stack %s not found", *stack.StackName)
			}

			var tags []Tag
			for _, tag := range stackOutput.Stacks[0].Tags {
				tags = append(tags, Tag{
					Key:   *tag.Key,
					Value: *tag.Value,
				})
			}

			resource := ResourceWithTags{
				ID:           *stack.StackId,
				CreationTime: aws.ToTime(stack.CreationTime),
				Tags:         tags,
			}

			if shouldDeleteResource(resource, input) {
				stacks = append(stacks, *stack.StackName)
			}
		}
	}

	return stacks, nil
}

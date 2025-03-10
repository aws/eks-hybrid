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

	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/errors"
)

const (
	stackRetryDelay      = 5 * time.Second
	stackDeletionTimeout = 8 * time.Minute
)

type CFNStackCleanup struct {
	CFN    *cloudformation.Client
	Logger logr.Logger
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
		describeStackInput := &cloudformation.DescribeStacksInput{
			StackName: aws.String(stackName),
		}
		stackOutput, err := c.CFN.DescribeStacks(ctx, describeStackInput)
		if err != nil && errors.IsCFNStackNotFound(err) {
			c.Logger.Info("Stack already deleted", "stack", stackName)
			return nil
		}
		if err != nil {
			return fmt.Errorf("deleting hybrid nodes cfn stack: %w", err)
		}

		input := &cloudformation.DeleteStackInput{
			StackName:    aws.String(stackName),
			DeletionMode: types.DeletionModeStandard,
		}

		if stackOutput.Stacks[0].StackStatus == types.StackStatusDeleteFailed {
			input.DeletionMode = types.DeletionModeForceDeleteStack
		}

		c.Logger.Info("Deleting hybrid nodes cfn stack with deletion mode", "stackName", stackName, "deletionMode", input.DeletionMode)
		_, err = c.CFN.DeleteStack(ctx, input)
		if err != nil && errors.IsCFNStackNotFound(err) {
			c.Logger.Info("Stack already deleted", "stack", stackName)
			return nil
		}
		if err != nil {
			return fmt.Errorf("deleting hybrid nodes cfn stack: %w", err)
		}

		waiter := cloudformation.NewStackDeleteCompleteWaiter(c.CFN, func(opts *cloudformation.StackDeleteCompleteWaiterOptions) {
			opts.MinDelay = stackRetryDelay
			opts.MaxDelay = stackRetryDelay
		})
		if err = waiter.Wait(ctx, describeStackInput, stackDeletionTimeout); err != nil {
			failureReason, err := GetStackFailureReason(ctx, c.CFN, stackName)
			if err != nil {
				c.Logger.Info("Retrying delete of cfn stack, failure reason not found", "stackName", stackName)
			} else {
				c.Logger.Info("Retrying delete of cfn stack", "stackName", stackName, "failureReason", failureReason)
			}
			continue
		}
		c.Logger.Info("E2E resources stack deleted successfully", "stackName", stackName)
		return nil
	}
	return fmt.Errorf("failed to delete hybrid nodes cfn stack: %s", stackName)
}

func (c *CFNStackCleanup) listStacks(ctx context.Context, input FilterInput, wantName func(string) bool) ([]string, error) {
	// all status except for StackStatusDeleteComplete
	paginator := cloudformation.NewListStacksPaginator(c.CFN, &cloudformation.ListStacksInput{
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

			if stack.StackStatus == types.StackStatusDeleteComplete {
				continue
			}

			describeStackInput := &cloudformation.DescribeStacksInput{
				StackName: stack.StackName,
			}
			stackOutput, err := c.CFN.DescribeStacks(ctx, describeStackInput)
			if err != nil && errors.IsCFNStackNotFound(err) {
				c.Logger.Info("Stack already deleted", "stack", *stack.StackName)
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

func GetStackFailureReason(ctx context.Context, client *cloudformation.Client, stackName string) (string, error) {
	resp, err := client.DescribeStackEvents(ctx, &cloudformation.DescribeStackEventsInput{
		StackName: &stackName,
	})
	if err != nil {
		return "", fmt.Errorf("describing events for stack %s: %w", stackName, err)
	}
	firstFailedEventTimestamp := time.Now()
	var firstFailedEventReason string
	for _, event := range resp.StackEvents {
		if event.ResourceStatus == types.ResourceStatusCreateFailed ||
			event.ResourceStatus == types.ResourceStatusUpdateFailed ||
			event.ResourceStatus == types.ResourceStatusDeleteFailed {
			if event.ResourceStatusReason == nil {
				continue
			}
			timestamp := aws.ToTime(event.Timestamp)
			if timestamp.Before(firstFailedEventTimestamp) {
				firstFailedEventTimestamp = timestamp
				firstFailedEventReason = *event.ResourceStatusReason
			}
		}
	}

	return firstFailedEventReason, nil
}

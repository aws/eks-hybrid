package ssm

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/go-logr/logr"
)

type ssmConfig struct {
	client     *ssm.SSM
	instanceID string
	commands   []string
}

func (s *ssmConfig) runCommandsOnInstance(ctx context.Context, logger logr.Logger, opts ...request.WaiterOption) ([]ssm.GetCommandInvocationOutput, error) {
	outputs := []ssm.GetCommandInvocationOutput{}

	logger.Info(fmt.Sprintf("Running command: %v\n", s.commands))
	input := &ssm.SendCommandInput{
		DocumentName: aws.String("AWS-RunShellScript"),
		Parameters: map[string][]*string{
			"commands": aws.StringSlice(s.commands),
		},
		InstanceIds: []*string{aws.String(s.instanceID)},
	}
	output, err := s.client.SendCommandWithContext(ctx, input)
	// Retry if the ThrottlingException occurred
	for err != nil && isThrottlingException(err) {
		logger.Info("ThrottlingException encountered, retrying..")
		output, err = s.client.SendCommandWithContext(ctx, input)
	}
	invocationInput := &ssm.GetCommandInvocationInput{
		CommandId:  output.Command.CommandId,
		InstanceId: aws.String(s.instanceID),
	}
	// Will wait on Pending, InProgress, or Cancelling until we reach a terminal status of Success, Cancelled, Failed, TimedOut
	_ = s.client.WaitUntilCommandExecutedWithContext(ctx, invocationInput)
	invocationOutput, err := s.client.GetCommandInvocationWithContext(ctx, invocationInput)
	if err != nil {
		return nil, fmt.Errorf("got an error calling GetCommandInvocation: %w", err)
	}
	logger.Info(invocationOutput.String())
	outputs = append(outputs, *invocationOutput)

	return outputs, nil
}

func (s *ssmConfig) runCommandsOnInstanceWaitForInProgress(ctx context.Context, logger logr.Logger) ([]ssm.GetCommandInvocationOutput, error) {
	opts := func(w *request.Waiter) {
		w.Acceptors = []request.WaiterAcceptor{
			{
				State:   request.SuccessWaiterState,
				Matcher: request.PathWaiterMatch, Argument: "Status",
				Expected: "InProgress",
			},
		}
	}
	return s.runCommandsOnInstance(ctx, logger, opts)
}

func isThrottlingException(err error) bool {
	if awsErr, ok := err.(awserr.Error); ok && awsErr != nil {
		return awsErr.Code() == "ThrottlingException"
	}
	return false
}

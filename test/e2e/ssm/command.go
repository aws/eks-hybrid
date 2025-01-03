package ssm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/go-logr/logr"

	e2eCommands "github.com/aws/eks-hybrid/test/e2e/commands"
)

// ssm commands run as root user on jumpbox
func makeSshCommand(instanceIP string, commands []string) string {
	return fmt.Sprintf("ssh %s \"%s\"", instanceIP, strings.Replace(strings.Join(commands, ";"), "\"", "\\\"", -1))
}

type SSHOnSSM struct {
	client            *ssm.SSM
	jumpboxInstanceId string
	logger            logr.Logger
}

func NewSSHOnSSMCommandRunner(client *ssm.SSM, jumpboxInstanceId string, logger logr.Logger) e2eCommands.RemoteCommandRunner {
	return &SSHOnSSM{
		client:            client,
		jumpboxInstanceId: jumpboxInstanceId,
		logger:            logger,
	}
}

func (s *SSHOnSSM) Run(ctx context.Context, ip string, commands []string) (e2eCommands.RemoteCommandOutput, error) {
	command := makeSshCommand(ip, commands)
	s.logger.Info(fmt.Sprintf("Running command: %v\n", command))
	input := &ssm.SendCommandInput{
		DocumentName: aws.String("AWS-RunShellScript"),
		Parameters: map[string][]*string{
			"commands": aws.StringSlice([]string{command}),
		},
		InstanceIds: []*string{aws.String(s.jumpboxInstanceId)},
	}
	output, err := s.client.SendCommandWithContext(ctx, input)
	// Retry if the ThrottlingException occurred
	for err != nil && isThrottlingException(err) {
		s.logger.Info("ThrottlingException encountered, retrying..")
		output, err = s.client.SendCommandWithContext(ctx, input)
	}
	if err != nil {
		return e2eCommands.RemoteCommandOutput{}, fmt.Errorf("got an error calling SendCommand: %w", err)
	}
	invocationInput := &ssm.GetCommandInvocationInput{
		CommandId:  output.Command.CommandId,
		InstanceId: aws.String(s.jumpboxInstanceId),
	}
	// Will wait on Pending, InProgress, or Cancelling until we reach a terminal status of Success, Cancelled, Failed, TimedOut
	_ = s.client.WaitUntilCommandExecutedWithContext(ctx, invocationInput, func(w *request.Waiter) {
		// some of the nodeadm commands take longer to complete than the default timeout and retries allows
		// these is mostly for rhel8 instances
		w.MaxAttempts = 100
		w.Delay = request.ConstantWaiterDelay(5 * time.Second)
	})
	invocationOutput, err := s.client.GetCommandInvocationWithContext(ctx, invocationInput)
	if err != nil {
		return e2eCommands.RemoteCommandOutput{}, fmt.Errorf("got an error calling GetCommandInvocation: %w", err)
	}

	commandOutput := e2eCommands.RemoteCommandOutput{
		ResponseCode:   *invocationOutput.ResponseCode,
		StandardError:  *invocationOutput.StandardErrorContent,
		StandardOutput: *invocationOutput.StandardOutputContent,
		Status:         *invocationOutput.Status,
	}

	s.logger.Info(fmt.Sprintf("Status: %s", commandOutput.Status))
	s.logger.Info(fmt.Sprintf("ResponseCode: %d", commandOutput.ResponseCode))
	s.logger.Info(fmt.Sprintf("Stdout: %s", commandOutput.StandardOutput))
	s.logger.Info(fmt.Sprintf("Stderr: %s", commandOutput.StandardError))

	return commandOutput, nil
}

func isThrottlingException(err error) bool {
	if awsErr, ok := err.(awserr.Error); ok && awsErr != nil {
		return awsErr.Code() == "ThrottlingException"
	}
	return false
}

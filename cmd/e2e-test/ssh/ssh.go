package ssh

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/integrii/flaggy"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/peered"
)

type command struct {
	flaggy     *flaggy.Subcommand
	instanceID string
}

func NewCommand() cli.Command {
	cmd := command{}

	setupCmd := flaggy.NewSubcommand("ssh")
	setupCmd.Description = "SSH into a E2E Hybrid Node running in the peered VPC through the jumpbox"
	setupCmd.AddPositionalValue(&cmd.instanceID, "INSTANCE_ID", 1, true, "The instance ID of the node to SSH into")

	cmd.flaggy = setupCmd

	return &cmd
}

func (c *command) Flaggy() *flaggy.Subcommand {
	return c.flaggy
}

func (s *command) Run(log *zap.Logger, opts *cli.GlobalOptions) error {
	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("reading AWS configuration: %w", err)
	}

	ec2Client := ec2.NewFromConfig(cfg)

	instances, err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{s.instanceID},
	})
	if err != nil {
		return fmt.Errorf("describing instance %s: %w", s.instanceID, err)
	}

	if len(instances.Reservations) == 0 || len(instances.Reservations[0].Instances) == 0 {
		return fmt.Errorf("no instance found with ID %s", s.instanceID)
	}

	targetInstance := instances.Reservations[0].Instances[0]

	var clusterName string
	for _, tag := range targetInstance.Tags {
		if *tag.Key == constants.TestClusterTagKey {
			clusterName = *tag.Value
			break
		}
	}

	if clusterName == "" {
		return fmt.Errorf("no cluster name found in instance %s tags", s.instanceID)
	}

	jumpbox, err := peered.JumpboxInstance(ctx, ec2Client, clusterName)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx,
		"aws",
		"ssm",
		"start-session",
		"--document",
		"AWS-StartInteractiveCommand",
		"--parameters",
		fmt.Sprintf("{\"command\":[\"sudo ssh %s\"]}", *targetInstance.PrivateIpAddress),
		"--target",
		*jumpbox.InstanceId,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running ssm start-session command: %w", err)
	}

	return nil
}

package ssh

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/integrii/flaggy"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/peered"
)

type Command struct {
	flaggy           *flaggy.Subcommand
	instanceIDOrName string
}

func NewCommand() *Command {
	cmd := Command{}

	setupCmd := flaggy.NewSubcommand("ssh")
	setupCmd.Description = "SSH into a E2E Hybrid Node running in the peered VPC through the jumpbox"
	setupCmd.AddPositionalValue(&cmd.instanceIDOrName, "INSTANCE_ID_OR_NAME", 1, true, "The instance ID or name of the node to SSH into")

	cmd.flaggy = setupCmd

	return &cmd
}

func (c *Command) Flaggy() *flaggy.Subcommand {
	return c.flaggy
}

func (c *Command) Commands() []cli.Command {
	return []cli.Command{c}
}

func (s *Command) Run(log *zap.Logger, opts *cli.GlobalOptions) error {
	ctx := context.Background()

	cfg, err := e2e.NewAWSConfig(ctx,
		config.WithRetryer(func() aws.Retryer {
			return retry.AddWithMaxBackoffDelay(
				retry.AddWithMaxAttempts(
					retry.NewStandard(),
					10, // Max 10 attempts
				),
				10*time.Second, // Max backoff delay
			)
		}),
	)
	if err != nil {
		return fmt.Errorf("reading AWS configuration: %w", err)
	}

	ec2Client := ec2.NewFromConfig(cfg)

	input := &ec2.DescribeInstancesInput{}
	if strings.HasPrefix(s.instanceIDOrName, "i-") {
		input.InstanceIds = []string{s.instanceIDOrName}
	} else {
		input.Filters = []types.Filter{
			{
				Name:   aws.String("tag:Name"),
				Values: []string{s.instanceIDOrName},
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: []string{"running"},
			},
		}
	}
	instances, err := ec2Client.DescribeInstances(ctx, input)
	if err != nil {
		return fmt.Errorf("describing instance %s: %w", s.instanceIDOrName, err)
	}

	if len(instances.Reservations) == 0 || len(instances.Reservations[0].Instances) == 0 {
		return fmt.Errorf("no instance found with ID or Name %s", s.instanceIDOrName)
	}

	targetInstance := instances.Reservations[0].Instances[0]

	isBottleRocket, err := isBottleRocket(ctx, ec2Client, *targetInstance.ImageId)
	if err != nil {
		return fmt.Errorf("validating if instance OS is BottleRoceket: %w", err)
	}
	var sshCommandFormat string
	if isBottleRocket {
		sshCommandFormat = "{\"command\":[\"sudo ssh ec2-user@%s\"]}"
	} else {
		sshCommandFormat = "{\"command\":[\"sudo ssh %s\"]}"
	}

	sshCommand := fmt.Sprintf(sshCommandFormat, *targetInstance.PrivateIpAddress)

	var clusterName string
	for _, tag := range targetInstance.Tags {
		if *tag.Key == constants.TestClusterTagKey {
			clusterName = *tag.Value
			break
		}
	}

	if clusterName == "" {
		return fmt.Errorf("no cluster name found in instance %s tags", s.instanceIDOrName)
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
		sshCommand,
		"--target",
		*jumpbox.InstanceId,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	signalCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func(sig chan os.Signal, cmd *exec.Cmd) {
		defer signal.Stop(sig)
		for {
			select {
			case triggeredSignal := <-sig:
				if err := cmd.Process.Signal(triggeredSignal); err != nil {
					log.Error(fmt.Sprintf("failed to signal ssm start-session command: %s", err))
				}
			case <-signalCtx.Done():
				return
			}
		}
	}(sig, cmd)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running ssm start-session command: %w", err)
	}

	return nil
}

func isBottleRocket(ctx context.Context, ec2Client *ec2.Client, imageId string) (bool, error) {
	images, err := ec2Client.DescribeImages(ctx, &ec2.DescribeImagesInput{
		ImageIds: []string{imageId},
	})
	if err != nil {
		return false, fmt.Errorf("describing image %s: %w", imageId, err)
	}

	if len(images.Images) == 0 {
		return false, fmt.Errorf("no image found with ID %s", imageId)
	}

	imageName := images.Images[0].Name
	if imageName == nil {
		return false, fmt.Errorf("image %s has no name", imageId)
	}

	return strings.Contains(*imageName, "bottlerocket"), nil
}

package setup

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/integrii/flaggy"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/cluster"
)

type Command struct {
	flaggy         *flaggy.Subcommand
	configFilePath string
}

func NewCommand() *Command {
	cmd := Command{}

	setupCmd := flaggy.NewSubcommand("setup")
	setupCmd.Description = "Create the E2E test infrastructure"
	setupCmd.AdditionalHelpPrepend = "This command will run the setup infrastructure for running E2E tests"

	setupCmd.String(&cmd.configFilePath, "s", "setup-config-path", "Path to setup config file")

	cmd.flaggy = setupCmd

	return &cmd
}

func NewCommandWithParams(configFilePath string) *Command {
	cmd := Command{
		configFilePath: configFilePath,
	}
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

	testResources, err := cluster.LoadTestResources(s.configFilePath)
	if err != nil {
		return fmt.Errorf("failed to load test resources: %w", err)
	}

	aws, err := e2e.NewAWSConfig(ctx,
		config.WithRegion(testResources.ClusterRegion),
		// We use a custom AppId so the requests show that they were
		// made by this command in the user-agent
		config.WithAppID("nodeadm-e2e-test-setup-cmd"),
	)
	if err != nil {
		return fmt.Errorf("reading AWS configuration: %w", err)
	}

	logger := e2e.NewLogger()
	create := cluster.NewCreate(aws, logger, testResources.EKS.Endpoint)

	logger.Info("Creating cluster infrastructure for E2E tests...")
	if err := create.Run(ctx, testResources); err != nil {
		return fmt.Errorf("creating E2E test infrastructure: %w", err)
	}

	fmt.Println("E2E test infrastructure setup completed successfully!")
	return nil
}

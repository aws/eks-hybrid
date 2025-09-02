package cleanup

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/integrii/flaggy"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"

	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/cluster"
)

type Command struct {
	flaggy            *flaggy.Subcommand
	resourcesFilePath string
}

func NewCommand() *Command {
	cmd := Command{}

	cleanup := flaggy.NewSubcommand("cleanup")
	cleanup.Description = "Delete the E2E test infrastructure"
	cleanup.AdditionalHelpPrepend = "This command will cleanup E2E test infrastructure."

	cleanup.String(&cmd.resourcesFilePath, "f", "filename", "Path to resources file")

	cmd.flaggy = cleanup

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
	logger := e2e.NewLogger()

	file, err := os.ReadFile(s.resourcesFilePath)
	if err != nil {
		return fmt.Errorf("failed to open configuration file: %w", err)
	}

	deleteCluster := cluster.DeleteInput{}
	if err = yaml.Unmarshal(file, &deleteCluster); err != nil {
		return fmt.Errorf("unmarshaling cleanup config: %w", err)
	}

	aws, err := e2e.NewAWSConfig(ctx,
		config.WithRegion(deleteCluster.ClusterRegion),
		// We use a custom AppId so the requests show that they were
		// made by this cleanup in the user-agent
		config.WithAppID("nodeadm-e2e-test-cleanup-cmd"),
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

	delete := cluster.NewDelete(aws, logger, deleteCluster.Endpoint)

	logger.Info("Cleaning up E2E cluster resources...")
	if err = delete.Run(ctx, deleteCluster); err != nil {
		return fmt.Errorf("error cleaning up e2e resources: %w", err)
	}

	logger.Info("Cleanup completed successfully!")
	return nil
}

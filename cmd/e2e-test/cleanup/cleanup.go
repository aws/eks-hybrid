package cleanup

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/integrii/flaggy"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"

	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/cleanup"
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

	aws, err := config.LoadDefaultConfig(ctx, config.WithRegion(deleteCluster.ClusterRegion))
	if err != nil {
		return fmt.Errorf("reading AWS configuration: %w", err)
	}

	delete := cluster.NewDelete(aws, logger, deleteCluster.Endpoint)

	// run the standard cluster cleanup as well as the sweeper fallback
	logger.Info("Cleaning up E2E cluster resources...")

	if deleteErr := delete.Run(ctx, deleteCluster); deleteErr != nil {
		err = fmt.Errorf("running cluster cleanup: %w", deleteErr)
		logger.Error(deleteErr, "running cluster cleanup")
	}

	logger.Info("Cleaning up E2E cluster resources with Sweeper...")
	sweeper := cleanup.NewSweeper(aws, logger)
	if sweeperErr := sweeper.Run(ctx, cleanup.SweeperInput{
		ClusterName: deleteCluster.ClusterName,
	}); sweeperErr != nil {
		err = errors.Join(err, fmt.Errorf("running sweeper cleanup: %w", sweeperErr))
	}

	if err != nil {
		return err
	}

	logger.Info("Cleanup completed successfully!")
	return nil
}

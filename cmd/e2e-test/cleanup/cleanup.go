package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/integrii/flaggy"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/cluster"
)

type command struct {
	flaggy            *flaggy.Subcommand
	resourcesFilePath string
}

func NewCommand() cli.Command {
	cmd := command{}

	cleanup := flaggy.NewSubcommand("cleanup")
	cleanup.Description = "Delete the E2E test infrastructure"
	cleanup.AdditionalHelpPrepend = "This command will cleanup E2E test infrastructure."

	cleanup.String(&cmd.resourcesFilePath, "f", "filename", "Path to resources file")

	cmd.flaggy = cleanup

	return &cmd
}

func (c *command) Flaggy() *flaggy.Subcommand {
	return c.flaggy
}

func (s *command) Run(log *zap.Logger, opts *cli.GlobalOptions) error {
	ctx := context.Background()
	logger := e2e.NewLogger()

	logger.Info("Cleaning up E2E resources...")

	file, err := os.ReadFile(s.resourcesFilePath)
	if err != nil {
		return fmt.Errorf("failed to open configuration file: %v", err)
	}

	deleteCluster := cluster.DeleteInput{}
	if err = yaml.Unmarshal(file, &deleteCluster); err != nil {
		return fmt.Errorf("failed to unmarshal configuration from YAML: %v", err)
	}

	aws, err := config.LoadDefaultConfig(ctx, config.WithRegion(deleteCluster.ClusterRegion))
	if err != nil {
		return fmt.Errorf("reading AWS configuration: %w", err)
	}

	delete := cluster.NewDelete(aws, logger)

	err = delete.Run(ctx, deleteCluster)
	if err != nil {
		return fmt.Errorf("error cleaning up e2e resources: %v", err)
	}
	return nil
}

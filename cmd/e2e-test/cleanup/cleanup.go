package cleanup

import (
	"context"
	"fmt"
	"os"
	"time"

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
	clusterPrefix     string
	ageThreshold      string
	dryRun            bool
	allOld            bool
}

func NewCommand() *Command {
	cmd := Command{}

	cleanup := flaggy.NewSubcommand("cleanup")
	cleanup.Description = "Delete the E2E test infrastructure"
	cleanup.AdditionalHelpPrepend = "This command will cleanup E2E test infrastructure."

	cleanup.String(&cmd.resourcesFilePath, "f", "filename", "Path to resources file")
	cleanup.String(&cmd.clusterPrefix, "p", "cluster-prefix", "Cluster name prefix to cleanup (will append * for search)")
	cleanup.String(&cmd.ageThreshold, "a", "age-threshold", "Age threshold for instance deletion (e.g. 24h, default: 24h)")
	cleanup.Bool(&cmd.dryRun, "dry-run", "dry-run", "Simulate the cleanup without making any changes")
	cleanup.Bool(&cmd.allOld, "all-old", "all-old", "Include all old resources in the cleanup")

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

	var deleteCluster cluster.DeleteInput

	if s.clusterPrefix != "" {
		deleteCluster = cluster.DeleteInput{
			ClusterNamePrefix: s.clusterPrefix,
		}
	} else if s.allOld {
		deleteCluster = cluster.DeleteInput{
			AllClusters: true,
		}
	} else {
		if s.resourcesFilePath == "" {
			return fmt.Errorf("either --filename or --cluster-prefix or --all-old must be specified")
		}
		file, err := os.ReadFile(s.resourcesFilePath)
		if err != nil {
			return fmt.Errorf("failed to open configuration file: %w", err)
		}

		if err = yaml.Unmarshal(file, &deleteCluster); err != nil {
			return fmt.Errorf("unmarshaling cleanup config: %w", err)
		}
	}

	deleteCluster.InstanceAgeThreshold = 24 * time.Hour
	if s.ageThreshold != "" {
		parsed, err := time.ParseDuration(s.ageThreshold)
		if err != nil {
			return fmt.Errorf("parsing age threshold duration: %w", err)
		}
		deleteCluster.InstanceAgeThreshold = parsed
	}

	deleteCluster.DryRun = s.dryRun

	if deleteCluster.ClusterRegion == "" {
		aws, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			return fmt.Errorf("reading AWS configuration: %w", err)
		}
		deleteCluster.ClusterRegion = aws.Region
	}

	aws, err := config.LoadDefaultConfig(ctx, config.WithRegion(deleteCluster.ClusterRegion))
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

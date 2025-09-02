package sweeper

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/integrii/flaggy"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/cleanup"
)

type SweeperCommand struct {
	flaggy        *flaggy.Subcommand
	clusterPrefix string
	clusterName   string
	ageThreshold  time.Duration
	dryRun        bool
	all           bool
	eksEndpoint   string
}

func NewSweeperCommand() *SweeperCommand {
	cmd := SweeperCommand{
		ageThreshold: 24 * time.Hour,
	}

	sweeper := flaggy.NewSubcommand("sweeper")
	sweeper.Description = "Sweep and delete E2E test infrastructure based on criteria"
	sweeper.AdditionalHelpPrepend = "This command will sweep and cleanup E2E test infrastructure based on specified criteria."

	sweeper.String(&cmd.clusterPrefix, "p", "cluster-prefix", "Cluster name prefix to cleanup (will append * for search)")
	sweeper.String(&cmd.clusterName, "c", "cluster-name", "Specific cluster name to cleanup")
	sweeper.Duration(&cmd.ageThreshold, "", "age", "Age threshold for instance deletion")
	sweeper.Bool(&cmd.dryRun, "", "dry-run", "Simulate the cleanup without making any changes")
	sweeper.Bool(&cmd.all, "", "all", "Include all resources based on the age threshold in the cleanup")
	sweeper.String(&cmd.eksEndpoint, "e", "eks-endpoint", "EKS API endpoint")

	cmd.flaggy = sweeper

	return &cmd
}

func (c *SweeperCommand) Flaggy() *flaggy.Subcommand {
	return c.flaggy
}

func (c *SweeperCommand) Commands() []cli.Command {
	return []cli.Command{c}
}

func (s *SweeperCommand) Run(log *zap.Logger, opts *cli.GlobalOptions) error {
	ctx := context.Background()
	logger := e2e.NewLogger()

	if s.clusterPrefix != "" && s.clusterName != "" {
		return fmt.Errorf("cannot use --cluster-prefix and --cluster-name together")
	}

	if s.clusterPrefix != "" && s.all {
		return fmt.Errorf("cannot use --cluster-prefix and --all together")
	}

	if s.clusterName != "" && s.all {
		return fmt.Errorf("cannot use --cluster-name and --all together")
	}

	if s.clusterPrefix == "" && s.clusterName == "" && !s.all {
		return fmt.Errorf("either --cluster-prefix, --cluster-name, or --all must be specified")
	}
	aws, err := e2e.NewAWSConfig(ctx,
		// We use a custom AppId so the requests show that they were
		// made by this command in the user-agent
		config.WithAppID("nodeadm-e2e-test-sweeper-cmd"),
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

	sweeper := cleanup.NewSweeper(aws, logger, s.eksEndpoint)
	input := cleanup.SweeperInput{
		AllClusters:          s.all,
		ClusterNamePrefix:    s.clusterPrefix,
		ClusterName:          s.clusterName,
		InstanceAgeThreshold: s.ageThreshold,
		DryRun:               s.dryRun,
	}
	logger.Info("Cleaning up E2E cluster resources...", "configuration", input)
	if err = sweeper.Run(ctx, input); err != nil {
		return fmt.Errorf("error cleaning up e2e resources: %w", err)
	}
	logger.Info("Cleanup completed successfully!")
	return nil
}

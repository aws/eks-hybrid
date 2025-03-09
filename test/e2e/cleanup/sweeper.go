package cleanup

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/rolesanywhere"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/go-logr/logr"
)

const deleteClusterTimeout = 5 * time.Minute

type SweeperInput struct {
	AllClusters          bool          `yaml:"allClusters"`
	DryRun               bool          `yaml:"dryRun"`
	ClusterName          string        `yaml:"clusterName"`
	ClusterNamePrefix    string        `yaml:"clusterNamePrefix"`
	InstanceAgeThreshold time.Duration `yaml:"instanceAgeThreshold"`
}

type Sweeper struct {
	cfn           *cloudformation.Client
	ec2Client     *ec2.Client
	eks           *eks.Client
	iam           *iam.Client
	logger        logr.Logger
	s3Client      *s3.Client
	ssm           *ssm.Client
	rolesAnywhere *rolesanywhere.Client
}

type FilterInput struct {
	ClusterName          string
	ClusterNamePrefix    string
	AllClusters          bool
	InstanceAgeThreshold time.Duration
	DryRun               bool
}

func NewSweeper(aws aws.Config, logger logr.Logger) Sweeper {
	return Sweeper{
		cfn:           cloudformation.NewFromConfig(aws),
		ec2Client:     ec2.NewFromConfig(aws),
		eks:           eks.NewFromConfig(aws),
		iam:           iam.NewFromConfig(aws),
		logger:        logger,
		ssm:           ssm.NewFromConfig(aws),
		s3Client:      s3.NewFromConfig(aws),
		rolesAnywhere: rolesanywhere.NewFromConfig(aws),
	}
}

func (c *Sweeper) Run(ctx context.Context, input SweeperInput) error {
	filterInput := FilterInput{
		ClusterName:          input.ClusterName,
		ClusterNamePrefix:    input.ClusterNamePrefix,
		AllClusters:          input.AllClusters,
		InstanceAgeThreshold: input.InstanceAgeThreshold,
		DryRun:               input.DryRun,
	}

	if err := c.cleanupSSMManagedInstances(ctx, filterInput); err != nil {
		return fmt.Errorf("cleaning up SSM managed instances: %w", err)
	}

	if err := c.cleanupSSMHybridActivations(ctx, filterInput); err != nil {
		return fmt.Errorf("cleaning up SSM hybrid activations: %w", err)
	}

	return nil
}

func (c *Sweeper) cleanupSSMManagedInstances(ctx context.Context, filterInput FilterInput) error {
	cleaner := NewSSMCleaner(c.ssm)
	instanceIds, err := cleaner.ListManagedInstances(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing managed instances: %w", err)
	}

	c.logger.Info("Deleting managed instances", "instanceIds", instanceIds)
	if filterInput.DryRun {
		c.logger.Info("Dry run, skipping managed instance deletion")
		return nil
	}

	if err := cleaner.DeleteManagedInstances(ctx, instanceIds, c.logger); err != nil {
		return fmt.Errorf("deleting managed instances: %w", err)
	}

	return nil
}

func (c *Sweeper) cleanupSSMHybridActivations(ctx context.Context, filterInput FilterInput) error {
	cleaner := NewSSMCleaner(c.ssm)
	activationIDs, err := cleaner.ListActivations(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing activations: %w", err)
	}

	c.logger.Info("Deleting activations", "activationIDs", activationIDs)
	if filterInput.DryRun {
		c.logger.Info("Dry run, skipping activation deletion")
		return nil
	}

	if err := cleaner.DeleteActivations(ctx, activationIDs, c.logger); err != nil {
		return fmt.Errorf("deleting activations: %w", err)
	}

	return nil
}

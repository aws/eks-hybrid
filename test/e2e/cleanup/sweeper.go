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

	if filterInput.DryRun {
		c.logger.Info("Dry run enabled, skipping deletions")
	}

	// Deletion ordering
	// - instances
	// - instance profiles since these are created after the test cfn
	//   - remove roles from instance profile first
	// - test credential cfn statck (creates the ssm/iam-ra roles/ec2role)
	// - eks clusters
	// - empty s3 pod identity buckets, infra cfn stack deletes the bucket
	// - infra cfn stack (creates vpcs/eks cluster)
	// - remaining leaking items which should only exist in the case where the cfn deletion is incomplete

	if err := c.emptyS3PodIdentityBuckets(ctx, filterInput); err != nil {
		return fmt.Errorf("emptying S3 pod identity buckets: %w", err)
	}
	if err := c.cleanupS3PodIdentityBuckets(ctx, filterInput); err != nil {
		return fmt.Errorf("cleaning up S3 pod identity buckets: %w", err)
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
	cleaner := NewSSMCleaner(c.ssm, c.logger)
	instanceIds, err := cleaner.ListManagedInstances(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing managed instances: %w", err)
	}

	c.logger.Info("Deleting managed instances", "instanceIds", instanceIds)
	if filterInput.DryRun {
		return nil
	}

	for _, instanceID := range instanceIds {
		if err := cleaner.DeleteManagedInstance(ctx, instanceID); err != nil {
			return fmt.Errorf("deleting managed instance: %w", err)
		}
	}

	return nil
}

func (c *Sweeper) cleanupSSMHybridActivations(ctx context.Context, filterInput FilterInput) error {
	cleaner := NewSSMCleaner(c.ssm, c.logger)
	activationIDs, err := cleaner.ListActivations(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing activations: %w", err)
	}

	c.logger.Info("Deleting activations", "activationIDs", activationIDs)
	if filterInput.DryRun {
		return nil
	}

	for _, activationID := range activationIDs {
		if err := cleaner.DeleteActivation(ctx, activationID); err != nil {
			return fmt.Errorf("deleting activation: %w", err)
		}
	}

	return nil
}

func (c *Sweeper) emptyS3PodIdentityBuckets(ctx context.Context, filterInput FilterInput) error {
	cleaner := NewS3Cleaner(c.s3Client, c.logger)
	bucketNames, err := cleaner.ListBuckets(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing buckets: %w", err)
	}

	c.logger.Info("Emptying S3 Pod Identity Buckets", "bucketNames", bucketNames)
	if filterInput.DryRun {
		return nil
	}

	for _, bucketName := range bucketNames {
		if err := cleaner.EmptyS3Bucket(ctx, bucketName); err != nil {
			return fmt.Errorf("emptying bucket %s: %w", bucketName, err)
		}
	}

	return nil
}

func (c *Sweeper) cleanupS3PodIdentityBuckets(ctx context.Context, filterInput FilterInput) error {
	cleaner := NewS3Cleaner(c.s3Client, c.logger)
	bucketNames, err := cleaner.ListBuckets(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing buckets: %w", err)
	}

	c.logger.Info("Deleting S3 Pod Identity Buckets", "bucketNames", bucketNames)
	if filterInput.DryRun {
		return nil
	}
	for _, bucketName := range bucketNames {
		if err := cleaner.DeleteBucket(ctx, bucketName); err != nil {
			return fmt.Errorf("deleting bucket %s: %w", bucketName, err)
		}
	}

	return nil
}

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

	if err := c.cleanupEC2Instances(ctx, filterInput); err != nil {
		return fmt.Errorf("cleaning up EC2 instances: %w", err)
	}

	if err := c.cleanupIAMInstanceProfiles(ctx, filterInput); err != nil {
		return fmt.Errorf("cleaning up IAM instance profiles: %w", err)
	}

	if err := c.cleanupCredentialStacks(ctx, filterInput); err != nil {
		return fmt.Errorf("cleaning up credential stacks: %w", err)
	}

	if err := c.cleanupEKSClusters(ctx, filterInput); err != nil {
		return fmt.Errorf("cleaning up EKS clusters: %w", err)
	}

	if err := c.emptyS3PodIdentityBuckets(ctx, filterInput); err != nil {
		return fmt.Errorf("emptying S3 pod identity buckets: %w", err)
	}

	if err := c.cleanupArchitectureStacks(ctx, filterInput); err != nil {
		return fmt.Errorf("cleaning up architecture stacks: %w", err)
	}

	// TODO: do we still need ot support skipping these on a region basis
	if err := c.cleanupRolesAnywhereProfiles(ctx, filterInput); err != nil {
		return fmt.Errorf("cleaning up Roles Anywhere resources: %w", err)
	}

	if err := c.cleanupRolesAnywhereTrustAnchors(ctx, filterInput); err != nil {
		return fmt.Errorf("cleaning up Roles Anywhere trust anchors: %w", err)
	}

	if err := c.cleanupIAMRoles(ctx, filterInput); err != nil {
		return fmt.Errorf("cleaning up IAM roles: %w", err)
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

func (c *Sweeper) cleanupCredentialStacks(ctx context.Context, filterInput FilterInput) error {
	cfnCleaner := NewCFNStackCleanup(c.cfn, c.logger)
	credStacks, err := cfnCleaner.ListCredentialStacks(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing credential stacks: %w", err)
	}

	c.logger.Info("Deleting credential stacks", "credentialStacks", credStacks)
	if filterInput.DryRun {
		return nil
	}
	for _, stack := range credStacks {
		if err := cfnCleaner.DeleteStack(ctx, stack); err != nil {
			return fmt.Errorf("deleting credential stack %s: %w", stack, err)
		}
	}

	return nil
}

func (c *Sweeper) cleanupArchitectureStacks(ctx context.Context, filterInput FilterInput) error {
	cfnCleaner := NewCFNStackCleanup(c.cfn, c.logger)
	archStacks, err := cfnCleaner.ListArchitectureStacks(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing architecture stacks: %w", err)
	}

	c.logger.Info("Deleting architecture stacks", "architectureStacks", archStacks)
	if filterInput.DryRun {
		return nil
	}

	for _, stack := range archStacks {
		if err := cfnCleaner.DeleteStack(ctx, stack); err != nil {
			return err
		}
	}
	return nil
}

func (c *Sweeper) cleanupEC2Instances(ctx context.Context, filterInput FilterInput) error {
	ec2Cleaner := NewEC2Cleaner(c.ec2Client, c.logger)
	instanceIDs, err := ec2Cleaner.ListTaggedInstances(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing tagged EC2 instances: %w", err)
	}

	c.logger.Info("Deleting tagged EC2 instances", "instanceIDs", instanceIDs)
	if filterInput.DryRun {
		return nil
	}

	if err := ec2Cleaner.DeleteInstances(ctx, instanceIDs); err != nil {
		return fmt.Errorf("deleting EC2 instances: %w", err)
	}
	return nil
}

func (c *Sweeper) cleanupIAMRoles(ctx context.Context, filterInput FilterInput) error {
	iamCleaner := NewIAMCleaner(c.iam, c.logger)
	roles, err := iamCleaner.ListRoles(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing IAM roles: %w", err)
	}

	c.logger.Info("Deleting IAM roles", "roles", roles)
	if filterInput.DryRun {
		return nil
	}

	for _, role := range roles {
		if err := iamCleaner.DeleteRole(ctx, role); err != nil {
			return fmt.Errorf("deleting IAM role %s: %w", role, err)
		}
	}

	return nil
}

func (c *Sweeper) cleanupIAMInstanceProfiles(ctx context.Context, filterInput FilterInput) error {
	iamCleaner := NewIAMCleaner(c.iam, c.logger)
	instanceProfiles, err := iamCleaner.ListInstanceProfiles(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing IAM instance profiles: %w", err)
	}

	profileRoles := map[string][]string{}
	for _, instanceProfile := range instanceProfiles {
		roles, err := iamCleaner.ListRolesForInstanceProfile(ctx, instanceProfile)
		if err != nil {
			return fmt.Errorf("listing roles for instance profile %s: %w", instanceProfile, err)
		}
		profileRoles[instanceProfile] = roles
	}
	c.logger.Info("Deleting IAM instance profiles", "instanceProfilesToRoles", profileRoles)
	if filterInput.DryRun {
		return nil
	}
	for instanceProfile, roles := range profileRoles {
		if err := iamCleaner.RemoveRolesFromInstanceProfile(ctx, roles, instanceProfile); err != nil {
			return fmt.Errorf("removing roles from instance profile %s: %w", instanceProfile, err)
		}
		if err := iamCleaner.DeleteInstanceProfile(ctx, instanceProfile); err != nil {
			return fmt.Errorf("deleting IAM instance profile %s: %w", instanceProfile, err)
		}
	}

	return nil
}

func (c *Sweeper) cleanupEKSClusters(ctx context.Context, filterInput FilterInput) error {
	eksCleaner := NewEKSClusterCleanup(c.eks, c.logger)
	clusterNames, err := eksCleaner.ListEKSClusters(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing EKS hybrid clusters: %w", err)
	}

	c.logger.Info("Deleting EKS hybrid clusters", "clusterNames", clusterNames)
	if filterInput.DryRun {
		return nil
	}

	for _, clusterName := range clusterNames {
		if err := eksCleaner.DeleteCluster(ctx, clusterName); err != nil {
			return fmt.Errorf("deleting EKS hybrid cluster %s: %w", clusterName, err)
		}
	}
	return nil
}

func (c *Sweeper) cleanupRolesAnywhereProfiles(ctx context.Context, filterInput FilterInput) error {
	rolesAnywhereCleaner := NewRolesAnywhereCleaner(c.rolesAnywhere, c.logger)

	profiles, err := rolesAnywhereCleaner.ListProfiles(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing Roles Anywhere profiles: %w", err)
	}

	c.logger.Info("Deleting Roles Anywhere profiles", "profiles", profiles)
	if filterInput.DryRun {
		return nil
	}

	for _, profile := range profiles {
		if err := rolesAnywhereCleaner.DeleteProfile(ctx, profile); err != nil {
			return fmt.Errorf("deleting Roles Anywhere profile %s: %w", profile, err)
		}
	}

	return nil
}

func (c *Sweeper) cleanupRolesAnywhereTrustAnchors(ctx context.Context, filterInput FilterInput) error {
	rolesAnywhereCleaner := NewRolesAnywhereCleaner(c.rolesAnywhere, c.logger)

	anchors, err := rolesAnywhereCleaner.ListTrustAnchors(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing Roles Anywhere trust anchors: %w", err)
	}

	c.logger.Info("Deleting Roles Anywhere trust anchors", "anchors", anchors)
	if filterInput.DryRun {
		return nil
	}

	for _, anchor := range anchors {
		if err := rolesAnywhereCleaner.DeleteTrustAnchor(ctx, anchor); err != nil {
			return fmt.Errorf("deleting Roles Anywhere trust anchor %s: %w", anchor, err)
		}
	}

	return nil
}

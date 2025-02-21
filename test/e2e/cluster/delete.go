package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	"github.com/aws/aws-sdk-go-v2/service/rolesanywhere"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/cleanup"
	"github.com/aws/eks-hybrid/test/e2e/credentials"
	"github.com/aws/eks-hybrid/test/e2e/errors"
)

const deleteClusterTimeout = 5 * time.Minute

type DeleteInput struct {
	ClusterName          string        `yaml:"clusterName"`
	ClusterNamePrefix    string        `yaml:"clusterNamePrefix"`
	ClusterRegion        string        `yaml:"clusterRegion"`
	Endpoint             string        `yaml:"endpoint"`
	AllClusters          bool          `yaml:"allClusters"`
	InstanceAgeThreshold time.Duration `yaml:"instanceAgeThreshold"`
	DryRun               bool          `yaml:"dryRun"`
}

type Delete struct {
	logger        logr.Logger
	rolesAnywhere *rolesanywhere.Client
	eks           *eks.Client
	ssm           *ssm.Client
	iam           *iam.Client
	stack         *stack
	tagging       *resourcegroupstaggingapi.Client
}

// NewDelete creates a new workflow to delete an EKS cluster. The EKS client will use
// the specified endpoint or the default endpoint if empty string is passed.
func NewDelete(aws aws.Config, logger logr.Logger, endpoint string) Delete {
	return Delete{
		logger: logger,
		eks: eks.NewFromConfig(aws, func(o *eks.Options) {
			o.EndpointResolverV2 = &e2e.EksResolverV2{
				Endpoint: endpoint,
			}
		}),
		ssm:           ssm.NewFromConfig(aws),
		iam:           iam.NewFromConfig(aws),
		rolesAnywhere: rolesanywhere.NewFromConfig(aws),
		tagging:       resourcegroupstaggingapi.NewFromConfig(aws),
		stack: &stack{
			cfn:       cloudformation.NewFromConfig(aws),
			ec2Client: ec2.NewFromConfig(aws),
			logger:    logger,
		},
	}
}

func (c *Delete) Run(ctx context.Context, deleteInput DeleteInput) error {
	filterInput := cleanup.FilterInput{
		ClusterName:          deleteInput.ClusterName,
		ClusterNamePrefix:    deleteInput.ClusterNamePrefix,
		AllClusters:          deleteInput.AllClusters,
		InstanceAgeThreshold: deleteInput.InstanceAgeThreshold,
		DryRun:               deleteInput.DryRun,
	}

	if err := c.cleanupEC2Instances(ctx, filterInput); err != nil {
		return fmt.Errorf("cleaning up EC2 instances: %w", err)
	}

	if err := c.cleanupIAMRoles(ctx, filterInput); err != nil {
		return fmt.Errorf("cleaning up IAM roles: %w", err)
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

	if err := c.cleanupSSMParameters(ctx, filterInput); err != nil {
		return fmt.Errorf("cleaning up SSM parameters: %w", err)
	}

	if err := c.cleanupSSMManagedInstances(ctx, filterInput); err != nil {
		return fmt.Errorf("cleaning up SSM managed instances: %w", err)
	}

	if err := c.cleanupSSMHybridActivations(ctx, filterInput); err != nil {
		return fmt.Errorf("cleaning up SSM hybrid activations: %w", err)
	}

	return nil
}

func (c *Delete) cleanupEC2Instances(ctx context.Context, filterInput cleanup.FilterInput) error {
	ec2Cleaner := &cleanup.EC2Cleaner{
		EC2:             c.stack.ec2Client,
		ResourceTagging: c.tagging,
		Logger:          c.logger,
	}
	instanceIDs, err := ec2Cleaner.ListTaggedInstances(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing tagged EC2 instances: %w", err)
	}

	c.logger.Info("Deleting tagged EC2 instances", "instanceIDs", instanceIDs)
	if filterInput.DryRun {
		c.logger.Info("Dry run, skipping instance termination")
		return nil
	}

	if err := ec2Cleaner.DeleteInstances(ctx, instanceIDs); err != nil {
		return fmt.Errorf("deleting EC2 instances: %w", err)
	}
	return nil
}

func (c *Delete) cleanupCredentialStacks(ctx context.Context, filterInput cleanup.FilterInput) error {
	cfnCleaner := &cleanup.CFNStackCleanup{
		CFN:             c.stack.cfn,
		ResourceTagging: c.tagging,
		Logger:          c.logger,
	}
	credStacks, err := cfnCleaner.ListCredentialStacks(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing credential stacks: %w", err)
	}

	c.logger.Info("Deleting credential stacks", "credentialStacks", credStacks)
	if filterInput.DryRun {
		c.logger.Info("Dry run, skipping credential stack deletion")
		return nil
	}

	for _, stack := range credStacks {
		if err := c.deleteCredentialStack(ctx, stack); err != nil {
			return err
		}
	}
	return nil
}

func (c *Delete) cleanupEKSClusters(ctx context.Context, filterInput cleanup.FilterInput) error {
	eksCleaner := &cleanup.EKSClusterCleanup{
		EKS:             c.eks,
		ResourceTagging: c.tagging,
		Logger:          c.logger,
	}
	clusterNames, err := eksCleaner.ListEKSClusters(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing EKS hybrid clusters: %w", err)
	}

	c.logger.Info("Deleting EKS hybrid clusters", "clusterNames", clusterNames)
	if filterInput.DryRun {
		c.logger.Info("Dry run, skipping cluster deletion")
		return nil
	}

	for _, clusterName := range clusterNames {
		if err := c.deleteCluster(ctx, DeleteInput{ClusterName: clusterName}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Delete) cleanupArchitectureStacks(ctx context.Context, filterInput cleanup.FilterInput) error {
	cfnCleaner := &cleanup.CFNStackCleanup{
		CFN:             c.stack.cfn,
		ResourceTagging: c.tagging,
		Logger:          c.logger,
	}
	archStacks, err := cfnCleaner.ListArchitectureStacks(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing architecture stacks: %w", err)
	}

	c.logger.Info("Deleting architecture stacks", "architectureStacks", archStacks)
	if filterInput.DryRun {
		c.logger.Info("Dry run, skipping architecture stack deletion")
		return nil
	}

	for _, stack := range archStacks {
		if err := c.stack.delete(ctx, stack.ClusterName); err != nil {
			return err
		}
	}
	return nil
}

func (c *Delete) cleanupSSMManagedInstances(ctx context.Context, filterInput cleanup.FilterInput) error {
	cleaner := cleanup.NewSSMCleaner(c.ssm, c.tagging)
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

func (c *Delete) cleanupSSMHybridActivations(ctx context.Context, filterInput cleanup.FilterInput) error {
	cleaner := cleanup.NewSSMCleaner(c.ssm, c.tagging)
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

func (c *Delete) deleteCredentialStack(ctx context.Context, cfnStack cleanup.CFNStack) error {
	c.logger.Info("Deleting Credential Stack", "stack", cfnStack.StackName)

	stack := &credentials.Stack{
		Name:        cfnStack.StackName,
		ClusterName: cfnStack.ClusterName,
		CFN:         c.stack.cfn,
		EKS:         c.eks,
	}
	stackOutput, err := stack.ReadStackOutput(ctx, c.logger)
	if err != nil {
		return fmt.Errorf("reading stack output: %w", err)
	}

	if err := stack.Delete(ctx, c.logger, stackOutput); err != nil {
		return fmt.Errorf("deleting stack %s: %w", cfnStack.StackName, err)
	}
	return nil
}

func (c *Delete) deleteCluster(ctx context.Context, cluster DeleteInput) error {
	c.logger.Info("Deleting EKS hybrid cluster", "cluster", cluster.ClusterName)

	_, err := c.eks.DeleteCluster(ctx, &eks.DeleteClusterInput{
		Name: aws.String(cluster.ClusterName),
	})
	if err != nil && errors.IsType(err, &types.ResourceNotFoundException{}) {
		c.logger.Info("Cluster already deleted", "cluster", cluster.ClusterName)
		return nil
	}

	if err != nil {
		return fmt.Errorf("deleting EKS hybrid cluster %s: %w", cluster.ClusterName, err)
	}

	c.logger.Info("Waiting for cluster deletion", "cluster", cluster.ClusterName)
	if err := waitForClusterDeletion(ctx, c.eks, cluster.ClusterName); err != nil {
		return fmt.Errorf("waiting for cluster %s deletion: %w", cluster.ClusterName, err)
	}

	return nil
}

// waitForClusterDeletion waits for the cluster to be deleted.
func waitForClusterDeletion(ctx context.Context, client *eks.Client, clusterName string) error {
	// Create a context that automatically cancels after the specified timeout
	ctx, cancel := context.WithTimeout(ctx, deleteClusterTimeout)
	defer cancel()

	return waitForCluster(ctx, client, clusterName, func(output *eks.DescribeClusterOutput, err error) (bool, error) {
		if err != nil {
			if errors.IsType(err, &types.ResourceNotFoundException{}) {
				return true, nil
			}

			return false, fmt.Errorf("describing cluster %s: %w", clusterName, err)
		}

		return false, nil
	})
}

func (c *Delete) cleanupIAMRoles(ctx context.Context, filterInput cleanup.FilterInput) error {
	iamCleaner := &cleanup.IAMCleaner{
		IAM:    c.iam,
		Logger: c.logger,
	}
	roles, err := iamCleaner.ListRoles(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing IAM roles: %w", err)
	}

	c.logger.Info("Deleting IAM roles", "roles", roles)
	if filterInput.DryRun {
		c.logger.Info("Dry run, skipping IAM role deletion")
		return nil
	}

	for _, role := range roles {
		if err := iamCleaner.DeleteRole(ctx, role); err != nil {
			return fmt.Errorf("deleting IAM role %s: %w", role, err)
		}
	}

	return nil
}

func (c *Delete) cleanupIAMInstanceProfiles(ctx context.Context, filterInput cleanup.FilterInput) error {
	iamCleaner := &cleanup.IAMCleaner{
		IAM:    c.iam,
		Logger: c.logger,
	}
	instanceProfiles, err := iamCleaner.ListInstanceProfiles(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing IAM instance profiles: %w", err)
	}

	c.logger.Info("Deleting IAM instance profiles", "instanceProfiles", instanceProfiles)
	if filterInput.DryRun {
		c.logger.Info("Dry run, skipping IAM instance profile deletion")
		return nil
	}

	for _, instanceProfile := range instanceProfiles {
		if err := iamCleaner.DeleteInstanceProfile(ctx, instanceProfile); err != nil {
			return fmt.Errorf("deleting IAM instance profile %s: %w", instanceProfile, err)
		}
	}

	return nil
}

func (c *Delete) cleanupRolesAnywhereProfiles(ctx context.Context, filterInput cleanup.FilterInput) error {
	rolesAnywhereCleaner := &cleanup.RolesAnywhereCleaner{
		RolesAnywhere: c.rolesAnywhere,
		Logger:        c.logger,
	}

	profiles, err := rolesAnywhereCleaner.ListProfiles(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing Roles Anywhere profiles: %w", err)
	}

	c.logger.Info("Deleting Roles Anywhere profiles", "profiles", profiles)
	if filterInput.DryRun {
		c.logger.Info("Dry run, skipping Roles Anywhere profile deletion")
		return nil
	}

	for _, profile := range profiles {
		if err := rolesAnywhereCleaner.DeleteProfile(ctx, profile); err != nil {
			return fmt.Errorf("deleting Roles Anywhere profile %s: %w", profile, err)
		}
	}

	return nil
}

func (c *Delete) cleanupRolesAnywhereTrustAnchors(ctx context.Context, filterInput cleanup.FilterInput) error {
	rolesAnywhereCleaner := &cleanup.RolesAnywhereCleaner{
		RolesAnywhere: c.rolesAnywhere,
		Logger:        c.logger,
	}

	anchors, err := rolesAnywhereCleaner.ListTrustAnchors(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing Roles Anywhere trust anchors: %w", err)
	}

	c.logger.Info("Deleting Roles Anywhere trust anchors", "anchors", anchors)
	if filterInput.DryRun {
		c.logger.Info("Dry run, skipping Roles Anywhere trust anchor deletion")
		return nil
	}

	for _, anchor := range anchors {
		if err := rolesAnywhereCleaner.DeleteTrustAnchor(ctx, anchor); err != nil {
			return fmt.Errorf("deleting Roles Anywhere trust anchor %s: %w", anchor, err)
		}
	}

	return nil
}

func (c *Delete) cleanupSSMParameters(ctx context.Context, filterInput cleanup.FilterInput) error {
	cleaner := cleanup.NewSSMCleaner(c.ssm, c.tagging)

	parameterNames, err := cleaner.ListParameters(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing SSM parameters: %w", err)
	}

	c.logger.Info("Deleting SSM parameters", "parameterNames", parameterNames)
	if filterInput.DryRun {
		c.logger.Info("Dry run, skipping SSM parameter deletion")
		return nil
	}

	if err := cleaner.DeleteParameters(ctx, parameterNames, c.logger); err != nil {
		return fmt.Errorf("deleting SSM parameters: %w", err)
	}

	return nil
}

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
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
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
	logger  logr.Logger
	eks     *eks.Client
	ssm     *ssm.Client
	stack   *stack
	tagging *resourcegroupstaggingapi.Client
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
		ssm:     ssm.NewFromConfig(aws),
		tagging: resourcegroupstaggingapi.NewFromConfig(aws),
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

	// Clean up EC2 instances first
	ec2Cleaner := &cleanup.EC2Cleaner{
		EC2:             c.stack.ec2Client,
		ResourceTagging: c.tagging,
		Logger:          c.logger,
	}
	if err := ec2Cleaner.DeleteTaggedInstances(ctx, filterInput); err != nil {
		return fmt.Errorf("deleting EC2 instances: %w", err)
	}

	// Clean up CloudFormation Credentials stacks
	c.logger.Info("Deleting credential stacks")
	cfnCleaner := &cleanup.CFNStackCleanup{
		CFN:             c.stack.cfn,
		ResourceTagging: c.tagging,
		Logger:          c.logger,
	}
	credStacks, err := cfnCleaner.ListCredentialStacks(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing credential stacks: %w", err)
	}

	for _, stack := range credStacks {
		if err := c.deleteCredentialStack(ctx, stack, filterInput.DryRun); err != nil {
			return err
		}
	}
	eksCleaner := &cleanup.EKSClusterCleanup{
		EKS:             c.eks,
		ResourceTagging: c.tagging,
		Logger:          c.logger,
	}
	clusterNames, err := eksCleaner.ListEKSClusters(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing EKS hybrid clusters: %w", err)
	}
	for _, clusterName := range clusterNames {
		if err := c.deleteCluster(ctx, DeleteInput{ClusterName: clusterName}, filterInput.DryRun); err != nil {
			return err
		}
	}

	archStacks, err := cfnCleaner.ListArchitectureStacks(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing architecture stacks: %w", err)
	}

	for _, stack := range archStacks {
		if err := c.stack.delete(ctx, stack.ClusterName, filterInput.DryRun); err != nil {
			return err
		}
	}

	// Clean up SSM resources
	cleaner := &cleanup.SSMCleaner{
		SSM:    c.ssm,
		Filter: filterInput,
	}

	if err := cleaner.DeleteManagedInstances(ctx, c.logger); err != nil {
		return fmt.Errorf("deleting managed instances: %w", err)
	}

	if err := cleaner.DeleteActivations(ctx, c.logger); err != nil {
		return fmt.Errorf("cleaning up SSM activations: %w", err)
	}

	return nil
}

func (c *Delete) deleteCredentialStack(ctx context.Context, cfnStack cleanup.CFNStack, dryRun bool) error {
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
	c.logger.Info("Deleting Stack", "stack", cfnStack.StackName)
	if dryRun {
		c.logger.Info("Dry run, skipping stack deletion")
		return nil
	}
	if err := stack.Delete(ctx, c.logger, stackOutput); err != nil {
		return fmt.Errorf("deleting stack %s: %w", cfnStack.StackName, err)
	}
	return nil
}

func (c *Delete) deleteCluster(ctx context.Context, cluster DeleteInput, dryRun bool) error {
	c.logger.Info("Deleting EKS hybrid cluster", "cluster", cluster.ClusterName)
	if dryRun {
		c.logger.Info("Dry run, skipping cluster deletion")
		return nil
	}
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

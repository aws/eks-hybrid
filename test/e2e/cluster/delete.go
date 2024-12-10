package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/eks-hybrid/test/e2e/errors"
	"github.com/go-logr/logr"
)

const deleteClusterTimeout = 5 * time.Minute

type DeleteInput struct {
	ClusterName   string `yaml:"clusterName"`
	ClusterRegion string `yaml:"clusterRegion"`
}

type Delete struct {
	logger logr.Logger
	eks    *eks.Client
	stack  *stack
}

func NewDelete(aws aws.Config, logger logr.Logger) Delete {
	return Delete{
		eks: eks.NewFromConfig(aws),
		stack: &stack{
			cfn:    cloudformation.NewFromConfig(aws),
			logger: logger,
		},
	}
}

func (c *Delete) Run(ctx context.Context, cluster DeleteInput) error {
	c.logger.Info("Cleaning up E2E cluster resources...")

	c.logger.Info("Deleting EKS hybrid cluster", "cluster", cluster.ClusterName)
	if err := c.deleteCluster(ctx, cluster); err != nil {
		return fmt.Errorf("deleting EKS hybrid cluster: %w", err)
	}

	c.logger.Info("Deleting E2E setup stack...")
	if err := c.stack.delete(ctx, cluster.ClusterName); err != nil {
		return fmt.Errorf("deleting E2E setup stack: %w", err)
	}

	c.logger.Info("Cleanup completed successfully!")
	return nil
}

func (c *Delete) deleteCluster(ctx context.Context, cluster DeleteInput) error {
	_, err := c.eks.DeleteCluster(ctx, &eks.DeleteClusterInput{
		Name: aws.String(cluster.ClusterName),
	})
	if err != nil && errors.IsType(err, &types.ResourceNotFoundException{}) {
		c.logger.Info("Cluster already deleted", "cluster", cluster.ClusterName)
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to delete EKS hybrid cluster %s: %v", cluster.ClusterName, err)
	}

	c.logger.Info("Cluster deletion initiated", "cluster", cluster.ClusterName)

	// Wait for the cluster to be fully deleted to check for any errors during the delete.
	err = waitForClusterDeletion(ctx, c.eks, cluster.ClusterName)
	if err != nil {
		return fmt.Errorf("error waiting for cluster %s deletion: %w", cluster.ClusterName, err)
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

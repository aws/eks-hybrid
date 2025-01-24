package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/errors"
)

const deleteClusterTimeout = 5 * time.Minute

type DeleteInput struct {
	ClusterName   string `yaml:"clusterName"`
	ClusterRegion string `yaml:"clusterRegion"`
	Endpoint      string `yaml:"endpoint"`
}

type Delete struct {
	logger logr.Logger
	eks    *eks.Client
	stack  *stack
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
		stack: &stack{
			cfn:    cloudformation.NewFromConfig(aws),
			logger: logger,
		},
	}
}

func (c *Delete) Run(ctx context.Context, cluster DeleteInput) error {
	if err := c.deleteCluster(ctx, cluster); err != nil {
		return fmt.Errorf("deleting EKS hybrid cluster: %w", err)
	}

	if err := c.stack.delete(ctx, cluster.ClusterName); err != nil {
		return fmt.Errorf("deleting E2E setup stack: %w", err)
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

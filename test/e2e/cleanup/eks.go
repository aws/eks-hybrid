package cleanup

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/errors"
)

const deleteClusterTimeout = 5 * time.Minute

type EKSClusterCleanup struct {
	eksClient *eks.Client
	logger    logr.Logger
}

func NewEKSClusterCleanup(eksClient *eks.Client, logger logr.Logger) *EKSClusterCleanup {
	return &EKSClusterCleanup{
		eksClient: eksClient,
		logger:    logger,
	}
}

func (c *EKSClusterCleanup) ListEKSClusters(ctx context.Context, input FilterInput) ([]string, error) {
	paginator := eks.NewListClustersPaginator(c.eksClient, &eks.ListClustersInput{})

	var clusterNames []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing EKS clusters: %w", err)
		}
		for _, clusterName := range page.Clusters {
			clusterInfo, err := c.eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{
				Name: aws.String(clusterName),
			})
			if err != nil {
				return nil, fmt.Errorf("describing cluster %s: %w", clusterName, err)
			}
			if shouldDeleteCluster(clusterInfo.Cluster, input) {
				clusterNames = append(clusterNames, clusterName)
			}
		}
	}

	return clusterNames, nil
}

func (c *EKSClusterCleanup) DeleteCluster(ctx context.Context, clusterName string) error {
	_, err := c.eksClient.DeleteCluster(ctx, &eks.DeleteClusterInput{
		Name: aws.String(clusterName),
	})
	if err != nil {
		if errors.IsType(err, &types.ResourceNotFoundException{}) {
			c.logger.Info("Cluster already deleted", "cluster", clusterName)
			return nil
		}
		return fmt.Errorf("deleting cluster %s: %w", clusterName, err)
	}

	waiter := eks.NewClusterDeletedWaiter(c.eksClient)
	err = waiter.Wait(ctx, &eks.DescribeClusterInput{Name: aws.String(clusterName)}, deleteClusterTimeout)
	if err != nil {
		return fmt.Errorf("waiting for cluster %s deletion: %w", clusterName, err)
	}

	return nil
}

func shouldDeleteCluster(cluster *types.Cluster, input FilterInput) bool {
	var tags []Tag
	for key, value := range cluster.Tags {
		tags = append(tags, Tag{
			Key:   key,
			Value: value,
		})
	}
	resource := ResourceWithTags{
		ID:           *cluster.Name,
		CreationTime: aws.ToTime(cluster.CreatedAt),
		Tags:         tags,
	}
	return shouldDeleteResource(resource, input)
}

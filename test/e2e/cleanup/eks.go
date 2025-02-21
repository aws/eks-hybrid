package cleanup

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/errors"
)

type EKSClusterCleanup struct {
	EKS             *eks.Client
	ResourceTagging *resourcegroupstaggingapi.Client
	Logger          logr.Logger
}

func (c *EKSClusterCleanup) ListEKSClusters(ctx context.Context, input FilterInput) ([]string, error) {
	tagger := &ResourceTagger{
		ResourceTagging: c.ResourceTagging,
		ClusterName:     input.ClusterName,
	}

	stackARNs, err := tagger.GetTaggedResources(ctx, "eks:cluster")
	if err != nil {
		return nil, fmt.Errorf("getting tagged EKS stacks: %w", err)
	}

	var clusterNames []string
	for _, stackARN := range stackARNs {
		clusterName := extractClusterName(stackARN)

		clusterInfo, err := c.EKS.DescribeCluster(ctx, &eks.DescribeClusterInput{
			Name: aws.String(clusterName),
		})
		if err != nil && errors.IsType(err, &types.ResourceNotFoundException{}) {
			c.Logger.Info("Cluster already deleted", "cluster", clusterName)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("describing cluster %s: %w", clusterName, err)
		}

		if shouldDeleteCluster(clusterInfo.Cluster, input) {
			clusterNames = append(clusterNames, clusterName)
		}
	}

	return clusterNames, nil
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

// arn format: arn:aws:eks:us-west-2:<account-id>:cluster/nodeadm-e2e-tests-1-31
func extractClusterName(stackARN string) string {
	parts := strings.Split(stackARN, ":")
	if len(parts) < 6 {
		return ""
	}
	parts = strings.Split(parts[5], "/")
	if len(parts) < 1 {
		return ""
	}
	return parts[1]
}

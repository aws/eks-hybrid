package eks

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/eks"
)

var NewClient = eks.NewFromConfig

// DescribeCluster wraps the EKS DescribeCluster API call
func DescribeCluster(ctx context.Context, c *eks.Client, name string) (*eks.DescribeClusterOutput, error) {
	input := &eks.DescribeClusterInput{
		Name: &name,
	}

	cluster, err := c.DescribeCluster(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("EKS DescribeCluster call failed: %w", err)
	}

	return cluster, nil
}

package eks

import (
	"context"
	"fmt"

	eks_sdk "github.com/aws/aws-sdk-go-v2/service/eks"
)

var NewClient = eks_sdk.NewFromConfig

func DescribeCluster(ctx context.Context, c *eks_sdk.Client, name string) (*eks_sdk.DescribeClusterOutput, error) {
	input := &eks_sdk.DescribeClusterInput{
		Name: &name,
	}

	cluster, err := c.DescribeCluster(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("EKS DescribeCluster call failed: %w", err)
	}

	return cluster, nil
}

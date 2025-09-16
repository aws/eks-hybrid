package cluster

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/cleanup"
)

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
		eks:    e2e.NewEKSClient(aws, endpoint),
		stack: &stack{
			iamClient: iam.NewFromConfig(aws),
			cfn:       cloudformation.NewFromConfig(aws),
			ec2Client: ec2.NewFromConfig(aws),
			logger:    logger,
			s3Client:  s3.NewFromConfig(aws),
			region: aws.Region,
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
	eksCleaner := cleanup.NewEKSClusterCleanup(c.eks, c.logger)
	if err := eksCleaner.DeleteCluster(ctx, cluster.ClusterName); err != nil {
		return fmt.Errorf("deleting EKS hybrid cluster: %w", err)
	}
	return nil
}

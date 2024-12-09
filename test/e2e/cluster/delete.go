package cluster

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/eks-hybrid/test/e2e"
)

type CleanupResources struct {
	ClusterName   string `yaml:"clusterName"`
	ClusterRegion string `yaml:"clusterRegion"`
}

func (c *CleanupResources) CleanupE2EResources(ctx context.Context) error {
	logger := e2e.NewLogger()
	logger.Info("Cleaning up E2E resources...")

	awsConfig, err := e2e.NewAWSConfig(ctx, c.ClusterRegion)
	if err != nil {
		return fmt.Errorf("initializing AWS config: %w", err)
	}
	cfnClient := cloudformation.NewFromConfig(awsConfig)
	eksClient := eks.NewFromConfig(awsConfig)

	logger.Info("Cleaning up EKS hybrid cluster", "cluster", c.ClusterName)
	if err = c.DeleteCluster(ctx, eksClient, logger); err != nil {
		return fmt.Errorf("deleting EKS hybrid cluster: %v", err)
	}

	logger.Info("Cleaning up E2E setup stack...")
	if err = c.DeleteStack(ctx, cfnClient, logger); err != nil {
		return fmt.Errorf("deleting E2E setup stack: %v", err)
	}

	logger.Info("Cleanup completed successfully!")
	return nil
}

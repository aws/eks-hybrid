package ec2

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/aws/ecr"
	"github.com/aws/eks-hybrid/internal/configenricher"
)

func (enp *ec2NodeProvider) Enrich(ctx context.Context, opts ...configenricher.ConfigEnricherOption) error {
	enp.logger.Info("Fetching instance details..")
	imdsClient := imds.New(imds.Options{})
	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithClientLogMode(aws.LogRetries), config.WithEC2IMDSRegion(func(o *config.UseEC2IMDSRegion) {
		o.Client = imdsClient
	}))
	if err != nil {
		return err
	}
	instanceDetails, err := api.GetInstanceDetails(ctx, imdsClient, ec2.NewFromConfig(awsConfig))
	if err != nil {
		return err
	}
	enp.nodeConfig.Status.Instance = *instanceDetails
	enp.logger.Info("Instance details populated", zap.Reflect("details", instanceDetails))
	region := instanceDetails.Region
	enp.logger.Info("Fetching default options...")

	// Apply options to build configuration
	config := &configenricher.ConfigEnricherConfig{}
	for _, opt := range opts {
		opt(config)
	}

	eksRegistry, err := ecr.GetEKSRegistry(region, config.RegionConfig)
	if err != nil {
		return err
	}
	enp.nodeConfig.Status.Defaults = api.DefaultOptions{
		SandboxImage: eksRegistry.GetSandboxImage(),
	}
	enp.logger.Info("Default options populated", zap.Reflect("defaults", enp.nodeConfig.Status.Defaults))
	return nil
}

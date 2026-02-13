package configenricher

import (
	"context"

	"github.com/aws/eks-hybrid/internal/aws"
)

// ConfigEnricherConfig holds the configuration options
type ConfigEnricherConfig struct {
	RegionConfig    *aws.RegionData
	PartitionConfig aws.PartitionConfig
}

// ConfigEnricherOption is a function that modifies ConfigEnricherConfig
type ConfigEnricherOption func(*ConfigEnricherConfig)

// WithRegionAndPartitionConfig creates a ConfigEnricherOption that sets region and partition config
func WithRegionAndPartitionConfig(regionConfig *aws.RegionData, partitionConfig aws.PartitionConfig) ConfigEnricherOption {
	return func(config *ConfigEnricherConfig) {
		config.RegionConfig = regionConfig
		config.PartitionConfig = partitionConfig
	}
}

type ConfigEnricher interface {
	Enrich(ctx context.Context, opts ...ConfigEnricherOption) error
}

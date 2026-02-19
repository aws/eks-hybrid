package configenricher

import (
	"context"

	"github.com/aws/eks-hybrid/internal/aws"
)

// ConfigEnricherConfig holds the configuration options
type ConfigEnricherConfig struct {
	RegionConfig *aws.RegionData
}

type ConfigEnricherOption func(*ConfigEnricherConfig)

// WithRegionConfig creates a ConfigEnricherOption that sets region config
func WithRegionConfig(regionConfig *aws.RegionData) ConfigEnricherOption {
	return func(config *ConfigEnricherConfig) {
		config.RegionConfig = regionConfig
	}
}

type ConfigEnricher interface {
	Enrich(ctx context.Context, opts ...ConfigEnricherOption) error
}

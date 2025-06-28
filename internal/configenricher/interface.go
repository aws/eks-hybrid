package configenricher

import (
	"context"

	"github.com/aws/eks-hybrid/internal/aws"
)

type ConfigEnricherOptions struct {
	RegionConfig *aws.RegionData
}

// WithRegionConfig creates ConfigEnricherOptions with the provided region config
func WithRegionConfig(regionConfig *aws.RegionData) ConfigEnricherOptions {
	return ConfigEnricherOptions{
		RegionConfig: regionConfig,
	}
}

type ConfigEnricher interface {
	Enrich(ctx context.Context, opts ...ConfigEnricherOptions) error
}

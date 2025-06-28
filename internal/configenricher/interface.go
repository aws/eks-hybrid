package configenricher

import (
	"context"

	"github.com/aws/eks-hybrid/internal/aws"
)

type ConfigEnricher interface {
	Enrich(ctx context.Context, regionConfig *aws.RegionData) error
}

package e2e

import (
	"context"
	"net/url"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	smithyendpoints "github.com/aws/smithy-go/endpoints"
)

// EksResolverV2 is used to resolve custom endpoints for EKS clients.
type EksResolverV2 struct {
	Endpoint string
}

// ResolveEndpoint resolves to a custom endpoint if not empty or default otherwise.
func (r *EksResolverV2) ResolveEndpoint(ctx context.Context, params eks.EndpointParameters) (
	smithyendpoints.Endpoint, error,
) {
	if r.Endpoint != "" {
		u, err := url.Parse(r.Endpoint)
		if err != nil {
			return smithyendpoints.Endpoint{}, err
		}
		return smithyendpoints.Endpoint{
			URI: *u,
		}, nil
	}

	// delegate back to the default v2 resolver otherwise
	return eks.NewDefaultEndpointResolverV2().ResolveEndpoint(ctx, params)
}

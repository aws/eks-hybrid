package iamrolesanywhere

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rolesanywhere"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/network"
	"github.com/aws/eks-hybrid/internal/retry"
	"github.com/aws/eks-hybrid/internal/validation"
)

func CheckEndpointAccess(ctx context.Context, config aws.Config) error {
	client := rolesanywhere.NewFromConfig(config)
	opts := client.Options()

	endpoint, err := opts.EndpointResolverV2.ResolveEndpoint(ctx, rolesanywhere.EndpointParameters{
		Region:   aws.String(opts.Region),
		Endpoint: opts.BaseEndpoint,
	})
	if err != nil {
		return fmt.Errorf("resolving IAM Roles Anywhere endpoint: %w", err)
	}

	err = retry.NetworkRequest(ctx, func(ctx context.Context) error {
		return network.CheckConnectionToHost(ctx, endpoint.URI)
	})
	if err != nil {
		return fmt.Errorf("checking connection to IAM Roles Anywhere endpoint: %w", err)
	}

	return nil
}

// AccessValidator validates access to the AWS IAM Roles Anywhere API endpoint.
type AccessValidator struct {
	aws aws.Config
}

// NewAccessValidator returns a new AccessValidator.
func NewAccessValidator(aws aws.Config) AccessValidator {
	return AccessValidator{
		aws: aws,
	}
}

func (a AccessValidator) Run(ctx context.Context, informer validation.Informer, _ *api.NodeConfig) error {
	var err error
	informer.Starting(ctx, "iam-roles-anywhere-endpoint-access", "Validating access to AWS IAM Roles Anywhere API endpoint")
	defer func() {
		informer.Done(ctx, "iam-roles-anywhere-endpoint-access", err)
	}()

	if err = CheckEndpointAccess(ctx, a.aws); err != nil {
		return validation.WithRemediation(err, "Ensure your network configuration allows access to the AWS IAM Roles Anywhere API endpoint")
	}

	return nil
}

package sts

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	sts_sdk "github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/network"
	"github.com/aws/eks-hybrid/internal/validation"
)

func CheckEndpointAccess(ctx context.Context, config aws.Config) error {
	client := sts_sdk.NewFromConfig(config)
	opts := client.Options()

	endpoint, err := opts.EndpointResolverV2.ResolveEndpoint(ctx, sts_sdk.EndpointParameters{
		Region:   aws.String(opts.Region),
		Endpoint: opts.BaseEndpoint,
	})
	if err != nil {
		return fmt.Errorf("resolving sts endpoint: %w", err)
	}

	if err := network.CheckConnectionToHost(ctx, endpoint.URI); err != nil {
		return fmt.Errorf("checking connection to sts endpoint: %w", err)
	}

	return nil
}

// AuthenticationValidator validates if the machine can authenticate against AWS.
type AuthenticationValidator struct {
	aws aws.Config
}

// NewAuthenticationValidator returns a new AuthenticationValidator.
func NewAuthenticationValidator(aws aws.Config) AuthenticationValidator {
	return AuthenticationValidator{
		aws: aws,
	}
}

func (a AuthenticationValidator) Run(ctx context.Context, informer validation.Informer, _ *api.NodeConfig) error {
	if err := CheckEndpointAccess(ctx, a.aws); err != nil {
		// If can't access the endpoint, we can't authenticate
		// This is not a requirement, so it's possible that the user never allowed
		// connection to the STS endpoint. In that case, assume success and let
		// other validation fail.
		return nil
	}

	var err error
	informer.Starting(ctx, "sts-authentication", "Validating authentication against AWS")
	defer func() {
		informer.Done(ctx, "sts-authentication", err)
	}()

	client := sts_sdk.NewFromConfig(a.aws)
	_, err = client.GetCallerIdentity(ctx, &sts_sdk.GetCallerIdentityInput{})
	if err != nil {
		err = validation.WithRemediation(err, "Check your AWS configuration and make sure you can obtain valid AWS credentials.")
		return err
	}

	return nil
}

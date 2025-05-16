package kubernetes

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/aws/eks"
	"github.com/aws/eks-hybrid/internal/retrier"
	"github.com/aws/eks-hybrid/internal/validation"
)

type AccessValidator struct {
	aws aws.Config
}

func NewAccessValidator(config aws.Config) AccessValidator {
	return AccessValidator{
		aws: config,
	}
}

func (a AccessValidator) Run(ctx context.Context, informer validation.Informer, node *api.NodeConfig) error {
	// APIServerEndpoint, CertificateAuthority are required for the validations we want to run here but are optional in the config
	// When not specified, we need to read them from the EKS API.
	var err error
	var cluster *api.ClusterDetails
	err = retrier.PollWithRetries(ctx, func(ctx context.Context) (bool, error) {
		// var err error
		cluster, err = eks.ReadClusterDetails(ctx, a.aws, node)
		return err == nil, err
	})
	if err != nil {
		err = validation.WithRemediation(err,
			"Either provide the Kubernetes API server endpoint or ensure the node has access and permissions to call DescribeCluster EKS API.",
		)

		// Only if reading the EKS fail is when we "start" a validation and signal it as failed.
		// Otherwise, there is no need to surface we are reading from the EKS API.
		informer.Starting(ctx, "kubernetes-endpoint-access", "Validating access to Kubernetes API endpoint")
		informer.Done(ctx, "kubernetes-endpoint-access", err)

		return err
	}

	nodeComplete := node.DeepCopy()
	nodeComplete.Spec.Cluster = *cluster

	// We run these validation from inside another because these all need a "complete"
	// node config, so we read the API once and pass it to all them.
	// We compose the validations in one for simplicity
	// We only want to continue running the next if the previous
	// has succeeded, since they are all pre-requirements to the next one.
	v := validation.UntilError(
		CheckConnection,
		CheckUnauthenticatedAccess,
	)

	if err := v(ctx, informer, nodeComplete); err != nil {
		return err
	}

	return nil
}

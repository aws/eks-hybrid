package kubernetes

import (
	"context"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/validation"
)

type AccessValidator struct {
	cluster *api.ClusterDetails
}

func NewAccessValidator(cluster *api.ClusterDetails) AccessValidator {
	return AccessValidator{
		cluster: cluster,
	}
}

func (a AccessValidator) Run(ctx context.Context, informer validation.Informer, node *api.NodeConfig) error {
	nodeComplete := node.DeepCopy()
	nodeComplete.Spec.Cluster = *a.cluster

	// We run these validation from inside another because these all need a "complete"
	// node config, so we read the API once and pass it to all them.
	// We compose the validations in one for simplicity
	// We only want to continue running the next if the previous
	// has succeeded, since they are all pre-requirements to the next one.
	v := validation.UntilError(
		ValidateAPIServerEndpointResolution,
		CheckUnauthenticatedAccess,
	)

	if err := v(ctx, informer, nodeComplete); err != nil {
		return err
	}

	return nil
}

package kubernetes

import (
	"context"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/validation"
)

type AccessValidator struct {
	clusterProvider ClusterProvider
}

func NewAccessValidator(clusterProvider ClusterProvider) AccessValidator {
	return AccessValidator{
		clusterProvider: clusterProvider,
	}
}

func (a AccessValidator) Run(ctx context.Context, informer validation.Informer, node *api.NodeConfig) error {
	cluster, err := a.clusterProvider.ReadClusterDetails(ctx, node)
	if err != nil {
		// Only if reading the EKS fail is when we "start" a validation and signal it as failed.
		// Otherwise, there is no need to surface we are reading from the EKS API.
		informer.Starting(ctx, "kubernetes-endpoint-access", "Validating access to Kubernetes API endpoint")
		informer.Done(ctx, "kubernetes-endpoint-access", err)
		return err
	}

	nodeComplete := node.DeepCopy()
	nodeComplete.Spec.Cluster = *cluster

	cv := NewConnectionValidator()

	// We run these validation from inside another because these all need a "complete"
	// node config, so we read the API once and pass it to all them.
	// We compose the validations in one for simplicity
	// We only want to continue running the next if the previous
	// has succeeded, since they are all pre-requirements to the next one.
	v := validation.UntilError(
		cv.Run,
		CheckUnauthenticatedAccess,
	)

	if err := v(ctx, informer, nodeComplete); err != nil {
		return err
	}

	return nil
}

package kubernetes

import (
	"context"
	"net/url"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/network"
	"github.com/aws/eks-hybrid/internal/retrier"
	"github.com/aws/eks-hybrid/internal/validation"
)

func CheckConnection(ctx context.Context, informer validation.Informer, node *api.NodeConfig) error {
	name := "kubernetes-endpoint-access"
	var err error
	informer.Starting(ctx, name, "Validating access to Kubernetes API endpoint")
	defer func() {
		informer.Done(ctx, name, err)
	}()

	endpoint, err := url.Parse(node.Spec.Cluster.APIServerEndpoint)
	if err != nil {
		err = validation.WithRemediation(err, "Ensure the Kubernetes API server endpoint provided is correct.")
		return err
	}

	err = retrier.PollWithRetries(ctx, func(ctx context.Context) (bool, error) {
		err := network.CheckConnectionToHost(ctx, *endpoint)
		return err == nil, err
	})
	if err != nil {
		err = validation.WithRemediation(err, "Ensure your network configuration allows the node to access the Kubernetes API endpoint.")
		return err
	}

	return nil
}

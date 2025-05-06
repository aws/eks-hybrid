package kubernetes

import (
	"context"
	"net/url"

	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/network"
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

	consecutiveErrors := 0
	err = wait.PollUntilContextTimeout(ctx, validation.ValidationInterval, validation.ValidationTimeout, true, func(ctx context.Context) (bool, error) {
		err = network.CheckConnectionToHost(ctx, *endpoint)
		if err != nil {
			consecutiveErrors += 1
			if consecutiveErrors == validation.ValidationMaxRetries {
				return false, err
			}
			return false, nil // continue polling
		}
		return true, nil
	})
	if err != nil {
		err = validation.WithRemediation(err, "Ensure your network configuration allows the node to access the Kubernetes API endpoint.")
		return err
	}

	return nil
}

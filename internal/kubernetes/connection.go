package kubernetes

import (
	"context"
	"fmt"
	"net"
	"net/url"

	"github.com/pkg/errors"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/network"
	"github.com/aws/eks-hybrid/internal/retry"
	"github.com/aws/eks-hybrid/internal/validation"
)

func CheckConnection(ctx context.Context, informer validation.Informer, node *api.NodeConfig) error {
	name := "kubernetes-endpoint-access"
	var err error
	informer.Starting(ctx, name, "Validating access to Kubernetes API endpoint")
	defer func() {
		informer.Done(ctx, name, err)
	}()

	endpoint, err := url.ParseRequestURI(node.Spec.Cluster.APIServerEndpoint)
	if err != nil {
		err = validation.WithRemediation(err, "Ensure the Kubernetes API server endpoint provided is correct.")
		return err
	}

	err = validateEndpointResolution(ctx, endpoint.Hostname())
	if err != nil {
		err = validation.WithRemediation(err, "Ensure DNS server settings and network connectivity are correct, and verify the hostname is reachable")
		return err
	}

	err = retry.NetworkRequest(ctx, func(ctx context.Context) error {
		return network.CheckConnectionToHost(ctx, *endpoint)
	})
	if err != nil {
		err = validation.WithRemediation(err, "Ensure your network configuration allows the node to access the Kubernetes API endpoint.")
		return err
	}

	return nil
}

// validateEndpointResolution validates that a hostname DNS resolves
func validateEndpointResolution(ctx context.Context, hostname string) error {
	if hostname == "" {
		return errors.New("hostname is empty")
	}

	// Resolve the hostname to IP addresses
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return fmt.Errorf("failed to resolve hostname %s: %w", hostname, err)
	}

	if len(ips) == 0 {
		return fmt.Errorf("hostname %s did not resolve to any IP addresses", hostname)
	}

	return nil
}
